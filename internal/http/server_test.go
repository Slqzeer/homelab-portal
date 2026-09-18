package portalhttp_test

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

type fixture struct {
	store    *catalog.Store
	sessions *auth.SessionManager
	now      time.Time
	logs     bytes.Buffer
	handler  *portalhttp.Handlers
}

func newFixture(t *testing.T, configure ...func(*portalhttp.Options)) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC), store: catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})}
	var err error
	f.sessions, err = auth.NewSessionManager(bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	options := portalhttp.Options{Store: f.store, Sessions: f.sessions, Assets: assets.FS(), BaseURL: "https://portal.example", Now: func() time.Time { return f.now }, Logger: slog.New(slog.NewJSONHandler(&f.logs, nil))}
	for _, configure := range configure {
		configure(&options)
	}
	f.handler, err = portalhttp.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func ingress(name, access, groups string) networkingv1.Ingress {
	class := "tailscale"
	annotations := map[string]string{"portal.homelab.io/enabled": "true", "portal.homelab.io/name": name, "portal.homelab.io/access": access, "portal.homelab.io/description": name + " description", "portal.homelab.io/category": name + " category"}
	if groups != "" {
		annotations["portal.homelab.io/groups"] = groups
	}
	return networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: strings.ToLower(name), Annotations: annotations}, Spec: networkingv1.IngressSpec{IngressClassName: &class}, Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: strings.ToLower(name) + ".tail.example"}}}}}
}

func (f *fixture) cookie(t *testing.T, groups ...string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if err := f.sessions.Create(w, auth.Claims{Groups: groups}, f.now); err != nil {
		t.Fatal(err)
	}
	return w.Result().Cookies()[0]
}

func (f *fixture) request(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://portal.example"+path, nil)
	for _, cookie := range cookies {
		if cookie != nil {
			r.AddCookie(cookie)
		}
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) operations(path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler.Operations.ServeHTTP(w, httptest.NewRequest("GET", "http://operations:8081"+path, nil))
	return w
}

func TestOperationsListenerHasNoPagesAuthOrAssets(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/", "/admin", "/auth/login", "/auth/callback", "/auth/logout", "/app.js", "/index.html", "/api/catalog"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := httptest.NewRequest(method, "https://portal.example"+path, nil)
			r.AddCookie(f.cookie(t, "portal-admin"))
			w := httptest.NewRecorder()
			f.handler.Operations.ServeHTTP(w, r)
			if w.Code != 404 || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" || len(w.Result().Cookies()) != 0 {
				t.Errorf("operations %s %s = %d, %v", method, path, w.Code, w.Header())
			}
		}
	}
}

func responseCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing cookie %s", name)
	return nil
}

func TestHomeOnlyRendersAuthorizedCatalogData(t *testing.T) {
	f := newFixture(t)
	f.store.Replace([]networkingv1.Ingress{ingress("Public", "public", ""), ingress("Members", "authenticated", ""), ingress("Editors", "groups", "editors"), ingress("Administrators", "admin", "")}, f.now)
	for _, tc := range []struct {
		name    string
		cookie  *http.Cookie
		visible []string
	}{
		{"anonymous", nil, []string{"Public"}},
		{"authenticated", f.cookie(t), []string{"Public", "Members"}},
		{"group member", f.cookie(t, "editors"), []string{"Public", "Members", "Editors"}},
		{"non-member", f.cookie(t, "Editors", "portal-admin-extra"), []string{"Public", "Members"}},
		{"exact administrator", f.cookie(t, "portal-admin"), []string{"Public", "Members", "Administrators"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request("GET", "/", tc.cookie)
			if w.Code != 200 {
				t.Fatalf("status = %d: %s", w.Code, w.Body)
			}
			for _, name := range []string{"Public", "Members", "Editors", "Administrators"} {
				want := false
				for _, allowed := range tc.visible {
					if name == allowed {
						want = true
					}
				}
				for _, text := range []string{name, name + " description", name + " category", "https://" + strings.ToLower(name) + ".tail.example"} {
					if strings.Contains(w.Body.String(), text) != want {
						t.Errorf("presence of %q must be %v in %s", text, want, w.Body)
					}
				}
			}
		})
	}
}

func TestAdminDiagnosticsAreRestrictedAndAllowlisted(t *testing.T) {
	f := newFixture(t)
	bad := ingress("SecretTargetName", "invalid-value-private", "")
	bad.Annotations["raw-sensitive-annotation"] = "SECRET-ANNOTATION"
	f.store.Replace([]networkingv1.Ingress{bad}, f.now)
	for _, cookie := range []*http.Cookie{nil, f.cookie(t), f.cookie(t, "Portal-admin", "/portal-admin")} {
		if w := f.request("GET", "/admin", cookie); w.Code != 404 {
			t.Errorf("non-admin status = %d", w.Code)
		}
	}
	w := f.request("GET", "/admin", f.cookie(t, "portal-admin"))
	if w.Code != 200 {
		t.Fatalf("admin status = %d", w.Code)
	}
	for _, allowed := range []string{"apps", "secrettargetname", "access", "set access to public, authenticated, groups, or admin"} {
		if !strings.Contains(w.Body.String(), allowed) {
			t.Errorf("diagnostic missing %q", allowed)
		}
	}
	for _, secret := range []string{"SecretTargetName", "SECRET-ANNOTATION", "invalid-value-private", "secrettargetname.tail.example"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("diagnostic leaked %q", secret)
		}
	}
}

func TestSecurityHeadersApplyToPagesErrorsAndStaticAssets(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/", "/admin", "/healthz", "/readyz", "/metrics", "/auth/login", "/auth/callback", "/auth/logout", "/missing", "/app.js", "/index.html", "/api/catalog"} {
		w := f.request("GET", path, nil)
		for key, want := range map[string]string{"Strict-Transport-Security": "max-age=31536000", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store"} {
			if w.Header().Get(key) != want {
				t.Errorf("%s: %s = %q", path, key, w.Header().Get(key))
			}
		}
		csp := w.Header().Get("Content-Security-Policy")
		for _, policy := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "frame-ancestors 'none'", "form-action 'self'"} {
			if !strings.Contains(csp, policy) {
				t.Errorf("%s: missing CSP %s", path, policy)
			}
		}
		if path == "/app.js" && w.Code != 200 {
			t.Errorf("embedded asset status = %d", w.Code)
		}
		if (path == "/index.html" || path == "/api/catalog") && w.Code != 404 {
			t.Errorf("unexpected public route %s: %d", path, w.Code)
		}
	}
	if w := f.request("POST", "/", nil); w.Code != 405 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("method rejection = %d, %v", w.Code, w.Header())
	}
}

func TestPublicListenerCannotServeOperationsWithAnyIdentityOrProxyHeaders(t *testing.T) {
	f := newFixture(t)
	f.store.Replace(nil, f.now)
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, cookie := range []*http.Cookie{nil, f.cookie(t, "portal-admin")} {
				r := httptest.NewRequest(method, "http://internal-service:8080"+path, nil)
				r.Header.Set("X-Forwarded-Host", "localhost")
				r.Header.Set("X-Forwarded-For", "127.0.0.1")
				if cookie != nil {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				f.handler.ServeHTTP(w, r)
				if w.Code != 404 || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
					t.Errorf("public %s %s = %d, %v", method, path, w.Code, w.Header())
				}
			}
		}
	}
}

func TestHomeReferencesEmbeddedAssetsAndEscapesPublicationText(t *testing.T) {
	f := newFixture(t)
	item := ingress("Public", "public", "")
	item.Annotations["portal.homelab.io/name"] = `<script>alert("secret")</script>`
	f.store.Replace([]networkingv1.Ingress{item}, f.now)
	w := f.request("GET", "/", nil)
	if strings.Contains(w.Body.String(), `<script>alert`) || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("publication text not escaped: %s", w.Body)
	}
	styles := regexp.MustCompile(`<link rel="stylesheet" href="(/[^"]+\.css)">`).FindAllStringSubmatch(w.Body.String(), -1)
	if len(styles) == 0 {
		t.Fatalf("no embedded stylesheet linked: %s", w.Body)
	}
	for _, style := range styles {
		if asset := f.request("GET", style[1], nil); asset.Code != 200 {
			t.Errorf("stylesheet %s = %d", style[1], asset.Code)
		}
	}
	if !strings.Contains(w.Body.String(), `<script src="/app.js" defer></script>`) {
		t.Error("missing local client behavior asset")
	}
	if !strings.Contains(w.Body.String(), `article data-catalog-item`) {
		t.Error("missing allowed-card filtering seam")
	}
}

func TestSessionCookiesAreClearedOrRefreshedWithoutExtendingAbsoluteExpiry(t *testing.T) {
	f := newFixture(t)
	w := f.request("GET", "/", &http.Cookie{Name: "__Host-portal_session", Value: "tampered"})
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("invalid session not cleared: %v", cookies)
	}
	cookie := f.cookie(t)
	expires := cookie.Expires
	for i := 0; i < 11; i++ {
		f.now = f.now.Add(20 * time.Minute)
		w = f.request("GET", "/", cookie)
		refreshed := responseCookie(t, w, "__Host-portal_session")
		if refreshed.MaxAge == -1 || !refreshed.Expires.Equal(expires) {
			t.Fatalf("valid refresh changed absolute expiry: %v", refreshed)
		}
		cookie = refreshed
	}
	f.now = f.now.Add(20 * time.Minute)
	w = f.request("GET", "/", cookie)
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("absolute-expired session not cleared: %v", cookies)
	}
}
