## Problem Statement

People connected to the Tailnet or local network need a trustworthy home page for the web interfaces intentionally exposed by the homelab cluster. Administrators also need a read-only way to understand why a service is absent from that catalogue. The portal must not become a Kubernetes administration console, disclose unpublished targets, or claim that an application is healthy merely because a link exists.

## Solution

Deliver one in-cluster Homelab Portal: a Go backend-for-frontend that serves statically built Astro/Tailwind assets, authenticates through Keycloak, watches eligible Kubernetes Ingresses, and produces a per-session catalogue. Publication remains explicit GitOps metadata. The portal is Tailscale-only, read-only, single-replica, least-privilege, observable, and delivered by immutable Argo CD/Kustomize releases.

## User Stories

1. As a Tailnet visitor, I want to see public catalog items without signing in, so that I can reach intentionally public homelab interfaces quickly.
2. As a signed-in user, I want to see authenticated catalog items, so that the portal reflects my Keycloak identity.
3. As a signed-in user, I want to see only group-authorized catalog items, so that unpublished target URLs and service names are not disclosed to me.
4. As a portal administrator, I want to see admin catalog items, so that administrative services remain hidden from ordinary users.
5. As a portal administrator, I want read-only admin diagnostics, so that I can remediate invalid Publication metadata and missing catalog items.
6. As a visitor, I want every catalog item to point to its current Tailscale HTTPS hostname, so that I do not land on guessed or stale URLs.
7. As a visitor, I want search and category filtering to work from the catalog items I am already allowed to receive, so that filtering cannot reveal hidden items.
8. As a keyboard and small-screen user, I want an accessible responsive interface, so that the portal is usable without a mouse or a desktop display.
9. As an Ingress owner, I want publication to be explicit, Git-versioned metadata, so that adding an exposed service cannot accidentally disclose it in the portal.
10. As an Ingress owner, I want invalid Publication metadata excluded from normal views and explained to administrators, so that configuration mistakes are safe and actionable.
11. As a security-conscious user, I want the portal to evaluate access server-side, so that browser JavaScript never receives unauthorized catalog items, URLs, or OAuth tokens.
12. As a Keycloak user, I want a secure sign-in and sign-out flow, so that the portal can obtain my groups without managing my password.
13. As an existing session holder, I want a temporary Keycloak outage not to erase my valid session immediately, so that the portal remains useful during a short identity-provider outage.
14. As an operator, I want the portal to survive transient Kubernetes watch failures using its last valid catalog, so that a short API interruption does not immediately make the home page empty.
15. As an operator, I want the portal to visibly report stale catalogue state and fail readiness after the agreed maximum age, so that stale information is never silently treated as current.
16. As an operator, I want probes and metrics available only inside the cluster, so that operational endpoints are not exposed through the Tailnet Ingress.
17. As an operator, I want structured safe logs and Prometheus alerts, so that failures are diagnosable without logging personal or credential data.
18. As a platform administrator, I want the Portal ServiceAccount to read only Ingress data, so that it cannot become a write path into Kubernetes or access Secrets.
19. As a platform administrator, I want default-deny network controls and a hardened non-root container, so that compromise of the portal has a constrained blast radius.
20. As a release manager, I want a single versioned deployable for UI and BFF, so that the rendered UI and its authorized BFF contract are always compatible.
21. As a release manager, I want multi-architecture immutable images with an SBOM, vulnerability scan, and signature, so that releases are traceable and suitable for the k3s cluster.
22. As a GitOps operator, I want Argo CD to reconcile pinned revisions and image digests, so that promotion and rollback are auditable Git changes.
23. As an application owner, I want my target application's own access control to remain authoritative, so that portal visibility never falsely promises target access.
24. As a security operator, I want visible temporary throttling of excessive sign-in attempts, so that abusive authentication traffic is limited without the portal handling passwords.
25. As an implementer, I want automated behavior-focused tests across the BFF boundaries, so that the security and discovery rules survive refactoring.

## Implementation Decisions

- Build one application repository with a `web` area for Astro/Tailwind/native JavaScript and Go BFF areas; release one image and one version for both. Astro builds static local assets that Go embeds and serves. No Node runtime, CDN, client framework, separate public API, database, Redis, or PVC is introduced.
- Use Go with `net/http`, `client-go`, `go-oidc`, and `slog`. Keep the BFF as the single HTTP seam between browser behavior and all authorization/catalog decisions.
- Target Kubernetes `v1.36.4+k3s1` and publish `linux/amd64` and `linux/arm64` images. Use a multi-stage reproducible build, a distroless non-root final image, pinned dependency lockfiles, immutable tags and digests.
- Discover only `networking.k8s.io/v1` Ingresses whose class is exactly `tailscale`. An Eligible Ingress must contain valid Publication metadata and exactly one usable `status.loadBalancer.ingress[].hostname`; derive its target only as `https://<hostname>`. Reject IPs, multiple or absent usable hostnames, inferred URLs, non-HTTP targets, and active probes. Exclude the portal's own Ingress from its catalogue.
- Interpret Publication metadata strictly: `enabled` is exactly `true`; required name and access are validated; optional description/category/icon/order use the limits and defaults already defined; groups are a trimmed, case-sensitive CSV allowed only for group access, with empties and duplicates rejected. Invalid candidates are hidden from all user catalogues and appear only in Admin diagnostics.
- Apply visibility on every BFF request: anonymous users receive public items; valid Portal sessions receive public, authenticated, matching group, and—only for `portal-admin`—admin items. The portal only controls catalog visibility; each Target application owns its own authentication and authorization.
- Configure Keycloak as confidential client `homelab-portal` with Authorization Code + PKCE, `openid profile groups`, JSON `groups`, callback `/auth/callback`, logout `/auth/logout`, and exact administrator group `portal-admin`. Validate issuer, audience, signature, expiry, and groups server-side. Use encrypted and signed Secure/HttpOnly/SameSite=Lax session cookies, no browser token and no refresh token; enforce a four-hour absolute and 30-minute idle timeout.
- Permit session-key rotation with current and previous keys from Vault through VSO. A logout destroys the local session. Existing sessions remain usable only until their local expiry during Keycloak outages.
- Initialize the catalogue by a 10-second list then maintain an atomic in-memory snapshot with list/watch. Reconnect a failed watch using 1–30-second exponential backoff and jitter; relist on `410 Gone`. Mark data stale after two minutes and expired after 15; retain last valid links with a stale warning, but fail readiness after expiry or before any successful initial list.
- Run one replica with requests `25m` CPU and `48Mi`, limits `100m` CPU and `128Mi`, and 20 seconds graceful termination. Explicitly accept session loss at restart/rollout; multi-replica availability requires a future shared-state design.
- Grant only cross-namespace `get`, `list`, and `watch` on Ingresses. Harden the pod with non-root execution, read-only root filesystem, no privilege escalation, dropped capabilities, RuntimeDefault seccomp, and only explicitly writable temporary mounts.
- Apply default-deny NetworkPolicy: ingress only from Tailscale proxy and approved metrics scraper; egress only to kube-dns, Kubernetes API, and Keycloak. Verify that the deployed CNI enforces it.
- Bind two independent HTTP listeners: public `PORT` (default 8080) serves `/`, auth routes, `/admin`, and explicit same-origin assets; internal `OPERATIONS_PORT` (default 8081) serves only `/healthz`, `/readyz`, and `/metrics`. Validate distinct ports in 1–65535. Cross-listener routes return 404 with normal security headers, regardless of host or proxy headers. Name container/Service ports `public` and `operations`; Tailscale Ingress routes only to `public`, while kubelet probes and ServiceMonitor use `operations`. NetworkPolicy grants proxy-to-public and scraper-to-operations in separate rules. Drain both servers and cancel the watcher within one shared 20-second shutdown deadline. Emit safe JSON stdout logs and Prometheus metrics; create GitOps alert rules for failed initial list, expired cache, prolonged watch reconnecting, and persistent invalid metadata.
- Use strict HTTP protections: CSP with local resources only, HSTS, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `frame-ancestors 'none'`, origin/CSRF validation for session-changing routes, and visible `429` throttling after 10 auth attempts per IP in five minutes for ten minutes.
- Package the application with Kustomize. The homelab repository owns namespace setup, VSO wiring, the Argo Application at sync wave 23, and the centralized Tailscale Ingress at wave 21. Promotion requires human approval after SBOM, scan, signature, and automated checks; rollback is a revert to the previous Git revision.

## Testing Decisions

- Treat the Go BFF HTTP interface as the highest primary test seam: assert response status, headers, rendered/returned authorized catalog data, redirects, and diagnostics visibility rather than handlers, locks, cache fields, or framework internals.
- Unit-test Publication metadata validation, hostname-to-URL derivation, access evaluation, sorting, stale-state calculation, and all invalid boundary cases.
- Use a fake Kubernetes client/watch seam to test initial list, add/update/delete, disconnect, retry, `410 Gone`, atomic replacement, and the last-valid-cache behavior.
- Test OIDC contracts with signed fixtures covering accepted claims plus wrong issuer, audience, signature, expiry, malformed group claim, missing group, login CSRF/state, session expiry, logout, and session key rotation.
- Test BFF routes for anonymous, authenticated, group, non-member, and `portal-admin` responses. Assert that unauthorized card names and URLs are absent, not merely hidden in the UI.
- Add RBAC-negative tests proving the Portal ServiceAccount cannot read Secrets or write Ingresses, plus deployment-policy checks for restricted security context, resource limits, and NetworkPolicy declarations.
- Add in-cluster end-to-end tests through the Tailscale Ingress for public, authenticated, group, and admin behavior, valid link derivation, absent unauthorized links, VSO synchronization, Argo Synced/Healthy state, and the proxy memory cap.
- Validate observable external behavior: health/readiness transitions, internal-only metrics exposure, safe structured logs, and alert metric conditions. No test should inspect implementation-private cache or session representations.

## Out of Scope

- Installing, operating, backing up, or administering Keycloak.
- Kubernetes, Git, Argo CD, or Tailscale mutations from the Portal.
- Automatic publication, annotations editing, DNS/certificate/ACL management, or discovery of Services, Pods, or non-HTTP protocols.
- Target application health probing, favicon retrieval, remote asset loading, SSRF-capable requests, favorites, analytics, notifications, or Internet/LAN exposure.
- Fine-grained target authorization beyond the Portal's catalog visibility rules.
- Multi-replica high availability, shared session storage, persistent catalogue storage, and automatic image promotion.

## Further Notes

- `available` is catalogue availability: valid Publication metadata plus the current validated Tailscale hostname. It is not target application health.
- Values such as Keycloak issuer URL, client secret, session-key material, Vault paths, registry address, and metrics routing are deployment configuration. Secrets must flow only through Vault/VSO and never Git, logs, ConfigMaps, or command lines.
- The Portal's own Ingress must not create a recursive Catalog item.
