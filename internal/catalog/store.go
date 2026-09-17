package catalog

import (
	"sort"
	"sync"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	staleAfter   = 2 * time.Minute
	expiredAfter = 15 * time.Minute
)

// Snapshot is an immutable caller-owned view of the latest successfully
// rebuilt catalog.
type Snapshot struct {
	Items       []CatalogItem
	Diagnostics []Diagnostic
	LastSuccess time.Time
	Stale       bool
	Expired     bool
	Initialized bool
}

// Store keeps the last successfully rebuilt catalog in memory.
type Store struct {
	mu            sync.RWMutex
	portalIngress types.NamespacedName
	snapshot      Snapshot
}

// NewStore creates an uninitialized catalog store.
func NewStore(portalIngress types.NamespacedName) *Store {
	return &Store{portalIngress: portalIngress}
}

// Replace atomically replaces the catalog after a successful Kubernetes list
// or watch state change.

func (s *Store) Replace(ingresses []networkingv1.Ingress, at time.Time) {
	items := make([]CatalogItem, 0, len(ingresses))
	diagnostics := make([]Diagnostic, 0)
	for _, ingress := range ingresses {
		candidate, diagnostic := ParseIngress(ingress, s.portalIngress)
		if candidate.Item != nil {
			items = append(items, cloneItem(*candidate.Item))
		}
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Namespace != diagnostics[j].Namespace {
			return diagnostics[i].Namespace < diagnostics[j].Namespace
		}
		if diagnostics[i].Ingress != diagnostics[j].Ingress {
			return diagnostics[i].Ingress < diagnostics[j].Ingress
		}
		return diagnostics[i].Rule < diagnostics[j].Rule
	})

	s.mu.Lock()
	s.snapshot = Snapshot{
		Items:       items,
		Diagnostics: diagnostics,
		LastSuccess: at,
		Initialized: true,
	}
	s.mu.Unlock()
}

// Snapshot returns a caller-owned copy with freshness derived at read time.
func (s *Store) Snapshot(now time.Time) Snapshot {
	s.mu.RLock()
	snapshot := cloneSnapshot(s.snapshot)
	s.mu.RUnlock()

	if snapshot.Initialized {
		age := now.Sub(snapshot.LastSuccess)
		snapshot.Stale = age >= staleAfter
		snapshot.Expired = age >= expiredAfter
	}

	return snapshot
}

func cloneSnapshot(source Snapshot) Snapshot {
	clone := source
	clone.Items = make([]CatalogItem, len(source.Items))
	for i := range source.Items {
		clone.Items[i] = cloneItem(source.Items[i])
	}
	clone.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	return clone
}

func cloneItem(source CatalogItem) CatalogItem {
	clone := source
	clone.Groups = append([]string(nil), source.Groups...)
	return clone
}
