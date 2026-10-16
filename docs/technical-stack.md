# Homelab Portal Technical Architecture

## Application Shape

The Portal is one application and one deployment. `web/` contains Astro,
Tailwind CSS, and native browser JavaScript. `cmd/` and `internal/` contain the Go
backend-for-frontend. Astro produces static assets during the build and the Go
binary embeds and serves them; Node is absent from the runtime image.

The Go service uses `net/http`, `client-go`, `go-oidc`, and `slog`. It owns the UI,
OIDC flow, catalog, and diagnostics without a separate public API, database, or
Redis dependency.

## Identity and Catalog

Keycloak provides a confidential `homelab-portal` OIDC client using Authorization
Code with PKCE. The browser receives an encrypted, signed, `Secure`, `HttpOnly`,
`SameSite=Lax` session cookie rather than OAuth tokens. A Portal Session lasts at
most four hours, expires after thirty minutes of inactivity, and contains no
refresh token. The current and previous session keys may overlap during rotation.

The catalog admits only `networking.k8s.io/v1` Ingresses with Tailscale class,
valid publication metadata, and exactly one usable LoadBalancer hostname. A
Catalog Item always targets `https://<hostname>`. The Portal does not probe target
applications and does not replace their authentication or authorization.
Administrative catalog entries and diagnostics require the exact `portal-admin`
group.

## Kubernetes and Security

The target cluster is k3s. The multi-platform image runs non-root with a read-only
root filesystem, no Linux capabilities, no privilege escalation, and
`RuntimeDefault` seccomp. The initial deployment uses one replica and no PVC.

RBAC grants only `get`, `list`, and `watch` on Ingresses across namespaces. It
grants no Secret, Pod, Service, Deployment, or write access. Deny-by-default
network policy permits only Tailscale proxy traffic, Prometheus scraping, DNS,
the Kubernetes API, and Keycloak. Live acceptance must verify that the installed
CNI enforces those rules.

The Kubernetes cache lists with a ten-second deadline and watches with jittered
backoff. The last valid catalog becomes degraded after two minutes and expired
after fifteen; readiness then fails while stale links remain visibly marked.

## Runtime Surfaces

The public listener uses `PORT`, default `8080`, and serves pages, assets, and
authentication. The operations listener uses `OPERATIONS_PORT`, default `8081`,
and serves only `/healthz`, `/readyz`, and `/metrics`. Each listener returns 404
for the other listener's routes. Kubernetes Services, probes, monitoring, and
network policies preserve that separation.

Logs are structured and exclude identities, addresses, groups, cookies, tokens,
authorization headers, OIDC codes, and sensitive URLs. Authentication routes are
rate limited. The service shuts down both listeners within the configured
graceful termination window.

## Delivery

Kustomize and Argo CD deliver the application. This repository owns the image and
application manifests; Homelab owns the namespace, Argo CD Application, shared
Keycloak/Vault integration, and centralized Tailscale Ingress.

Release images are immutable multi-platform indexes with per-platform SBOMs,
vulnerability scans, signatures, and provenance. Human approval selects the
exact digest and Git revision for a reviewed Homelab change. Rollback restores a
previous reviewed GitOps commit and its matching immutable digest. The detailed
procedure and evidence requirements are canonical in the
[release runbook](runbooks/release.md).
