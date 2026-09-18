package e2e_test

import (
	"context"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDirectionControlsPreventOppositePolicyFalsePositives(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		sourceDeny, backendDeny bool
	}{
		{"source egress blocks ingress test", true, false},
		{"backend ingress blocks egress test", false, true},
		{"both unrelated directions deny", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset()
			if tc.sourceDeny {
				_, _ = client.NetworkingV1().NetworkPolicies("fixtures").Create(context.Background(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny"}, Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}}, metav1.CreateOptions{})
			}
			if tc.backendDeny {
				_, _ = client.NetworkingV1().NetworkPolicies("fixtures").Create(context.Background(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "deny-backend"}, Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}}, metav1.CreateOptions{})
			}
			run := "accept-test-abcdefgh"
			pod := func(ns, name, role string) *corev1.Pod {
				return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{"acceptance.homelab.io/run": run, "acceptance.homelab.io/disposable": "true", "acceptance.homelab.io/role": role}}}
			}
			denied, egress, backend := pod("fixtures", "denied", "denied"), pod("portal", "egress", "egress"), pod("fixtures", "backend", "backend")
			backend.Labels["acceptance.homelab.io/backend"] = run
			created := 0
			err := e2e.CreateDirectionControls(context.Background(), client, run, denied, egress, backend, func(*networkingv1.NetworkPolicy) { created++ })
			if err != nil || created != 2 {
				t.Fatal("direction controls were not created/tracked")
			}
			policies, _ := client.NetworkingV1().NetworkPolicies("fixtures").List(context.Background(), metav1.ListOptions{})
			var sourceAllowed, backendAllowed bool
			for _, p := range policies.Items {
				selector, _ := metav1.LabelSelectorAsSelector(&p.Spec.PodSelector)
				if selector.Matches(labels.Set(denied.Labels)) {
					for _, r := range p.Spec.Egress {
						if len(r.To) == 0 && len(r.Ports) == 0 {
							sourceAllowed = true
						}
					}
				}
				if selector.Matches(labels.Set(backend.Labels)) {
					for _, r := range p.Spec.Ingress {
						for _, peer := range r.From {
							if peer.NamespaceSelector == nil || peer.PodSelector == nil {
								continue
							}
							nsSel, _ := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
							podSel, _ := metav1.LabelSelectorAsSelector(peer.PodSelector)
							if nsSel.Matches(labels.Set{"kubernetes.io/metadata.name": "portal"}) && podSel.Matches(labels.Set(egress.Labels)) && len(r.Ports) == 0 {
								backendAllowed = true
							}
						}
					}
				}
			}
			if !sourceAllowed {
				t.Fatal("source default-deny can impersonate portal ingress denial")
			}
			if !backendAllowed {
				t.Fatal("backend default-deny can impersonate portal egress denial")
			}
			portalPolicies, _ := client.NetworkingV1().NetworkPolicies("portal").List(context.Background(), metav1.ListOptions{})
			if len(portalPolicies.Items) != 0 {
				t.Fatal("controls altered the portal policy direction under test")
			}
		})
	}
}
