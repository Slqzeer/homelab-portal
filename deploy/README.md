# Application package

Render with `kustomize build deploy/overlays/homelab` (Kustomize 5.7.1), or
`kubectl kustomize deploy/overlays/homelab`. Test with `go test ./tests/manifest`.
Tests need either renderer on PATH and decode built-in resources strictly using
the project's Kubernetes v0.36.4 Go API types. That checks field names/types,
not API-server admission or CNI enforcement.

The checked-in overlay is a template, not a deployable release. Replace its zero
image digest with an approved immutable digest. External homelab GitOps owns
namespace bootstrap, VaultAuth/Vault policy provisioning, Prometheus discovery,
the Argo Application (wave 23), and the centralized Tailscale Ingress (wave 21).
The Namespace included here declares the required restricted Pod Security
labels; external GitOps must coordinate its ownership instead of reconciling
conflicting definitions. This application package creates no Ingress.

## Required site configuration

Patch the following non-secret values in the consuming overlay. Never widen
policies to whole namespaces, CIDR ranges, or all ports to work around a failure.

| Resource | Values to set or verify |
| --- | --- |
| Deployment image patch | Approved registry and SHA-256 image digest |
| ConfigMap | Portal HTTPS URL, Keycloak issuer URL, portal Ingress identity |
| VaultStaticSecret | Existing VaultAuth reference, KV-v2 mount and path |
| NetworkPolicy ingress | Tailscale operator namespace and exact parent Ingress identity labels; Prometheus namespace and named scraper labels |
| NetworkPolicy DNS egress | Actual kube-dns namespace/labels and TCP/UDP 53; NodeLocal DNS needs a separately reviewed narrow address exception |
| NetworkPolicy API egress | Replace `192.0.2.1/32` (Service VIP) and `198.51.100.1/32` (API endpoint) with actual single-host addresses; verify 443/6443 and every HA endpoint |
| NetworkPolicy Keycloak egress | Keycloak namespace, pod labels, and TLS target port (example 8443) |
| ServiceMonitor / PrometheusRule | Labels accepted by the existing Prometheus selectors, and permission to discover namespace `portal` |

The homelab overlay exposes network selector/address/Keycloak-port settings as
explicit JSON patches. API address examples are documentation-only IPs and
intentionally fail closed. Kubernetes policies do not select Services or DNS
names. CNI DNAT ordering determines whether Service VIPs or endpoint addresses
are matched. Verify actual paths in the disposable-cluster acceptance task,
including kubelet probes and DNS. The Keycloak issuer hostname must resolve to
the approved Keycloak TLS endpoint (with a matching trusted certificate), not a
general ingress gateway or Tailnet proxy outside those selectors. No Vault
egress is granted to the application: VSO performs synchronization separately.

## Secrets and restart behavior

VSO must populate `homelab-portal-secrets` with `oidc-client-secret` and
`session-current-key`. The key format is the application's existing secret-file
contract; no key or credential belongs in Git or a ConfigMap. Files mount as
read-only, group-readable projections for UID/GID 65532. The Go process needs no
writable temporary volume.

Secrets are read at startup, so VSO restarts the Deployment on changes. For key
rotation, provision `session-previous-key` through VSO and add the non-secret
`SESSION_PREVIOUS_KEY_FILE=/var/run/portal-secrets/session-previous-key` ConfigMap
entry; remove that entry when the previous key is retired. ConfigMap-only
changes also require an explicit rollout. Recreate strategy preserves the
single-process session model, accepting downtime/session loss on restart.

## Internal operations

The ClusterIP Service exposes port 8080. Kubelet probes use that same named pod
port directly; using Service DNS in readiness would introduce a routing cycle.
The Service publishes unready addresses so failed-initial-list and expired-cache
metrics remain scrapeable and the application can still show its last valid
links. Readiness still fails; it is not a traffic gate for this Service.

NetworkPolicy restricts peers and ports, not HTTP paths. External Tailscale
Ingress must explicitly allow only UI/auth/catalog routes and exclude
`/healthz`, `/readyz`, and `/metrics`; a catch-all `/` rule would expose them.
The metrics scraper has access to this internal HTTP port. Prometheus rules
scope metrics to namespace `portal` and Service `homelab-portal`; retain those
target labels when customizing discovery. Reconnect alerts use the elapsed-time
gauge directly (>120 seconds), with no second two-minute delay. Invalid metadata
must persist for ten minutes to warn. Prometheus scrape/evaluation intervals
add their normal detection latency.

Task 9 must verify real RBAC denials (`kubectl auth can-i --as
system:serviceaccount:portal:homelab-portal`) for Secret get/list/watch and Ingress
create/update/patch/delete, plus CNI enforcement, VSO synchronization, and
external route isolation. Local rendering/static tests do not prove these.
