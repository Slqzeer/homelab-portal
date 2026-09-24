package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoginURLUsesAuthorizationCodePKCEAndCorrelatedStateNonce(t *testing.T) {
	const publicIssuer = "https://public.example/realms/homelab"
	var discoveryRequests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveryRequests.Add(1)
		require.Equal(t, "/realms/homelab/.well-known/openid-configuration", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": publicIssuer, "authorization_endpoint": publicIssuer + "/authorize", "token_endpoint": publicIssuer + "/token", "jwks_uri": publicIssuer + "/keys", "userinfo_endpoint": publicIssuer + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer provider.Close()
	client, err := auth.NewOIDC(context.Background(), config.Config{OIDCIssuerURL: publicIssuer, OIDCBackchannelURL: provider.URL + "/realms/homelab", OIDCClientID: "homelab-portal", OIDCClientSecret: "fixture-secret", OIDCGroupsClaim: "groups", PortalBaseURL: "https://portal.example"})
	require.NoError(t, err)
	// RFC 7636 Appendix B gives an independent S256 verifier/challenge vector.
	login, err := url.Parse(client.LoginURL("state-value", "nonce-value", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	require.NoError(t, err)
	q := login.Query()
	require.Equal(t, "public.example", login.Host)
	require.EqualValues(t, 1, discoveryRequests.Load())
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "homelab-portal", q.Get("client_id"))
	require.Equal(t, "https://portal.example/auth/callback", q.Get("redirect_uri"))
	require.Equal(t, "openid profile groups", q.Get("scope"))
	require.Equal(t, "state-value", q.Get("state"))
	require.Equal(t, "nonce-value", q.Get("nonce"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", q.Get("code_challenge"))
	require.Empty(t, q.Get("code_verifier"))
	require.Empty(t, q.Get("client_secret"))
}

func TestNewOIDCRejectsUnsafeProviderEndpoints(t *testing.T) {
	const issuer = "https://public.example/realms/homelab"
	valid := map[string]string{
		"authorization_endpoint": issuer + "/authorize",
		"token_endpoint":         issuer + "/token",
		"jwks_uri":               issuer + "/keys",
		"userinfo_endpoint":      issuer + "/userinfo",
	}
	invalid := map[string]string{
		"empty":             "",
		"relative":          "/realms/homelab/token",
		"malformed":         "https://[::1",
		"wrong host":        "https://attacker.example/realms/homelab/token",
		"wrong scheme":      "http://public.example/realms/homelab/token",
		"sibling prefix":    "https://public.example/realms/homelab-evil/token",
		"encoded separator": "https://public.example/realms/homelab%2ftoken",
		"dot segment":       "https://public.example/realms/homelab/../other",
	}
	for _, field := range []string{"authorization_endpoint", "token_endpoint", "jwks_uri", "userinfo_endpoint"} {
		for name, value := range invalid {
			t.Run(field+"/"+name, func(t *testing.T) {
				metadata := map[string]any{
					"issuer":                                issuer,
					"authorization_endpoint":                valid["authorization_endpoint"],
					"token_endpoint":                        valid["token_endpoint"],
					"jwks_uri":                              valid["jwks_uri"],
					"userinfo_endpoint":                     valid["userinfo_endpoint"],
					"id_token_signing_alg_values_supported": []string{"RS256"},
				}
				metadata[field] = value
				internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "/realms/homelab/.well-known/openid-configuration", r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					require.NoError(t, json.NewEncoder(w).Encode(metadata))
				}))
				t.Cleanup(internal.Close)

				client, err := auth.NewOIDC(context.Background(), config.Config{
					OIDCIssuerURL:      issuer,
					OIDCBackchannelURL: internal.URL + "/realms/homelab",
					OIDCClientID:       "homelab-portal",
					OIDCClientSecret:   "fixture-secret",
					OIDCGroupsClaim:    "groups",
					PortalBaseURL:      "https://portal.example",
				})
				require.Nil(t, client)
				require.EqualError(t, err, "OIDC provider unavailable")
				if value != "" {
					require.NotContains(t, err.Error(), value)
				}
			})
		}
	}
}

func TestNewOIDCReturnsGenericErrorWhenDiscoveryIsUnavailable(t *testing.T) {
	internal := httptest.NewServer(http.NotFoundHandler())
	internalURL := internal.URL
	internal.Close()

	client, err := auth.NewOIDC(context.Background(), config.Config{
		OIDCIssuerURL:      "https://public.example/realms/homelab",
		OIDCBackchannelURL: internalURL + "/realms/homelab",
		OIDCClientID:       "homelab-portal",
		OIDCClientSecret:   "fixture-secret",
		OIDCGroupsClaim:    "groups",
		PortalBaseURL:      "https://portal.example",
	})
	require.Nil(t, client)
	require.EqualError(t, err, "OIDC provider unavailable")
	require.NotContains(t, err.Error(), "public.example")
	require.NotContains(t, err.Error(), internalURL)
	require.NotContains(t, err.Error(), "fixture-secret")
}
