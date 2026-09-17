package kube

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"os"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	initialListTimeout = 10 * time.Second
	minRetryDelay      = time.Second
	maxRetryDelay      = 30 * time.Second
)

// Option configures a Watcher dependency.
type Option func(*Watcher)

// WithLogger configures the structured logger used for safe operational events.
func WithLogger(logger *slog.Logger) Option {
	return func(watcher *Watcher) {
		if logger != nil {
			watcher.logger = logger
		}
	}
}

// WithJitter configures retry-delay jitter. The resulting delay is always
// clamped to the supported retry range.
func WithJitter(jitter func(time.Duration) time.Duration) Option {
	return func(watcher *Watcher) {
		if jitter != nil {
			watcher.jitter = jitter
		}
	}
}

// WithSleeper configures context-aware retry waiting.
func WithSleeper(sleeper func(context.Context, time.Duration) error) Option {
	return func(watcher *Watcher) {
		if sleeper != nil {
			watcher.sleep = sleeper
		}
	}
}

// WithClock configures the success timestamp clock.
func WithClock(now func() time.Time) Option {
	return func(watcher *Watcher) {
		if now != nil {
			watcher.now = now
		}
	}
}

// Watcher maintains a last-valid catalog from Kubernetes Ingress list/watch
// state.
type Watcher struct {
	source IngressSource
	store  *catalog.Store
	logger *slog.Logger
	now    func() time.Time
	jitter func(time.Duration) time.Duration
	sleep  func(context.Context, time.Duration) error
}

// NewWatcher creates a read-only Kubernetes catalog watcher.
func NewWatcher(source IngressSource, store *catalog.Store, options ...Option) *Watcher {
	watcher := &Watcher{
		source: source,
		store:  store,
		logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		now:    time.Now,
		jitter: func(base time.Duration) time.Duration {
			return base + time.Duration(rand.Int64N(int64(base/2)+1))
		},
		sleep: sleepContext,
	}
	for _, option := range options {
		option(watcher)
	}
	return watcher
}

// Run lists current Ingresses, then applies watch state changes until the
// context is cancelled or the stream disconnects.
func (watcher *Watcher) Run(ctx context.Context) {
	backoff := minRetryDelay
	for {
		listCtx, cancel := context.WithTimeout(ctx, initialListTimeout)
		list, err := watcher.source.List(listCtx)
		cancel()
		if err != nil {
			watcher.logger.Warn("kubernetes ingress observation", "event", "list", "result", "failed")
			if !watcher.waitToRetry(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		resources := make(map[types.NamespacedName]networkingv1.Ingress, len(list.Items))
		for i := range list.Items {
			ingress := list.Items[i].DeepCopy()
			resources[types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Name}] = *ingress
		}
		watcher.store.Replace(ingressSlice(resources), watcher.now())
		watcher.logger.Info("kubernetes ingress observation", "event", "list", "result", "rebuilt", "count", len(resources))
		resourceVersion := list.ResourceVersion
		backoff = minRetryDelay

		relist := false
		for !relist {
			stream, watchErr := watcher.source.Watch(ctx, resourceVersion)
			if watchErr != nil {
				if resourceVersionExpired(watchErr) {
					watcher.logger.Warn("kubernetes ingress observation", "event", "watch", "result", "resource_expired")
					relist = true
					continue
				}
				watcher.logger.Warn("kubernetes ingress observation", "event", "watch", "result", "failed")
				if !watcher.waitToRetry(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
				continue
			}

			outcome := watcher.consumeStream(ctx, stream, resources, &resourceVersion, &backoff)
			stream.Stop()
			switch outcome {
			case streamStopped:
				return
			case streamExpired:
				relist = true
			case streamDisconnected:
				if !watcher.waitToRetry(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
			}
		}
	}
}

type streamOutcome int

const (
	streamStopped streamOutcome = iota
	streamDisconnected
	streamExpired
)

func (watcher *Watcher) consumeStream(
	ctx context.Context,
	stream watch.Interface,
	resources map[types.NamespacedName]networkingv1.Ingress,
	resourceVersion *string,
	backoff *time.Duration,
) streamOutcome {
	for {
		select {
		case <-ctx.Done():
			return streamStopped
		case event, ok := <-stream.ResultChan():
			if !ok {
				watcher.logger.Warn("kubernetes ingress observation", "event", "watch", "result", "disconnected")
				return streamDisconnected
			}
			if event.Type == watch.Error {
				err := apierrors.FromObject(event.Object)
				if resourceVersionExpired(err) {
					watcher.logger.Warn("kubernetes ingress observation", "event", "watch", "result", "resource_expired")
					return streamExpired
				}
				watcher.logger.Warn("kubernetes ingress observation", "event", "watch", "result", "failed")
				return streamDisconnected
			}

			ingress, ok := event.Object.(*networkingv1.Ingress)
			if !ok {
				watcher.logger.Warn("kubernetes ingress observation", "event", string(event.Type), "result", "ignored")
				continue
			}
			key := types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Name}
			switch event.Type {
			case watch.Added, watch.Modified:
				resources[key] = *ingress.DeepCopy()
			case watch.Deleted:
				delete(resources, key)
			default:
				watcher.logger.Warn("kubernetes ingress observation", "event", string(event.Type), "result", "ignored")
				continue
			}
			if ingress.ResourceVersion != "" {
				*resourceVersion = ingress.ResourceVersion
			}

			watcher.store.Replace(ingressSlice(resources), watcher.now())
			*backoff = minRetryDelay
			watcher.logger.Info(
				"kubernetes ingress observation",
				"event", string(event.Type),
				"result", "rebuilt",
				"count", len(resources),
				"namespace", ingress.Namespace,
				"name", ingress.Name,
			)
		}
	}
}

func ingressSlice(resources map[types.NamespacedName]networkingv1.Ingress) []networkingv1.Ingress {
	ingresses := make([]networkingv1.Ingress, 0, len(resources))
	for _, ingress := range resources {
		ingresses = append(ingresses, ingress)
	}
	return ingresses
}

func (watcher *Watcher) waitToRetry(ctx context.Context, base time.Duration) bool {
	delay := watcher.jitter(base)
	if delay < minRetryDelay {
		delay = minRetryDelay
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	watcher.logger.Info("kubernetes ingress observation", "event", "retry", "result", "scheduled")
	return watcher.sleep(ctx, delay) == nil
}

func nextBackoff(current time.Duration) time.Duration {
	if current >= maxRetryDelay/2 {
		return maxRetryDelay
	}
	return current * 2
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func resourceVersionExpired(err error) bool {
	return apierrors.IsResourceExpired(err) || apierrors.IsGone(err)
}
