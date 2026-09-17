// Package config loads and validates the portal's process configuration.
package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort                   = 8080
	defaultOIDCClientID           = "homelab-portal"
	defaultOIDCGroupsClaim        = "groups"
	defaultPortalIngressNamespace = "portal"
	defaultPortalIngressName      = "homelab-portal"
	cacheStaleAfter               = 2 * time.Minute
	cacheExpireAfter              = 15 * time.Minute
	sessionAbsoluteTTL            = 4 * time.Hour
	sessionIdleTTL                = 30 * time.Minute
)

// Config contains the portal's validated runtime configuration.
type Config struct {
	Port                   int
	PortalBaseURL          string
	OIDCIssuerURL          string
	OIDCClientID           string
	OIDCGroupsClaim        string
	OIDCClientSecret       string
	SessionCurrentKey      []byte
	SessionPreviousKey     []byte
	PortalIngressNamespace string
	PortalIngressName      string
	CacheStaleAfter        time.Duration
	CacheExpireAfter       time.Duration
	SessionAbsoluteTTL     time.Duration
	SessionIdleTTL         time.Duration
}

// Load parses the supplied environment without consulting process-global state.
func Load(env []string) (Config, error) {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		for i := range entry {
			if entry[i] == '=' {
				values[entry[:i]] = entry[i+1:]
				break
			}
		}
	}

	if values["OIDC_ISSUER_URL"] == "" {
		return Config{}, fmt.Errorf("OIDC_ISSUER_URL is required")
	}
	issuerURL, err := httpsURL(values, "OIDC_ISSUER_URL")
	if err != nil {
		return Config{}, err
	}
	baseURL, err := httpsURL(values, "PORTAL_BASE_URL")
	if err != nil {
		return Config{}, err
	}
	port, err := parsePort(values["PORT"])
	if err != nil {
		return Config{}, err
	}

	clientSecret, err := readSecret(values, "OIDC_CLIENT_SECRET_FILE", true)
	if err != nil {
		return Config{}, err
	}
	currentKey, err := readSecret(values, "SESSION_CURRENT_KEY_FILE", true)
	if err != nil {
		return Config{}, err
	}
	previousKey, err := readSecret(values, "SESSION_PREVIOUS_KEY_FILE", false)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:                   port,
		PortalBaseURL:          baseURL,
		OIDCIssuerURL:          issuerURL,
		OIDCClientID:           stringValue(values, "OIDC_CLIENT_ID", defaultOIDCClientID),
		OIDCGroupsClaim:        stringValue(values, "OIDC_GROUPS_CLAIM", defaultOIDCGroupsClaim),
		OIDCClientSecret:       string(clientSecret),
		SessionCurrentKey:      currentKey,
		SessionPreviousKey:     previousKey,
		PortalIngressNamespace: stringValue(values, "PORTAL_INGRESS_NAMESPACE", defaultPortalIngressNamespace),
		PortalIngressName:      stringValue(values, "PORTAL_INGRESS_NAME", defaultPortalIngressName),
		CacheStaleAfter:        cacheStaleAfter,
		CacheExpireAfter:       cacheExpireAfter,
		SessionAbsoluteTTL:     sessionAbsoluteTTL,
		SessionIdleTTL:         sessionIdleTTL,
	}, nil
}

func httpsURL(values map[string]string, name string) (string, error) {
	raw := values[name]
	if raw == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an absolute HTTPS URL without credentials, query, or fragment", name)
	}
	return raw, nil
}

func stringValue(values map[string]string, name, fallback string) string {
	if values[name] == "" {
		return fallback
	}
	return values[name]
}

func parsePort(raw string) (int, error) {
	if raw == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	return port, nil
}

func readSecret(values map[string]string, name string, required bool) ([]byte, error) {
	path := values[name]
	if path == "" {
		if required {
			return nil, fmt.Errorf("%s is required", name)
		}
		return nil, nil
	}

	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	value = bytes.TrimRight(value, "\r\n")
	if len(value) == 0 {
		return nil, fmt.Errorf("%s must not be empty", name)
	}
	return value, nil
}
