package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/require"
)

func TestLoginURLUsesAuthorizationCodePKCEAndCorrelatedStateNonce(t *testing.T) {
	var issuer string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer provider.Close()
	issuer = provider.URL
	client, err := auth.NewOIDC(oidc.ClientContext(context.Background(), provider.Client()), config.Config{OIDCIssuerURL: issuer, OIDCClientID: "homelab-portal", OIDCClientSecret: "fixture-secret", OIDCGroupsClaim: "groups", PortalBaseURL: "https://portal.example"})
	require.NoError(t, err)
	// RFC 7636 Appendix B gives an independent S256 verifier/challenge vector.
	login, err := url.Parse(client.LoginURL("state-value", "nonce-value", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	require.NoError(t, err)
	q := login.Query()
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
