package auth

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var (
	errBackchannelURLRejected     = errors.New("OIDC backchannel URL rejected")
	errBackchannelRequestRejected = errors.New("OIDC backchannel request rejected")
	errBackchannelRequestFailed   = errors.New("OIDC backchannel request failed")
	errBackchannelConfig          = errors.New("invalid OIDC backchannel configuration")
)

type backchannelTransport struct {
	issuer, backchannel *url.URL
	base                http.RoundTripper
}

func rewriteBackchannelURL(target, issuer, backchannel *url.URL) (*url.URL, error) {
	if target == nil || issuer == nil || backchannel == nil ||
		target.Scheme != issuer.Scheme || target.Host != issuer.Host ||
		target.User != nil || target.Opaque != "" || target.Fragment != "" ||
		target.RawPath != "" || path.Clean(target.Path) != target.Path ||
		(target.Path != issuer.Path && !strings.HasPrefix(target.Path, issuer.Path+"/")) {
		return nil, errBackchannelURLRejected
	}

	suffix := strings.TrimPrefix(target.Path, issuer.Path)
	rewritten := *target
	rewritten.Scheme = backchannel.Scheme
	rewritten.Host = backchannel.Host
	rewritten.User = nil
	rewritten.Path = backchannel.Path + suffix
	rewritten.RawPath = ""
	return &rewritten, nil
}

func (t *backchannelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errBackchannelRequestRejected
	}
	rewritten, err := rewriteBackchannelURL(req.URL, t.issuer, t.backchannel)
	if err != nil {
		return nil, errBackchannelRequestRejected
	}
	clone := req.Clone(req.Context())
	clone.URL = rewritten
	clone.Host = ""
	resp, err := t.base.RoundTrip(clone)
	if err == nil {
		return resp, nil
	}
	if contextErr := req.Context().Err(); contextErr != nil {
		return nil, errors.Join(errBackchannelRequestFailed, contextErr)
	}
	return nil, errBackchannelRequestFailed
}

func newBackchannelClient(issuerRaw, backchannelRaw string, timeout time.Duration) (*http.Client, error) {
	issuer, err := parseBackchannelRoot(issuerRaw, "https")
	if err != nil {
		return nil, errBackchannelConfig
	}
	backchannel, err := parseBackchannelRoot(backchannelRaw, "http", "https")
	if err != nil || issuer.Path != backchannel.Path || timeout <= 0 {
		return nil, errBackchannelConfig
	}
	transport := &backchannelTransport{
		issuer:      issuer,
		backchannel: backchannel,
		base:        http.DefaultTransport.(*http.Transport).Clone(),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OIDC redirect rejected")
		},
	}, nil
}

func parseBackchannelRoot(raw string, schemes ...string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	allowed := false
	if err == nil && parsed != nil {
		for _, scheme := range schemes {
			allowed = allowed || parsed.Scheme == scheme
		}
	}
	if !allowed || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.RawPath != "" ||
		parsed.Path == "" || parsed.Path == "/" || path.Clean(parsed.Path) != parsed.Path {
		return nil, errBackchannelConfig
	}
	return parsed, nil
}
