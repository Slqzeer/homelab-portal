package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRejectsMissingRequiredOIDCValues(t *testing.T) {
	for _, name := range []string{"OIDC_ISSUER_URL", "OIDC_BACKCHANNEL_URL"} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(withoutEnv(validEnv(t), name))
			require.ErrorContains(t, err, name)
		})
	}
}

func TestLoadUsesFixedCacheAndSessionDurations(t *testing.T) {
	cfg, err := Load(validEnv(t))
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.CacheStaleAfter)
	assert.Equal(t, 15*time.Minute, cfg.CacheExpireAfter)
	assert.Equal(t, 4*time.Hour, cfg.SessionAbsoluteTTL)
	assert.Equal(t, 30*time.Minute, cfg.SessionIdleTTL)
}

func TestLoadUsesSafeDefaultsAndReadsSecretsFromFiles(t *testing.T) {
	env := withoutEnv(validEnv(t),
		"PORT",
		"OIDC_CLIENT_ID",
		"OIDC_GROUPS_CLAIM",
		"SESSION_PREVIOUS_KEY_FILE",
		"PORTAL_INGRESS_NAMESPACE",
		"PORTAL_INGRESS_NAME",
	)

	cfg, err := Load(env)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "homelab-portal", cfg.OIDCClientID)
	assert.Equal(t, "groups", cfg.OIDCGroupsClaim)
	assert.Equal(t, "portal", cfg.PortalIngressNamespace)
	assert.Equal(t, "homelab-portal", cfg.PortalIngressName)
	assert.Equal(t, "client-secret", cfg.OIDCClientSecret)
	assert.Equal(t, []byte("current-session-key"), cfg.SessionCurrentKey)
	assert.Nil(t, cfg.SessionPreviousKey)
}

func TestLoadRequiresHTTPSBaseAndIssuerURLs(t *testing.T) {
	tests := []struct {
		name      string
		variable  string
		value     string
		errorText string
	}{
		{name: "base URL", variable: "PORTAL_BASE_URL", value: "http://portal.example.test", errorText: "PORTAL_BASE_URL"},
		{name: "issuer URL", variable: "OIDC_ISSUER_URL", value: "http://id.example.test/realms/homelab", errorText: "OIDC_ISSUER_URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(replaceEnv(validEnv(t), tt.variable, tt.value))
			require.ErrorContains(t, err, tt.errorText)
		})
	}

	cfg, err := Load(validEnv(t))
	require.NoError(t, err)
	assert.Equal(t, "https://portal.example.test", cfg.PortalBaseURL)
	assert.Equal(t, "https://id.example.test/realms/homelab", cfg.OIDCIssuerURL)
	assert.Equal(t, "https://id.example.test/realms/homelab", cfg.OIDCBackchannelURL)
}

func TestLoadRejectsInvalidPorts(t *testing.T) {
	for _, port := range []string{"not-a-number", "0", "65536"} {
		t.Run(port, func(t *testing.T) {
			_, err := Load(replaceEnv(validEnv(t), "PORT", port))
			require.ErrorContains(t, err, "PORT")
		})
	}
}

func TestLoadSeparatesPublicAndOperationsPorts(t *testing.T) {
	cfg, err := Load(validEnv(t))
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 8081, cfg.OperationsPort)
	cfg, err = Load(append(validEnv(t), "PORT=9000", "OPERATIONS_PORT=9001"))
	require.NoError(t, err)
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 9001, cfg.OperationsPort)
	for _, value := range []string{"private-invalid-value", "0", "-1", "65536", "8080"} {
		_, err := Load(append(validEnv(t), "OPERATIONS_PORT="+value))
		require.ErrorContains(t, err, "OPERATIONS_PORT")
		assert.NotContains(t, err.Error(), value)
	}
	_, err = Load(append(validEnv(t), "PORT=8081"))
	require.ErrorContains(t, err, "OPERATIONS_PORT")
}

func TestLoadRejectsAbsentCurrentSessionKey(t *testing.T) {
	_, err := Load(withoutEnv(validEnv(t), "SESSION_CURRENT_KEY_FILE"))
	require.ErrorContains(t, err, "SESSION_CURRENT_KEY_FILE")
}

func validEnv(t *testing.T) []string {
	t.Helper()

	secretDir := t.TempDir()
	writeSecret := func(name, value string) string {
		t.Helper()
		path := filepath.Join(secretDir, name)
		require.NoError(t, os.WriteFile(path, []byte(value), 0o600))
		return path
	}

	return []string{
		"PORT=8080",
		"PORTAL_BASE_URL=https://portal.example.test",
		"OIDC_ISSUER_URL=https://id.example.test/realms/homelab",
		"OIDC_BACKCHANNEL_URL=https://id.example.test/realms/homelab",
		"OIDC_CLIENT_ID=homelab-portal",
		"OIDC_GROUPS_CLAIM=groups",
		fmt.Sprintf("OIDC_CLIENT_SECRET_FILE=%s", writeSecret("oidc-client-secret", "client-secret")),
		fmt.Sprintf("SESSION_CURRENT_KEY_FILE=%s", writeSecret("session-current-key", "current-session-key")),
		fmt.Sprintf("SESSION_PREVIOUS_KEY_FILE=%s", writeSecret("session-previous-key", "previous-session-key")),
		"PORTAL_INGRESS_NAMESPACE=portal",
		"PORTAL_INGRESS_NAME=homelab-portal",
	}
}

func withoutEnv(env []string, names ...string) []string {
	omit := make(map[string]struct{}, len(names))
	for _, name := range names {
		omit[name] = struct{}{}
	}

	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		name := entry
		for i := range entry {
			if entry[i] == '=' {
				name = entry[:i]
				break
			}
		}
		if _, found := omit[name]; !found {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func replaceEnv(env []string, name, value string) []string {
	prefix := name + "="
	for index, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func TestLoadValidatesOIDCBackchannelURL(t *testing.T) {
	env := validEnv(t)
	for _, value := range []string{
		"http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab",
		"https://id.example.test/realms/homelab/",
	} {
		t.Run("accepts "+value, func(t *testing.T) {
			cfg, err := Load(replaceEnv(env, "OIDC_BACKCHANNEL_URL", value))
			require.NoError(t, err)
			require.Equal(t, strings.TrimSuffix(value, "/"), cfg.OIDCBackchannelURL)
		})
	}

	for _, value := range []string{
		"ftp://id.example.test/realms/homelab",
		"http://user@id.example.test/realms/homelab",
		"http://id.example.test/realms/homelab?secret=value",
		"http://id.example.test/realms/homelab?",
		"http://id.example.test/realms/homelab#fragment",
		"http://id.example.test/realms/homelab#",
		"http://id.example.test/realms/other",
		"http://id.example.test/realms/homelab/../other",
		"http://id.example.test/realms%2fhomelab",
		"//id.example.test/realms/homelab",
	} {
		t.Run("rejects "+value, func(t *testing.T) {
			_, err := Load(replaceEnv(env, "OIDC_BACKCHANNEL_URL", value))
			require.ErrorContains(t, err, "OIDC_BACKCHANNEL_URL")
			require.NotContains(t, err.Error(), value)
		})
	}
}

func TestLoadNormalizesAndValidatesOIDCIssuerPath(t *testing.T) {
	cfg, err := Load(replaceEnv(validEnv(t), "OIDC_ISSUER_URL", "https://id.example.test/realms/homelab/"))
	require.NoError(t, err)
	require.Equal(t, "https://id.example.test/realms/homelab", cfg.OIDCIssuerURL)

	for _, value := range []string{
		"https://id.example.test/realms%2fhomelab",
		"https://id.example.test/realms/homelab/../other",
		"https://id.example.test/realms/homelab#",
	} {
		t.Run("rejects "+value, func(t *testing.T) {
			_, err := Load(replaceEnv(validEnv(t), "OIDC_ISSUER_URL", value))
			require.ErrorContains(t, err, "OIDC_ISSUER_URL")
			require.NotContains(t, err.Error(), value)
		})
	}
}
