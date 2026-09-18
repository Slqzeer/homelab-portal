package e2e_test

import (
	"context"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProbeCannotBecomeAServiceEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		port                         intstr.IntOrString
		publish, declared, match, ok bool
	}{
		{"numeric port even when unready", intstr.FromInt32(8080), false, false, true, false},
		{"numeric publish-not-ready", intstr.FromInt32(8080), true, false, true, false},
		{"default numeric target", intstr.IntOrString{}, true, false, true, false},
		{"resolvable named target", intstr.FromString("http"), true, true, true, false},
		{"unresolved named target", intstr.FromString("http"), true, false, true, true},
		{"different selector", intstr.FromInt32(8080), true, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Labels: map[string]string{"app": "portal"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "probe"}}}}
			if tc.declared {
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}
			}
			sel := "portal"
			if !tc.match {
				sel = "other"
			}
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Name: "stale"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": sel}, PublishNotReadyAddresses: tc.publish, Ports: []corev1.ServicePort{{Port: 80, TargetPort: tc.port}}}}
			err := e2e.EnsureProbeNotRoutable(context.Background(), fake.NewClientset(svc), pod)
			if (err == nil) != tc.ok {
				t.Fatal("probe Service-routing decision is unsafe")
			}
		})
	}
}

func TestProbeServiceInventoryIsNamespaceScoped(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Labels: map[string]string{"app": "portal"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "probe"}}}}
	unrelated := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "matching"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "portal"}, Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(8080)}}}}
	client := fake.NewClientset(unrelated)
	if err := e2e.EnsureProbeNotRoutable(context.Background(), client, pod); err != nil {
		t.Fatal("Service in another namespace blocked the probe")
	}
	actions := client.Actions()
	if len(actions) != 1 || actions[0].GetVerb() != "list" || actions[0].GetResource().Resource != "services" || actions[0].GetNamespace() != "portal" {
		t.Fatal("probe inventory did not use a namespace-scoped Service list")
	}
}

func TestProbeServiceInventoryRejectsMissingNamespaceBeforeList(t *testing.T) {
	client := fake.NewClientset()
	err := e2e.EnsureProbeNotRoutable(context.Background(), client, &corev1.Pod{})
	if err == nil {
		t.Fatal("namespace-less probe inventory was accepted")
	}
	if len(client.Actions()) != 0 {
		t.Fatal("namespace-less probe triggered a cluster-wide Service list")
	}
}
