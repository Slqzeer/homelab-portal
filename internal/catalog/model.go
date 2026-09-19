package catalog

// Access is the publication visibility policy for a catalog item.
type Access string

const (
	AccessPublic        Access = "public"
	AccessAuthenticated Access = "authenticated"
	AccessGroups        Access = "groups"
	AccessAdmin         Access = "admin"
)

// CatalogItem is the allowlisted data that may be exposed to a portal user.
type CatalogItem struct {
	ID          string
	Namespace   string
	Ingress     string
	Name        string
	Description string
	Category    string
	Icon        string
	Access      Access
	Groups      []string
	Order       int
	TargetURL   string
}

// Candidate contains a catalog item when an Ingress is validly published.
type Candidate struct {
	Item *CatalogItem
}

// Diagnostic contains only allowlisted resource identity and remediation data.
type Diagnostic struct {
	Namespace   string
	Ingress     string
	Rule        string
	Remediation string
}

// Identity is the server-validated identity used for catalog visibility.
type Identity struct {
	Authenticated bool
	DisplayName   string
	Groups        map[string]struct{}
	IsAdmin       bool
}
