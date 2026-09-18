// Package portalhttp serves the authorized portal and its internal operations endpoints.
package portalhttp

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/catalog"
	"github.com/Slqzeer/homelab-portal/internal/kube"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Options struct {
	Store    *catalog.Store
	Sessions *auth.SessionManager
	OIDC     *auth.OIDC
	Limiter  *auth.LoginLimiter
	Assets   fs.FS
	BaseURL  string
	Now      func() time.Time
	Logger   *slog.Logger
	Watcher  *kube.Watcher
	Registry *prometheus.Registry
}

type server struct {
	Options
	transactions *loginTransactions
	origin       string
	metrics      *metrics
	styles       []string
}

// Handlers exposes disjoint route tables sharing one catalogue/session runtime.
// ServeHTTP is the public listener; Operations must use a separate listener.
type Handlers struct {
	Public     http.Handler
	Operations http.Handler
}

func (h *Handlers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Public.ServeHTTP(w, r)
}

func New(options Options) (*Handlers, error) {
	if options.Store == nil || options.Sessions == nil || options.Assets == nil {
		return nil, errors.New("portal HTTP dependencies are required")
	}
	if options.Logger == nil {
		options.Logger = NewLogger(os.Stdout)
	}
	if options.Limiter == nil {
		options.Limiter = auth.NewLoginLimiter()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Registry == nil {
		options.Registry = prometheus.NewRegistry()
	}
	s := &server{Options: options}
	base, err := url.Parse(options.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("portal base URL must be an HTTPS origin")
	}
	s.origin = base.Scheme + "://" + base.Host
	s.transactions, err = newLoginTransactions()
	if err != nil {
		return nil, err
	}
	s.metrics = newMetrics(s)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /admin", s.admin)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	files := http.FileServer(http.FS(options.Assets))
	err = fs.WalkDir(options.Assets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && (strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".svg")) {
			mux.Handle("GET /"+path, files)
			if strings.HasSuffix(path, ".css") {
				s.styles = append(s.styles, "/"+path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := options.Registry.Register(s.metrics); err != nil {
		return nil, err
	}
	operations := http.NewServeMux()
	operations.HandleFunc("GET /healthz", s.health)
	operations.HandleFunc("GET /readyz", s.ready)
	operations.Handle("GET /metrics", promhttp.HandlerFor(options.Registry, promhttp.HandlerOpts{}))
	return &Handlers{Public: securityHeaders(s.observe(mux)), Operations: securityHeaders(s.observe(operations))}, nil
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	now := s.Now()
	identity := s.identity(w, r, now)
	snapshot := s.Store.Snapshot(now)
	data := pageData{Stale: snapshot.Stale, Styles: s.styles}
	if identity != nil {
		data.Authenticated = identity.Authenticated
		data.IsAdmin = identity.IsAdmin
		for _, cookie := range (&http.Response{Header: w.Header()}).Cookies() {
			if cookie.Name == "__Host-portal_session" && cookie.MaxAge != -1 {
				data.CSRFToken = s.csrfToken(w, r, cookie.Expires)
			}
		}
	}
	data.Items = catalog.Visible(snapshot.Items, identity)
	categorySet := make(map[string]struct{}, len(data.Items))
	for _, item := range data.Items {
		categorySet[item.Category] = struct{}{}
	}
	for category := range categorySet {
		data.Categories = append(data.Categories, category)
	}
	sort.Strings(data.Categories)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = homeTemplate.Execute(w, data)
}

func (s *server) identity(w http.ResponseWriter, r *http.Request, now time.Time) *catalog.Identity {
	identity, err := s.Sessions.Load(r, now)
	if errors.Is(err, auth.ErrInvalidSession) {
		s.Sessions.Destroy(w)
		return nil
	}
	if err != nil {
		return nil
	}
	if identity != nil && s.Sessions.Refresh(w, r, now) != nil {
		return nil
	}
	return identity
}

func (s *server) admin(w http.ResponseWriter, r *http.Request) {
	now := s.Now()
	identity := s.identity(w, r, now)
	if identity == nil || !identity.Authenticated || !identity.IsAdmin {
		http.NotFound(w, r)
		return
	}
	if _, ok := identity.Groups["portal-admin"]; !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminTemplate.Execute(w, adminData{Diagnostics: s.Store.Snapshot(now).Diagnostics, Watcher: s.watchStatus(now).State, Styles: s.styles})
}

func (s *server) watchStatus(now time.Time) kube.Status {
	if s.Watcher == nil {
		return kube.Status{State: "starting"}
	}
	return s.Watcher.Status(now)
}
