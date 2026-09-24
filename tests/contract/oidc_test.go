package contract_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/catalog"
	"github.com/Slqzeer/homelab-portal/internal/config"
	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

const fixtureVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

type providerFixture struct {
	server            *httptest.Server
	publicIssuer      string
	key               *rsa.PrivateKey
	claims            map[string]any
	discoveryRequests atomic.Int32
	tokenRequests     atomic.Int32
	jwksRequests      atomic.Int32
	tokenFailure      atomic.Bool
	tokenUnavailable  atomic.Bool
	jwksFailure       atomic.Bool
	requestMu         sync.Mutex
	requests          []string
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &providerFixture{key: key, publicIssuer: "https://public.example/realms/homelab"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requestMu.Lock()
		f.requests = append(f.requests, r.Host+r.URL.RequestURI())
		f.requestMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/realms/homelab/.well-known/openid-configuration":
			f.discoveryRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.publicIssuer, "authorization_endpoint": f.publicIssuer + "/authorize", "token_endpoint": f.publicIssuer + "/token", "jwks_uri": f.publicIssuer + "/keys", "userinfo_endpoint": f.publicIssuer + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/realms/homelab/keys":
			f.jwksRequests.Add(1)
			if f.jwksFailure.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}})
		case "/realms/homelab/token":
			f.tokenRequests.Add(1)
			if f.tokenUnavailable.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			client, secret, ok := r.BasicAuth()
			_ = r.ParseForm()
			if f.tokenFailure.Load() || !ok || client != "homelab-portal" || secret != "fixture-secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "secret-code" || r.Form.Get("code_verifier") != fixtureVerifier || r.Form.Get("redirect_uri") != "https://portal.example/auth/callback" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "secret-code access-token refresh-token fixture-secret"})
				return
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: "fixture"}}, nil)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			payload, _ := json.Marshal(f.claims)
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "refresh_token": "refresh-token", "token_type": "Bearer", "expires_in": 300, "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	f.claims = map[string]any{"iss": f.publicIssuer, "aud": "homelab-portal", "sub": "private-subject", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "fixture-nonce", "groups": []string{"portal-admin", "Team A"}}
	return f
}

func (f *providerFixture) client(t *testing.T) (*auth.OIDC, context.Context) {
	t.Helper()
	ctx := context.Background()
	client, err := auth.NewOIDC(ctx, config.Config{OIDCIssuerURL: f.publicIssuer, OIDCBackchannelURL: f.server.URL + "/realms/homelab", OIDCClientID: "homelab-portal", OIDCClientSecret: "fixture-secret", OIDCGroupsClaim: "groups", PortalBaseURL: "https://portal.example"})
	require.NoError(t, err)
	return client, ctx
}

func (f *providerFixture) recordedRequests() []string {
	f.requestMu.Lock()
	defer f.requestMu.Unlock()
	return append([]string(nil), f.requests...)
}

func callbackParams() auth.CallbackParams {
	return auth.CallbackParams{Code: "secret-code", State: "fixture-state", ExpectedState: "fixture-state", Nonce: "fixture-nonce", Verifier: fixtureVerifier}
}

func TestOIDCCallbackAcceptsSignedClaimsAndReturnsOnlyAuthorizationFacts(t *testing.T) {
	f := newProviderFixture(t)
	client, ctx := f.client(t)
	claims, err := client.Callback(ctx, callbackParams())
	require.NoError(t, err)
	require.Equal(t, auth.Claims{Groups: []string{"portal-admin", "Team A"}}, claims)
	encoded, err := json.Marshal(claims)
	require.NoError(t, err)
	require.EqualValues(t, 1, f.discoveryRequests.Load())
	require.EqualValues(t, 1, f.tokenRequests.Load())
	require.EqualValues(t, 1, f.jwksRequests.Load())
	internalAuthority := f.server.Listener.Addr().String()
	require.Equal(t, []string{
		internalAuthority + "/realms/homelab/.well-known/openid-configuration",
		internalAuthority + "/realms/homelab/token",
		internalAuthority + "/realms/homelab/keys",
	}, f.recordedRequests())
	for _, private := range []string{"private-subject", "secret-code", "access-token", "refresh-token", "id_token"} {
		require.NotContains(t, string(encoded), private)
	}
}

func TestOIDCCallbackRejectsInvalidSignedIdentity(t *testing.T) {
	cases := map[string]func(*providerFixture){
		"wrong issuer":   func(f *providerFixture) { f.claims["iss"] = "https://other.example" },
		"wrong audience": func(f *providerFixture) { f.claims["aud"] = "another-client" },
		"expired":        func(f *providerFixture) { f.claims["exp"] = time.Now().Add(-time.Minute).Unix() },
		"wrong signature": func(f *providerFixture) {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			f.key = key
		},
		"absent groups":     func(f *providerFixture) { delete(f.claims, "groups") },
		"string groups":     func(f *providerFixture) { f.claims["groups"] = "portal-admin" },
		"object groups":     func(f *providerFixture) { f.claims["groups"] = map[string]any{"portal-admin": true} },
		"mixed groups":      func(f *providerFixture) { f.claims["groups"] = []any{"portal-admin", 5} },
		"null groups":       func(f *providerFixture) { f.claims["groups"] = nil },
		"null group member": func(f *providerFixture) { f.claims["groups"] = []any{"portal-admin", nil} },
		"wrong nonce":       func(f *providerFixture) { f.claims["nonce"] = "other-transaction" },
		"absent nonce":      func(f *providerFixture) { delete(f.claims, "nonce") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProviderFixture(t)
			mutate(f)
			client, ctx := f.client(t)
			claims, err := client.Callback(ctx, callbackParams())
			require.ErrorIs(t, err, auth.ErrLoginFailed)
			require.Empty(t, claims.Groups)
			for _, private := range []string{"secret-code", "access-token", "refresh-token", "fixture-secret"} {
				require.NotContains(t, err.Error(), private)
			}
		})
	}
}

func TestOIDCCallbackAcceptsEmptyExactGroupList(t *testing.T) {
	f := newProviderFixture(t)
	f.claims["groups"] = []string{}
	client, ctx := f.client(t)
	claims, err := client.Callback(ctx, callbackParams())
	require.NoError(t, err)
	require.Empty(t, claims.Groups)
}

func TestOIDCCallbackRequiresTransactionCorrelationBeforeExchange(t *testing.T) {
	cases := map[string]func(*auth.CallbackParams){
		"wrong state":            func(p *auth.CallbackParams) { p.State = "attacker-state" },
		"missing returned state": func(p *auth.CallbackParams) { p.State = "" },
		"missing expected state": func(p *auth.CallbackParams) { p.ExpectedState = "" },
		"missing nonce":          func(p *auth.CallbackParams) { p.Nonce = "" },
		"missing verifier":       func(p *auth.CallbackParams) { p.Verifier = "" },
		"invalid verifier":       func(p *auth.CallbackParams) { p.Verifier = "invalid verifier" },
		"missing code":           func(p *auth.CallbackParams) { p.Code = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProviderFixture(t)
			client, ctx := f.client(t)
			params := callbackParams()
			mutate(&params)
			_, err := client.Callback(ctx, params)
			require.ErrorIs(t, err, auth.ErrLoginFailed)
			require.Zero(t, f.tokenRequests.Load(), "invalid transaction must not send code to provider")
		})
	}
}

func TestOIDCCallbackRejectsCrossedPKCEVerifierAndSanitizesProviderErrors(t *testing.T) {
	for _, upstreamFailure := range []bool{false, true} {
		f := newProviderFixture(t)
		f.tokenFailure.Store(upstreamFailure)
		client, ctx := f.client(t)
		params := callbackParams()
		if !upstreamFailure {
			params.Verifier = "crossed_verifier_from_other_transaction_00000"
		}
		_, err := client.Callback(ctx, params)
		require.ErrorIs(t, err, auth.ErrLoginFailed)
		require.Equal(t, "portal login failed", err.Error())
	}
}

func TestExistingPortalSessionAuthorizesUntilLocalExpiryDuringProviderOutage(t *testing.T) {
	f := newProviderFixture(t)
	client, ctx := f.client(t)
	claims, err := client.Callback(ctx, callbackParams())
	require.NoError(t, err)
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	now := time.Now()
	w := httptest.NewRecorder()
	require.NoError(t, manager.Create(w, claims, now))
	cookie := w.Result().Cookies()[0]
	f.server.Close()
	r := httptest.NewRequest("GET", "https://portal.example/api/catalog", nil)
	r.AddCookie(cookie)
	identity, err := manager.Load(r, now.Add(29*time.Minute))
	require.NoError(t, err)
	items := []catalog.CatalogItem{{Name: "Admin", Access: catalog.AccessAdmin}, {Name: "Team", Access: catalog.AccessGroups, Groups: []string{"Team A"}}}
	require.Len(t, catalog.Visible(items, identity), 2)
	identity, err = manager.Load(r, now.Add(30*time.Minute))
	require.ErrorIs(t, err, auth.ErrInvalidSession)
	require.Empty(t, catalog.Visible(items, identity))
}

func TestOIDCCallbackReturnsGenericFailureForBackchannelOutages(t *testing.T) {
	cases := map[string]func(*providerFixture){
		"token": func(f *providerFixture) { f.tokenUnavailable.Store(true) },
		"JWKS":  func(f *providerFixture) { f.jwksFailure.Store(true) },
	}
	for name, makeUnavailable := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProviderFixture(t)
			makeUnavailable(f)
			client, ctx := f.client(t)

			claims, err := client.Callback(ctx, callbackParams())
			require.Empty(t, claims)
			require.ErrorIs(t, err, auth.ErrLoginFailed)
			require.EqualError(t, err, "portal login failed")
			for _, private := range []string{f.publicIssuer, f.server.URL, "secret-code", "access-token", "refresh-token", "fixture-secret"} {
				require.NotContains(t, err.Error(), private)
			}
		})
	}
}

func TestOIDCCallbackHonorsCancelledContextWithoutSendingCredentials(t *testing.T) {
	f := newProviderFixture(t)
	client, _ := f.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	claims, err := client.Callback(ctx, callbackParams())
	require.Empty(t, claims)
	require.ErrorIs(t, err, auth.ErrLoginFailed)
	require.EqualError(t, err, "portal login failed")
	require.Zero(t, f.tokenRequests.Load())
	for _, private := range []string{f.publicIssuer, f.server.URL, "secret-code", "fixture-secret"} {
		require.NotContains(t, err.Error(), private)
	}
}
