package kube

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// IngressSource is the portal's read-only Kubernetes boundary.
type IngressSource interface {
	List(ctx context.Context) (*networkingv1.IngressList, error)
	Watch(ctx context.Context, resourceVersion string) (watch.Interface, error)
}

// IngressClient is the read-only subset implemented by the generated
// networking.k8s.io/v1 Ingress client.
type IngressClient interface {
	List(ctx context.Context, options metav1.ListOptions) (*networkingv1.IngressList, error)
	Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error)
}

type ingressSource struct {
	client IngressClient
}

// NewIngressSource limits a generated Kubernetes client to list/watch access.
func NewIngressSource(client IngressClient) IngressSource {
	return &ingressSource{client: client}
}

func (source *ingressSource) List(ctx context.Context) (*networkingv1.IngressList, error) {
	return source.client.List(ctx, metav1.ListOptions{})
}

func (source *ingressSource) Watch(ctx context.Context, resourceVersion string) (watch.Interface, error) {
	return source.client.Watch(ctx, metav1.ListOptions{ResourceVersion: resourceVersion})
}
