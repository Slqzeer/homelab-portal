package catalog

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

const annotationPrefix = "portal.homelab.io/"

var localIcons = map[string]struct{}{
	"argocd":     {},
	"generic":    {},
	"grafana":    {},
	"keycloak":   {},
	"kubernetes": {},
	"prometheus": {},
	"tailscale":  {},
	"vault":      {},
}

// ParseIngress derives an allowlisted catalog item from publication metadata.
func ParseIngress(ing networkingv1.Ingress, portalIngress types.NamespacedName) (Candidate, *Diagnostic) {
	annotations := ing.GetAnnotations()
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "tailscale" ||
		annotations[annotationPrefix+"enabled"] != "true" ||
		(portalIngress.Namespace == ing.Namespace && portalIngress.Name == ing.Name) {
		return Candidate{}, nil
	}

	name := strings.TrimSpace(annotations[annotationPrefix+"name"])
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 80 {
		return invalid(ing, "name", "set a non-empty name of at most 80 characters")
	}
	description := strings.TrimSpace(annotations[annotationPrefix+"description"])
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 240 {
		return invalid(ing, "description", "use a description of at most 240 characters")
	}
	category := strings.TrimSpace(annotations[annotationPrefix+"category"])
	if !utf8.ValidString(category) || utf8.RuneCountInString(category) > 40 {
		return invalid(ing, "category", "use a category of at most 40 characters")
	}
	if category == "" {
		category = "Autres"
	}
	icon := annotations[annotationPrefix+"icon"]
	if _, known := localIcons[icon]; !known {
		icon = "generic"
	}
	access := Access(annotations[annotationPrefix+"access"])
	switch access {
	case AccessPublic, AccessAuthenticated, AccessGroups, AccessAdmin:
	default:
		return invalid(ing, "access", "set access to public, authenticated, groups, or admin")
	}
	groups, hasGroups := annotations[annotationPrefix+"groups"]
	parsedGroups := []string(nil)
	if access != AccessGroups && hasGroups {
		return invalid(ing, "groups", "remove groups unless access is groups")
	}
	if access == AccessGroups {
		if !hasGroups || !utf8.ValidString(groups) {
			return invalid(ing, "groups", "set a non-empty CSV of unique, exact group names")
		}
		seen := make(map[string]struct{})
		for _, group := range strings.Split(groups, ",") {
			group = strings.TrimSpace(group)
			if group == "" {
				return invalid(ing, "groups", "set a non-empty CSV of unique, exact group names")
			}
			if _, duplicate := seen[group]; duplicate {
				return invalid(ing, "groups", "set a non-empty CSV of unique, exact group names")
			}
			seen[group] = struct{}{}
			parsedGroups = append(parsedGroups, group)
		}
	}
	order := 1000
	if rawOrder, ok := annotations[annotationPrefix+"order"]; ok {
		var err error
		order, err = strconv.Atoi(rawOrder)
		if err != nil || order < 0 || order > 9999 {
			return invalid(ing, "order", "set order to an integer from 0 through 9999")
		}
	}
	loadBalancers := ing.Status.LoadBalancer.Ingress
	if len(loadBalancers) != 1 || loadBalancers[0].IP != "" {
		return invalid(ing, "exactly_one_hostname", "wait for exactly one LoadBalancer hostname and remove IP or extra entries")
	}
	hostname := loadBalancers[0].Hostname
	if hostname == "" || net.ParseIP(hostname) != nil || hasIPv4NumberSyntax(hostname) || len(validation.IsDNS1123Subdomain(hostname)) != 0 {
		return invalid(ing, "exactly_one_hostname", "wait for exactly one valid DNS LoadBalancer hostname")
	}
	target := (&url.URL{
		Scheme: "https",
		Host:   hostname,
	}).String()
	item := CatalogItem{
		ID:          ing.Namespace + "/" + ing.Name,
		Namespace:   ing.Namespace,
		Ingress:     ing.Name,
		Name:        name,
		Description: description,
		Category:    category,
		Icon:        icon,
		Access:      access,
		Groups:      parsedGroups,
		Order:       order,
		TargetURL:   target,
	}

	return Candidate{Item: &item}, nil
}

// hasIPv4NumberSyntax rejects the alternate decimal, octal, hexadecimal, and
// abbreviated forms that browsers can interpret as IPv4 addresses. Tailscale
// DNS hostnames contain non-numeric labels, so conservative rejection is safe.
func hasIPv4NumberSyntax(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) > 4 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}
		if strings.HasPrefix(part, "0x") {
			if len(part) == 2 || !allASCIIHexDigits(part[2:]) {
				return false
			}
			continue
		}
		if !allASCIIDecimalDigits(part) {
			return false
		}
	}

	return true
}

func allASCIIDecimalDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func allASCIIHexDigits(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func invalid(ing networkingv1.Ingress, rule, remediation string) (Candidate, *Diagnostic) {
	return Candidate{}, &Diagnostic{
		Namespace:   ing.Namespace,
		Ingress:     ing.Name,
		Rule:        rule,
		Remediation: remediation,
	}
}
