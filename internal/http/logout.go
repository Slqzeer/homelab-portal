package portalhttp

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"
)

const csrfCookieName = "__Host-portal_csrf"

// A Secure, host-only double-submit token survives encrypted session refresh.
// Exact Origin validation and the __Host prefix prevent cross-site/subdomain
// cookie injection. Login rotates it; logout clears it.
func (s *server) csrfToken(w http.ResponseWriter, r *http.Request, expires time.Time) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && validCSRF(cookie.Value) {
		return cookie.Value
	}
	return setCSRF(w, expires)
}

func validCSRF(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && len(value) == 43
}

func setCSRF(w http.ResponseWriter, expires time.Time) string {
	token := randomToken()
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires})
	return token
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != s.origin {
		http.Error(w, "Logout request rejected.", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || len(r.PostForm["csrf_token"]) != 1 {
		http.Error(w, "Logout request rejected.", http.StatusForbidden)
		return
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || !validCSRF(cookie.Value) || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(cookie.Value)) != 1 {
		http.Error(w, "Logout request rejected.", http.StatusForbidden)
		return
	}
	identity, err := s.Sessions.Load(r, s.Now())
	if err != nil || identity == nil {
		s.Sessions.Destroy(w)
		http.Error(w, "Logout request rejected.", http.StatusForbidden)
		return
	}
	s.Sessions.Destroy(w)
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
