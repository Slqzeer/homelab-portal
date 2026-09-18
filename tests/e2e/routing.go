package e2e

import (
	"context"
	"errors"
	"net"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// ResolveServiceBackend requires one actual Pod identity across every endpoint,
// a Service-owned slice, matching addresses/selector, and the exact target port.
// Unknown intermediary Services and ambiguous targets fail closed.
func ResolveServiceBackend(ctx context.Context, k kubernetes.Interface, namespace string, backend networkingv1.IngressServiceBackend) (*corev1.Pod, int32, error) {
	return resolveServiceBackend(ctx, k, namespace, backend, "")
}

func resolveServiceBackend(ctx context.Context, k kubernetes.Interface, namespace string, backend networkingv1.IngressServiceBackend, requiredPortName string) (*corev1.Pod, int32, error) {
	bad := errors.New("Service route is absent, ambiguous, or unverified")
	if (backend.Port.Name == "") == (backend.Port.Number == 0) {
		return nil, 0, bad
	}
	svc, err := k.CoreV1().Services(namespace).Get(ctx, backend.Name, metav1.GetOptions{})
	if err != nil || svc.UID == "" || svc.Spec.Type != corev1.ServiceTypeClusterIP || net.ParseIP(svc.Spec.ClusterIP) == nil || len(svc.Spec.Selector) == 0 {
		return nil, 0, bad
	}
	var port *corev1.ServicePort
	for _, p := range svc.Spec.Ports {
		if (backend.Port.Name != "" && p.Name == backend.Port.Name) || (backend.Port.Number > 0 && p.Port == backend.Port.Number) {
			if port != nil || (p.Protocol != "" && p.Protocol != corev1.ProtocolTCP) {
				return nil, 0, bad
			}
			copy := p
			port = &copy
		}
	}
	if port == nil || (requiredPortName != "" && port.Name != requiredPortName) {
		return nil, 0, bad
	}
	slices, err := k.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set{discoveryv1.LabelServiceName: svc.Name}.String()})
	if err != nil || len(slices.Items) == 0 {
		return nil, 0, bad
	}
	var resolved *corev1.Pod
	var resolvedPort int32
	for _, slice := range slices.Items {
		owned := false
		for _, ref := range slice.OwnerReferences {
			if ref.Kind == "Service" && ref.Name == svc.Name && ref.UID == svc.UID {
				owned = true
			}
		}
		if !owned || len(slice.Endpoints) == 0 || (slice.AddressType != discoveryv1.AddressTypeIPv4 && slice.AddressType != discoveryv1.AddressTypeIPv6) {
			return nil, 0, bad
		}
		for _, endpoint := range slice.Endpoints {
			ref := endpoint.TargetRef
			if ref == nil || ref.Kind != "Pod" || (ref.APIVersion != "" && ref.APIVersion != "v1") || ref.Namespace != namespace || ref.UID == "" || len(endpoint.Addresses) == 0 || (endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating) || (endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready && !svc.Spec.PublishNotReadyAddresses) {
				return nil, 0, bad
			}
			pod, err := k.CoreV1().Pods(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
			if err != nil || pod.UID != ref.UID || !labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) || (resolved != nil && pod.UID != resolved.UID) {
				return nil, 0, bad
			}
			for _, address := range endpoint.Addresses {
				match := address == pod.Status.PodIP
				for _, ip := range pod.Status.PodIPs {
					if address == ip.IP {
						match = true
					}
				}
				if !match || net.ParseIP(address) == nil {
					return nil, 0, bad
				}
			}
			target := port.TargetPort.IntVal
			if port.TargetPort.Type == intstr.String {
				target = 0
				for _, container := range pod.Spec.Containers {
					for _, p := range container.Ports {
						if p.Name == port.TargetPort.StrVal && (p.Protocol == "" || p.Protocol == corev1.ProtocolTCP) {
							if target != 0 {
								return nil, 0, bad
							}
							target = p.ContainerPort
						}
					}
				}
			} else if target == 0 {
				target = port.Port
			}
			if target <= 0 || target > 65535 {
				return nil, 0, bad
			}
			matchingPort := false
			for _, p := range slice.Ports {
				name := ""
				if p.Name != nil {
					name = *p.Name
				}
				if name == port.Name {
					if matchingPort || p.Port == nil || *p.Port != target || (p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP) {
						return nil, 0, bad
					}
					matchingPort = true
				}
			}
			if !matchingPort || (resolvedPort != 0 && resolvedPort != target) {
				return nil, 0, bad
			}
			resolved, resolvedPort = pod, target
		}
	}
	if resolved == nil {
		return nil, 0, bad
	}
	return resolved, resolvedPort, nil
}

// PortalListenerPorts binds the two roles to unique TCP declarations owned by
// the portal container. The internal probes use the operations port directly;
// neither a Service alias nor a conventional numeric port establishes its role.
func PortalListenerPorts(pod *corev1.Pod) (int32, int32, error) {
	bad := errors.New("portal listener ports are absent, conflicting, or ambiguous")
	if pod == nil {
		return 0, 0, bad
	}
	ports := map[string]int32{}
	portalCount := 0
	for _, c := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
		if c.Name == "portal" {
			portalCount++
		}
		for _, p := range c.Ports {
			if p.Name != "public" && p.Name != "operations" {
				continue
			}
			if c.Name != "portal" || ports[p.Name] != 0 || p.ContainerPort < 1 || p.ContainerPort > 65535 || (p.Protocol != "" && p.Protocol != corev1.ProtocolTCP) {
				return 0, 0, bad
			}
			ports[p.Name] = p.ContainerPort
		}
	}
	if portalCount != 1 || ports["public"] == 0 || ports["operations"] == 0 || ports["public"] == ports["operations"] {
		return 0, 0, bad
	}
	return ports["public"], ports["operations"], nil
}

func VerifyIngressPodRoute(ctx context.Context, k kubernetes.Interface, ing *networkingv1.Ingress, expected *corev1.Pod) error {
	bad := errors.New("Ingress does not route exclusively to the verified portal Pod/image")
	publicPort, operationsPort, err := PortalListenerPorts(expected)
	if err != nil || ing == nil || ing.Namespace != expected.Namespace {
		return bad
	}
	var backends []networkingv1.IngressBackend
	if ing.Spec.DefaultBackend != nil {
		backends = append(backends, *ing.Spec.DefaultBackend)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			return bad
		}
		for _, path := range rule.HTTP.Paths {
			backends = append(backends, path.Backend)
		}
	}
	if len(backends) == 0 {
		return bad
	}
	wantImage := ""
	for _, c := range expected.Spec.Containers {
		if c.Name == "portal" {
			wantImage = c.Image
		}
	}
	if wantImage == "" {
		return bad
	}
	for _, backend := range backends {
		if backend.Service == nil || backend.Resource != nil {
			return bad
		}
		pod, port, err := resolveServiceBackend(ctx, k, ing.Namespace, *backend.Service, "public")
		if err != nil || pod.UID != expected.UID || pod.Name != expected.Name || port != publicPort {
			return bad
		}
		actualPublic, actualOperations, err := PortalListenerPorts(pod)
		if err != nil || actualPublic != publicPort || actualOperations != operationsPort {
			return bad
		}
		match := false
		for _, c := range pod.Spec.Containers {
			if c.Name == "portal" && c.Image == wantImage {
				match = true
			}
		}
		if !match {
			return bad
		}
	}
	return nil
}
