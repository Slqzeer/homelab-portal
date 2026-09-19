package integration_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/assets"
	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/catalog"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	store := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	store.Replace([]networkingv1.Ingress{
		publishedIngress("Grafana", "Observability", "public", "grafana"),
		publishedIngress("Secret Admin", "Private operations", "admin", "unknown-icon"),
	}, now)
	sessions, err := auth.NewSessionManager(bytes.Repeat([]byte{9}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := portalhttp.New(portalhttp.Options{
		Store: store, Sessions: sessions, Assets: assets.FS(), BaseURL: "https://portal.example",
		Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	anonymous := renderHome(t, handler, nil)
	assertContainsAll(t, anonymous,
		`<label for="catalog-search">Search catalog</label>`,
		`data-catalog-search`, `data-catalog-clear-search hidden>Clear search</button>`, `data-category-filter="" aria-pressed="true"`,
		`data-empty-results hidden`, `data-empty-clear-search>Clear search</button>`,
		`data-empty-clear-category>Return to All</button>`, `No catalog items match these filters.`,
		`data-catalog-status role="status" aria-live="polite"`,
		`<button class="button button-secondary theme-toggle" type="button" data-theme-toggle>`,
		`data-theme-toggle-icon aria-hidden="true"`,
		`data-theme-toggle-label>Switch to light theme</span>`,
		`data-catalog-item`, `data-catalog-name`, `data-catalog-description`, `data-catalog-category`,
		`href="/auth/login"`, `src="/icons/grafana.svg"`,
	)
	assertHTMLOrder(t, anonymous,
		`<nav class="identity-actions" aria-label="Account">`,
		`data-theme-toggle`,
		`href="/auth/login"`,
	)
	assertThemeInitializer(t, anonymous)
	assertContainsNone(t, anonymous, "Secret Admin", "Private operations", "secret-admin.tail.example", ">Sign out<", `href="/admin"`, `href="/profile"`)

	authenticatedCookie := sessionCookie(t, sessions, now)
	authenticated := renderHome(t, handler, authenticatedCookie)
	assertContainsAll(t, authenticated, `href="/profile"`, `Profile: Signed-in user`, `Signed in.`)
	assertContainsNone(t, authenticated, `>Sign in<`, `href="/admin"`, "Secret Admin", "secret-admin.tail.example")

	adminCookie := sessionCookie(t, sessions, now, "portal-admin")
	admin := renderHome(t, handler, adminCookie)
	assertContainsAll(t, admin, `href="/profile"`, `href="/admin"`, "Secret Admin", "https://secret-admin.tail.example", `src="/icons/generic.svg"`)
	assertContainsNone(t, admin, `>Sign in<`)

	adminPage := serve(t, handler, http.MethodGet, "/admin", adminCookie)
	assertContainsAll(t, adminPage, `<html lang="en" data-theme="dark">`)
	assertThemeInitializer(t, adminPage)

	for _, match := range regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(anonymous, -1) {
		value := match[1]
		if strings.HasSuffix(value, ".css") || strings.HasSuffix(value, ".js") || strings.HasSuffix(value, ".svg") {
			if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
				t.Errorf("asset is not same-origin: %q", value)
			}
		}
	}

	script := serve(t, handler, http.MethodGet, "/app.js", nil)
	assertContainsNone(t, script, "Secret Admin", "secret-admin.tail.example", "fetch(", "XMLHttpRequest")
}

func TestRenderedCatalogPlacesInformationBeforeControls(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	store := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	store.Replace([]networkingv1.Ingress{
		publishedIngress("Grafana", "Observability", "public", "grafana"),
	}, now)
	sessions, err := auth.NewSessionManager(bytes.Repeat([]byte{6}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := portalhttp.New(portalhttp.Options{
		Store: store, Sessions: sessions, Assets: assets.FS(), BaseURL: "https://portal.example",
		Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	html := renderHome(t, handler, nil)
	assertContainsAll(t, html, "Tailnet catalogue", "Homelab Portal", "Available to you")
	if got := strings.Count(html, `data-catalog-status`); got != 1 {
		t.Fatalf("catalog status count = %d, want 1", got)
	}
	assertHTMLOrder(t, html,
		`<aside class="catalog-information-rail"`,
		`data-catalog-status role="status" aria-live="polite" aria-atomic="true"`,
		`<section class="catalog-controls"`,
		`<div id="catalog-items" class="catalog-grid" data-catalog-grid>`,
	)
}

func TestRenderedStaleStateDescribesCatalogueFreshnessOnly(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	store := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	store.Replace([]networkingv1.Ingress{publishedIngress("Grafana", "Observability", "public", "grafana")}, now.Add(-3*time.Minute))
	sessions, err := auth.NewSessionManager(bytes.Repeat([]byte{8}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := portalhttp.New(portalhttp.Options{Store: store, Sessions: sessions, Assets: assets.FS(), BaseURL: "https://portal.example", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	html := renderHome(t, handler, nil)
	assertContainsAll(t, html, `role="status"`, "Catalogue information is not current", "last known catalogue links")
	assertContainsNone(t, strings.ToLower(html), "healthy", "unhealthy", "target status", "service status")
	assertHTMLOrder(t, html,
		`<aside class="catalog-information-rail"`,
		`<aside class="stale-banner" role="status" aria-live="polite">`,
		`<section class="catalog-controls"`,
	)
}

func publishedIngress(name, category, access, icon string) networkingv1.Ingress {
	class := "tailscale"
	ingressName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: ingressName, Annotations: map[string]string{
			"portal.homelab.io/enabled": "true", "portal.homelab.io/name": name,
			"portal.homelab.io/description": name + " description", "portal.homelab.io/category": category,
			"portal.homelab.io/access": access, "portal.homelab.io/icon": icon,
		}},
		Spec:   networkingv1.IngressSpec{IngressClassName: &class},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: ingressName + ".tail.example"}}}},
	}
}

func sessionCookie(t *testing.T, sessions *auth.SessionManager, now time.Time, groups ...string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if err := sessions.Create(w, auth.Claims{Groups: groups}, now); err != nil {
		t.Fatal(err)
	}
	return w.Result().Cookies()[0]
}

func renderHome(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	return serve(t, handler, http.MethodGet, "/", cookie)
}

func serve(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie) string {
	t.Helper()
	r := httptest.NewRequest(method, "https://portal.example"+path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d: %s", method, path, w.Code, w.Body)
	}
	return w.Body.String()
}

func assertContainsAll(t *testing.T, value string, expected ...string) {
	t.Helper()
	for _, item := range expected {
		if !strings.Contains(value, item) {
			t.Errorf("missing %q in %s", item, value)
		}
	}
}

func assertContainsNone(t *testing.T, value string, unexpected ...string) {
	t.Helper()
	for _, item := range unexpected {
		if strings.Contains(value, item) {
			t.Errorf("unexpected %q in %s", item, value)
		}
	}
}

func assertThemeInitializer(t *testing.T, html string) {
	t.Helper()
	script := `<script src="/theme.js"></script>`
	if count := strings.Count(html, script); count != 1 {
		t.Fatalf("theme initializer count = %d, want 1", count)
	}
	scriptAt := strings.Index(html, script)
	stylesheetAt := strings.Index(html, `<link rel="stylesheet"`)
	if scriptAt < 0 || (stylesheetAt >= 0 && scriptAt > stylesheetAt) {
		t.Fatalf("theme initializer must precede styles: %s", html)
	}
	assertContainsNone(t, html, `<script>`, "localStorage", "matchMedia")
}

func assertHTMLOrder(t *testing.T, html string, markers ...string) {
	t.Helper()
	offset := 0
	for _, marker := range markers {
		index := strings.Index(html[offset:], marker)
		if index < 0 {
			t.Fatalf("missing %q in rendered HTML", marker)
		}
		offset += index + len(marker)
	}
}
