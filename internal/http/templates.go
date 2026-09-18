package portalhttp

import (
	"html/template"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
)

type pageData struct {
	Items         []catalog.CatalogItem
	Categories    []string
	Stale         bool
	Authenticated bool
	IsAdmin       bool
	CSRFToken     string
	Styles        []string
}

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="description" content="Trusted links to intentionally published homelab services."><title>Homelab Portal</title>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head>
<body><a class="skip-link" href="#main-content">Skip to content</a>
<header class="site-header catalog-header"><p class="eyebrow catalog-header-label">Tailnet catalogue</p><nav class="identity-actions" aria-label="Account">
{{if .Authenticated}}<form action="/auth/logout" method="post"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="button button-secondary" type="submit">Sign out</button></form>{{else}}<a class="button button-secondary" href="/auth/login">Sign in</a>{{end}}
{{if .IsAdmin}}<a class="button button-secondary" href="/admin">Admin diagnostics</a>{{end}}</nav></header>
<main id="main-content" class="site-main" tabindex="-1"><div class="catalog-shell"><aside class="catalog-information-rail" aria-labelledby="portal-title"><h1 id="portal-title">Homelab Portal</h1><p class="eyebrow">Available to you</p><h2>Service catalogue</h2><p>Open an intentionally published service. Each target application manages its own access.</p>
<p class="catalog-status" data-catalog-status role="status" aria-live="polite" aria-atomic="true">{{len .Items}} catalog {{if eq (len .Items) 1}}item{{else}}items{{end}}</p>
{{if .Stale}}<aside class="stale-banner" role="status" aria-live="polite"><strong>Catalogue is stale. Catalogue information is not current.</strong> Showing the last known catalogue links.</aside>{{end}}</aside><div class="catalog-main">
<section class="catalog-controls" aria-labelledby="catalog-filters-heading"><h3 id="catalog-filters-heading" class="visually-hidden">Find a catalog item</h3><div class="search-control"><label for="catalog-search">Search catalog</label><div class="search-input-row"><input id="catalog-search" type="search" data-catalog-search autocomplete="off" placeholder="Name, description, or category"><button class="clear-search" type="button" data-catalog-clear-search hidden>Clear search</button></div></div>
<div class="category-filters" role="group" aria-label="Filter by category"><button class="filter-button" type="button" data-category-filter="" aria-pressed="true" aria-controls="catalog-items">All</button>{{range .Categories}}<button class="filter-button" type="button" data-category-filter="{{.}}" aria-pressed="false" aria-controls="catalog-items">{{.}}</button>{{end}}</div>
 </section>
<section aria-labelledby="catalog-results-heading"><h3 id="catalog-results-heading" class="visually-hidden">Catalog items</h3><div id="catalog-items" class="catalog-grid" data-catalog-grid>
{{range .Items}}<article data-catalog-item class="catalog-card"><img class="catalog-icon" src="/icons/{{.Icon}}.svg" width="40" height="40" alt="" aria-hidden="true"><div><p class="catalog-category" data-catalog-category>{{.Category}}</p><h4 data-catalog-name><a href="{{.TargetURL}}" rel="noreferrer" aria-label="Open {{.Name}}">{{.Name}}</a></h4><p data-catalog-description>{{.Description}}</p></div></article>{{else}}<p class="empty-catalog">No catalog items are available.</p>{{end}}
</div></section></div></div></main><script src="/app.js" defer></script></body></html>`))

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Admin diagnostics · Homelab Portal</title>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head><body><a class="skip-link" href="#main-content">Skip to content</a><header class="site-header"><div><p class="eyebrow">Read-only operations</p><h1>Admin diagnostics</h1></div><nav aria-label="Portal"><a class="button button-secondary" href="/">Back to catalogue</a></nav></header><main id="main-content" class="site-main" tabindex="-1">
<p class="watcher-status"><strong>Catalogue watcher:</strong> {{.Watcher}}</p>
<div class="table-region" role="region" aria-label="Publication diagnostics" tabindex="0"><table><thead><tr><th scope="col">Namespace</th><th scope="col">Ingress</th><th scope="col">Rule</th><th scope="col">Remediation</th></tr></thead><tbody>
{{range .Diagnostics}}<tr><td>{{.Namespace}}</td><td>{{.Ingress}}</td><td>{{.Rule}}</td><td>{{.Remediation}}</td></tr>{{end}}
</tbody></table></div></main></body></html>`))

type adminData struct {
	Diagnostics []catalog.Diagnostic
	Watcher     string
	Styles      []string
}
