package portalhttp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
)

func TestLogoutRequiresSameOriginAndBrowserBoundCSRF(t *testing.T) {
	f := newFixture(t)
	f.store.Replace([]networkingv1.Ingress{ingress("Members", "authenticated", "")}, f.now)
	w := f.request("GET", "/profile", f.cookie(t))
	match := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatalf("missing logout CSRF form: %s", w.Body)
	}
	token := match[1]
	cookie := responseCookie(t, w, "__Host-portal_session")
	csrfCookie := responseCookie(t, w, "__Host-portal_csrf")
	if !csrfCookie.Secure || !csrfCookie.HttpOnly || csrfCookie.Path != "/" || csrfCookie.Domain != "" {
		t.Fatalf("unsafe CSRF cookie: %v", csrfCookie)
	}
	other := f.request("GET", "/profile", f.cookie(t))
	otherCSRF := responseCookie(t, other, "__Host-portal_csrf")
	for _, tc := range []struct {
		name, origin, token string
		csrf                *http.Cookie
		want                int
	}{
		{"missing origin", "", token, csrfCookie, 403},
		{"foreign origin", "https://attacker.example", token, csrfCookie, 403},
		{"suffix origin", "https://portal.example.attacker.example", token, csrfCookie, 403},
		{"missing token", "https://portal.example", "", csrfCookie, 403},
		{"other browser", "https://portal.example", token, otherCSRF, 403},
		{"valid", "https://portal.example", token, csrfCookie, 303},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://portal.example/auth/logout", strings.NewReader(url.Values{"csrf_token": {tc.token}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", tc.origin)
			r.AddCookie(cookie)
			r.AddCookie(tc.csrf)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("logout = %d: %s", w.Code, w.Body)
			}
			if tc.want == 403 && len(w.Result().Cookies()) != 0 {
				t.Error("rejected request mutated session")
			}
			if tc.want == 303 {
				if w.Header().Get("Location") != "/" {
					t.Error("logout redirect is not local")
				}
				if c := responseCookie(t, w, "__Host-portal_session"); c.MaxAge != -1 {
					t.Fatalf("session not cleared: %v", c)
				}
				if c := responseCookie(t, w, "__Host-portal_csrf"); c.MaxAge != -1 {
					t.Fatalf("CSRF cookie not cleared: %v", c)
				}
				if home := f.request("GET", "/", nil); strings.Contains(home.Body.String(), "Members") {
					t.Error("logged out browser receives authenticated item")
				}
			}
		})
	}
	if w := f.request("GET", "/auth/logout", cookie); w.Code != 405 {
		t.Errorf("GET logout = %d", w.Code)
	}
}

func TestLogoutFormSurvivesSessionRefreshInAnotherTab(t *testing.T) {
	f := newFixture(t)
	w := f.request("GET", "/profile", f.cookie(t))
	token := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())[1]
	csrf := responseCookie(t, w, "__Host-portal_csrf")
	session := responseCookie(t, w, "__Host-portal_session")
	w = f.request("GET", "/profile", session, csrf)
	session = responseCookie(t, w, "__Host-portal_session")
	r := httptest.NewRequest("POST", "https://portal.example/auth/logout", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://portal.example")
	r.AddCookie(session)
	r.AddCookie(csrf)
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatalf("form from earlier tab rejected after refresh: %d", w.Code)
	}
}
