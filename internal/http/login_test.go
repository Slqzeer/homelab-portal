package portalhttp_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/config"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
)

type loginProvider struct {
	server           *httptest.Server
	ctx              context.Context
	client           *auth.OIDC
	requests         atomic.Int32
	nonce, challenge atomic.Value
}

func newLoginProvider(t *testing.T) *loginProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &loginProvider{}
	p.nonce.Store("")
	p.challenge.Store("")
	p.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			p.requests.Add(1)
			_ = r.ParseForm()
			client, secret, ok := r.BasicAuth()
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || client != "homelab-portal" || secret != "fixture-secret" || r.Form.Get("code") != "secret-code" || base64.RawURLEncoding.EncodeToString(challenge[:]) != p.challenge.Load().(string) {
				http.Error(w, "secret provider failure", 400)
				return
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "fixture"}}, nil)
			claims, _ := json.Marshal(map[string]any{"iss": p.server.URL, "aud": "homelab-portal", "sub": "private-subject", "exp": time.Now().Add(time.Hour).Unix(), "nonce": p.nonce.Load().(string), "groups": []string{"portal-admin"}})
			signed, _ := signer.Sign(claims)
			raw, _ := signed.CompactSerialize()
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "private-access-token", "refresh_token": "private-refresh-token", "token_type": "Bearer", "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.server.Close)
	p.ctx = oidc.ClientContext(context.Background(), p.server.Client())
	p.client, err = auth.NewOIDC(p.ctx, config.Config{OIDCIssuerURL: p.server.URL, OIDCClientID: "homelab-portal", OIDCClientSecret: "fixture-secret", OIDCGroupsClaim: "groups", PortalBaseURL: "https://portal.example"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *loginProvider) start(t *testing.T, f *fixture) (*http.Cookie, string) {
	t.Helper()
	w := f.request("GET", "/auth/login", nil)
	if w.Code != 302 {
		t.Fatalf("login status = %d: %s", w.Code, w.Body)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	p.nonce.Store(q.Get("nonce"))
	p.challenge.Store(q.Get("code_challenge"))
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("incomplete authorization request: %s", u)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-portal_login" {
			if !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge > 300 {
				t.Fatalf("unsafe login cookie: %v", c)
			}
			if strings.Contains(c.Value, q.Get("state")) || strings.Contains(c.Value, q.Get("nonce")) {
				t.Fatal("login cookie is not encrypted")
			}
			return c, q.Get("state")
		}
	}
	t.Fatal("missing browser transaction cookie")
	return nil, ""
}

func (p *loginProvider) callback(f *fixture, cookie *http.Cookie, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://portal.example/auth/callback?"+query, nil).WithContext(p.ctx)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func TestLoginCallbackConsumesBrowserBoundTransactionOnce(t *testing.T) {
	p := newLoginProvider(t)
	f := newFixture(t, func(o *portalhttp.Options) { o.OIDC = p.client })
	cookie, state := p.start(t, f)
	w := p.callback(f, cookie, "code=secret-code&state="+url.QueryEscape(state))
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("callback = %d: %s", w.Code, w.Body)
	}
	cleared := false
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-portal_login" && c.MaxAge == -1 {
			cleared = true
		}
		if c.Name == "__Host-portal_session" {
			session = c
		}
	}
	if !cleared || session == nil {
		t.Fatalf("callback cookies: %v", w.Result().Cookies())
	}
	if w := f.request("GET", "/admin", session); w.Code != 200 {
		t.Fatalf("signed claims did not authorize admin: %d", w.Code)
	}
	if w := p.callback(f, cookie, "code=secret-code&state="+url.QueryEscape(state)); w.Code != 400 {
		t.Errorf("replayed transaction = %d", w.Code)
	}
	if p.requests.Load() != 1 {
		t.Errorf("provider exchange count = %d", p.requests.Load())
	}
}

func TestCallbackRejectsInvalidTransactionsAndAlwaysClearsCookie(t *testing.T) {
	p := newLoginProvider(t)
	for _, scenario := range []string{"missing cookie", "tampered cookie", "expired", "wrong state", "missing state", "duplicate state", "duplicate code", "provider rejection", "crossed browser"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, func(o *portalhttp.Options) { o.OIDC = p.client })
			cookie, state := p.start(t, f)
			query := "code=secret-code&state=" + url.QueryEscape(state)
			switch scenario {
			case "missing cookie":
				cookie = nil
			case "tampered cookie":
				cookie.Value = "!" + cookie.Value
			case "expired":
				f.now = f.now.Add(5 * time.Minute)
			case "wrong state":
				query = "code=secret-code&state=attacker"
			case "missing state":
				query = "code=secret-code"
			case "duplicate state":
				query += "&state=attacker"
			case "duplicate code":
				query += "&code=other"
			case "provider rejection":
				query += "&error=access_denied&error_description=provider-secret"
			case "crossed browser":
				cookie, _ = p.start(t, f)
			}
			before := p.requests.Load()
			w := p.callback(f, cookie, query)
			if w.Code != 400 {
				t.Fatalf("invalid callback = %d", w.Code)
			}
			cleared := false
			for _, c := range w.Result().Cookies() {
				if c.Name == "__Host-portal_login" && c.MaxAge == -1 {
					cleared = true
				}
				if c.Name == "__Host-portal_session" {
					t.Error("invalid callback created session")
				}
			}
			if !cleared {
				t.Error("transaction cookie not cleared")
			}
			if p.requests.Load() != before {
				t.Error("invalid transaction contacted provider")
			}
			for _, secret := range []string{"secret-code", "provider-secret", "attacker"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Errorf("response leaked %s", secret)
				}
			}
		})
	}
}

func TestLoginThrottlesTrustedPeerAndRoundsRetryAfterUp(t *testing.T) {
	p := newLoginProvider(t)
	f := newFixture(t, func(o *portalhttp.Options) { o.OIDC = p.client })
	request := func(peer, forwarded string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://portal.example/auth/login", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", forwarded)
		r.Header.Set("Forwarded", "for="+forwarded)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 10; i++ {
		if w := request("192.0.2.9:1234", "198.51.100.1"); w.Code != 302 {
			t.Fatalf("permitted attempt %d = %d", i, w.Code)
		}
	}
	w := request("192.0.2.9:9999", "198.51.100.2")
	if w.Code != 429 || w.Header().Get("Retry-After") != "600" || !strings.Contains(w.Body.String(), "Too many sign-in attempts") {
		t.Fatalf("throttle = %d %v %s", w.Code, w.Header(), w.Body)
	}
	f.now = f.now.Add(1500 * time.Millisecond)
	if w = request("192.0.2.9:1111", "198.51.100.3"); w.Code != 429 || w.Header().Get("Retry-After") != "599" {
		t.Fatalf("rounded throttle = %d %v", w.Code, w.Header())
	}
	if w = request("192.0.2.10:1234", "198.51.100.1"); w.Code != 302 {
		t.Errorf("independent peer = %d", w.Code)
	}
}

func TestConcurrentCallbacksCanConsumeTransactionOnlyOnce(t *testing.T) {
	p := newLoginProvider(t)
	f := newFixture(t, func(o *portalhttp.Options) {
		o.OIDC = p.client
		o.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	})
	cookie, state := p.start(t, f)
	var attempts sync.WaitGroup
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		attempts.Go(func() { results <- p.callback(f, cookie, "code=secret-code&state="+state).Code })
	}
	attempts.Wait()
	close(results)
	success, failed := 0, 0
	for result := range results {
		switch result {
		case 303:
			success++
		case 400:
			failed++
		default:
			t.Errorf("unexpected concurrent callback status %d", result)
		}
	}
	if success != 1 || failed != 7 || p.requests.Load() != 1 {
		t.Errorf("one-time consumption: success=%d failed=%d provider exchanges=%d", success, failed, p.requests.Load())
	}
}
