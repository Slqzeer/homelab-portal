package portalhttp_test

import (
	networkingv1 "k8s.io/api/networking/v1"
	"strings"
	"testing"
	"time"
)

func TestReadinessFollowsCatalogFreshnessWhileHealthAndLastLinksSurvive(t *testing.T) {
	f := newFixture(t)
	if w := f.operations("/readyz"); w.Code != 503 {
		t.Errorf("initial readiness = %d", w.Code)
	}
	if w := f.operations("/healthz"); w.Code != 200 {
		t.Errorf("initial health = %d", w.Code)
	}
	f.store.Replace([]networkingv1.Ingress{ingress("Public", "public", "")}, f.now)
	for _, tc := range []struct {
		age   time.Duration
		ready int
		stale bool
	}{{0, 200, false}, {2 * time.Minute, 200, true}, {15 * time.Minute, 503, true}} {
		f.now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC).Add(tc.age)
		if w := f.operations("/readyz"); w.Code != tc.ready {
			t.Errorf("at %s: readiness = %d", tc.age, w.Code)
		}
		if w := f.operations("/healthz"); w.Code != 200 {
			t.Errorf("at %s: health = %d", tc.age, w.Code)
		}
		w := f.request("GET", "/", nil)
		if strings.Contains(w.Body.String(), "Catalogue is stale") != tc.stale {
			t.Errorf("at %s: stale warning = %s", tc.age, w.Body)
		}
		if !strings.Contains(w.Body.String(), "https://public.tail.example") {
			t.Errorf("at %s: lost last valid link", tc.age)
		}
	}
}
