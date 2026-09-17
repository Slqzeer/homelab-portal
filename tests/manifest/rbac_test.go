package manifest_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestPortalIdentityCanOnlyReadIngressesAcrossNamespaces(t *testing.T) {
	objects := render(t)
	sa := decode[corev1.ServiceAccount](t, objectOf(t, objects, "ServiceAccount", "homelab-portal"))
	require.Equal(t, "portal", sa.Namespace)
	role := decode[rbacv1.ClusterRole](t, objectOf(t, objects, "ClusterRole", "homelab-portal"))
	require.Nil(t, role.AggregationRule, "aggregation could silently add permissions")
	require.Len(t, role.Rules, 1)
	rule := role.Rules[0]
	require.Equal(t, []string{"networking.k8s.io"}, rule.APIGroups)
	require.Equal(t, []string{"ingresses"}, rule.Resources)
	require.ElementsMatch(t, []string{"get", "list", "watch"}, rule.Verbs)
	require.Empty(t, rule.ResourceNames, "discovery must cover all namespaces")
	require.Empty(t, rule.NonResourceURLs)
	binding := decode[rbacv1.ClusterRoleBinding](t, objectOf(t, objects, "ClusterRoleBinding", "homelab-portal"))
	require.Equal(t, rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: role.Name}, binding.RoleRef)
	require.Equal(t, []rbacv1.Subject{{Kind: "ServiceAccount", Name: sa.Name, Namespace: sa.Namespace}}, binding.Subjects)

	// Exercise explicit forbidden requests, as well as the exact allowlist above.
	allows := func(group, resource, verb string) bool {
		matches := func(values []string, value string) bool {
			return slices.Contains(values, value) || slices.Contains(values, "*")
		}
		for _, rule := range role.Rules {
			if matches(rule.APIGroups, group) && matches(rule.Resources, resource) && matches(rule.Verbs, verb) {
				return true
			}
		}
		return false
	}
	for _, verb := range []string{"get", "list", "watch"} {
		require.True(t, allows("networking.k8s.io", "ingresses", verb))
		require.False(t, allows("", "secrets", verb), "Secret %s must be denied", verb)
	}
	for _, verb := range []string{"create", "update", "patch", "delete", "deletecollection"} {
		require.False(t, allows("networking.k8s.io", "ingresses", verb), "Ingress %s must be denied", verb)
	}
	counts := map[string]int{}
	for _, object := range objects {
		counts[object.GetKind()]++
	}
	require.Equal(t, 1, counts["ServiceAccount"])
	require.Equal(t, 1, counts["ClusterRole"])
	require.Equal(t, 1, counts["ClusterRoleBinding"])
	require.Zero(t, counts["Role"])
	require.Zero(t, counts["RoleBinding"])
}
