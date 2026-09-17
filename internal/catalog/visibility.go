package catalog

import "sort"

const portalAdminGroup = "portal-admin"

// Visible returns only the catalog items authorized for identity, in stable
// presentation order. It never mutates the source snapshot.
func Visible(items []CatalogItem, identity *Identity) []CatalogItem {
	visible := make([]CatalogItem, 0, len(items))
	for _, item := range items {
		if itemVisible(item, identity) {
			visible = append(visible, item)
		}
	}

	sort.SliceStable(visible, func(i, j int) bool {
		left, right := visible[i], visible[j]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return false
	})

	return visible
}

func itemVisible(item CatalogItem, identity *Identity) bool {
	if item.Access == AccessPublic {
		return true
	}
	if identity == nil || !identity.Authenticated {
		return false
	}

	switch item.Access {
	case AccessAuthenticated:
		return true
	case AccessGroups:
		for _, group := range item.Groups {
			if _, member := identity.Groups[group]; member {
				return true
			}
		}
		return false
	case AccessAdmin:
		_, exactAdminGroup := identity.Groups[portalAdminGroup]
		return identity.IsAdmin && exactAdminGroup
	default:
		return false
	}
}
