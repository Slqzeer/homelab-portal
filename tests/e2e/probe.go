package e2e

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// EnsureProbeNotRoutable deliberately does not trust readiness: Services can
// publish unready addresses. Numeric ports resolve without containerPort
// declarations. Only unresolved named ports are safe for a selected probe.
func EnsureProbeNotRoutable(ctx context.Context, k kubernetes.Interface, pod *corev1.Pod) error {
	services, err := k.CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.New("probe Service-selection inventory unavailable")
	}
	for _, svc := range services.Items {
		if len(svc.Spec.Selector) == 0 || !labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
			continue
		}
		for _, port := range svc.Spec.Ports {
			if port.TargetPort.Type == intstr.Int || port.TargetPort.StrVal == "" {
				return errors.New("Service numeric targetPort could route traffic to probe")
			}
			for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
				for _, declared := range container.Ports {
					if declared.Name == port.TargetPort.StrVal {
						return errors.New("Service named targetPort could route traffic to probe")
					}
				}
			}
		}
	}
	return nil
}
