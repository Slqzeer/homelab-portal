# Homelab Portal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a single, read-only Go BFF and embedded static UI that shows only the catalogue items a Tailnet visitor is authorized to receive, with secure OIDC sessions and GitOps deployment.

**Architecture:** `cmd/portal` composes configuration, an Ingress-backed atomic catalogue, OIDC/session services, metrics, and the `net/http` BFF. `web/` builds local Astro/Tailwind/native-JavaScript assets which are embedded into the binary and rendered by the BFF; no browser token or catalogue API exists. Kubernetes manifests package the application boundary while the separate homelab GitOps repository owns namespace/bootstrap, the centralized Tailscale Ingress, Argo Application, VSO secret source, and published target annotations.

**Tech Stack:** Go (`net/http`, `slog`, `client-go`, `go-oidc`), Astro, Tailwind CSS, native browser JavaScript, Kubernetes v1.36.4+k3s1, Kustomize, Argo CD, Vault Secrets Operator, Prometheus, GitHub Actions, Docker Buildx, Syft, Trivy, Cosign.

**Spec:** `docs/specs/2026-09-17-homelab-portal-implementation.md` (canonical issue #1); supporting design: `docs/superpowers/specs/2026-09-16-homelab-portal-design.md`

## Global Constraints

- Discover only `networking.k8s.io/v1` Ingresses with `spec.ingressClassName: tailscale`; never read or mutate another Kubernetes resource.
- A catalogue target is only `https://<the single validated LoadBalancer hostname>`; reject IPs, zero/multiple hostnames, inferred URLs, and all target probes.
- Metadata is deny-by-default: only exact `portal.homelab.io/enabled: "true"` with a valid publication may produce a catalogue item; exclude this portal's own Ingress.
- Browser responses must contain only already-authorized names, metadata, and URLs. The browser receives no OAuth, refresh, or session token.
- Sessions use current/previous rotating keys, `Secure`, `HttpOnly`, `SameSite=Lax` cookies, a four-hour absolute TTL, and 30-minute idle TTL. The only admin group is exact, case-sensitive `portal-admin`.
- A cache becomes stale after 2 minutes and expired after 15; retain its last valid links, show stale status, and fail readiness before the first list succeeds or after expiry.
- V1 has one replica, no database/Redis/PVC, no Node runtime, no CDN, no client framework, no public JSON API, and no multi-replica support.
- Release `linux/amd64` and `linux/arm64` immutable images. The final container is distroless/non-root with a read-only root filesystem.
- Never store secrets in Git, ConfigMaps, logs, URLs, command lines, or browser-delivered content. VSO supplies the client secret and session key material.

---

## Planned file structure

| Area | Files | Responsibility |
| --- | --- | --- |
| Domain catalogue | `internal/catalog/{model,publication,visibility,store}.go` | Validate publications, derive safe URLs, filter/sort an immutable snapshot, and report diagnostics. |
| Kubernetes adapter | `internal/kube/{client,watcher}.go` | List/watch Ingresses with bounded initialization and backoff while preserving the last valid snapshot. |
| Identity | `internal/auth/{oidc,session,ratelimit}.go` | OIDC code+PKCE contract, encrypted/signed local session, CSRF state, logout, and login throttling. |
| HTTP BFF | `internal/http/{server,headers,templates,health,metrics}.go` | Server-render authorized HTML, internal operational endpoints, safe headers/logging, and Prometheus metrics. |
| Web | `web/{package.json,astro.config.mjs,tailwind.config.js,src/pages/index.astro,src/pages/admin.astro,src/scripts/catalog.js,src/styles/app.css}` | Local accessible pages and client-only filtering of already-authorized cards. |
| Runtime/build | `cmd/portal/main.go`, `Dockerfile`, `.dockerignore`, `go.mod`, `go.sum`, `.github/workflows/{test,release}.yaml` | One embedded deployable and verified, signed multi-arch releases. |
| Kubernetes package | `deploy/base/*.yaml`, `deploy/overlays/homelab/kustomization.yaml` | Least-privilege service deployment, VSO consumption, policies, alerts, and immutable image selection. |
| Verification | `internal/**/**_test.go`, `tests/{integration,contract,manifest,e2e}` | Behavior-first Go, OIDC, fake-watch, manifest/RBAC, and cluster acceptance coverage. |

### Task 1: Establish the reproducible application and web build

**Files:**
- Create: `go.mod`, `go.sum`, `cmd/portal/main.go`, `internal/config/config.go`, `internal/assets/assets.go`
- Create: `web/package.json`, `web/package-lock.json`, `web/astro.config.mjs`, `web/tailwind.config.js`, `web/src/styles/app.css`
- Create: `Makefile`, `.gitignore`, `.dockerignore`, `Dockerfile`
- Test: `internal/config/config_test.go`, `internal/assets/assets_test.go`

**Interfaces:**
- Produces `config.Load(env []string) (config.Config, error)` and `assets.FS() fs.FS`.
- Produces the executable contract `portal` with `PORT`, `PORTAL_BASE_URL`, `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_GROUPS_CLAIM`, `OIDC_CLIENT_SECRET_FILE`, `SESSION_CURRENT_KEY_FILE`, `SESSION_PREVIOUS_KEY_FILE`, and `PORTAL_INGRESS_NAMESPACE`/`PORTAL_INGRESS_NAME` configuration.

- [ ] **Step 1: Write failing config tests for required values and safe defaults.**

```go
func TestLoadRejectsMissingRequiredOIDCValues(t *testing.T) {
    _, err := Load([]string{"PORT=8080"})
    require.ErrorContains(t, err, "OIDC_ISSUER_URL")
}

func TestLoadUsesFixedCacheAndSessionDurations(t *testing.T) {
    cfg, err := Load(validEnv())
    require.NoError(t, err)
    assert.Equal(t, 2*time.Minute, cfg.CacheStaleAfter)
    assert.Equal(t, 15*time.Minute, cfg.CacheExpireAfter)
    assert.Equal(t, 4*time.Hour, cfg.SessionAbsoluteTTL)
    assert.Equal(t, 30*time.Minute, cfg.SessionIdleTTL)
}
```

- [ ] **Step 2: Run `go test ./internal/config` and confirm it fails because the package does not yet exist.**

- [ ] **Step 3: Add the Go module, pinned dependencies, and `config.Load`.** Use a supported Go release selected by the project toolchain; parse only allowlisted configuration, validate HTTPS base/issuer URLs, read secret values from files, reject an absent current session key, and make all durations constants rather than environment overrides.

- [ ] **Step 4: Create the static web build and embedded asset seam.** Configure Astro static output to `web/dist`, Tailwind to emit local CSS, and `go:embed` to embed the build output. Add a minimal home shell, skip link, semantic `main`, local stylesheet/script references, and no remote imports. `make web-build`, `make test`, `make lint`, and `make build` must be the documented local commands.

- [ ] **Step 5: Add a multi-stage Dockerfile.** Build web assets in a pinned Node builder, compile Go for `TARGETOS`/`TARGETARCH`, then copy only `/portal` into a pinned distroless non-root final image. Set `USER nonroot:nonroot`, `EXPOSE 8080`, and no shell/package manager.

- [ ] **Step 6: Run `npm ci && npm run build`, `go test ./...`, and `docker build .`; confirm the binary serves the embedded build.**

- [ ] **Step 7: Commit.**

```bash
git add go.mod go.sum cmd internal web Makefile Dockerfile .dockerignore .gitignore
git commit -m "chore: scaffold embedded portal application"
```

### Task 2: Model and validate publication metadata

**Files:**
- Create: `internal/catalog/model.go`, `internal/catalog/publication.go`, `internal/catalog/publication_test.go`, `internal/catalog/visibility.go`, `internal/catalog/visibility_test.go`

**Interfaces:**
- Produces `ParseIngress(ing networkingv1.Ingress, portalIngress types.NamespacedName) (Candidate, *Diagnostic)`.
- Produces `CatalogItem{ID, Namespace, Ingress, Name, Description, Category, Icon, Access, Groups, Order, TargetURL}` and `Diagnostic{Namespace, Ingress, Rule, Remediation}`.
- Produces `Visible(items []CatalogItem, identity *Identity) []CatalogItem`, where `Identity` has `Authenticated bool`, `Groups map[string]struct{}`, and `IsAdmin bool`.

- [ ] **Step 1: Write table-driven failing tests for every metadata boundary.** Cover non-Tailscale Ingress, disabled/missing enabled annotation, empty/over-80 name, over-240 description, over-40 category, unknown icon fallback, invalid/missing access, allowed/default order, out-of-range/non-integer order, group-only CSV rules (trimmed exact names, no empties/duplicates), forbidden groups on other access types, portal self-ingress exclusion, zero/multiple/IP hostname rejection, and `https://hostname` derivation.

```go
func TestParseIngressRejectsTwoHostnames(t *testing.T) {
    ing := publishedIngress("tools", "grafana", "public")
    ing.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{
        {Hostname: "a.ts.net"}, {Hostname: "b.ts.net"},
    }
    _, diagnostic := ParseIngress(ing, types.NamespacedName{})
    require.NotNil(t, diagnostic)
    assert.Equal(t, "exactly_one_hostname", diagnostic.Rule)
}
```

- [ ] **Step 2: Run `go test ./internal/catalog` and confirm failure.**

- [ ] **Step 3: Implement strict parsing.** Treat an annotation value as untrusted input; trim human labels before limits, accept only local icon IDs from a fixed Go map and normalize unknown values to `generic`, use `net/url` to construct only `https` with the validated hostname, and never preserve arbitrary annotations in an item or diagnostic.

- [ ] **Step 4: Implement visibility and deterministic sorting.** `public` is unconditional; `authenticated` needs a valid identity; `groups` needs a non-empty exact set intersection; `admin` needs `portal-admin`. Sort visible items by `Order`, `Category`, then `Name`; test an anonymous user, ordinary member, non-member, and admin.

- [ ] **Step 5: Run `go test ./internal/catalog` and `go test ./...`; confirm all edge cases pass.**

- [ ] **Step 6: Commit.**

```bash
git add internal/catalog
git commit -m "feat: validate ingress publications"
```

### Task 3: Maintain a last-valid Kubernetes catalogue

**Files:**
- Create: `internal/catalog/store.go`, `internal/catalog/store_test.go`
- Create: `internal/kube/client.go`, `internal/kube/watcher.go`, `internal/kube/watcher_test.go`

**Interfaces:**
- Consumes `catalog.ParseIngress` and a narrow `IngressSource` interface: `List(ctx context.Context) (*networkingv1.IngressList, error)` and `Watch(ctx context.Context, resourceVersion string) (watch.Interface, error)`.
- Produces `Store.Snapshot(now time.Time) catalog.Snapshot` with `Items`, `Diagnostics`, `LastSuccess`, `Stale`, `Expired`, and `Initialized`.
- Produces `Watcher.Run(ctx context.Context)`; it has no write-capable Kubernetes client methods.

- [ ] **Step 1: Write failing integration tests using a fake list/watch source.** Assert initial list has a 10-second context deadline; add/update/delete events atomically replace the entire candidate view; a disconnect retains the previous view; `apierrors.IsResourceExpired` triggers relist; retry delay stays within 1–30 seconds (inject jitter/clock); stale at 2 minutes; expired at 15; and no successful initial list yields `Initialized == false`.

- [ ] **Step 2: Run `go test ./internal/kube ./internal/catalog` and confirm failure.**

- [ ] **Step 3: Implement `Store` as a copy-on-write immutable snapshot.** Rebuild candidates/diagnostics from a complete list or watch event state, then atomically swap the snapshot. Do not expose maps/slices that callers can mutate, and do not erase the last successful snapshot on list/watch failure.

- [ ] **Step 4: Implement the watcher loop with `context`, Kubernetes `ListOptions.ResourceVersion`, relist after 410 Gone, capped exponential backoff plus injectable jitter, and structured safe logs.** Log only event/result/count/resource namespace/name where diagnostic policy allows; never log annotations, hostname URLs, identity, cookies, or tokens.

- [ ] **Step 5: Run `go test ./internal/kube ./internal/catalog -race` and `go test ./...`; confirm race-free behavior.**

- [ ] **Step 6: Commit.**

```bash
git add internal/catalog/store.go internal/catalog/store_test.go internal/kube
git commit -m "feat: watch eligible ingresses into catalog"
```

### Task 4: Implement OIDC login, local sessions, and throttling

**Files:**
- Create: `internal/auth/session.go`, `internal/auth/session_test.go`, `internal/auth/oidc.go`, `internal/auth/oidc_test.go`, `internal/auth/ratelimit.go`, `internal/auth/ratelimit_test.go`
- Create: `tests/contract/oidc_test.go`

**Interfaces:**
- Produces `SessionManager.Load(*http.Request, now time.Time) (*catalog.Identity, error)`, `Create(http.ResponseWriter, Claims, now)`, and `Destroy(http.ResponseWriter)`.
- Produces `OIDC.LoginURL(state, nonce, verifier string) string` and `OIDC.Callback(ctx, code, nonce) (Claims, error)`.
- Produces `LoginLimiter.Allow(clientIP string, now time.Time) (allowed bool, retryAfter time.Duration)`.

- [ ] **Step 1: Write failing signed-fixture tests.** Verify a valid issuer/audience/signature/expiry/groups claim is accepted and wrong issuer, audience, signature, expired token, absent group claim, and non-`[]string` group claim are rejected. Test authorization-code state/nonce/PKCE verifier correlation and callback errors without echoing the OIDC code.

- [ ] **Step 2: Write failing session tests.** Verify cookies are encrypted/signed, `Secure`, `HttpOnly`, and `SameSite=Lax`; current and previous keys both read; a session obeys four-hour absolute and 30-minute idle expiry; logout expires the cookie; and a local valid session continues to authorize without a live provider call.

- [ ] **Step 3: Write failing limiter tests.** Ten login starts from one trusted client IP in five minutes pass, the eleventh receives a ten-minute `Retry-After`, and another IP is unaffected. Define trusted proxy handling in deployment: use `RemoteAddr` unless the Service is configured with an explicit trusted proxy CIDR; never trust arbitrary `X-Forwarded-For`.

- [ ] **Step 4: Run `go test ./internal/auth ./tests/contract` and confirm failure.**

- [ ] **Step 5: Implement Authorization Code + PKCE.** Use `go-oidc` provider verification and an OAuth2 confidential-client exchange. Keep raw tokens local only for callback validation and discard them before session creation. Persist only subject-independent authorization facts necessary for the local session (authenticated flag, exact group set, issued/last-seen/absolute-expiry); do not persist a refresh token.

- [ ] **Step 6: Implement the rotating-key authenticated-encryption cookie and bounded in-memory login limiter.** Reject malformed, expired, tampered, or unknown-key cookies as anonymous, clear them, and never log their contents. Expire the cookie on logout with the same name/path/domain attributes.

- [ ] **Step 7: Run `go test ./internal/auth ./tests/contract`, `go test ./...`, and `go vet ./...`; confirm all identity contracts pass.**

- [ ] **Step 8: Commit.**

```bash
git add internal/auth tests/contract
git commit -m "feat: add oidc portal sessions"
```

### Task 5: Deliver the secured BFF HTTP contract and operations endpoints

**Files:**
- Create: `internal/http/server.go`, `internal/http/headers.go`, `internal/http/health.go`, `internal/http/metrics.go`, `internal/http/templates.go`
- Create: `internal/http/server_test.go`, `internal/http/health_test.go`, `internal/http/metrics_test.go`
- Modify: `cmd/portal/main.go`

**Interfaces:**
- Consumes `catalog.Store`, `auth.SessionManager`, `auth.OIDC`, `auth.LoginLimiter`, `assets.FS`, and a Prometheus registry.
- Produces routes: `GET /`, `GET /auth/login`, `GET /auth/callback`, `POST /auth/logout`, `GET /admin`, `GET /healthz`, `GET /readyz`, `GET /metrics`, and embedded static assets only.

- [ ] **Step 1: Write behavior-level route tests before handlers.** Assert anonymous HTML has only public card text/URLs; authenticated users get public+authenticated; group members get only matching group cards; non-members lack both hidden names and URLs; admins get admin cards and `/admin`; non-admin `/admin` gets 404 (not a reveal). Test stale banner, pre-initial-list and expired `503 /readyz`, `200 /healthz`, and Prometheus output.

- [ ] **Step 2: Add failing security tests for every response.** Require CSP with local sources and `frame-ancestors 'none'`, HSTS, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`. Require CSRF/origin validation on `POST /auth/logout`, state validation on callback, no cache for per-session pages, and a visible `429` login response with `Retry-After`.

- [ ] **Step 3: Run `go test ./internal/http` and confirm failure.**

- [ ] **Step 4: Implement server-rendered routes.** Build each home response from `Visible(store.Snapshot(), identity)` immediately before rendering. Render admin diagnostics only after exact admin authorization, with only namespace, ingress, allowlisted rule, remediation, and watcher status. Do not create JSON endpoints or include hidden data in attributes/scripts.

- [ ] **Step 5: Implement operational behavior.** `/healthz` confirms process responsiveness only; `/readyz` is successful only after a non-expired initialized snapshot. Export catalogue age/state/count, invalid-publication count, watch reconnect duration, login outcomes, and authorization denials. Use JSON `slog` with request ID/event/result only and redaction-aware middleware.

- [ ] **Step 6: Wire graceful HTTP shutdown with a 20-second deadline, startup of the watcher, and signal cancellation in `main`.**

- [ ] **Step 7: Run `go test ./internal/http ./...` and confirm all HTTP assertions pass.**

- [ ] **Step 8: Commit.**

```bash
git add cmd/portal/main.go internal/http
git commit -m "feat: serve authorized portal catalog"
```

### Task 6: Complete the accessible, local-only web experience

**Files:**
- Create: `web/src/pages/index.astro`, `web/src/pages/admin.astro`, `web/src/components/{CatalogCard,CategoryFilter,StaleBanner}.astro`, `web/src/scripts/catalog.js`
- Modify: `web/src/styles/app.css`, `web/package.json`
- Test: `web/tests/catalog.test.mjs`, `tests/integration/rendered_ui_test.go`

**Interfaces:**
- Consumes only the server-rendered `article[data-catalog-item]` cards and their visible name/description/category text.
- Produces keyboard-accessible client-side search/category filtering; it must not fetch catalogue data or alter authorization.

- [ ] **Step 1: Write browser-level failing tests.** Given a rendered allowed-card fixture, assert search matches name/description/category case-insensitively, category buttons expose pressed state, clearing filters restores visible cards, focus remains usable, and no network request occurs. Test narrow viewport layout and visible focus styles with an accessibility checker.

- [ ] **Step 2: Run `npm test` and confirm failure.**

- [ ] **Step 3: Implement the page components and native filtering.** Use semantic headings, labeled search, buttons for category filters, live count/status text, accessible card links, and responsive CSS. Render all copy text-escaped; use only embedded SVG/icon assets and a generic icon fallback. Present stale state as catalogue freshness, never target health.

- [ ] **Step 4: Add rendered-HTML integration coverage.** Verify the login/logout and admin controls vary only with the BFF-provided identity, hidden cards cannot become visible by filter interaction, and all assets are same-origin.

- [ ] **Step 5: Run `npm run build && npm test`, `go test ./tests/integration`, and `go test ./...`.**

- [ ] **Step 6: Commit.**

```bash
git add web tests/integration
git commit -m "feat: add accessible catalog interface"
```

### Task 7: Package the restricted Kubernetes deployment and alerting

**Files:**
- Create: `deploy/base/{namespace,serviceaccount,rbac,configmap,vso-secret,deployment,service,networkpolicy,servicemonitor,prometheusrule,kustomization}.yaml`
- Create: `deploy/overlays/homelab/{kustomization,image-patch}.yaml`
- Create: `tests/manifest/policy_test.go`, `tests/manifest/rbac_test.go`

**Interfaces:**
- Consumes image `registry.example/homelab-portal@sha256:...` via the overlay, a VSO-generated Secret named `homelab-portal-secrets`, and non-secret ConfigMap values.
- Produces a `portal` Namespace, one `homelab-portal` Deployment/ServiceAccount/Service, cross-namespace Ingress-only ClusterRole binding, internal metrics/probes, and alert rules.

- [ ] **Step 1: Write failing manifest tests.** Parse rendered Kustomize output and assert one replica; 25m/48Mi requests, 100m/128Mi limits; 20-second termination; non-root/read-only/no privilege escalation/all capabilities dropped/RuntimeDefault; no PVC; and `/healthz`, `/readyz`, `/metrics` absent from any Ingress manifest.

- [ ] **Step 2: Add RBAC-negative tests.** Assert the only rule is `networking.k8s.io`, resource `ingresses`, verbs `get,list,watch`; explicitly assert no Secret read and no Ingress create/update/patch/delete. Use `kubectl auth can-i` equivalents in the cluster acceptance script as a second check.

- [ ] **Step 3: Write failing policy/alert tests.** Require default-deny ingress/egress, ingress exceptions only for named Tailscale proxy and metrics-scraper labels, egress exceptions only for kube-dns, Kubernetes API, and configured Keycloak selector/port; require alerts for failed initial list, expired cache, watch reconnecting more than two minutes, and persistent invalid metadata.

- [ ] **Step 4: Run `go test ./tests/manifest` and confirm failure.**

- [ ] **Step 5: Write the Kustomize package.** Mount VSO secret files read-only, set only non-secret environment variables in ConfigMap, permit an `emptyDir` only where the hardened runtime needs writable temporary storage, and target the internal Service for probes/ServiceMonitor. Keep the portal public endpoint out of this repository's application manifests.

- [ ] **Step 6: Run `kustomize build deploy/overlays/homelab`, `go test ./tests/manifest`, and a schema validator against Kubernetes v1.36.4; confirm the rendered package passes.**

- [ ] **Step 7: Commit.**

```bash
git add deploy tests/manifest
git commit -m "feat: package hardened portal deployment"
```

### Task 8: Add CI and immutable supply-chain release controls

**Files:**
- Create: `.github/workflows/test.yaml`, `.github/workflows/release.yaml`, `.github/dependabot.yml`
- Create: `scripts/{verify,release}.sh`, `docs/runbooks/release.md`
- Test: `tests/integration/release_contract_test.go`

**Interfaces:**
- Produces CI artifacts: tested source, multi-platform image digest, CycloneDX/SPDX SBOM, Trivy report, and Cosign signature/attestation bound to that digest.
- Consumes only immutable Git refs/tags and registry credentials provided by GitHub Actions secrets/OIDC, never repository files.

- [ ] **Step 1: Write failing workflow/contract tests.** Assert pull requests run web lockfile install/build/test, Go test/race/vet, manifest tests, image build, and vulnerability scan; assert release runs Buildx for `linux/amd64,linux/arm64`, pushes an immutable tag/digest, creates an SBOM, scans the pushed artifact, and signs the digest before publishing deployment instructions.

- [ ] **Step 2: Run `go test ./tests/integration -run Release` and confirm failure.**

- [ ] **Step 3: Implement CI.** Pin actions by commit digest, use least-privilege workflow permissions, cache only dependency stores, fail on an uncommitted lockfile, and upload reports as artifacts. Release must gate promotion on human environment approval; it must print the resulting digest for a separate GitOps revision update, never rewrite manifests automatically.

- [ ] **Step 4: Document the exact release/rollback procedure.** Promotion is a human-approved Git change to an image digest; rollback is a revert to the previous Git revision/digest. Include verification of signature, SBOM, scan policy, and Argo state.

- [ ] **Step 5: Run the workflow linter, `go test ./tests/integration -run Release`, `go test ./...`, and `npm ci && npm run build`.**

- [ ] **Step 6: Commit.**

```bash
git add .github scripts docs/runbooks tests/integration
git commit -m "ci: add signed multiarch release pipeline"
```

### Task 9: Define and automate cluster acceptance testing

**Files:**
- Create: `tests/e2e/{README.md,portal_e2e_test.go,fixtures}.yaml`, `scripts/verify-cluster.ps1`
- Create: `docs/runbooks/acceptance.md`

**Interfaces:**
- Consumes explicit non-secret test environment variables for portal URL, admin/non-admin test credentials supplied by the CI secret store, and a disposable published fixture Ingress.
- Produces a non-destructive acceptance report; it does not create application resources through the portal.

- [ ] **Step 1: Write the e2e test matrix as executable table cases.** Cover anonymous public visibility, authenticated visibility, matching/non-matching group visibility, admin diagnostics, derived current `https` Tailscale hostname, absent unauthorized name/URL, stale/readiness state, VSO Secret synchronization, Argo `Synced/Healthy`, Tailscale proxy 128Mi cap, ServiceAccount `can-i` positive/negative permissions, and enforcement of the CNI NetworkPolicy.

- [ ] **Step 2: Make the test harness fail closed when required environment values or isolated fixtures are absent.** It must never use production identities or alter existing published Ingresses.

- [ ] **Step 3: Implement e2e flows through the actual Tailscale Ingress.** Use HTTP-level assertions, not internal cache/session fields; mask credentials/tokens in test output; tear down only fixtures that the harness created and labels with its run ID.

- [ ] **Step 4: Add the acceptance runbook.** State the prerequisites: healthy Keycloak client and group mapper, VSO secret material, deployed CNI enforcement, registry digest, Argo Application, centralized Ingress hostname, and disposable fixture namespace. Include commands and expected observable outcomes.

- [ ] **Step 5: Run compile/static checks locally; run the e2e suite only against the explicitly supplied disposable cluster and retain its report artifact.**

- [ ] **Step 6: Commit.**

```bash
git add tests/e2e scripts/verify-cluster.ps1 docs/runbooks/acceptance.md
git commit -m "test: add portal cluster acceptance suite"
```

### Task 10: Apply the cross-repository GitOps contract and release gate

**Files (homelab GitOps repository, not this repository):**
- Modify: `bootstrap/namespaces/namespaces.yaml`
- Create: `environments/homelab/apps/homelab-portal.yaml`
- Create: `infrastructure/ingress/config/portal-ingress.yaml`
- Modify: `infrastructure/ingress/config/kustomization.yaml`
- Modify: existing owner-managed published Tailscale Ingress manifests
- Test: GitOps repository Kustomize/Argo validation and in-cluster suite from Task 9

**Interfaces:**
- Consumes a signed image digest from Task 8, a pinned portal repository revision, the VSO secret contract from Task 7, a Keycloak confidential client `homelab-portal`, and designated service-owner publication decisions.
- Produces namespace `portal`, an Argo Application at sync wave 23, centralized Tailscale Ingress at wave 21 with host `portal`, and intentionally published target annotations.

- [ ] **Step 1: Record and verify external prerequisites with their owners.** Keycloak must expose the exact HTTPS callback `https://portal.<tailnet-domain>/auth/callback`, logout URL, confidential client `homelab-portal`, scopes `openid profile groups`, JSON groups claim, and exact `portal-admin` group. Vault/VSO must create current/previous session keys and client secret in the portal namespace without putting their values in Git.

- [ ] **Step 2: Add the portal namespace and Argo Application.** Point it to a reviewed, immutable portal repository revision and image digest, use sync wave `23`, and do not add dependencies on PostgreSQL or Redis.

- [ ] **Step 3: Add centralized `portal-ingress.yaml` at sync wave `21`.** Set `ingressClassName: tailscale`, `tailscale.com/proxy-class: homelab`, host `portal`, TLS as used by existing Tailscale Ingresses, and route only the user-facing BFF Service port. Ensure no operational route is separately exposed and retain the proxy 128Mi memory cap in the shared ProxyClass.

- [ ] **Step 4: Annotate only service-owner-approved Ingresses.** Apply the exact `portal.homelab.io/*` metadata contract from the spec; leave all other Ingresses unannotated. Check the portal's own Ingress is excluded by name/namespace configuration.

- [ ] **Step 5: Validate GitOps and execute the Task 9 acceptance suite.** Confirm Argo is `Synced/Healthy`, VSO reports secret synchronization without exposing values, the Ingress status contains one hostname, and every e2e acceptance case passes. If CNI enforcement cannot be demonstrated, stop release rather than claiming the declared policy is active.

- [ ] **Step 6: Promote with human approval and record the deployed Git revision/image digest; use a Git revert for rollback.**

## Plan self-review

- **Spec coverage:** Tasks 1–6 cover the single embedded BFF/UI, strict discovery, per-request visibility, diagnostics, OIDC/session behavior, stale cache, headers, throttling, metrics, and accessible filtering. Tasks 7–10 cover least-privilege Kubernetes packaging, internal-only operations routes, network controls, alerting, immutable multi-architecture release, GitOps integration, and all required acceptance tests.
- **Intentional external boundary:** The portal repository cannot itself create namespace, Argo, centralized Ingress, Keycloak, Vault, or target-owner metadata in the separate homelab repository; Task 10 names the exact changes and verification gate rather than silently treating that work as complete.
- **Placeholder scan:** No implementation task leaves validation, error handling, types, test seams, or release behavior unspecified. Deployment-specific secret values and tailnet domain remain deliberately external configuration, as required by the spec.
- **Interface consistency:** Catalogue parsing creates only safe items/diagnostics; watcher snapshots feed BFF visibility; OIDC sessions feed the same `catalog.Identity`; UI operates only on rendered allowed cards; manifest and e2e tests verify public behavior instead of private cache/session representation.
