// Package e2e defines the fail-closed acceptance configuration contract.
package e2e

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type Config map[string]string

func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv("ACCEPT_DISPOSABLE") != "isolated-test-cluster" {
		return nil, fmt.Errorf("ACCEPT_DISPOSABLE must authorize an isolated test cluster")
	}
	if getenv("ACCEPT_TEST_IDENTITIES") != "disposable-only" {
		return nil, fmt.Errorf("ACCEPT_TEST_IDENTITIES must attest disposable identities")
	}
	c := Config{}
	keys := strings.Fields(`KUBECONFIG ACCEPT_CONTEXT ACCEPT_CLUSTER_UID ACCEPT_RUN_ID ACCEPT_PORTAL_URL ACCEPT_STALE_URL
		ACCEPT_PORTAL_NAMESPACE ACCEPT_FIXTURE_NAMESPACE ACCEPT_PROBE_NAMESPACE ACCEPT_PORTAL_INGRESS ACCEPT_STALE_INGRESS
		ACCEPT_PORTAL_POD ACCEPT_STALE_POD ACCEPT_SERVICE_ACCOUNT ACCEPT_FIXTURE_SERVICE ACCEPT_ARGO_NAMESPACE
		ACCEPT_ARGO_APPLICATION ACCEPT_ARGO_REVISION ACCEPT_VSO_NAME ACCEPT_PROXY_NAMESPACE ACCEPT_IMAGE ACCEPT_PROBE_IMAGE
		ACCEPT_GROUP ACCEPT_STALE_ITEM_NAME ACCEPT_STALE_ITEM_URL ACCEPT_MEMBER_COOKIE ACCEPT_NONMEMBER_COOKIE ACCEPT_ADMIN_COOKIE`)
	for _, key := range keys {
		value := getenv(key)
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("invalid or missing %s", key)
		}
		c[key] = value
	}
	for _, key := range strings.Fields(`ACCEPT_CONTEXT ACCEPT_CLUSTER_UID ACCEPT_PORTAL_NAMESPACE ACCEPT_FIXTURE_NAMESPACE
		ACCEPT_PROBE_NAMESPACE ACCEPT_PORTAL_INGRESS ACCEPT_STALE_INGRESS ACCEPT_PORTAL_POD ACCEPT_STALE_POD ACCEPT_SERVICE_ACCOUNT
		ACCEPT_FIXTURE_SERVICE ACCEPT_ARGO_NAMESPACE ACCEPT_ARGO_APPLICATION ACCEPT_VSO_NAME ACCEPT_PROXY_NAMESPACE`) {
		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,252}$`).MatchString(c[key]) {
			return nil, fmt.Errorf("invalid %s", key)
		}
	}
	if !regexp.MustCompile(`^accept-[a-z0-9-]{8,35}$`).MatchString(c["ACCEPT_RUN_ID"]) {
		return nil, fmt.Errorf("invalid ACCEPT_RUN_ID")
	}
	for _, key := range []string{"ACCEPT_PORTAL_URL", "ACCEPT_STALE_URL", "ACCEPT_STALE_ITEM_URL"} {
		u, err := url.Parse(c[key])
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".ts.net") || u.Host != u.Hostname() || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return nil, fmt.Errorf("invalid %s: expected a Tailnet HTTPS origin", key)
		}
	}
	if c["ACCEPT_PORTAL_URL"] == c["ACCEPT_STALE_URL"] || c["ACCEPT_PORTAL_POD"] == c["ACCEPT_STALE_POD"] || c["ACCEPT_PORTAL_INGRESS"] == c["ACCEPT_STALE_INGRESS"] {
		return nil, fmt.Errorf("healthy and stale fixtures must be distinct")
	}
	if c["ACCEPT_FIXTURE_NAMESPACE"] == c["ACCEPT_PORTAL_NAMESPACE"] || c["ACCEPT_FIXTURE_NAMESPACE"] == c["ACCEPT_PROBE_NAMESPACE"] {
		return nil, fmt.Errorf("fixture namespace must be isolated")
	}
	for _, key := range []string{"ACCEPT_IMAGE", "ACCEPT_PROBE_IMAGE"} {
		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./:_-]*@sha256:[a-f0-9]{64}$`).MatchString(c[key]) {
			return nil, fmt.Errorf("invalid %s: digest required", key)
		}
	}
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(c["ACCEPT_ARGO_REVISION"]) {
		return nil, fmt.Errorf("invalid ACCEPT_ARGO_REVISION")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{2,50}$`).MatchString(c["ACCEPT_GROUP"]) {
		return nil, fmt.Errorf("invalid ACCEPT_GROUP")
	}
	seen := map[string]bool{}
	for _, key := range []string{"ACCEPT_MEMBER_COOKIE", "ACCEPT_NONMEMBER_COOKIE", "ACCEPT_ADMIN_COOKIE"} {
		if !regexp.MustCompile(`^[A-Za-z0-9_.=-]+$`).MatchString(c[key]) || seen[c[key]] {
			return nil, fmt.Errorf("invalid or reused test cookie: %s", key)
		}
		seen[c[key]] = true
	}
	return c, nil
}
