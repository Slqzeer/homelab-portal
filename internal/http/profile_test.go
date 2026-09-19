package portalhttp_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestProfileRequiresAuthenticationAndRendersApprovedAccountData(t *testing.T) {
	f := newFixture(t)
	if w := f.request(http.MethodGet, "/profile", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("anonymous profile = %d", w.Code)
	}
	w := f.request(http.MethodGet, "/profile", f.cookie(t, "portal-admin"))
	if w.Code != http.StatusOK { t.Fatalf("profile = %d: %s", w.Code, w.Body) }
	body := w.Body.String()
	for _, want := range []string{"Signed-in user", "Signed in.", "csrf_token", "Sign out", "Back to catalogue", "Admin diagnostics"} {
		if !strings.Contains(body, want) { t.Errorf("profile missing %q", want) }
	}
	for _, forbidden := range []string{"portal-admin", "private-subject", "private-access-token", "private-refresh-token"} {
		if strings.Contains(body, forbidden) { t.Errorf("profile leaked %q", forbidden) }
	}
}

func TestProfileIsNotOnOperationsListener(t *testing.T) {
	f := newFixture(t)
	if w := f.operations("/profile"); w.Code != http.StatusNotFound { t.Fatalf("operations profile = %d", w.Code) }
}
