package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestStoreSnapshotIsUninitializedUntilFirstSuccessfulReplace(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	store := NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})

	before := store.Snapshot(now)
	assert.False(t, before.Initialized)
	assert.False(t, before.Stale)
	assert.False(t, before.Expired)
	assert.True(t, before.LastSuccess.IsZero())

	store.Replace(nil, now)
	after := store.Snapshot(now)
	assert.True(t, after.Initialized)
	assert.Equal(t, now, after.LastSuccess)
	assert.False(t, after.Stale)
	assert.False(t, after.Expired)
}

func TestStoreSnapshotRebuildsCatalogAndCannotMutateStoredView(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	store := NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	valid := publishedIngress("tools", "grafana", "groups")
	valid.Annotations["portal.homelab.io/groups"] = "operators"
	invalid := publishedIngress("tools", "broken", "public")
	invalid.Annotations["portal.homelab.io/name"] = ""

	store.Replace([]networkingv1.Ingress{valid, invalid}, now)
	first := store.Snapshot(now)
	require.Len(t, first.Items, 1)
	require.Len(t, first.Diagnostics, 1)
	assert.Equal(t, "tools/grafana", first.Items[0].ID)
	assert.Equal(t, []string{"operators"}, first.Items[0].Groups)
	assert.Equal(t, "name", first.Diagnostics[0].Rule)

	first.Items[0].Name = "mutated"
	first.Items[0].Groups[0] = "mutated"
	first.Diagnostics[0].Rule = "mutated"
	first.Items = append(first.Items, CatalogItem{ID: "injected"})

	second := store.Snapshot(now)
	assert.Equal(t, "Grafana", second.Items[0].Name)
	assert.Equal(t, []string{"operators"}, second.Items[0].Groups)
	assert.Equal(t, "name", second.Diagnostics[0].Rule)
	assert.Len(t, second.Items, 1)
}

func TestStoreSnapshotReportsStaleAtTwoMinutesAndExpiredAtFifteen(t *testing.T) {
	lastSuccess := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	store := NewStore(types.NamespacedName{})
	store.Replace(nil, lastSuccess)

	justBeforeStale := store.Snapshot(lastSuccess.Add(2*time.Minute - time.Nanosecond))
	assert.False(t, justBeforeStale.Stale)
	assert.False(t, justBeforeStale.Expired)

	stale := store.Snapshot(lastSuccess.Add(2 * time.Minute))
	assert.True(t, stale.Stale)
	assert.False(t, stale.Expired)

	expired := store.Snapshot(lastSuccess.Add(15 * time.Minute))
	assert.True(t, expired.Stale)
	assert.True(t, expired.Expired)
}
