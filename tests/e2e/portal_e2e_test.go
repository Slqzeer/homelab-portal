//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const runLabel = "acceptance.homelab.io/run"
const isolatedLabel = "acceptance.homelab.io/disposable"

type result struct {
	Case   string `json:"case"`
	Passed bool   `json:"passed"`
}
type report struct {
	Schema        int      `json:"schema"`
	Started       string   `json:"started"`
	RunID         string   `json:"runId,omitempty"`
	Image         string   `json:"image,omitempty"`
	Revision      string   `json:"gitopsRevision,omitempty"`
	Passed        bool     `json:"passed"`
	LiveAttempted bool     `json:"liveAttempted"`
	Results       []result `json:"results"`
}

func TestPortalAcceptance(t *testing.T) {
	r := report{Schema: 1, Started: time.Now().UTC().Format(time.RFC3339), Results: []result{}}
	path := os.Getenv("ACCEPT_REPORT")
	if path == "" {
		path = filepath.Join("..", "..", "reports", "acceptance.json")
	}
	t.Cleanup(func() {
		r.Passed = !t.Failed()
		data, err := json.MarshalIndent(r, "", "  ")
		if err == nil {
			err = os.MkdirAll(filepath.Dir(path), 0700)
		}
		if err == nil {
			err = os.WriteFile(path, data, 0600)
		}
		if err != nil {
			t.Error("could not retain acceptance report")
		}
	})
	c, err := e2e.LoadConfig(os.Getenv)
	if err != nil {
		r.Results = append(r.Results, result{"configuration", false})
		t.Fatal(err)
	}
	r.RunID, r.Image, r.Revision = c["ACCEPT_RUN_ID"], c["ACCEPT_IMAGE"], c["ACCEPT_ARGO_REVISION"]
	h := newHarness(t, c)
	check := func(name string, f func(*testing.T)) bool {
		ok := t.Run(name, func(t *testing.T) {
			if name != "isolated_prerequisites" {
				h.routes(t)
			}
			f(t)
		})
		r.Results = append(r.Results, result{name, ok})
		return ok
	}
	r.LiveAttempted = true
	if !check("isolated_prerequisites", h.preflight) {
		return
	}
	var items []e2e.Item
	if !check("create_owned_publication_fixtures", func(t *testing.T) { items = h.ingresses(t) }) {
		return
	}
	// Each case exercises the actual public Tailscale endpoint. No session or cache internals.
	for _, tc := range []struct {
		name, cookie string
		visible      []int
	}{
		{"anonymous_public", "", []int{0}},
		{"authenticated_nonmember", c["ACCEPT_NONMEMBER_COOKIE"], []int{0, 1}},
		{"exact_group_member", c["ACCEPT_MEMBER_COOKIE"], []int{0, 1, 2}},
		{"admin_catalog", c["ACCEPT_ADMIN_COOKIE"], []int{0, 1, 4}},
	} {
		check(tc.name, func(t *testing.T) {
			var visible, hidden []e2e.Item
			for i, item := range items {
				show := false
				for _, v := range tc.visible {
					if i == v {
						show = true
					}
				}
				if show {
					visible = append(visible, item)
				} else {
					hidden = append(hidden, item)
				}
			}
			if err := e2e.CheckCatalog(h.http, c["ACCEPT_PORTAL_URL"], tc.cookie, visible, hidden, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name, cookie string
		status       int
	}{
		{"anonymous_no_diagnostics", "", 404},
		{"nonadmin_no_diagnostics", c["ACCEPT_MEMBER_COOKIE"], 404},
		{"admin_diagnostics", c["ACCEPT_ADMIN_COOKIE"], 200},
	} {
		check(tc.name, func(t *testing.T) {
			status, body, err := e2e.Fetch(h.http, c["ACCEPT_PORTAL_URL"], "/admin", tc.cookie)
			if err != nil || status != tc.status {
				t.Fatal("diagnostics access disagrees with identity")
			}
			if tc.status == 200 && (!strings.Contains(body, c["ACCEPT_RUN_ID"]+"-invalid") || !strings.Contains(body, "access")) {
				t.Fatal("admin publication diagnostic absent")
			}
		})
	}
	for _, route := range []string{"/healthz", "/readyz", "/metrics"} {
		check("external_exclusion"+route, func(t *testing.T) {
			for _, origin := range []string{c["ACCEPT_PORTAL_URL"], c["ACCEPT_STALE_URL"]} {
				for _, cookie := range []string{"", c["ACCEPT_ADMIN_COOKIE"]} {
					status, _, err := e2e.Fetch(h.http, origin, route, cookie)
					if err != nil || status != 404 {
						t.Fatal("operational route must return external HTTP 404")
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		name string
		f    func(*testing.T)
	}{
		{"stale_last_valid_catalog", h.stale}, {"vso_secret_sync", h.vso}, {"argo_synced_healthy", h.argo},
		{"tailscale_proxy_128Mi", h.proxy}, {"serviceaccount_permissions", h.rbac}, {"cni_enforcement_and_readiness", h.network},
	} {
		check(tc.name, tc.f)
	}
}

type harness struct {
	c                e2e.Config
	k                kubernetes.Interface
	d                dynamic.Interface
	metadataHTTP     *http.Client
	apiOrigin        string
	http             *http.Client
	ctx              context.Context
	owner            *testing.T
	portal, stalePod *corev1.Pod
}

func newHarness(t *testing.T, c e2e.Config) *harness {
	t.Helper()
	// Do not let library warnings print untrusted API error bodies.
	klog.SetOutput(io.Discard)
	klog.LogToStderr(false)
	raw, err := clientcmd.LoadFromFile(c["KUBECONFIG"])
	if err != nil {
		t.Fatal("isolated kubeconfig unavailable")
	}
	selected := raw.Contexts[c["ACCEPT_CONTEXT"]]
	if selected == nil {
		t.Fatal("explicit kubeconfig context absent")
	}
	cluster, user := raw.Clusters[selected.Cluster], raw.AuthInfos[selected.AuthInfo]
	if cluster == nil || user == nil || cluster.InsecureSkipTLSVerify || !strings.HasPrefix(cluster.Server, "https://") || user.Exec != nil || user.AuthProvider != nil {
		t.Fatal("kubeconfig must use verified HTTPS and direct CI credentials without executable plugins")
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, c["ACCEPT_CONTEXT"], &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		t.Fatal("isolated kubeconfig invalid")
	}
	cfg.Timeout = 15 * time.Second
	cfg.WarningHandler = rest.NoWarnings{}
	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal("Kubernetes client unavailable")
	}
	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal("Kubernetes CRD client unavailable")
	}
	m, err := rest.HTTPClientFor(cfg)
	if err != nil {
		t.Fatal("Kubernetes metadata client unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	t.Cleanup(cancel)
	return &harness{c: c, k: k, d: d, metadataHTTP: m, apiOrigin: cfg.Host, ctx: ctx, owner: t, http: &http.Client{Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (h *harness) owned(m metav1.Object) bool {
	return m.GetLabels()[runLabel] == h.c["ACCEPT_RUN_ID"] && m.GetLabels()[isolatedLabel] == "true"
}
func (h *harness) labels() map[string]string {
	return map[string]string{runLabel: h.c["ACCEPT_RUN_ID"], isolatedLabel: "true"}
}

func (h *harness) preflight(t *testing.T) {
	system, err := h.k.CoreV1().Namespaces().Get(h.ctx, "kube-system", metav1.GetOptions{})
	if err != nil || string(system.UID) != h.c["ACCEPT_CLUSTER_UID"] {
		t.Fatal("cluster UID does not match isolated cluster")
	}
	for _, key := range []string{"ACCEPT_PORTAL_NAMESPACE", "ACCEPT_FIXTURE_NAMESPACE", "ACCEPT_PROBE_NAMESPACE"} {
		ns, err := h.k.CoreV1().Namespaces().Get(h.ctx, h.c[key], metav1.GetOptions{})
		if err != nil || !h.owned(ns) || ns.Status.Phase != corev1.NamespaceActive {
			t.Fatal("required disposable namespace/run labels absent")
		}
	}
	for _, pair := range []struct {
		pod, ingress, origin string
		target               **corev1.Pod
	}{
		{"ACCEPT_PORTAL_POD", "ACCEPT_PORTAL_INGRESS", "ACCEPT_PORTAL_URL", &h.portal},
		{"ACCEPT_STALE_POD", "ACCEPT_STALE_INGRESS", "ACCEPT_STALE_URL", &h.stalePod},
	} {
		pod, err := h.k.CoreV1().Pods(h.c["ACCEPT_PORTAL_NAMESPACE"]).Get(h.ctx, h.c[pair.pod], metav1.GetOptions{})
		if err != nil || !h.owned(pod) || pod.Status.Phase != corev1.PodRunning || net.ParseIP(pod.Status.PodIP) == nil || pod.Spec.ServiceAccountName != h.c["ACCEPT_SERVICE_ACCOUNT"] {
			t.Fatal("isolated portal pod identity/state invalid")
		}
		found := false
		for _, container := range pod.Spec.Containers {
			if container.Name == "portal" && container.Image == h.c["ACCEPT_IMAGE"] {
				found = true
			}
		}
		if !found {
			t.Fatal("portal pod does not use expected immutable image")
		}
		*pair.target = pod
		ing, err := h.k.NetworkingV1().Ingresses(pod.Namespace).Get(h.ctx, h.c[pair.ingress], metav1.GetOptions{})
		if err != nil || !h.owned(ing) || ingressURL(ing) != h.c[pair.origin] {
			t.Fatal("portal endpoint does not match isolated Tailscale Ingress status")
		}
		if err := e2e.VerifyIngressPodRoute(h.ctx, h.k, ing, pod); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := h.k.CoreV1().Services(h.c["ACCEPT_FIXTURE_NAMESPACE"]).Get(h.ctx, h.c["ACCEPT_FIXTURE_SERVICE"], metav1.GetOptions{})
	if err != nil || !h.owned(svc) || svc.Spec.Type != corev1.ServiceTypeClusterIP || len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 80 {
		t.Fatal("disposable fixture backend Service absent")
	}
	// Detect absent CRDs and named external prerequisites before fixture writes.
	for _, target := range []struct {
		gvr      schema.GroupVersionResource
		ns, name string
	}{
		{schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}, h.c["ACCEPT_ARGO_NAMESPACE"], h.c["ACCEPT_ARGO_APPLICATION"]},
		{schema.GroupVersionResource{Group: "secrets.hashicorp.com", Version: "v1beta1", Resource: "vaultstaticsecrets"}, h.c["ACCEPT_PORTAL_NAMESPACE"], h.c["ACCEPT_VSO_NAME"]},
	} {
		obj, err := h.d.Resource(target.gvr).Namespace(target.ns).Get(h.ctx, target.name, metav1.GetOptions{})
		if err != nil || !h.owned(obj) {
			t.Fatal("isolated Argo/VSO prerequisite absent")
		}
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Fatal("kubectl prerequisite absent")
	}
}

func (h *harness) routes(t *testing.T) {
	t.Helper()
	for _, pair := range []struct {
		pod             *corev1.Pod
		ingress, origin string
	}{
		{h.portal, "ACCEPT_PORTAL_INGRESS", "ACCEPT_PORTAL_URL"}, {h.stalePod, "ACCEPT_STALE_INGRESS", "ACCEPT_STALE_URL"},
	} {
		ing, err := h.k.NetworkingV1().Ingresses(pair.pod.Namespace).Get(h.ctx, h.c[pair.ingress], metav1.GetOptions{})
		if err != nil || !h.owned(ing) || ingressURL(ing) != h.c[pair.origin] {
			t.Fatal("tested Ingress identity or origin changed")
		}
		if err := e2e.VerifyIngressPodRoute(h.ctx, h.k, ing, pair.pod); err != nil {
			t.Fatal(err)
		}
	}
}

func ingressURL(ing *networkingv1.Ingress) string {
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "tailscale" || len(ing.Status.LoadBalancer.Ingress) != 1 {
		return ""
	}
	lb := ing.Status.LoadBalancer.Ingress[0]
	if lb.IP != "" || !strings.HasSuffix(lb.Hostname, ".ts.net") || strings.ContainsAny(lb.Hostname, "/:@?# ") {
		return ""
	}
	return "https://" + lb.Hostname
}

func (h *harness) ingresses(t *testing.T) []e2e.Item {
	var items []e2e.Item
	for _, access := range []string{"public", "authenticated", "groups", "case-mismatch", "admin", "invalid"} {
		name := h.c["ACCEPT_RUN_ID"] + "-" + access
		annotations := map[string]string{"portal.homelab.io/enabled": "true", "portal.homelab.io/name": name, "portal.homelab.io/access": access}
		if access == "groups" || access == "case-mismatch" {
			annotations["portal.homelab.io/access"] = "groups"
			annotations["portal.homelab.io/groups"] = h.c["ACCEPT_GROUP"]
			if access == "case-mismatch" {
				annotations["portal.homelab.io/groups"] = strings.ToUpper(h.c["ACCEPT_GROUP"])
			}
		}
		class, pathType := "tailscale", networkingv1.PathTypePrefix
		ing, err := h.k.NetworkingV1().Ingresses(h.c["ACCEPT_FIXTURE_NAMESPACE"]).Create(h.ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: h.labels(), Annotations: annotations},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &class, TLS: []networkingv1.IngressTLS{{Hosts: []string{name}}},
				Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
						Path: "/", PathType: &pathType,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: h.c["ACCEPT_FIXTURE_SERVICE"], Port: networkingv1.ServiceBackendPort{Number: 80}}},
					}}},
				}}},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal("fixture create failed; existing resources are never adopted")
		}
		h.cleanup("ingresses", ing.Namespace, ing.Name, ing.UID)
		var target string
		if !h.eventually(3*time.Minute, func() bool {
			current, err := h.k.NetworkingV1().Ingresses(ing.Namespace).Get(h.ctx, ing.Name, metav1.GetOptions{})
			if err != nil || current.UID != ing.UID || !h.owned(current) {
				return false
			}
			target = ingressURL(current)
			return target != ""
		}) {
			t.Fatal("fixture Tailscale hostname not provisioned")
		}
		items = append(items, e2e.Item{Name: name, URL: target})
	}
	if !h.eventually(time.Minute, func() bool {
		return e2e.CheckCatalog(h.http, h.c["ACCEPT_PORTAL_URL"], "", items[:1], items[1:], false) == nil
	}) {
		t.Fatal("fixture catalog did not converge")
	}
	return items
}

func (h *harness) eventually(timeout time.Duration, f func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return true
		}
		select {
		case <-h.ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

func (h *harness) cleanup(resourceName, ns, name string, uid types.UID) {
	h.owner.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		gvr := schema.GroupVersionResource{Version: "v1", Resource: resourceName}
		if resourceName == "ingresses" || resourceName == "networkpolicies" {
			gvr.Group = "networking.k8s.io"
		}
		api := h.d.Resource(gvr).Namespace(ns)
		current, err := api.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil || current.GetUID() != uid || !h.owned(current) {
			h.owner.Error("cleanup refused: created resource identity/ownership cannot be verified")
			return
		}
		rv := current.GetResourceVersion()
		if err := api.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); err != nil && !apierrors.IsNotFound(err) {
			h.owner.Error("owned fixture cleanup failed")
			return
		}
		for {
			remaining, err := api.Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) || (err == nil && remaining.GetUID() != uid) {
				return
			}
			if err != nil {
				h.owner.Error("owned fixture deletion could not be confirmed")
				return
			}
			select {
			case <-ctx.Done():
				h.owner.Error("owned fixture deletion timed out; inspect finalizers")
				return
			case <-time.After(time.Second):
			}
		}
	})
}

func (h *harness) stale(t *testing.T) {
	if err := e2e.CheckCatalog(h.http, h.c["ACCEPT_STALE_URL"], "", []e2e.Item{{Name: h.c["ACCEPT_STALE_ITEM_NAME"], URL: h.c["ACCEPT_STALE_ITEM_URL"]}}, nil, true); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) vso(t *testing.T) {
	obj, err := h.d.Resource(schema.GroupVersionResource{Group: "secrets.hashicorp.com", Version: "v1beta1", Resource: "vaultstaticsecrets"}).Namespace(h.c["ACCEPT_PORTAL_NAMESPACE"]).Get(h.ctx, h.c["ACCEPT_VSO_NAME"], metav1.GetOptions{})
	if err != nil || !h.owned(obj) {
		t.Fatal("VSO resource unavailable")
	}
	generation, _, _ := unstructured.NestedInt64(obj.Object, "status", "lastGeneration")
	if generation != obj.GetGeneration() || generation == 0 {
		t.Fatal("VSO generation not synchronized")
	}
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	synced := false
	for _, raw := range conditions {
		condition, ok := raw.(map[string]interface{})
		if ok && condition["type"] == "SecretSynced" && condition["status"] == "True" {
			synced = true
		}
	}
	if !synced {
		t.Fatal("VSO SecretSynced=True absent")
	}
	name, _, _ := unstructured.NestedString(obj.Object, "spec", "destination", "name")
	if name == "" {
		t.Fatal("VSO destination absent")
	}
	// PartialObjectMetadata avoids retrieving Secret data at all.
	secret, err := e2e.ReadSecretMetadata(h.ctx, h.metadataHTTP, h.apiOrigin, obj.GetNamespace(), name)
	if err != nil {
		t.Fatal("VSO destination Secret metadata unavailable")
	}
	owned := false
	for _, ref := range secret.OwnerReferences {
		if ref.UID == obj.GetUID() {
			owned = true
		}
	}
	if !owned {
		t.Fatal("destination Secret is not owned by expected VSO resource")
	}
}

func (h *harness) argo(t *testing.T) {
	obj, err := h.d.Resource(schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}).Namespace(h.c["ACCEPT_ARGO_NAMESPACE"]).Get(h.ctx, h.c["ACCEPT_ARGO_APPLICATION"], metav1.GetOptions{})
	if err != nil || !h.owned(obj) {
		t.Fatal("Argo Application unavailable")
	}
	for _, field := range []struct {
		path []string
		want string
	}{
		{[]string{"status", "sync", "status"}, "Synced"}, {[]string{"status", "health", "status"}, "Healthy"},
		{[]string{"status", "sync", "revision"}, h.c["ACCEPT_ARGO_REVISION"]}, {[]string{"spec", "destination", "namespace"}, h.c["ACCEPT_PORTAL_NAMESPACE"]},
	} {
		got, _, _ := unstructured.NestedString(obj.Object, field.path...)
		if got != field.want {
			t.Fatal("Argo state/revision/destination does not match expected release")
		}
	}
}

func (h *harness) proxy(t *testing.T) {
	selector := fmt.Sprintf("tailscale.com/parent-resource=%s,tailscale.com/parent-resource-ns=%s,tailscale.com/parent-resource-type=ingress", h.c["ACCEPT_PORTAL_INGRESS"], h.c["ACCEPT_PORTAL_NAMESPACE"])
	pods, err := h.k.CoreV1().Pods(h.c["ACCEPT_PROXY_NAMESPACE"]).List(h.ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		t.Fatal("portal Tailscale proxy absent")
	}
	cap := resource.MustParse("128Mi")
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			t.Fatal("Tailscale proxy is not running")
		}
		for _, container := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
			limit, exists := container.Resources.Limits[corev1.ResourceMemory]
			if !exists || limit.Sign() <= 0 || limit.Cmp(cap) > 0 {
				t.Fatal("Tailscale proxy memory exceeds 128Mi or has no cap")
			}
		}
	}
}

func (h *harness) kubectl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(h.ctx, 25*time.Second)
	defer cancel()
	base := []string{"--kubeconfig", h.c["KUBECONFIG"], "--context", h.c["ACCEPT_CONTEXT"], "--request-timeout=15s"}
	cmd := exec.CommandContext(ctx, "kubectl", append(base, args...)...)
	// Capture and discard stderr, never include arguments or output in errors.
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return out, errors.New("kubectl operation failed")
	}
	return out, nil
}

func (h *harness) rbac(t *testing.T) {
	namespaces, err := h.k.CoreV1().Namespaces().List(h.ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal("cannot enumerate namespace permission boundaries")
	}
	for _, tc := range []struct {
		verb, resource string
		allowed        bool
	}{
		{"get", "ingresses.networking.k8s.io", true}, {"list", "ingresses.networking.k8s.io", true}, {"watch", "ingresses.networking.k8s.io", true},
		{"get", "secrets", false}, {"list", "secrets", false}, {"watch", "secrets", false},
		{"create", "ingresses.networking.k8s.io", false}, {"update", "ingresses.networking.k8s.io", false}, {"patch", "ingresses.networking.k8s.io", false}, {"delete", "ingresses.networking.k8s.io", false},
	} {
		t.Run(tc.verb+"_"+tc.resource, func(t *testing.T) {
			identity := "system:serviceaccount:" + h.c["ACCEPT_PORTAL_NAMESPACE"] + ":" + h.c["ACCEPT_SERVICE_ACCOUNT"]
			scopes := [][]string{{"--all-namespaces"}}
			if !tc.allowed {
				// A negative cluster-wide review alone misses namespaced grants.
				for _, ns := range namespaces.Items {
					scopes = append(scopes, []string{"--namespace", ns.Name})
				}
			}
			for _, scope := range scopes {
				args := []string{"auth", "can-i", tc.verb, tc.resource, "--as", identity,
					"--as-group", "system:serviceaccounts", "--as-group", "system:serviceaccounts:" + h.c["ACCEPT_PORTAL_NAMESPACE"], "--as-group", "system:authenticated"}
				out, err := h.kubectl(append(args, scope...)...)
				answer := strings.TrimSpace(string(out))
				if tc.allowed && (err != nil || answer != "yes") || !tc.allowed && answer != "no" {
					t.Fatal("ServiceAccount authorization differs from least-privilege contract")
				}
			}
		})
	}
}

func (h *harness) probe(t *testing.T, role string) *corev1.Pod {
	ns, suffix := h.c["ACCEPT_FIXTURE_NAMESPACE"], "denied"
	labels := h.labels()
	if role == "denied" {
		labels[e2e.RoleLabel] = "denied"
	}
	if role == "allowed" {
		ns, suffix = h.c["ACCEPT_PROBE_NAMESPACE"], "allowed"
		labels["app.kubernetes.io/name"] = "prometheus"
		labels["operator.prometheus.io/name"] = "homelab"
	}
	if role == "egress" {
		ns, suffix = h.c["ACCEPT_PORTAL_NAMESPACE"], "egress"
		// Copy all NetworkPolicy selector labels from the actual portal pod.
		for k, v := range h.portal.Labels {
			labels[k] = v
		}
		labels[runLabel], labels[isolatedLabel] = h.c["ACCEPT_RUN_ID"], "true"
	}
	no, yes, uid := false, true, int64(65532)
	meta := metav1.ObjectMeta{Name: h.c["ACCEPT_RUN_ID"] + "-" + suffix, Namespace: ns, Labels: labels}
	if role == "egress" {
		// An existing controller owner prevents a ReplicaSet from adopting the
		// probe when its selectors match the copied portal labels.
		meta.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: h.portal.Name, UID: h.portal.UID, Controller: &yes, BlockOwnerDeletion: &no}}
	}
	pod := &corev1.Pod{ObjectMeta: meta, Spec: corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &no,
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{Name: "probe", Image: h.c["ACCEPT_PROBE_IMAGE"], Command: []string{"sleep", "900"},
			// Defense in depth only; Service safety is checked independently below.
			ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(1)}}, PeriodSeconds: 2},
			VolumeMounts:    []corev1.VolumeMount{{Name: "cluster-ca", MountPath: "/var/run/acceptance-ca", ReadOnly: true}},
			SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &yes, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5m"), corev1.ResourceMemory: resource.MustParse("8Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("32Mi")}},
		}},
		Volumes: []corev1.Volume{{Name: "cluster-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}}}}},
	}}
	if role == "egress" {
		if err := e2e.EnsureProbeNotRoutable(h.ctx, h.k, pod); err != nil {
			t.Fatal(err)
		}
	}
	pod, err := h.k.CoreV1().Pods(ns).Create(h.ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("disposable probe create failed")
	}
	h.cleanup("pods", pod.Namespace, pod.Name, pod.UID)
	if role == "egress" {
		if err := e2e.EnsureProbeNotRoutable(h.ctx, h.k, pod); err != nil {
			t.Fatal(err)
		}
	}
	if !h.eventually(time.Minute, func() bool {
		current, err := h.k.CoreV1().Pods(ns).Get(h.ctx, pod.Name, metav1.GetOptions{})
		return err == nil && current.UID == pod.UID && current.Status.Phase == corev1.PodRunning
	}) {
		t.Fatal("disposable probe not running")
	}
	return pod
}

func (h *harness) network(t *testing.T) {
	allowed, denied := h.probe(t, "allowed"), h.probe(t, "denied")
	egress := h.probe(t, "egress")
	backend, backendPort, err := e2e.ResolveServiceBackend(h.ctx, h.k, h.c["ACCEPT_FIXTURE_NAMESPACE"], networkingv1.IngressServiceBackend{Name: h.c["ACCEPT_FIXTURE_SERVICE"], Port: networkingv1.ServiceBackendPort{Number: 80}})
	if err != nil || !h.owned(backend) {
		t.Fatal("direction-control backend is not a unique run-owned Pod")
	}
	if err := e2e.CreateDirectionControls(h.ctx, h.k, h.c["ACCEPT_RUN_ID"], denied, egress, backend, func(p *networkingv1.NetworkPolicy) { h.cleanup("networkpolicies", p.Namespace, p.Name, p.UID) }); err != nil {
		t.Fatal(err)
	}
	backendURL := "http://" + net.JoinHostPort(backend.Status.PodIP, fmt.Sprint(backendPort)) + "/"
	probe := func(pod *corev1.Pod, url string) (string, error) {
		out, err := h.kubectl("-n", pod.Namespace, "exec", pod.Name, "--", "curl", "--silent", "--output", "/dev/null", "--write-out", "%{http_code}", "--connect-timeout", "3", "--max-time", "5", url)
		return strings.TrimSpace(string(out)), err
	}
	for _, tc := range []struct {
		name         string
		pod          *corev1.Pod
		path, status string
	}{
		{"healthy_readiness", h.portal, "/readyz", "200"}, {"internal_metrics", h.portal, "/metrics", "200"},
		{"stale_liveness", h.stalePod, "/healthz", "200"}, {"expired_readiness", h.stalePod, "/readyz", "503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, err := probe(allowed, "http://"+net.JoinHostPort(tc.pod.Status.PodIP, "8080")+tc.path)
			if err != nil || status != tc.status {
				t.Fatal("allowed internal HTTP observation failed")
			}
		})
	}
	// Positive control proves denied probe's curl/network work; negative control
	// uses the same live destination that was reachable from the allowed peer.
	status, err := probe(denied, backendURL)
	if err != nil || status != "200" {
		t.Fatal("denied probe positive control failed")
	}
	for i := 0; i < 3; i++ {
		status, err := probe(denied, "http://"+net.JoinHostPort(h.portal.Status.PodIP, "8080")+"/healthz")
		if err == nil || status != "000" {
			t.Fatal("CNI did not deny unauthorized peer traffic")
		}
	}
	// Same deployed egress selectors, separate non-ready disposable pod. DNS
	// and API traffic must succeed; fixture backend traffic must be dropped.
	out, err := h.kubectl("-n", egress.Namespace, "exec", egress.Name, "--", "curl", "--silent", "--cacert", "/var/run/acceptance-ca/ca.crt",
		"--output", "/dev/null", "--write-out", "%{http_code}", "--connect-timeout", "3", "--max-time", "5", "https://kubernetes.default.svc/version")
	apiStatus := strings.TrimSpace(string(out))
	if err != nil || (apiStatus != "200" && apiStatus != "401" && apiStatus != "403") {
		t.Fatal("CNI allowed DNS/API egress positive control failed")
	}
	for i := 0; i < 3; i++ {
		status, err := probe(egress, backendURL)
		if err == nil || status != "000" {
			t.Fatal("CNI did not deny unauthorized portal egress")
		}
	}
	// Recheck both destinations after denials: a target outage is not proof of
	// NetworkPolicy enforcement.
	status, err = probe(allowed, "http://"+net.JoinHostPort(h.portal.Status.PodIP, "8080")+"/healthz")
	if err != nil || status != "200" {
		t.Fatal("portal positive control unavailable after denial checks")
	}
	status, err = probe(denied, backendURL)
	if err != nil || status != "200" {
		t.Fatal("backend positive control unavailable after denial checks")
	}
}
