package kube

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

type fakeIngressSource struct {
	list  func(context.Context) (*networkingv1.IngressList, error)
	watch func(context.Context, string) (watch.Interface, error)
}

type recordingIngressClient struct {
	listOptions  chan metav1.ListOptions
	watchOptions chan metav1.ListOptions
	stream       watch.Interface
}

func (client *recordingIngressClient) List(_ context.Context, options metav1.ListOptions) (*networkingv1.IngressList, error) {
	client.listOptions <- options
	return &networkingv1.IngressList{}, nil
}

func (client *recordingIngressClient) Watch(_ context.Context, options metav1.ListOptions) (watch.Interface, error) {
	client.watchOptions <- options
	return client.stream, nil
}

func (f fakeIngressSource) List(ctx context.Context) (*networkingv1.IngressList, error) {
	return f.list(ctx)
}

func (f fakeIngressSource) Watch(ctx context.Context, resourceVersion string) (watch.Interface, error) {
	return f.watch(ctx, resourceVersion)
}

func TestWatcherListsWithDeadlineAndAtomicallyRebuildsForWatchEvents(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	deadlineRemaining := make(chan time.Duration, 1)
	watchResourceVersion := make(chan string, 1)
	initial := publishedIngress("tools", "grafana", "Grafana")
	initial.ResourceVersion = "10"
	source := fakeIngressSource{
		list: func(ctx context.Context) (*networkingv1.IngressList, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				deadlineRemaining <- 0
			} else {
				deadlineRemaining <- time.Until(deadline)
			}
			return &networkingv1.IngressList{
				ListMeta: metav1.ListMeta{ResourceVersion: "10"},
				Items:    []networkingv1.Ingress{initial},
			}, nil
		},
		watch: func(_ context.Context, resourceVersion string) (watch.Interface, error) {
			watchResourceVersion <- resourceVersion
			return stream, nil
		},
	}
	store := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	successAt := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	watcher := NewWatcher(
		source,
		store,
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithClock(func() time.Time { return successAt }),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		stream.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("watcher did not stop after cancellation")
		}
	})

	remaining := <-deadlineRemaining
	assert.Greater(t, remaining, 9*time.Second)
	assert.LessOrEqual(t, remaining, 10*time.Second)
	assert.Equal(t, "10", <-watchResourceVersion)
	require.Eventually(t, func() bool {
		snapshot := store.Snapshot(time.Now())
		return snapshot.Initialized && snapshot.LastSuccess.Equal(successAt) &&
			len(snapshot.Items) == 1 && snapshot.Items[0].ID == "tools/grafana"
	}, time.Second, 5*time.Millisecond)

	second := publishedIngress("tools", "prometheus", "Prometheus")
	second.ResourceVersion = "11"
	stream.Add(&second)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(
			[]string{"tools/grafana", "tools/prometheus"},
			itemIDs(store.Snapshot(time.Now()).Items),
		)
	}, time.Second, 5*time.Millisecond)

	invalid := initial.DeepCopy()
	invalid.ResourceVersion = "12"
	invalid.Annotations["portal.homelab.io/name"] = ""
	stream.Modify(invalid)
	require.Eventually(t, func() bool {
		snapshot := store.Snapshot(time.Now())
		return assert.ObjectsAreEqual([]string{"tools/prometheus"}, itemIDs(snapshot.Items)) &&
			len(snapshot.Diagnostics) == 1 && snapshot.Diagnostics[0].Ingress == "grafana"
	}, time.Second, 5*time.Millisecond)

	second.ResourceVersion = "13"
	stream.Delete(&second)
	require.Eventually(t, func() bool {
		snapshot := store.Snapshot(time.Now())
		return len(snapshot.Items) == 0 && len(snapshot.Diagnostics) == 1
	}, time.Second, 5*time.Millisecond)
}

func TestIngressSourcePassesOnlyReadOptionsToKubernetesClient(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	client := &recordingIngressClient{
		listOptions:  make(chan metav1.ListOptions, 1),
		watchOptions: make(chan metav1.ListOptions, 1),
		stream:       stream,
	}
	source := NewIngressSource(client)

	_, listErr := source.List(context.Background())
	returnedStream, watchErr := source.Watch(context.Background(), "27")

	require.NoError(t, listErr)
	require.NoError(t, watchErr)
	assert.Equal(t, metav1.ListOptions{}, <-client.listOptions)
	assert.Equal(t, metav1.ListOptions{ResourceVersion: "27"}, <-client.watchOptions)
	assert.Same(t, stream, returnedStream)
	stream.Stop()
}

func TestWatcherRetainsLastValidSnapshotAndReconnectsAfterDisconnect(t *testing.T) {
	firstStream := watch.NewRaceFreeFake()
	secondStream := watch.NewRaceFreeFake()
	watchCalls := make(chan string, 2)
	delays := make(chan time.Duration, 1)
	var listCalls atomic.Int32
	var watchCallCount atomic.Int32
	ingress := publishedIngress("tools", "grafana", "Grafana")
	source := fakeIngressSource{
		list: func(context.Context) (*networkingv1.IngressList, error) {
			listCalls.Add(1)
			return &networkingv1.IngressList{
				ListMeta: metav1.ListMeta{ResourceVersion: "10"},
				Items:    []networkingv1.Ingress{ingress},
			}, nil
		},
		watch: func(_ context.Context, resourceVersion string) (watch.Interface, error) {
			watchCalls <- resourceVersion
			if watchCallCount.Add(1) == 1 {
				return firstStream, nil
			}
			return secondStream, nil
		},
	}
	store := catalog.NewStore(types.NamespacedName{})
	watcher := NewWatcher(
		source,
		store,
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithJitter(func(time.Duration) time.Duration { return 0 }),
		WithSleeper(func(_ context.Context, delay time.Duration) error {
			delays <- delay
			return nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		firstStream.Stop()
		secondStream.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("watcher did not stop after cancellation")
		}
	})

	assert.Equal(t, "10", <-watchCalls)
	require.Eventually(t, func() bool {
		return len(store.Snapshot(time.Now()).Items) == 1
	}, time.Second, 5*time.Millisecond)
	lastSuccess := store.Snapshot(time.Now()).LastSuccess

	firstStream.Stop()
	assert.Equal(t, time.Second, <-delays)
	assert.Equal(t, "10", <-watchCalls)
	snapshot := store.Snapshot(time.Now())
	assert.Equal(t, []string{"tools/grafana"}, itemIDs(snapshot.Items))
	assert.Equal(t, lastSuccess, snapshot.LastSuccess)
	assert.Equal(t, int32(1), listCalls.Load())
}

func TestWatcherRelistsAfterResourceVersionExpires(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	var listCalls atomic.Int32
	var watchCalls atomic.Int32
	first := publishedIngress("tools", "grafana", "Grafana")
	second := publishedIngress("tools", "prometheus", "Prometheus")
	source := fakeIngressSource{
		list: func(context.Context) (*networkingv1.IngressList, error) {
			call := listCalls.Add(1)
			if call == 1 {
				return &networkingv1.IngressList{
					ListMeta: metav1.ListMeta{ResourceVersion: "10"},
					Items:    []networkingv1.Ingress{first},
				}, nil
			}
			return &networkingv1.IngressList{
				ListMeta: metav1.ListMeta{ResourceVersion: "20"},
				Items:    []networkingv1.Ingress{second},
			}, nil
		},
		watch: func(_ context.Context, resourceVersion string) (watch.Interface, error) {
			if watchCalls.Add(1) == 1 {
				assert.Equal(t, "10", resourceVersion)
				return nil, apierrors.NewResourceExpired("too old")
			}
			assert.Equal(t, "20", resourceVersion)
			return stream, nil
		},
	}
	store := catalog.NewStore(types.NamespacedName{})
	watcher := NewWatcher(
		source,
		store,
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithSleeper(func(context.Context, time.Duration) error { return nil }),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		stream.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("watcher did not stop after cancellation")
		}
	})

	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(
			[]string{"tools/prometheus"},
			itemIDs(store.Snapshot(time.Now()).Items),
		)
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(2), listCalls.Load())
}

func TestWatcherRemainsUninitializedUntilAListSucceeds(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	sleepEntered := make(chan struct{}, 1)
	releaseRetry := make(chan struct{})
	var listCalls atomic.Int32
	source := fakeIngressSource{
		list: func(context.Context) (*networkingv1.IngressList, error) {
			if listCalls.Add(1) == 1 {
				return nil, errors.New("temporarily unavailable")
			}
			return &networkingv1.IngressList{ListMeta: metav1.ListMeta{ResourceVersion: "10"}}, nil
		},
		watch: func(context.Context, string) (watch.Interface, error) { return stream, nil },
	}
	store := catalog.NewStore(types.NamespacedName{})
	watcher := NewWatcher(
		source,
		store,
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithSleeper(func(ctx context.Context, _ time.Duration) error {
			sleepEntered <- struct{}{}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseRetry:
				return nil
			}
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		stream.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("watcher did not stop after cancellation")
		}
	})

	<-sleepEntered
	assert.False(t, store.Snapshot(time.Now()).Initialized)
	close(releaseRetry)
	require.Eventually(t, func() bool {
		return store.Snapshot(time.Now()).Initialized
	}, time.Second, 5*time.Millisecond)
}

func TestWatcherCapsExponentialRetryDelays(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	delays := make(chan time.Duration, 7)
	var watchCalls atomic.Int32
	source := fakeIngressSource{
		list: func(context.Context) (*networkingv1.IngressList, error) {
			return &networkingv1.IngressList{ListMeta: metav1.ListMeta{ResourceVersion: "10"}}, nil
		},
		watch: func(context.Context, string) (watch.Interface, error) {
			if watchCalls.Add(1) <= 7 {
				return nil, errors.New("watch unavailable")
			}
			return stream, nil
		},
	}
	store := catalog.NewStore(types.NamespacedName{})
	watcher := NewWatcher(
		source,
		store,
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithJitter(func(base time.Duration) time.Duration { return base }),
		WithSleeper(func(_ context.Context, delay time.Duration) error {
			delays <- delay
			return nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		stream.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("watcher did not stop after cancellation")
		}
	})

	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	got := make([]time.Duration, len(want))
	for i := range got {
		got[i] = <-delays
	}
	assert.Equal(t, want, got)
}

func TestWatcherLogsOnlySafeIngressMetadata(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	ingress := publishedIngress("tools", "grafana", "Private Display Name")
	ingress.Annotations["portal.homelab.io/secret"] = "never-log-this-token"
	source := fakeIngressSource{
		list: func(context.Context) (*networkingv1.IngressList, error) {
			return &networkingv1.IngressList{
				ListMeta: metav1.ListMeta{ResourceVersion: "10"},
				Items:    []networkingv1.Ingress{ingress},
			}, nil
		},
		watch: func(context.Context, string) (watch.Interface, error) { return stream, nil },
	}
	var logs bytes.Buffer
	store := catalog.NewStore(types.NamespacedName{})
	watcher := NewWatcher(source, store, WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return store.Snapshot(time.Now()).Initialized
	}, time.Second, 5*time.Millisecond)
	initialSuccess := store.Snapshot(time.Now()).LastSuccess
	stream.Modify(&ingress)
	require.Eventually(t, func() bool {
		return store.Snapshot(time.Now()).LastSuccess.After(initialSuccess)
	}, time.Second, 5*time.Millisecond)
	cancel()
	stream.Stop()
	<-done

	output := logs.String()
	assert.NotContains(t, output, "never-log-this-token")
	assert.NotContains(t, output, "Private Display Name")
	assert.NotContains(t, output, "grafana.tailnet.ts.net")
	assert.Contains(t, output, `"namespace":"tools"`)
	assert.Contains(t, output, `"name":"grafana"`)
}

func publishedIngress(namespace, name, displayName string) networkingv1.Ingress {
	className := "tailscale"
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				"portal.homelab.io/enabled":     "true",
				"portal.homelab.io/name":        displayName,
				"portal.homelab.io/description": "Metrics",
				"portal.homelab.io/category":    "Observability",
				"portal.homelab.io/icon":        "grafana",
				"portal.homelab.io/access":      "public",
			},
		},
		Spec: networkingv1.IngressSpec{IngressClassName: &className},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: name + ".tailnet.ts.net"}},
		}},
	}
}

func itemIDs(items []catalog.CatalogItem) []string {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	return ids
}
