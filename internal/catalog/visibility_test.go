package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVisibleFiltersForAnonymousMemberNonMemberAndAdmin(t *testing.T) {
	items := []CatalogItem{
		{ID: "admin", Name: "Admin", Access: AccessAdmin},
		{ID: "group", Name: "Group", Access: AccessGroups, Groups: []string{"team-a"}},
		{ID: "authenticated", Name: "Authenticated", Access: AccessAuthenticated},
		{ID: "public", Name: "Public", Access: AccessPublic},
		{ID: "unknown", Name: "Unknown", Access: Access("unknown")},
	}

	tests := []struct {
		name     string
		identity *Identity
		wantIDs  []string
	}{
		{name: "anonymous", wantIDs: []string{"public"}},
		{
			name:     "invalid identity is anonymous",
			identity: &Identity{Groups: groupSet("team-a", "portal-admin"), IsAdmin: true},
			wantIDs:  []string{"public"},
		},
		{
			name:     "ordinary member",
			identity: &Identity{Authenticated: true, Groups: groupSet("team-a")},
			wantIDs:  []string{"authenticated", "group", "public"},
		},
		{
			name:     "non-member",
			identity: &Identity{Authenticated: true, Groups: groupSet("team-b")},
			wantIDs:  []string{"authenticated", "public"},
		},
		{
			name: "admin",
			identity: &Identity{
				Authenticated: true,
				Groups:        groupSet("team-a", "portal-admin"),
				IsAdmin:       true,
			},
			wantIDs: []string{"admin", "authenticated", "group", "public"},
		},
		{
			name:     "admin group is case sensitive",
			identity: &Identity{Authenticated: true, Groups: groupSet("Portal-Admin")},
			wantIDs:  []string{"authenticated", "public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visible := Visible(items, tt.identity)
			assert.Equal(t, tt.wantIDs, itemIDs(visible))
		})
	}
}

func TestVisibleUsesExactCaseSensitiveGroupIntersection(t *testing.T) {
	items := []CatalogItem{
		{ID: "exact", Access: AccessGroups, Groups: []string{"Homelab Users"}},
		{ID: "different-case", Access: AccessGroups, Groups: []string{"homelab users"}},
	}
	identity := &Identity{Authenticated: true, Groups: groupSet("Homelab Users")}

	assert.Equal(t, []string{"exact"}, itemIDs(Visible(items, identity)))
}

func TestVisibleSortsWithoutMutatingInput(t *testing.T) {
	items := []CatalogItem{
		{ID: "zeta", Name: "Zeta", Category: "Beta", Order: 10, Access: AccessPublic},
		{ID: "bravo", Name: "Bravo", Category: "Alpha", Order: 10, Access: AccessPublic},
		{ID: "alpha-2", Name: "Alpha", Category: "Alpha", Order: 10, Access: AccessPublic},
		{ID: "alpha-1", Name: "Alpha", Category: "Alpha", Order: 10, Access: AccessPublic},
		{ID: "first", Name: "First", Category: "Zulu", Order: 1, Access: AccessPublic},
	}
	originalIDs := itemIDs(items)

	visible := Visible(items, nil)

	assert.Equal(t, []string{"first", "alpha-1", "alpha-2", "bravo", "zeta"}, itemIDs(visible))
	assert.Equal(t, originalIDs, itemIDs(items))
}

func groupSet(groups ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		set[group] = struct{}{}
	}
	return set
}

func itemIDs(items []CatalogItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
