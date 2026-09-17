package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Slqzeer/homelab-portal/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDC struct {
	oauth       oauth2.Config
	verifier    *oidc.IDTokenVerifier
	groupsClaim string
}

// CallbackParams separates untrusted callback query values (Code, State) from
// ExpectedState, Nonce and Verifier recovered from the authenticated transaction.
// The HTTP layer must consume that browser-bound transaction once, with expiry.
type CallbackParams struct {
	Code          string
	State         string
	ExpectedState string
	Nonce         string
	Verifier      string
}

// ErrLoginFailed deliberately excludes upstream errors, codes, and tokens.
var ErrLoginFailed = errors.New("portal login failed")

var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func (o *OIDC) Callback(ctx context.Context, params CallbackParams) (Claims, error) {
	if params.Code == "" || params.ExpectedState == "" || params.Nonce == "" || !pkceVerifier.MatchString(params.Verifier) || subtle.ConstantTimeCompare([]byte(params.State), []byte(params.ExpectedState)) != 1 {
		return Claims{}, ErrLoginFailed
	}
	token, err := o.oauth.Exchange(ctx, params.Code, oauth2.VerifierOption(params.Verifier))
	if err != nil {
		return Claims{}, ErrLoginFailed
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Claims{}, ErrLoginFailed
	}
	idToken, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, ErrLoginFailed
	}
	if params.Nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(params.Nonce)) != 1 {
		return Claims{}, ErrLoginFailed
	}
	var payload map[string]json.RawMessage
	if idToken.Claims(&payload) != nil {
		return Claims{}, ErrLoginFailed
	}
	var values any
	if json.Unmarshal(payload[o.groupsClaim], &values) != nil {
		return Claims{}, ErrLoginFailed
	}
	list, ok := values.([]any)
	if !ok {
		return Claims{}, ErrLoginFailed
	}
	groups := make([]string, 0, len(list))
	for _, value := range list {
		group, ok := value.(string)
		if !ok {
			return Claims{}, ErrLoginFailed
		}
		groups = append(groups, group)
	}
	return Claims{Groups: groups}, nil
}

func NewOIDC(ctx context.Context, cfg config.Config) (*OIDC, error) {
	if cfg.OIDCClientID == "" || cfg.OIDCClientSecret == "" || cfg.OIDCGroupsClaim == "" {
		return nil, errors.New("invalid OIDC configuration")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, errors.New("OIDC provider unavailable")
	}
	endpoint := provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return &OIDC{
		oauth:       oauth2.Config{ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret, RedirectURL: strings.TrimRight(cfg.PortalBaseURL, "/") + "/auth/callback", Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID, "profile", "groups"}},
		verifier:    provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		groupsClaim: cfg.OIDCGroupsClaim,
	}, nil
}

// LoginURL is side-effect free. The route must retain state, nonce, and verifier
// in an authenticated browser-bound transaction until the callback is consumed.
func (o *OIDC) LoginURL(state, nonce, verifier string) string {
	return o.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}
