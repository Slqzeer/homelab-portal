package e2e_test

import (
	"strings"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
)

func TestAcceptanceRequiresExplicitDisposableInputs(t *testing.T) {
	_, err := e2e.LoadConfig(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "ACCEPT_DISPOSABLE") {
		t.Fatal("missing isolation authorization must fail before cluster access")
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		"ACCEPT_DISPOSABLE": "isolated-test-cluster", "ACCEPT_TEST_IDENTITIES": "disposable-only",
		"ACCEPT_RUN_ID": "accept-20260917-abcdef12", "ACCEPT_CONTEXT": "acceptance", "KUBECONFIG": "isolated.config",
		"ACCEPT_CLUSTER_UID": "11111111-1111-1111-1111-111111111111",
		"ACCEPT_PORTAL_URL":  "https://portal.test.ts.net", "ACCEPT_STALE_URL": "https://stale.test.ts.net",
		"ACCEPT_PORTAL_NAMESPACE": "portal", "ACCEPT_FIXTURE_NAMESPACE": "accept-fixtures", "ACCEPT_PROBE_NAMESPACE": "monitoring",
		"ACCEPT_PORTAL_INGRESS": "homelab-portal", "ACCEPT_STALE_INGRESS": "stale-portal", "ACCEPT_PORTAL_POD": "portal-abc",
		"ACCEPT_STALE_POD": "stale-abc", "ACCEPT_SERVICE_ACCOUNT": "homelab-portal", "ACCEPT_FIXTURE_SERVICE": "echo",
		"ACCEPT_ARGO_NAMESPACE": "argocd", "ACCEPT_ARGO_APPLICATION": "portal", "ACCEPT_ARGO_REVISION": strings.Repeat("a", 40),
		"ACCEPT_VSO_NAME": "homelab-portal", "ACCEPT_PROXY_NAMESPACE": "tailscale",
		"ACCEPT_IMAGE":       "ghcr.io/example/portal@sha256:" + strings.Repeat("a", 64),
		"ACCEPT_PROBE_IMAGE": "registry.example/curl@sha256:" + strings.Repeat("b", 64),
		"ACCEPT_GROUP":       "accept-members", "ACCEPT_STALE_ITEM_NAME": "stale-sentinel",
		"ACCEPT_STALE_ITEM_URL": "https://sentinel.test.ts.net",
		"ACCEPT_MEMBER_COOKIE":  "member-secret", "ACCEPT_NONMEMBER_COOKIE": "other-secret", "ACCEPT_ADMIN_COOKIE": "admin-secret",
	}
}

func TestAcceptanceConfigurationRejectsUnsafeInputsWithoutEchoingThem(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing credential", "ACCEPT_ADMIN_COOKIE", ""},
		{"production identities", "ACCEPT_TEST_IDENTITIES", "production"},
		{"URL credentials", "ACCEPT_PORTAL_URL", "https://secret:password@portal.test.ts.net"},
		{"URL query", "ACCEPT_PORTAL_URL", "https://portal.test.ts.net/?token=secret"},
		{"non Tailnet URL", "ACCEPT_PORTAL_URL", "https://example.com"},
		{"unisolated namespace", "ACCEPT_FIXTURE_NAMESPACE", "portal"},
		{"option injection", "ACCEPT_CONTEXT", "--token=secret"},
		{"header injection", "ACCEPT_MEMBER_COOKIE", "secret\r\nX: injected"},
		{"reused identity", "ACCEPT_NONMEMBER_COOKIE", "member-secret"},
		{"mutable image", "ACCEPT_IMAGE", "registry/portal:latest"},
		{"missing run ID", "ACCEPT_RUN_ID", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnvironment()
			env[tc.key] = tc.value
			_, err := e2e.LoadConfig(func(k string) string { return env[k] })
			if err == nil {
				t.Fatal("unsafe acceptance configuration was accepted")
			}
			if tc.value != "" && strings.Contains(err.Error(), tc.value) {
				t.Fatal("configuration error disclosed input")
			}
		})
	}
	if _, err := e2e.LoadConfig(func(k string) string { return validEnvironment()[k] }); err != nil {
		t.Fatal(err)
	}
}
