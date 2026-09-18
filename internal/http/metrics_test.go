package portalhttp_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	"github.com/Slqzeer/homelab-portal/internal/kube"
	"github.com/prometheus/client_golang/prometheus"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

type unavailableWatch struct{}

func (unavailableWatch) List(context.Context) (*networkingv1.IngressList, error) {
	return &networkingv1.IngressList{}, nil
}
func (unavailableWatch) Watch(context.Context, string) (watch.Interface, error) {
	return nil, errors.New("raw-private-watch-error https://private.example")
}

func TestMetricsExposeCatalogAndWatcherStateWithoutResourceLabels(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	retrying := make(chan struct{}, 1)
	watcher := kube.NewWatcher(unavailableWatch{}, catalog.NewStore(types.NamespacedName{}), kube.WithClock(func() time.Time { return start }), kube.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), kube.WithSleeper(func(ctx context.Context, _ time.Duration) error {
		retrying <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watcher.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	<-retrying
	f := newFixture(t, func(o *portalhttp.Options) { o.Watcher = watcher; o.Registry = prometheus.NewRegistry() })
	if w := f.operations("/metrics"); w.Code != 200 || !strings.Contains(w.Body.String(), `portal_catalog_state{state="uninitialized"} 1`) {
		t.Fatalf("initial metrics = %d %s", w.Code, w.Body)
	}
	f.store.Replace([]networkingv1.Ingress{ingress("PrivateName", "admin", ""), ingress("InvalidName", "broken-secret", "")}, start)
	f.now = start.Add(125 * time.Second)
	w := f.operations("/metrics")
	for _, sample := range []string{`portal_catalog_age_seconds 125`, `portal_catalog_items 1`, `portal_invalid_publications 1`, `portal_catalog_initialized 1`, `portal_catalog_state{state="stale"} 1`, `portal_watch_reconnect_duration_seconds 125`, `portal_watch_state{state="reconnecting"} 1`} {
		if !strings.Contains(w.Body.String(), sample) {
			t.Errorf("missing %s in %s", sample, w.Body)
		}
	}
	admin := f.request("GET", "/admin", f.cookie(t, "portal-admin"))
	if !strings.Contains(admin.Body.String(), "reconnecting") {
		t.Errorf("admin lacks watcher state: %s", admin.Body)
	}
	for _, secret := range []string{"PrivateName", "InvalidName", "broken-secret", "private.example", "raw-private-watch-error", "privatename.tail.example"} {
		if strings.Contains(w.Body.String(), secret) || strings.Contains(admin.Body.String(), secret) {
			t.Errorf("operations leak %s", secret)
		}
	}
	f.now = start.Add(15 * time.Minute)
	if w := f.operations("/metrics"); !strings.Contains(w.Body.String(), `portal_catalog_state{state="expired"} 1`) {
		t.Errorf("expired metric missing: %s", w.Body)
	}
	// A second server owns its registry; no process-global registration collision.
	other := newFixture(t)
	if w := other.operations("/metrics"); w.Code != 200 {
		t.Errorf("second server metrics = %d", w.Code)
	}
}

func TestMetricsCountLoginOutcomesAndAuthorizationDenials(t *testing.T) {
	p := newLoginProvider(t)
	f := newFixture(t, func(o *portalhttp.Options) { o.OIDC = p.client })
	cookie, state := p.start(t, f)
	p.callback(f, cookie, "code=secret-code&state="+state)
	p.callback(f, cookie, "code=secret-code&state="+state)
	f.request("GET", "/admin", nil)
	for i := 0; i < 10; i++ {
		f.request("GET", "/auth/login", nil)
	}
	w := f.operations("/metrics")
	for _, sample := range []string{`portal_login_total{outcome="success"} 1`, `portal_login_total{outcome="failed"} 1`, `portal_login_total{outcome="throttled"} 1`, `portal_authorization_denials_total{route="admin"} 1`} {
		if !strings.Contains(w.Body.String(), sample) {
			t.Errorf("missing %s in %s", sample, w.Body)
		}
	}
}
