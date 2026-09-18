package e2e

import (
	"context"
	"errors"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const RunLabel = "acceptance.homelab.io/run"
const DisposableLabel = "acceptance.homelab.io/disposable"
const RoleLabel = "acceptance.homelab.io/role"

// CreateDirectionControls removes only the opposite-direction confounders.
// Kubernetes NetworkPolicies combine allows additively. Neither policy selects
// the portal or the egress probe; portal ingress/egress remain unchanged.
func CreateDirectionControls(ctx context.Context, k kubernetes.Interface, run string, denied, egress, backend *corev1.Pod, onCreate func(*networkingv1.NetworkPolicy)) error {
	for _, pod := range []*corev1.Pod{denied, egress, backend} {
		if pod.Labels[RunLabel] != run || pod.Labels[DisposableLabel] != "true" {
			return errors.New("direction control ownership absent")
		}
	}
	if denied.Namespace != backend.Namespace || denied.Namespace == egress.Namespace || denied.Labels[RoleLabel] != "denied" || backend.Labels["acceptance.homelab.io/backend"] != run {
		return errors.New("direction control topology invalid")
	}
	meta := func(suffix string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: run + "-" + suffix, Namespace: denied.Namespace, Labels: map[string]string{RunLabel: run, DisposableLabel: "true"}}
	}
	policies := []*networkingv1.NetworkPolicy{
		{ObjectMeta: meta("source-egress-control"), Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{RunLabel: run, RoleLabel: "denied"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{}},
		}},
		{ObjectMeta: meta("backend-ingress-control"), Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{RunLabel: run, "acceptance.homelab.io/backend": run}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": egress.Namespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: egress.Labels}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": denied.Namespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{RunLabel: run, RoleLabel: "denied"}}},
			}}},
		}},
	}
	for _, policy := range policies {
		created, err := k.NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, metav1.CreateOptions{})
		if err != nil {
			return errors.New("direction control creation failed")
		}
		onCreate(created)
		if !reflect.DeepEqual(created.Spec, policy.Spec) {
			return errors.New("direction-control policy was changed by admission")
		}
	}
	return nil
}
