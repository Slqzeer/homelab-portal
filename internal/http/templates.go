package portalhttp

import (
	"html/template"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
)

type pageData struct {
	Items         []catalog.CatalogItem
	Stale         bool
	Authenticated bool
	IsAdmin       bool
	CSRFToken     string
	Styles        []string
}

var homeTemplate = template.Must(template.New("home").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Homelab Portal</title>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head>
<body><a class="skip-link" href="#main-content">Skip to content</a><header class="site-header"><h1>Homelab Portal</h1>
{{if .Authenticated}}<form action="/auth/logout" method="post"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button type="submit">Sign out</button></form>{{else}}<a href="/auth/login">Sign in</a>{{end}}
{{if .IsAdmin}}<a href="/admin">Admin diagnostics</a>{{end}}</header><main id="main-content" class="site-main" tabindex="-1"><h2>Service catalogue</h2>
{{if .Stale}}<p role="status">Catalogue is stale. These are the last available links.</p>{{end}}
{{range .Items}}<article data-catalog-item><h3><a href="{{.TargetURL}}" rel="noreferrer">{{.Name}}</a></h3><p>{{.Description}}</p><span>{{.Category}}</span></article>{{else}}<p>No catalog items are available.</p>{{end}}
</main><script src="/app.js" defer></script></body></html>`))

var adminTemplate = template.Must(template.New("admin").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Admin diagnostics</title>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}</head><body><main class="site-main"><h1>Admin diagnostics</h1>
<p>Watcher: {{.Watcher}}</p>
<table><thead><tr><th>Namespace</th><th>Ingress</th><th>Rule</th><th>Remediation</th></tr></thead><tbody>
{{range .Diagnostics}}<tr><td>{{.Namespace}}</td><td>{{.Ingress}}</td><td>{{.Rule}}</td><td>{{.Remediation}}</td></tr>{{end}}
</tbody></table></main></body></html>`))

type adminData struct {
	Diagnostics []catalog.Diagnostic
	Watcher     string
	Styles      []string
}
