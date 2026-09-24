package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestRewriteBackchannelURLAcceptsOnlyCanonicalIssuerPrefix(t *testing.T) {
	issuer := mustURL(t, "https://keycloak.taildf6cd4.ts.net/realms/homelab")
	backchannel := mustURL(t, "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab")
	cases := []struct {
		name     string
		target   string
		want     string
		rejected bool
	}{
		{"issuer root", "https://keycloak.taildf6cd4.ts.net/realms/homelab", "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab", false},
		{"discovery", "https://keycloak.taildf6cd4.ts.net/realms/homelab/.well-known/openid-configuration", "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab/.well-known/openid-configuration", false},
		{"query", "https://keycloak.taildf6cd4.ts.net/realms/homelab/token?mode=code", "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab/token?mode=code", false},
		{"sibling prefix", "https://keycloak.taildf6cd4.ts.net/realms/homelab-evil/token", "", true},
		{"wrong host", "https://attacker.example/realms/homelab/token", "", true},
		{"wrong scheme", "http://keycloak.taildf6cd4.ts.net/realms/homelab/token", "", true},
		{"encoded separator", "https://keycloak.taildf6cd4.ts.net/realms/homelab%2ftoken", "", true},
		{"dot segment", "https://keycloak.taildf6cd4.ts.net/realms/homelab/../other", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteBackchannelURL(mustURL(t, tc.target), issuer, backchannel)
			if tc.rejected {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
		})
	}
}

func TestBackchannelTransportClonesRequestAndUsesInternalAuthority(t *testing.T) {
	issuer := mustURL(t, "https://public.example/realms/homelab")
	backchannel := mustURL(t, "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab")
	var sent *http.Request
	transport := &backchannelTransport{
		issuer: issuer, backchannel: backchannel,
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			sent = req
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
		}),
	}
	req, err := http.NewRequest(http.MethodPost, "https://public.example/realms/homelab/token?mode=code&mode=refresh", strings.NewReader("payload"))
	require.NoError(t, err)
	req.Host = "public.example"
	req.Header.Set("Authorization", "private")
	originalURL := req.URL
	originalURLText := req.URL.String()

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, sent)
	require.NotSame(t, req, sent)
	require.NotSame(t, req.URL, sent.URL)
	require.Equal(t, "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab/token?mode=code&mode=refresh", sent.URL.String())
	require.Empty(t, sent.Host)
	require.Equal(t, []string{"code", "refresh"}, sent.URL.Query()["mode"])
	require.Equal(t, "private", sent.Header.Get("Authorization"))
	require.Same(t, originalURL, req.URL)
	require.Equal(t, originalURLText, req.URL.String())
	require.Equal(t, "public.example", req.Host)
}

func TestBackchannelTransportReturnsGenericRejection(t *testing.T) {
	transport := &backchannelTransport{
		issuer:      mustURL(t, "https://public.example/realms/homelab"),
		backchannel: mustURL(t, "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab"),
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("rejected request reached base transport")
			return nil, nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://attacker.example/realms/homelab/token", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.EqualError(t, err, "OIDC backchannel request rejected")
	require.NotContains(t, err.Error(), "attacker")
	require.NotContains(t, err.Error(), "keycloak")
}

func TestNewBackchannelClientRejectsInvalidRootsWithoutEchoingValues(t *testing.T) {
	cases := []struct {
		name, issuer, backchannel string
		timeout                   time.Duration
	}{
		{"issuer scheme", "http://public.example/realms/homelab", "http://internal.example/realms/homelab", time.Second},
		{"backchannel scheme", "https://public.example/realms/homelab", "ftp://internal.example/realms/homelab", time.Second},
		{"different path", "https://public.example/realms/homelab", "http://internal.example/realms/other", time.Second},
		{"encoded path", "https://public.example/realms%2fhomelab", "http://internal.example/realms/homelab", time.Second},
		{"dot path", "https://public.example/realms/homelab/../other", "http://internal.example/realms/other", time.Second},
		{"query", "https://public.example/realms/homelab", "http://internal.example/realms/homelab?secret=value", time.Second},
		{"zero timeout", "https://public.example/realms/homelab", "http://internal.example/realms/homelab", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := newBackchannelClient(tc.issuer, tc.backchannel, tc.timeout)
			require.Nil(t, client)
			require.EqualError(t, err, "invalid OIDC backchannel configuration")
			require.NotContains(t, err.Error(), tc.issuer)
			require.NotContains(t, err.Error(), tc.backchannel)
		})
	}
}

func TestBackchannelClientPropagatesContextCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer internal.Close()
	client, err := newBackchannelClient("https://public.example/realms/homelab", internal.URL+"/realms/homelab", time.Second)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://public.example/realms/homelab/keys", nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, requestErr := client.Do(req)
		done <- requestErr
	}()
	<-requestStarted
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestBackchannelClientHasFiniteTimeout(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer internal.Close()
	client, err := newBackchannelClient("https://public.example/realms/homelab", internal.URL+"/realms/homelab", 20*time.Millisecond)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, "https://public.example/realms/homelab/keys", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestBackchannelClientRejectsRedirectWithoutForwardingCredentials(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetRequests.Add(1) }))
	defer target.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer internal.Close()
	client, err := newBackchannelClient("https://public.example/realms/homelab", internal.URL+"/realms/homelab", time.Second)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://public.example/realms/homelab/token", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "private")

	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)
	require.Zero(t, targetRequests.Load())
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestBackchannelClientDoesNotUseConfiguredDefaultProxy(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	var proxyCalls atomic.Int32
	configured := originalTransport.(*http.Transport).Clone()
	configured.Proxy = func(*http.Request) (*url.URL, error) {
		proxyCalls.Add(1)
		return nil, errors.New("proxy must not be used")
	}
	http.DefaultTransport = configured

	var internalRequests atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer internal.Close()

	client, err := newBackchannelClient("https://public.example/realms/homelab", internal.URL+"/realms/homelab", time.Second)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, "https://public.example/realms/homelab/keys", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 1, internalRequests.Load())
	require.Zero(t, proxyCalls.Load())
}

func TestNewBackchannelClientRejectsNonHTTPDefaultTransportWithoutPanicking(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected request")
	})

	var client *http.Client
	var err error
	require.NotPanics(t, func() {
		client, err = newBackchannelClient(
			"https://public.example/realms/homelab",
			"http://internal.example/realms/homelab",
			time.Second,
		)
	})
	require.Nil(t, client)
	require.EqualError(t, err, "invalid OIDC backchannel configuration")
}
