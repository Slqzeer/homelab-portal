package e2e_test

import (
	"context"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func routeFixture() (*networkingv1.Ingress, *corev1.Service, *corev1.Pod, *discoveryv1.EndpointSlice) {
	name, port, protocol, ready := "http", int32(8080), corev1.ProtocolTCP, true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Name: "verified", UID: "verified-uid", Labels: map[string]string{"app": "portal"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "portal", Image: "registry/portal@sha256:verified", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}}}}, Status: corev1.PodStatus{PodIP: "10.0.0.1"}}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Name: "web", UID: "service-uid"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10", Selector: map[string]string{"app": "portal"}, Ports: []corev1.ServicePort{{Name: name, Port: 80, TargetPort: intstr.FromString("http"), Protocol: protocol}}}}
	slice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Name: "web-abc", Labels: map[string]string{discoveryv1.LabelServiceName: "web"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: "web", UID: svc.UID}}}, AddressType: discoveryv1.AddressTypeIPv4, Ports: []discoveryv1.EndpointPort{{Name: &name, Port: &port, Protocol: &protocol}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}, TargetRef: &corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Namespace: "portal", Name: pod.Name, UID: pod.UID}}}}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Namespace: "portal", Name: "published"}, Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web", Port: networkingv1.ServiceBackendPort{Number: 80}}}}}
	return ing, svc, pod, slice
}

func TestIngressRouteMustResolveOnlyToVerifiedPod(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Service, *corev1.Pod, *discoveryv1.EndpointSlice)
		ok     bool
	}{
		{"bound route", func(*corev1.Service, *corev1.Pod, *discoveryv1.EndpointSlice) {}, true},
		{"same name replaced UID", func(_ *corev1.Service, _ *corev1.Pod, s *discoveryv1.EndpointSlice) {
			s.Endpoints[0].TargetRef.UID = "other-uid"
		}, false},
		{"unrelated target address", func(_ *corev1.Service, _ *corev1.Pod, s *discoveryv1.EndpointSlice) {
			s.Endpoints[0].Addresses = []string{"10.0.0.9"}
		}, false},
		{"extra unverified backend", func(_ *corev1.Service, _ *corev1.Pod, s *discoveryv1.EndpointSlice) {
			s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{Addresses: []string{"10.0.0.9"}})
		}, false},
		{"forged service label", func(_ *corev1.Service, _ *corev1.Pod, s *discoveryv1.EndpointSlice) {
			s.OwnerReferences[0].UID = "unrelated-service"
		}, false},
		{"missing endpoints", func(_ *corev1.Service, _ *corev1.Pod, s *discoveryv1.EndpointSlice) { s.Endpoints = nil }, false},
		{"headless intermediary", func(s *corev1.Service, _ *corev1.Pod, _ *discoveryv1.EndpointSlice) {
			s.Spec.ClusterIP = corev1.ClusterIPNone
		}, false},
		{"wrong target port", func(s *corev1.Service, _ *corev1.Pod, _ *discoveryv1.EndpointSlice) {
			s.Spec.Ports[0].TargetPort = intstr.FromInt32(9090)
		}, false},
		{"in-place image change", func(_ *corev1.Service, p *corev1.Pod, _ *discoveryv1.EndpointSlice) {
			p.Spec.Containers[0].Image = "registry/other:latest"
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ing, svc, pod, slice := routeFixture()
			expected := pod.DeepCopy()
			tc.mutate(svc, pod, slice)
			client := fake.NewClientset(svc, pod, slice)
			err := e2e.VerifyIngressPodRoute(context.Background(), client, ing, expected)
			if (err == nil) != tc.ok {
				t.Fatal("route accepted an unverified or ambiguous target")
			}
		})
	}
}
