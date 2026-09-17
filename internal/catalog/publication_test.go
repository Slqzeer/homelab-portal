package catalog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestParseIngressBuildsCatalogItemFromValidPublication(t *testing.T) {
	ing := publishedIngress("tools", "grafana", "public")

	candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

	require.Nil(t, diagnostic)
	require.NotNil(t, candidate.Item)
	assert.Equal(t, CatalogItem{
		ID:          "tools/grafana",
		Namespace:   "tools",
		Ingress:     "grafana",
		Name:        "Grafana",
		Description: "Metrics dashboards",
		Category:    "Observability",
		Icon:        "grafana",
		Access:      AccessPublic,
		Order:       20,
		TargetURL:   "https://grafana.tailnet.ts.net",
	}, *candidate.Item)
}

func TestParseIngressTrimsHumanLabelsAndAppliesDefaults(t *testing.T) {
	ing := publishedIngress("tools", "grafana", "public")
	ing.Annotations["portal.homelab.io/name"] = "  Grafana  "
	ing.Annotations["portal.homelab.io/description"] = "  Metrics dashboards  "
	ing.Annotations["portal.homelab.io/category"] = "   "
	ing.Annotations["portal.homelab.io/icon"] = "https://attacker.invalid/icon.svg"
	delete(ing.Annotations, "portal.homelab.io/order")

	candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

	require.Nil(t, diagnostic)
	require.NotNil(t, candidate.Item)
	assert.Equal(t, "Grafana", candidate.Item.Name)
	assert.Equal(t, "Metrics dashboards", candidate.Item.Description)
	assert.Equal(t, "Autres", candidate.Item.Category)
	assert.Equal(t, "generic", candidate.Item.Icon)
	assert.Equal(t, 1000, candidate.Item.Order)
}

func TestParseIngressValidatesHumanLabelBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		value      string
		rule       string
	}{
		{name: "missing name", annotation: "portal.homelab.io/name", value: "", rule: "name"},
		{name: "blank name", annotation: "portal.homelab.io/name", value: "   ", rule: "name"},
		{name: "name over 80 characters", annotation: "portal.homelab.io/name", value: strings.Repeat("é", 81), rule: "name"},
		{name: "description over 240 characters", annotation: "portal.homelab.io/description", value: strings.Repeat("d", 241), rule: "description"},
		{name: "category over 40 characters", annotation: "portal.homelab.io/category", value: strings.Repeat("c", 41), rule: "category"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", "public")
			ing.Annotations[tt.annotation] = tt.value

			candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

			assert.Nil(t, candidate.Item)
			require.NotNil(t, diagnostic)
			assert.Equal(t, "tools", diagnostic.Namespace)
			assert.Equal(t, "grafana", diagnostic.Ingress)
			assert.Equal(t, tt.rule, diagnostic.Rule)
			assert.NotEmpty(t, diagnostic.Remediation)
			if tt.value != "" {
				assert.NotContains(t, diagnostic.Remediation, tt.value)
			}
		})
	}
}

func TestParseIngressAcceptsLabelLimitsAndKnownIcon(t *testing.T) {
	ing := publishedIngress("tools", "grafana", "public")
	ing.Annotations["portal.homelab.io/name"] = strings.Repeat("é", 80)
	ing.Annotations["portal.homelab.io/description"] = strings.Repeat("d", 240)
	ing.Annotations["portal.homelab.io/category"] = strings.Repeat("c", 40)
	ing.Annotations["portal.homelab.io/icon"] = "vault"

	candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

	require.Nil(t, diagnostic)
	require.NotNil(t, candidate.Item)
	assert.Equal(t, "vault", candidate.Item.Icon)
}

func TestParseIngressValidatesAccessAndGroups(t *testing.T) {
	tests := []struct {
		name   string
		access string
		groups *string
		rule   string
	}{
		{name: "missing access", access: "", rule: "access"},
		{name: "unknown access", access: "members", rule: "access"},
		{name: "access is case sensitive", access: "Public", rule: "access"},
		{name: "groups access requires annotation", access: "groups", rule: "groups"},
		{name: "groups access rejects empty entry", access: "groups", groups: stringPointer("admins,,users"), rule: "groups"},
		{name: "groups access rejects blank entry", access: "groups", groups: stringPointer("admins,  ,users"), rule: "groups"},
		{name: "groups access rejects exact duplicate", access: "groups", groups: stringPointer("admins, admins"), rule: "groups"},
		{name: "public forbids groups", access: "public", groups: stringPointer("users"), rule: "groups"},
		{name: "authenticated forbids groups", access: "authenticated", groups: stringPointer("users"), rule: "groups"},
		{name: "admin forbids groups", access: "admin", groups: stringPointer("portal-admin"), rule: "groups"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", tt.access)
			if tt.groups != nil {
				ing.Annotations["portal.homelab.io/groups"] = *tt.groups
			}

			candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

			assert.Nil(t, candidate.Item)
			require.NotNil(t, diagnostic)
			assert.Equal(t, tt.rule, diagnostic.Rule)
		})
	}
}

func TestParseIngressParsesTrimmedCaseSensitiveGroups(t *testing.T) {
	ing := publishedIngress("tools", "grafana", "groups")
	ing.Annotations["portal.homelab.io/groups"] = " admins, Homelab Users,Admins "

	candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

	require.Nil(t, diagnostic)
	require.NotNil(t, candidate.Item)
	assert.Equal(t, []string{"admins", "Homelab Users", "Admins"}, candidate.Item.Groups)
}

func TestParseIngressAcceptsEveryNonGroupAccess(t *testing.T) {
	for _, access := range []Access{AccessPublic, AccessAuthenticated, AccessAdmin} {
		t.Run(string(access), func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", string(access))

			candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

			require.Nil(t, diagnostic)
			require.NotNil(t, candidate.Item)
			assert.Equal(t, access, candidate.Item.Access)
			assert.Empty(t, candidate.Item.Groups)
		})
	}
}

func TestParseIngressValidatesOrder(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
		want  int
	}{
		{name: "minimum", value: "0", valid: true, want: 0},
		{name: "maximum", value: "9999", valid: true, want: 9999},
		{name: "negative", value: "-1"},
		{name: "over maximum", value: "10000"},
		{name: "not an integer", value: "first"},
		{name: "whitespace is not accepted", value: " 20 "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", "public")
			ing.Annotations["portal.homelab.io/order"] = tt.value

			candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

			if tt.valid {
				require.Nil(t, diagnostic)
				require.NotNil(t, candidate.Item)
				assert.Equal(t, tt.want, candidate.Item.Order)
				return
			}
			assert.Nil(t, candidate.Item)
			require.NotNil(t, diagnostic)
			assert.Equal(t, "order", diagnostic.Rule)
		})
	}
}

func TestParseIngressRequiresExactlyOneValidatedHostname(t *testing.T) {
	tests := []struct {
		name    string
		entries []networkingv1.IngressLoadBalancerIngress
	}{
		{name: "no status entries"},
		{name: "empty hostname", entries: []networkingv1.IngressLoadBalancerIngress{{}}},
		{name: "two hostnames", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "a.ts.net"}, {Hostname: "b.ts.net"}}},
		{name: "IP status", entries: []networkingv1.IngressLoadBalancerIngress{{IP: "100.64.0.1"}}},
		{name: "hostname value is an IP", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "100.64.0.1"}}},
		{name: "abbreviated IPv4 hostname", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "127.1"}}},
		{name: "single-number IPv4 hostname", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "2130706433"}}},
		{name: "hexadecimal IPv4 hostname", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "0x7f000001"}}},
		{name: "octal IPv4 hostname", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "0177.0.0.1"}}},
		{name: "hostname and IP in one entry", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "grafana.ts.net", IP: "100.64.0.1"}}},
		{name: "hostname plus IP entry", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "grafana.ts.net"}, {IP: "100.64.0.1"}}},
		{name: "URL instead of hostname", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "https://grafana.ts.net/path"}}},
		{name: "hostname with port", entries: []networkingv1.IngressLoadBalancerIngress{{Hostname: "grafana.ts.net:8443"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", "public")
			ing.Status.LoadBalancer.Ingress = tt.entries

			candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

			assert.Nil(t, candidate.Item)
			require.NotNil(t, diagnostic)
			assert.Equal(t, "exactly_one_hostname", diagnostic.Rule)
		})
	}
}

func TestParseIngressDerivesTargetOnlyFromStatusHostname(t *testing.T) {
	ing := publishedIngress("tools", "grafana", "public")
	ing.Spec.Rules = []networkingv1.IngressRule{{Host: "attacker.invalid"}}
	ing.Annotations["portal.homelab.io/target"] = "http://attacker.invalid/private"

	candidate, diagnostic := ParseIngress(ing, types.NamespacedName{})

	require.Nil(t, diagnostic)
	require.NotNil(t, candidate.Item)
	assert.Equal(t, "https://grafana.tailnet.ts.net", candidate.Item.TargetURL)
}

func stringPointer(value string) *string {
	return &value
}

func TestParseIngressIgnoresIneligibleIngresses(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*networkingv1.Ingress)
		portalIngress types.NamespacedName
	}{
		{
			name: "missing class",
			mutate: func(ing *networkingv1.Ingress) {
				ing.Spec.IngressClassName = nil
			},
		},
		{
			name: "different case class",
			mutate: func(ing *networkingv1.Ingress) {
				className := "Tailscale"
				ing.Spec.IngressClassName = &className
			},
		},
		{
			name: "legacy class annotation only",
			mutate: func(ing *networkingv1.Ingress) {
				ing.Spec.IngressClassName = nil
				ing.Annotations["kubernetes.io/ingress.class"] = "tailscale"
			},
		},
		{
			name: "missing enabled annotation",
			mutate: func(ing *networkingv1.Ingress) {
				delete(ing.Annotations, "portal.homelab.io/enabled")
			},
		},
		{
			name: "enabled value is not exact",
			mutate: func(ing *networkingv1.Ingress) {
				ing.Annotations["portal.homelab.io/enabled"] = "TRUE"
			},
		},
		{
			name: "portal self ingress",
			portalIngress: types.NamespacedName{
				Namespace: "tools",
				Name:      "grafana",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := publishedIngress("tools", "grafana", "public")
			if tt.mutate != nil {
				tt.mutate(&ing)
			}

			candidate, diagnostic := ParseIngress(ing, tt.portalIngress)

			assert.Nil(t, candidate.Item)
			assert.Nil(t, diagnostic)
		})
	}
}

func publishedIngress(namespace, name, access string) networkingv1.Ingress {
	className := "tailscale"

	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				"portal.homelab.io/enabled":     "true",
				"portal.homelab.io/name":        "Grafana",
				"portal.homelab.io/description": "Metrics dashboards",
				"portal.homelab.io/category":    "Observability",
				"portal.homelab.io/icon":        "grafana",
				"portal.homelab.io/access":      access,
				"portal.homelab.io/order":       "20",
			},
		},
		Spec: networkingv1.IngressSpec{IngressClassName: &className},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{
					Hostname: "grafana.tailnet.ts.net",
				}},
			},
		},
	}
}
