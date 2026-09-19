package portalhttp

import (
	"html/template"
	"strings"
	"unicode/utf8"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
)

type pageData struct {
	Items         []catalog.CatalogItem
	Categories    []string
	Stale         bool
	Authenticated bool
	DisplayName   string
	Initials      string
	IsAdmin       bool
	CSRFToken     string
	Styles        []string
}

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="description" content="Trusted links to intentionally published homelab services."><title>Homelab Portal</title><script src="/theme.js"></script>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head>
<body><a class="skip-link" href="#main-content">Skip to content</a>
<header class="site-header catalog-header"><p class="eyebrow catalog-header-label">Tailnet catalogue</p><nav class="identity-actions" aria-label="Account"><button class="button button-secondary theme-toggle" type="button" data-theme-toggle><span data-theme-toggle-icon aria-hidden="true">&#9789;</span><span data-theme-toggle-label>Switch to light theme</span></button>
{{if .Authenticated}}<span class="profile-entry"><a class="profile-link" href="/profile" aria-label="Profile: {{if .DisplayName}}{{.DisplayName}}{{else}}Signed-in user{{end}}" aria-describedby="profile-preview">{{.Initials}}</a><span id="profile-preview" class="profile-preview">{{if .DisplayName}}{{.DisplayName}}{{else}}Signed-in user{{end}}<br>Signed in.</span></span>{{else}}<a class="button button-secondary" href="/auth/login">Sign in</a>{{end}}
{{if .IsAdmin}}<a class="button button-secondary" href="/admin">Admin diagnostics</a>{{end}}</nav></header>
<main id="main-content" class="site-main" tabindex="-1"><div class="catalog-shell"><aside class="catalog-information-rail" aria-labelledby="portal-title"><h1 id="portal-title">Homelab Portal</h1><p class="eyebrow">Available to you</p><h2>Service catalogue</h2><p>Open an intentionally published service. Each target application manages its own access.</p>
<p class="catalog-status" data-catalog-status role="status" aria-live="polite" aria-atomic="true">{{len .Items}} catalog {{if eq (len .Items) 1}}item{{else}}items{{end}}</p>
{{if .Stale}}<aside class="stale-banner" role="status" aria-live="polite"><strong>Catalogue is stale. Catalogue information is not current.</strong> Showing the last known catalogue links.</aside>{{end}}</aside><div class="catalog-main">
<section class="catalog-controls" aria-labelledby="catalog-filters-heading"><h3 id="catalog-filters-heading" class="visually-hidden">Find a catalog item</h3><div class="search-control"><label for="catalog-search">Search catalog</label><div class="search-input-row"><input id="catalog-search" type="search" data-catalog-search autocomplete="off" placeholder="Name, description, or category"><button class="clear-search" type="button" data-catalog-clear-search hidden>Clear search</button></div></div>
<div class="category-filters" role="group" aria-label="Filter by category"><button class="filter-button" type="button" data-category-filter="" aria-pressed="true" aria-controls="catalog-items">All</button>{{range .Categories}}<button class="filter-button" type="button" data-category-filter="{{.}}" aria-pressed="false" aria-controls="catalog-items">{{.}}</button>{{end}}</div>
 </section>
<section aria-labelledby="catalog-results-heading"><h3 id="catalog-results-heading" class="visually-hidden">Catalog items</h3><section class="empty-filter-results" data-empty-results hidden aria-labelledby="empty-filter-results-heading"><h3 id="empty-filter-results-heading">No catalog items match these filters.</h3><p>Clear a filter to see available catalog items.</p><div class="empty-filter-actions"><button type="button" data-empty-clear-search>Clear search</button><button type="button" data-empty-clear-category>Return to All</button></div></section><div id="catalog-items" class="catalog-grid" data-catalog-grid>
{{range .Items}}<article data-catalog-item class="catalog-card"><img class="catalog-icon" src="/icons/{{.Icon}}.svg" width="40" height="40" alt="" aria-hidden="true"><div><p class="catalog-category" data-catalog-category>{{.Category}}</p><h4 data-catalog-name><a href="{{.TargetURL}}" rel="noreferrer" aria-label="Open {{.Name}}">{{.Name}}</a></h4><p data-catalog-description>{{.Description}}</p></div></article>{{else}}<p class="empty-catalog">No catalog items are available.</p>{{end}}
</div></section></div></div></main><script src="/app.js" defer></script></body></html>`))

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Admin diagnostics · Homelab Portal</title><script src="/theme.js"></script>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head><body><a class="skip-link" href="#main-content">Skip to content</a><header class="site-header"><div><p class="eyebrow">Read-only operations</p><h1>Admin diagnostics</h1></div><nav aria-label="Portal"><a class="button button-secondary" href="/">Back to catalogue</a></nav></header><main id="main-content" class="site-main" tabindex="-1">
<p class="watcher-status"><strong>Catalogue watcher:</strong> {{.Watcher}}</p>
<div class="table-region" role="region" aria-label="Publication diagnostics" tabindex="0"><table><thead><tr><th scope="col">Namespace</th><th scope="col">Ingress</th><th scope="col">Rule</th><th scope="col">Remediation</th></tr></thead><tbody>
{{range .Diagnostics}}<tr><td>{{.Namespace}}</td><td>{{.Ingress}}</td><td>{{.Rule}}</td><td>{{.Remediation}}</td></tr>{{end}}
</tbody></table></div></main></body></html>`))

var profileTemplate = template.Must(template.New("profile").Parse(`<!doctype html><html lang="en" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Profile · Homelab Portal</title><script src="/theme.js"></script>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head><body><a class="skip-link" href="#main-content">Skip to content</a><header class="site-header"><p class="eyebrow">Account</p><nav class="identity-actions" aria-label="Account"><a class="button button-secondary" href="/">Back to catalogue</a>{{if .IsAdmin}}<a class="button button-secondary" href="/admin">Admin diagnostics</a>{{end}}</nav></header><main id="main-content" class="site-main" tabindex="-1"><h1>{{if .DisplayName}}{{.DisplayName}}{{else}}Signed-in user{{end}}</h1><p>Signed in.</p><form action="/auth/logout" method="post"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="button button-secondary" type="submit">Sign out</button></form></main></body></html>`))

type adminData struct {
	Diagnostics []catalog.Diagnostic
	Watcher     string
	Styles      []string
}

func profileInitials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 { return "?" }
	first, _ := utf8.DecodeRuneInString(fields[0])
	if len(fields) == 1 { return string(first) }
	last, _ := utf8.DecodeRuneInString(fields[len(fields)-1])
	return string(first) + string(last)
}
