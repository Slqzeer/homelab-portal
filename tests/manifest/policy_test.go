package manifest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// The seam is the deployable output, so patches and namespace transformations
// are exercised as well as the base. kubectl embeds the same Kustomize engine.
func render(t *testing.T) []unstructured.Unstructured {
	return renderPath(t, "deploy/overlays/homelab")
}

func renderPath(t *testing.T, path string) []unstructured.Unstructured {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	tool, err := exec.LookPath("kustomize")
	args := []string{"build", path}
	if err != nil {
		tool, err = exec.LookPath("kubectl")
		args[0] = "kustomize"
	}
	require.NoError(t, err, "install kustomize or kubectl to test the rendered package")
	cmd := exec.Command(tool, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		t.Logf("render error: %s", exit.Stderr)
	}
	require.NoError(t, err)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	var objects []unstructured.Unstructured
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if len(object.Object) != 0 {
			objects = append(objects, object)
		}
	}
	require.NotEmpty(t, objects)
	return objects
}

func TestSecretsStayInFilesAndOperationsStayOnInternalService(t *testing.T) {
	objects := render(t)
	deployment := decode[appsv1.Deployment](t, objectOf(t, objects, "Deployment", "homelab-portal"))
	pod := deployment.Spec.Template.Spec
	container := pod.Containers[0]
	require.EqualValues(t, 65532, *pod.SecurityContext.RunAsUser)
	require.EqualValues(t, 65532, *pod.SecurityContext.RunAsGroup)
	require.EqualValues(t, 65532, *pod.SecurityContext.FSGroup, "non-root process must be able to read group-readable projected secrets")
	config := decode[corev1.ConfigMap](t, objectOf(t, objects, "ConfigMap", "homelab-portal"))
	// This allowlist prevents adding credential-valued environment variables.
	require.Equal(t, map[string]string{
		"PORT": "8080", "OPERATIONS_PORT": "8081", "PORTAL_BASE_URL": "https://portal.taildf6cd4.ts.net",
		"OIDC_ISSUER_URL":      "https://keycloak.taildf6cd4.ts.net/realms/homelab",
		"OIDC_BACKCHANNEL_URL": "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab",
		"OIDC_CLIENT_ID":       "homelab-portal", "OIDC_GROUPS_CLAIM": "groups",
		"PORTAL_INGRESS_NAMESPACE": "portal", "PORTAL_INGRESS_NAME": "homelab-portal",
		"OIDC_CLIENT_SECRET_FILE":  "/var/run/portal-secrets/oidc-client-secret",
		"SESSION_CURRENT_KEY_FILE": "/var/run/portal-secrets/session-current-key",
	}, config.Data)
	require.Empty(t, config.BinaryData)
	require.Empty(t, container.Env)
	require.Len(t, container.EnvFrom, 1)
	require.Nil(t, container.EnvFrom[0].SecretRef)
	require.Equal(t, config.Name, container.EnvFrom[0].ConfigMapRef.Name)
	require.Len(t, pod.Volumes, 1)
	require.NotNil(t, pod.Volumes[0].Secret)
	require.Equal(t, "homelab-portal-secrets", pod.Volumes[0].Secret.SecretName)
	require.EqualValues(t, 0440, *pod.Volumes[0].Secret.DefaultMode)
	require.Len(t, container.VolumeMounts, 1)
	mount := container.VolumeMounts[0]
	require.Equal(t, pod.Volumes[0].Name, mount.Name)
	require.Equal(t, "/var/run/portal-secrets", mount.MountPath)
	require.True(t, mount.ReadOnly)
	require.Empty(t, mount.SubPath, "directory projection must receive secret rotations")
	vso := objectOf(t, objects, "VaultStaticSecret", "homelab-portal")
	require.Equal(t, "secrets.hashicorp.com/v1beta1", vso.GetAPIVersion())
	destination, _, err := unstructured.NestedMap(vso.Object, "spec", "destination")
	require.NoError(t, err)
	require.Equal(t, "homelab-portal-secrets", destination["name"])
	require.Equal(t, true, destination["create"])
	targets, _, err := unstructured.NestedSlice(vso.Object, "spec", "rolloutRestartTargets")
	require.NoError(t, err)
	require.Equal(t, []interface{}{map[string]interface{}{"kind": "Deployment", "name": deployment.Name}}, targets, "secrets are loaded at startup")
	for _, object := range objects {
		require.NotEqual(t, "Secret", object.GetKind(), "only VSO creates the Secret")
	}
	service := decode[corev1.Service](t, objectOf(t, objects, "Service", "homelab-portal"))
	require.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	require.Empty(t, service.Spec.ExternalIPs)
	require.Empty(t, service.Spec.LoadBalancerIP)
	require.True(t, service.Spec.PublishNotReadyAddresses, "failure metrics and last valid links must remain reachable when readiness fails")
	require.Equal(t, deployment.Spec.Template.Labels, service.Spec.Selector)
	require.Equal(t, []corev1.ServicePort{
		{Name: "public", Port: 8080, TargetPort: intstr.FromString("public"), Protocol: corev1.ProtocolTCP},
		{Name: "operations", Port: 8081, TargetPort: intstr.FromString("operations"), Protocol: corev1.ProtocolTCP},
	}, service.Spec.Ports)
	require.Equal(t, []corev1.ContainerPort{
		{Name: "public", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
		{Name: "operations", ContainerPort: 8081, Protocol: corev1.ProtocolTCP},
	}, container.Ports)
	for _, check := range []struct {
		probe *corev1.Probe
		path  string
	}{
		{container.LivenessProbe, "/healthz"}, {container.ReadinessProbe, "/readyz"}, {container.StartupProbe, "/healthz"},
	} {
		require.NotNil(t, check.probe)
		require.NotNil(t, check.probe.HTTPGet)
		require.Equal(t, check.path, check.probe.HTTPGet.Path)
		require.Equal(t, intstr.FromString("operations"), check.probe.HTTPGet.Port)
		require.Empty(t, check.probe.HTTPGet.Host, "kubelet probes its own pod, avoiding a Service readiness cycle")
	}
	monitor := objectOf(t, objects, "ServiceMonitor", "homelab-portal")
	selector, _, err := unstructured.NestedStringMap(monitor.Object, "spec", "selector", "matchLabels")
	require.NoError(t, err)
	require.Equal(t, service.Labels, selector)
	namespaces, _, err := unstructured.NestedStringSlice(monitor.Object, "spec", "namespaceSelector", "matchNames")
	require.NoError(t, err)
	require.Equal(t, []string{service.Namespace}, namespaces)
	endpoints, _, err := unstructured.NestedSlice(monitor.Object, "spec", "endpoints")
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	endpoint := endpoints[0].(map[string]interface{})
	require.Equal(t, "operations", endpoint["port"])
	require.Equal(t, "/metrics", endpoint["path"])
}

func objectOf(t *testing.T, objects []unstructured.Unstructured, kind, name string) unstructured.Unstructured {
	t.Helper()
	var found []unstructured.Unstructured
	for _, object := range objects {
		if object.GetKind() == kind && object.GetName() == name {
			found = append(found, object)
		}
	}
	require.Len(t, found, 1, "%s/%s must occur once", kind, name)
	return found[0]
}

func decode[T any](t *testing.T, object unstructured.Unstructured) T {
	t.Helper()
	data, err := json.Marshal(object.Object)
	require.NoError(t, err)
	var result T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&result), "strict Kubernetes v0.36.4 API decoding")
	return result
}

func TestPortalRunsWithinRestrictedResourceBoundary(t *testing.T) {
	objects := render(t)
	deployment := decode[appsv1.Deployment](t, objectOf(t, objects, "Deployment", "homelab-portal"))
	require.EqualValues(t, 1, *deployment.Spec.Replicas)
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type, "never run overlapping replicas with local session state")
	pod := deployment.Spec.Template.Spec
	require.Equal(t, "homelab-portal", pod.ServiceAccountName)
	require.EqualValues(t, 20, *pod.TerminationGracePeriodSeconds)
	require.NotNil(t, pod.SecurityContext)
	require.True(t, *pod.SecurityContext.RunAsNonRoot)
	require.Positive(t, *pod.SecurityContext.RunAsUser)
	require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, pod.SecurityContext.SeccompProfile.Type)
	require.False(t, pod.HostNetwork)
	require.False(t, pod.HostPID)
	require.False(t, pod.HostIPC)
	require.Empty(t, pod.InitContainers)
	require.Len(t, pod.Containers, 1)
	container := pod.Containers[0]
	require.Equal(t, "25m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "48Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "100m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "128Mi", container.Resources.Limits.Memory().String())
	require.NotNil(t, container.SecurityContext)
	if container.SecurityContext.Privileged != nil {
		require.False(t, *container.SecurityContext.Privileged)
	}
	require.True(t, *container.SecurityContext.RunAsNonRoot)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	require.Empty(t, container.SecurityContext.Capabilities.Add)
	require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, container.SecurityContext.SeccompProfile.Type)
	require.Equal(t, approvedProductionImage, container.Image, "production overlay must render the reviewed release")
	require.True(t, immutableProductionImage(container.Image), "production image needs a unique build tag and nonzero digest")
	for _, volume := range pod.Volumes {
		require.Nil(t, volume.PersistentVolumeClaim)
		require.Nil(t, volume.HostPath)
		require.Nil(t, volume.EmptyDir, "the Go runtime does not need writable temporary storage")
	}
	deploymentCount := 0
	for _, object := range objects {
		if object.GetKind() == "Deployment" {
			deploymentCount++
		}
		require.NotEqual(t, "PersistentVolumeClaim", object.GetKind())
		require.NotEqual(t, "Ingress", object.GetKind(), "all exposure, including operational paths, belongs to external GitOps")
	}
	require.Equal(t, 1, deploymentCount)
}

func TestProductionOverlayDelegatesNamespaceToBootstrap(t *testing.T) {
	for _, object := range render(t) {
		require.NotEqual(t, "Namespace", object.GetKind(), "production bootstrap owns Namespace/portal")
	}
	base := renderPath(t, "deploy/base")
	namespace := decode[corev1.Namespace](t, objectOf(t, base, "Namespace", "portal"))
	require.Equal(t, "restricted", namespace.Labels["pod-security.kubernetes.io/enforce"])
	require.Equal(t, "v1.36", namespace.Labels["pod-security.kubernetes.io/enforce-version"])
}

func TestProductionOverlayContainsOnlyObservedSiteValues(t *testing.T) {
	objects := render(t)
	rendered, err := json.Marshal(objects)
	require.NoError(t, err)
	for _, placeholder := range []string{"192.0.2.1", "198.51.100.1", "registry.example", "example.ts.net"} {
		require.NotContains(t, string(rendered), placeholder, "production render must not retain template value %q", placeholder)
	}
}

func TestNetworkBoundaryDeniesEverythingExceptNamedDependencies(t *testing.T) {
	objects := render(t)
	var policies []networkingv1.NetworkPolicy
	for _, object := range objects {
		if object.GetKind() == "NetworkPolicy" {
			policies = append(policies, decode[networkingv1.NetworkPolicy](t, object))
		}
	}
	require.Len(t, policies, 2, "additional additive policies could bypass the boundary")
	deny := decode[networkingv1.NetworkPolicy](t, objectOf(t, objects, "NetworkPolicy", "default-deny"))
	require.Equal(t, metav1.LabelSelector{}, deny.Spec.PodSelector, "default deny covers the entire portal namespace")
	require.ElementsMatch(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, deny.Spec.PolicyTypes)
	require.Empty(t, deny.Spec.Ingress)
	require.Empty(t, deny.Spec.Egress)
	allow := decode[networkingv1.NetworkPolicy](t, objectOf(t, objects, "NetworkPolicy", "homelab-portal"))
	require.Equal(t, metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "homelab-portal"}}, allow.Spec.PodSelector)
	require.ElementsMatch(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, allow.Spec.PolicyTypes)
	peer := func(namespace string, labels map[string]string) networkingv1.NetworkPolicyPeer {
		return networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
			PodSelector:       &metav1.LabelSelector{MatchLabels: labels},
		}
	}
	port := func(protocol corev1.Protocol, number int) networkingv1.NetworkPolicyPort {
		value := intstr.FromInt(number)
		return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
	}
	require.ElementsMatch(t, []networkingv1.NetworkPolicyIngressRule{
		{From: []networkingv1.NetworkPolicyPeer{peer("tailscale", map[string]string{
			"tailscale.com/parent-resource": "homelab-portal", "tailscale.com/parent-resource-ns": "portal", "tailscale.com/parent-resource-type": "ingress",
		})}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 8080)}},
		{From: []networkingv1.NetworkPolicyPeer{peer("monitoring", map[string]string{
			"app.kubernetes.io/name": "prometheus", "operator.prometheus.io/name": "homelab",
		})}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 8081)}},
	}, allow.Spec.Ingress, "each peer must have only its own port: proxy/public and scraper/operations")
	require.ElementsMatch(t, []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{peer("kube-system", map[string]string{"k8s-app": "kube-dns"})}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolUDP, 53), port(corev1.ProtocolTCP, 53)}},
		{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.43.0.1/32"}}}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 443)}},
		{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "192.168.1.201/32"}}}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 6443)}},
		{To: []networkingv1.NetworkPolicyPeer{peer("keycloak", map[string]string{"app": "keycloak"})}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 8080)}},
	}, allow.Spec.Egress, "each Kubernetes API address must be paired only with its exact port")
	approvedIPBlocks := map[string]bool{"10.43.0.1/32": true, "192.168.1.201/32": true}
	for _, rule := range allow.Spec.Egress {
		for _, networkPort := range rule.Ports {
			require.False(t, forbiddenTCPPort(networkPort), "TCP 80 and obsolete Keycloak TCP 8443 must not be admitted")
		}
		for _, destination := range rule.To {
			if destination.PodSelector != nil {
				require.NotEqual(t, metav1.LabelSelector{}, *destination.PodSelector, "egress must not select every pod in a namespace")
			}
			if destination.IPBlock != nil {
				require.True(t, approvedIPBlocks[destination.IPBlock.CIDR], "unexpected egress IP block %q", destination.IPBlock.CIDR)
			}
		}
	}
}

func forbiddenTCPPort(networkPort networkingv1.NetworkPolicyPort) bool {
	if networkPort.Port == nil || networkPort.Port.Type != intstr.Int {
		return false
	}
	if networkPort.Protocol != nil && *networkPort.Protocol != corev1.ProtocolTCP {
		return false
	}
	port := networkPort.Port.IntValue()
	return port == 80 || port == 8443
}

func TestForbiddenTCPPortDistinguishesTCPFromUDP(t *testing.T) {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	port := func(protocol *corev1.Protocol, number int) networkingv1.NetworkPolicyPort {
		value := intstr.FromInt(number)
		return networkingv1.NetworkPolicyPort{Protocol: protocol, Port: &value}
	}
	cases := []struct {
		name        string
		networkPort networkingv1.NetworkPolicyPort
		want        bool
	}{
		{"explicit TCP 80", port(&tcp, 80), true},
		{"explicit TCP 8443", port(&tcp, 8443), true},
		{"default TCP 80", port(nil, 80), true},
		{"default TCP 8443", port(nil, 8443), true},
		{"UDP 80", port(&udp, 80), false},
		{"UDP 8443", port(&udp, 8443), false},
		{"allowed TCP 8080", port(&tcp, 8080), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, forbiddenTCPPort(tc.networkPort))
		})
	}
}

func TestAlertsCoverStartupExpiryReconnectAndPersistentInvalidMetadata(t *testing.T) {
	rules := objectOf(t, render(t), "PrometheusRule", "homelab-portal")
	require.Equal(t, "monitoring.coreos.com/v1", rules.GetAPIVersion())
	groups, _, err := unstructured.NestedSlice(rules.Object, "spec", "groups")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	entries := groups[0].(map[string]interface{})["rules"].([]interface{})
	require.Len(t, entries, 4)
	expected := map[string]struct{ expr, duration, severity string }{
		"PortalInitialListFailed": {`portal_catalog_initialized{namespace="portal",service="homelab-portal"} == 0`, "10s", "critical"},
		"PortalCatalogExpired":    {`portal_catalog_state{namespace="portal",service="homelab-portal",state="expired"} == 1`, "0s", "critical"},
		// The metric already measures elapsed reconnect time; adding for: 2m
		// would accidentally delay the requested alert until four minutes.
		"PortalWatchReconnecting": {`portal_watch_reconnect_duration_seconds{namespace="portal",service="homelab-portal"} > 120`, "0s", "warning"},
		"PortalInvalidMetadata":   {`portal_invalid_publications{namespace="portal",service="homelab-portal"} > 0`, "10m", "warning"},
	}
	for _, entry := range entries {
		rule := entry.(map[string]interface{})
		name := rule["alert"].(string)
		want, ok := expected[name]
		require.True(t, ok, "unexpected or duplicate alert %s", name)
		require.Equal(t, want.expr, rule["expr"], "alert must query the emitted metric with the specified threshold")
		require.Equal(t, want.duration, rule["for"])
		require.Equal(t, want.severity, rule["labels"].(map[string]interface{})["severity"])
		require.NotEmpty(t, rule["annotations"].(map[string]interface{})["summary"])
		delete(expected, name)
	}
	require.Empty(t, expected)
}
