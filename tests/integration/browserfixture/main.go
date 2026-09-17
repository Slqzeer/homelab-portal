// Command browserfixture serves the real portal handler with deterministic test data.
package main

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/assets"
	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/catalog"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const address = "127.0.0.1:4173"

var (
	longName        = strings.Repeat("N", 80)
	longDescription = strings.Repeat("D", 240)
	longCategory    = strings.Repeat("C", 40)
)

func main() {
	now := time.Now().UTC().Truncate(time.Second)
	store := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
	store.Replace([]networkingv1.Ingress{
		publishedIngress("grafana", "Grafana", "Dashboards and visualizations", "Monitoring", "public", "grafana"),
		publishedIngress("prometheus", "Prometheus", "Time-series metrics", "Monitoring", "public", "prometheus"),
		publishedIngress("keycloak", "Keycloak", "Single sign-on", "Identity", "public", "keycloak"),
		publishedIngress("long-content", longName, longDescription, longCategory, "public", "generic"),
		publishedIngress("secret-admin", "Secret Admin", "Private operations", "Administration", "admin", "unknown-icon"),
	}, now)

	sessions, err := auth.NewSessionManager(bytes.Repeat([]byte{6}, 32), nil)
	if err != nil {
		log.Fatal(err)
	}
	portal, err := portalhttp.New(portalhttp.Options{
		Store: store, Sessions: sessions, Assets: assets.FS(), BaseURL: "https://" + address,
		Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /__test/admin", func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.Create(w, auth.Claims{Groups: []string{"portal-admin"}}, now); err != nil {
			http.Error(w, "could not create test session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.Handle("/", portal)

	server := httptest.NewUnstartedServer(mux)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	if err := server.Listener.Close(); err != nil {
		log.Fatal(err)
	}
	server.Listener, err = net.Listen("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	select {}
}

func publishedIngress(slug, name, description, category, access, icon string) networkingv1.Ingress {
	class := "tailscale"
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: slug, Annotations: map[string]string{
			"portal.homelab.io/enabled": "true", "portal.homelab.io/name": name,
			"portal.homelab.io/description": description, "portal.homelab.io/category": category,
			"portal.homelab.io/access": access, "portal.homelab.io/icon": icon,
		}},
		Spec:   networkingv1.IngressSpec{IngressClassName: &class},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: slug + ".tail.example"}}}},
	}
}
