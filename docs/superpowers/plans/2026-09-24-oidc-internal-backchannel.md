# OIDC Internal Backchannel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep Keycloak's canonical browser and token issuer on public HTTPS while routing every portal-to-Keycloak OIDC request through the exact in-cluster HTTP Service.

**Architecture:** Add a validated `OIDC_BACKCHANNEL_URL` and a dedicated HTTP client whose transport rewrites only URLs beneath the canonical issuer prefix to the internal prefix. Continue normal `go-oidc` discovery and issuer verification, validate every discovered endpoint before use, retain the public authorization URL, and narrow Kubernetes egress to the observed Keycloak pods on TCP 8080.

**Tech Stack:** Go 1.26, `github.com/coreos/go-oidc/v3` v3.21.0, `golang.org/x/oauth2` v0.37.0, Kubernetes 1.36 manifests, Kustomize 5.7.1, Testify, GitHub Actions, BuildKit, Trivy, Cosign.

**Spec:** `docs/superpowers/specs/2026-09-24-oidc-backchannel-design.md`

## Global Constraints

- `OIDC_ISSUER_URL` remains an absolute HTTPS URL and the sole issuer used for discovery comparison and ID-token `iss` validation.
- Production issuer is exactly `https://keycloak.taildf6cd4.ts.net/realms/homelab`.
- Production backchannel is exactly `http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab`.
- `OIDC_BACKCHANNEL_URL` permits only absolute HTTP or HTTPS URLs without credentials, query, or fragment and with the same normalized realm path as the issuer.
- Equal issuer and backchannel origins remain valid for local/test fixtures; the homelab manifest requires distinct origins.
- Browser authorization always uses the public HTTPS endpoint; discovery, token, JWKS, and future userinfo traffic use the dedicated backchannel client.
- Discovery issuer equality, signature, audience, nonce, state, and PKCE validation must not be weakened.
- Redirects and requests outside the exact issuer prefix fail closed; failures expose no upstream URL, response, code, token, secret, or header to the browser or logs.
- The only Keycloak egress is namespace `keycloak`, pod label `app: keycloak`, TCP port `8080`; do not add general HTTP or Internet egress.
- Secrets continue to enter only through file-backed VSO projections; `OIDC_BACKCHANNEL_URL` is non-secret ConfigMap data.
- Implementation is test-driven, reviewed before tagging, released as immutable `v0.0.5`, evidence-reviewed, then pinned by unique workflow tag and digest in a manifest-only commit.

## Review Focus

- A sibling path such as `/realms/homelab-evil/token` must be rejected instead of matching the issuer prefix; Task 2 adds an explicit rewrite-table case.
- Encoded separators or dot segments must not let a discovery endpoint escape the approved realm; Tasks 1 and 2 add encoded-path and cleaned-path rejection cases.
- A redirect from the approved internal Service must fail without following it or forwarding credentials; Task 2 records both servers and asserts zero requests at the redirect target.
- Missing, relative, malformed, or off-origin authorization/token/JWKS/userinfo metadata must stop startup; Task 3 covers every endpoint class.
- Callback cancellation or an unavailable token/JWKS endpoint must return only `portal login failed`, while an already-issued local session retains its existing bounded behavior; Task 3 extends the contract fixture for both conditions.

---

### Task 1: Validate and normalize the backchannel configuration

**Files:**
- Modify: `internal/config/config.go:26-122`
- Modify: `internal/config/config_test.go:12-130`

**Interfaces:**
- Consumes: environment entries parsed by `config.Load(env []string) (Config, error)`.
- Produces: `Config.OIDCBackchannelURL string`, containing a normalized URL without a trailing slash; `OIDCIssuerURL` is normalized by the same path-safe parser.

- [ ] **Step 1: Write failing required-value and success tests**

Add `OIDC_BACKCHANNEL_URL=https://id.example.test/realms/homelab` to `validEnv`. Extend `TestLoadRejectsMissingRequiredOIDCValues` to remove each OIDC URL independently, and assert the parsed field in the success test:

```go
func TestLoadRejectsMissingRequiredOIDCValues(t *testing.T) {
	for _, name := range []string{"OIDC_ISSUER_URL", "OIDC_BACKCHANNEL_URL"} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(withoutEnv(validEnv(t), name))
			require.ErrorContains(t, err, name)
		})
	}
}

// In TestLoadRequiresHTTPSBaseAndIssuerURLs after loading validEnv:
assert.Equal(t, "https://id.example.test/realms/homelab", cfg.OIDCBackchannelURL)
```

- [ ] **Step 2: Run the focused tests and observe the red state**

Run: `go test ./internal/config -run 'TestLoadRejectsMissingRequiredOIDCValues|TestLoadRequiresHTTPSBaseAndIssuerURLs' -count=1`

Expected: FAIL because `Config.OIDCBackchannelURL` and its required check do not exist.

- [ ] **Step 3: Write failing validation tables**

Add a table proving HTTP is accepted only for the backchannel, trailing slashes normalize, realm paths match, and unsafe forms fail:

```go
func TestLoadValidatesOIDCBackchannelURL(t *testing.T) {
	env := validEnv(t)
	for _, value := range []string{
		"http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab",
		"https://id.example.test/realms/homelab/",
	} {
		cfg, err := Load(replaceEnv(env, "OIDC_BACKCHANNEL_URL", value))
		require.NoError(t, err)
		require.Equal(t, strings.TrimSuffix(value, "/"), cfg.OIDCBackchannelURL)
	}

	for _, value := range []string{
		"ftp://id.example.test/realms/homelab",
		"http://user@id.example.test/realms/homelab",
		"http://id.example.test/realms/homelab?secret=value",
		"http://id.example.test/realms/homelab#fragment",
		"http://id.example.test/realms/other",
		"http://id.example.test/realms/homelab/../other",
		"http://id.example.test/realms%2fhomelab",
		"//id.example.test/realms/homelab",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(replaceEnv(env, "OIDC_BACKCHANNEL_URL", value))
			require.ErrorContains(t, err, "OIDC_BACKCHANNEL_URL")
			require.NotContains(t, err.Error(), value)
		})
	}
}
```

Also add issuer cases for a trailing slash, encoded separator, and dot segment so both prefixes have one canonical path representation.

- [ ] **Step 4: Run the validation test and observe the red state**

Run: `go test ./internal/config -run TestLoadValidatesOIDCBackchannelURL -count=1`

Expected: FAIL because the backchannel value is not parsed or validated.

- [ ] **Step 5: Implement one URL parser and the new field**

Add `OIDCBackchannelURL string` beside `OIDCIssuerURL`. Keep the existing `httpsURL` helper for the root-valued `PORTAL_BASE_URL`. Add an OIDC-specific helper that requires a canonical non-root realm path, rejects opaque/user/query/fragment forms, trims one or more trailing slashes, and accepts an explicit scheme set:

```go
func oidcEndpointURL(values map[string]string, name string, schemes ...string) (string, error) {
	raw := values[name]
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "", fmt.Errorf("%s must be an absolute canonical URL with an approved scheme and no credentials, query, or fragment", name)
	}
	allowed := false
	for _, scheme := range schemes {
		allowed = allowed || parsed.Scheme == scheme
	}
	canonicalPath := strings.TrimRight(parsed.Path, "/")
	if !allowed || parsed.Host == "" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		canonicalPath == "" || path.Clean(canonicalPath) != canonicalPath {
		return "", fmt.Errorf("%s must be an absolute canonical URL with an approved scheme and no credentials, query, or fragment", name)
	}
	parsed.Path = canonicalPath
	return parsed.String(), nil
}
```

Call it with `("https")` for the issuer and `("http", "https")` for the backchannel. Continue calling `httpsURL` for the portal base URL. Parse both normalized OIDC URLs and require `issuer.Path == backchannel.Path`; return an `OIDC_BACKCHANNEL_URL` error without echoing either value. Populate `Config.OIDCBackchannelURL`.

- [ ] **Step 6: Run package tests and commit**

Run: `gofmt -w internal/config/config.go internal/config/config_test.go && go test ./internal/config -count=1`

Expected: PASS.

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(auth): validate OIDC backchannel URL"
```

### Task 2: Build a fail-closed issuer-to-backchannel transport

**Files:**
- Create: `internal/auth/backchannel.go`
- Create: `internal/auth/backchannel_test.go`

**Interfaces:**
- Consumes: normalized issuer/backchannel strings from Task 1.
- Produces: `newBackchannelClient(issuer, backchannel string, timeout time.Duration) (*http.Client, error)` and a transport that preserves suffix/query while rewriting only exact-prefix requests.

- [ ] **Step 1: Write the failing rewrite table**

Use a recording `RoundTripper` and table-driven tests around an unexported `rewriteBackchannelURL(target, issuer, backchannel *url.URL) (*url.URL, error)`:

```go
func TestRewriteBackchannelURLAcceptsOnlyCanonicalIssuerPrefix(t *testing.T) {
	issuer := mustURL(t, "https://keycloak.taildf6cd4.ts.net/realms/homelab")
	backchannel := mustURL(t, "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab")
	cases := []struct{ name, target, want string; rejected bool }{
		{"discovery", "https://keycloak.taildf6cd4.ts.net/realms/homelab/.well-known/openid-configuration", "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab/.well-known/openid-configuration", false},
		{"query", "https://keycloak.taildf6cd4.ts.net/realms/homelab/token?mode=code", "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab/token?mode=code", false},
		{"sibling prefix", "https://keycloak.taildf6cd4.ts.net/realms/homelab-evil/token", "", true},
		{"wrong host", "https://attacker.example/realms/homelab/token", "", true},
		{"wrong scheme", "http://keycloak.taildf6cd4.ts.net/realms/homelab/token", "", true},
		{"encoded separator", "https://keycloak.taildf6cd4.ts.net/realms/homelab%2ftoken", "", true},
		{"dot segment", "https://keycloak.taildf6cd4.ts.net/realms/homelab/../other", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteBackchannelURL(mustURL(t, tc.target), issuer, backchannel)
			if tc.rejected {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
		})
	}
}
```

- [ ] **Step 2: Run the focused transport test and observe the red state**

Run: `go test ./internal/auth -run TestRewriteBackchannelURLAcceptsOnlyCanonicalIssuerPrefix -count=1`

Expected: FAIL because the helper is undefined.

- [ ] **Step 3: Implement canonical exact-prefix rewriting**

In `backchannel.go`, parse both roots defensively. Reject a target unless scheme and host exactly equal the issuer, `RawPath` is empty, `path.Clean(target.Path) == target.Path`, and its path is either `issuer.Path` or begins `issuer.Path + "/"`. Clone the target and replace only the approved prefix:

```go
suffix := strings.TrimPrefix(target.Path, issuer.Path)
rewritten := *target
rewritten.Scheme = backchannel.Scheme
rewritten.Host = backchannel.Host
rewritten.Path = backchannel.Path + suffix
rewritten.RawPath = ""
return &rewritten, nil
```

Do not copy issuer userinfo or fragments; configuration has already rejected them.

- [ ] **Step 4: Write failing request-integrity, timeout, and redirect tests**

Add tests that assert `RoundTrip` leaves the caller's `Request`, URL, and Host unchanged; sends the clone to the internal authority; preserves query values; and propagates context cancellation. Add two HTTP servers where the internal server redirects to a second server, then assert the client returns an error and the second server receives zero requests, even when the original request has `Authorization` set.

```go
func TestBackchannelClientRejectsRedirectWithoutForwardingCredentials(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetRequests.Add(1) }))
	defer target.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer internal.Close()
	client, err := newBackchannelClient("https://public.example/realms/homelab", internal.URL+"/realms/homelab", time.Second)
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodPost, "https://public.example/realms/homelab/token", nil)
	req.Header.Set("Authorization", "private")
	_, err = client.Do(req)
	require.Error(t, err)
	require.Zero(t, targetRequests.Load())
}
```

- [ ] **Step 5: Implement the immutable request transport and client**

Define `backchannelTransport` with parsed roots and a cloned `http.DefaultTransport`. Its `RoundTrip` clones the request, assigns the rewritten URL, clears `clone.Host` so Go emits the internal authority, and delegates only the clone. Return generic errors that contain neither URL. Construct the client as:

```go
return &http.Client{
	Transport: transport,
	Timeout: timeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("OIDC redirect rejected")
	},
}, nil
```

- [ ] **Step 6: Run transport tests and commit**

Run: `gofmt -w internal/auth/backchannel.go internal/auth/backchannel_test.go && go test ./internal/auth -run 'TestRewriteBackchannelURL|TestBackchannelClient' -count=1`

Expected: PASS.

```bash
git add internal/auth/backchannel.go internal/auth/backchannel_test.go
git commit -m "feat(auth): add strict OIDC backchannel transport"
```

### Task 3: Route OIDC discovery, token exchange, and JWKS through the client

**Files:**
- Modify: `internal/auth/oidc.go:18-120`
- Modify: `internal/auth/oidc_test.go:1-45`
- Modify: `tests/contract/oidc_test.go:19-210`
- Modify: `cmd/portal/main.go:52-70`

**Interfaces:**
- Consumes: `newBackchannelClient` from Task 2 and `Config.OIDCBackchannelURL` from Task 1.
- Produces: `OIDC.client *http.Client`; `validateProviderEndpoints(provider *oidc.Provider, issuer string) error`; unchanged public `NewOIDC`, `LoginURL`, and `Callback` APIs.

- [ ] **Step 1: Replace the fixture with distinct public and internal origins**

Change `providerFixture` to expose a canonical `publicIssuer` such as `https://public.example/realms/homelab`, while its `httptest.Server` handles discovery, `/token`, and `/keys`. Discovery must return the public issuer and public endpoint URLs. Pass `OIDCBackchannelURL: f.server.URL + "/realms/homelab"` to `NewOIDC`, and record paths/hosts seen by the internal server.

Extend `TestLoginURLUsesAuthorizationCodePKCEAndCorrelatedStateNonce` to assert the returned authorization URL has host `public.example`, while the discovery counter proves the request reached the internal server.

- [ ] **Step 2: Run the login and callback tests and observe the red state**

Run: `go test ./internal/auth ./tests/contract -run 'TestLoginURL|TestOIDCCallbackAcceptsSignedClaims' -count=1`

Expected: FAIL because discovery still contacts the public origin and callback contexts do not carry the dedicated client.

- [ ] **Step 3: Write failing discovery metadata tests**

Decode provider claims into this private structure after normal discovery:

```go
type providerMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURL               string `json:"jwks_uri"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}
```

Add table cases that replace each endpoint in turn with an empty string, relative path, malformed URL, wrong host, wrong scheme, sibling prefix, encoded separator, and dot segment. Every case must make `NewOIDC` return exactly `OIDC provider unavailable`, with no endpoint text in the error.

- [ ] **Step 4: Implement provider validation and client ownership**

Add `client *http.Client` to `OIDC`. In `NewOIDC`:

```go
client, err := newBackchannelClient(cfg.OIDCIssuerURL, cfg.OIDCBackchannelURL, 10*time.Second)
if err != nil { return nil, errors.New("OIDC provider unavailable") }
providerCtx := oidc.ClientContext(ctx, client)
provider, err := oidc.NewProvider(providerCtx, cfg.OIDCIssuerURL)
if err != nil { return nil, errors.New("OIDC provider unavailable") }
if err := validateProviderEndpoints(provider, cfg.OIDCIssuerURL); err != nil {
	return nil, errors.New("OIDC provider unavailable")
}
```

`validateProviderEndpoints` calls `provider.Claims(&metadata)`, parses all four non-empty endpoint fields, and runs each through the same exact canonical issuer-prefix predicate used by Task 2. Keep the public endpoint strings in `oauth2.Config`; do not replace them with internal URLs.

- [ ] **Step 5: Route callback network work through the stored client**

At the start of `Callback`, after local transaction validation and before exchange, create `providerCtx := oidc.ClientContext(ctx, o.client)`. Use it for both `o.oauth.Exchange` and `o.verifier.Verify`. Keep all returned failures mapped to `ErrLoginFailed`.

In `cmd/portal/main.go`, retain the 10-second discovery context but remove the generic `oidc.ClientContext` and its now-unused imports; `NewOIDC` owns the finite-timeout client retained by remote JWKS refreshes.

- [ ] **Step 6: Add JWKS, cancellation, and generic-failure assertions**

Extend the contract fixture with discovery/token/JWKS counters. Assert a successful callback makes token and JWKS requests only to the internal server. Add an unreachable discovery Service case that makes `NewOIDC` return only `OIDC provider unavailable`. Add a cancelled callback context and separate token/JWKS outage cases; each must return `ErrLoginFailed`, contain none of the public/internal URLs or fixture credentials, and preserve the existing `TestExistingPortalSessionAuthorizesUntilLocalExpiryDuringProviderOutage` result.

- [ ] **Step 7: Run OIDC and startup tests and commit**

Run: `gofmt -w internal/auth/oidc.go internal/auth/oidc_test.go tests/contract/oidc_test.go cmd/portal/main.go && go test ./internal/auth ./tests/contract ./cmd/portal -count=1`

Expected: PASS.

```bash
git add internal/auth/oidc.go internal/auth/oidc_test.go tests/contract/oidc_test.go cmd/portal/main.go
git commit -m "feat(auth): use internal OIDC backchannel"
```

### Task 4: Pin the production configuration and network boundary

**Files:**
- Modify: `deploy/base/configmap.yaml:5-15`
- Modify: `deploy/base/networkpolicy.yaml:69-79`
- Modify: `deploy/overlays/homelab/kustomization.yaml:5-48`
- Modify: `tests/manifest/policy_test.go:68-82,267-274`
- Modify: `deploy/README.md:13-43`
- Modify: `docs/technical-stack.md:25-36`
- Modify: `docs/runbooks/release.md:65-80`

**Interfaces:**
- Consumes: the configuration contract from Task 1 and network behavior from Tasks 2–3.
- Produces: rendered production values and exact egress compatible with the original Phase 26 Task 4 GitOps Application.

- [ ] **Step 1: Write failing rendered-manifest assertions**

Extend the ConfigMap allowlist with:

```go
"OIDC_ISSUER_URL":      "https://keycloak.taildf6cd4.ts.net/realms/homelab",
"OIDC_BACKCHANNEL_URL": "http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab",
```

Replace the expected Keycloak egress rule with:

```go
{To: []networkingv1.NetworkPolicyPeer{
	peer("keycloak", map[string]string{"app": "keycloak"}),
}, Ports: []networkingv1.NetworkPolicyPort{port(corev1.ProtocolTCP, 8080)}},
```

Add negative assertions that no egress rule admits TCP 80, TCP 8443, an empty pod selector, or an `ipBlock` beyond the two exact Kubernetes API addresses.

- [ ] **Step 2: Run the manifest test and observe the red state**

Run: `go test ./tests/manifest -run 'TestSecretsStayInFiles|TestNetworkBoundary' -count=1`

Expected: FAIL because the new setting and observed Keycloak selector/port are absent.

- [ ] **Step 3: Update base and homelab manifests**

Add a non-deployable HTTPS example `OIDC_BACKCHANNEL_URL` to the base ConfigMap. Add a homelab `ConfigMap/homelab-portal` JSON patch setting the exact public issuer and internal HTTP backchannel. Change the base Keycloak peer to only `app: keycloak` and TCP 8080, then remove obsolete overlay operations that patch the former instance label and port 8443.

Do not change VSO, Secrets, ingress, DNS, or Kubernetes API egress.

- [ ] **Step 4: Update operator documentation**

Document the two-URL model in `deploy/README.md` and `docs/technical-stack.md`: browsers/tokens retain public HTTPS identity, the portal rewrites server-side calls to the in-cluster Service, and equal origins are only a local/test convenience. Update the release runbook's coordinated configuration list to require both exact URLs and the `app=keycloak` TCP 8080 egress check. State that public HTTP issuers remain invalid.

- [ ] **Step 5: Render, test, and commit**

Run:

```bash
kustomize build deploy/overlays/homelab > /tmp/portal-oidc-backchannel.yaml
test -s /tmp/portal-oidc-backchannel.yaml
go test ./tests/manifest -count=1
git diff --check
```

Expected: render succeeds, both tests/checks pass, and the render contains no `Namespace` or Secret object.

```bash
git add deploy/base/configmap.yaml deploy/base/networkpolicy.yaml deploy/overlays/homelab/kustomization.yaml tests/manifest/policy_test.go deploy/README.md docs/technical-stack.md docs/runbooks/release.md
git commit -m "deploy: route OIDC through Keycloak service"
```

### Task 5: Verify, release v0.0.5, review evidence, and pin the digest

**Files:**
- Modify after successful evidence review: `deploy/overlays/homelab/image-patch.yaml`
- Verify: all source, browser, manifest, container, workflow, provenance, SBOM, scan, and signature artifacts

**Interfaces:**
- Consumes: Tasks 1–4 and the protected `Release` workflow.
- Produces: immutable tag `v0.0.5`, a reviewed unique GHCR tag/digest, and a manifest-only application revision for the outer Phase 26 Task 4.

- [ ] **Step 1: Run the complete local source gates**

Run:

```bash
bash scripts/verify.sh source
git diff --check
git status --short
```

Expected: every web, Go, race, vet, manifest-schema, workflow, and shell gate passes; status is clean. If the known watcher timing test alone flakes under `-race`, rerun the exact race command once and record both results; any repeat failure blocks release.

- [ ] **Step 2: Request fresh whole-branch review and resolve findings**

Use `superpowers:requesting-code-review` against the range beginning after `d8aa9be`. The reviewer must compare the implementation with the design and this plan, concentrating on URL-prefix bypasses, redirect/credential leakage, provider endpoint validation, callback/JWKS client propagation, and network-policy breadth. Fix any valid finding with a failing regression test and a separate commit, then request rereview until clean.

- [ ] **Step 3: Obtain explicit owner confirmation before the immutable tag push**

Show the clean source revision and verification result. Do not create or push `v0.0.5` until the owner explicitly confirms this exact revision for release.

- [ ] **Step 4: Create and push the immutable tag**

Run:

```bash
RELEASE_SOURCE=$(git rev-parse HEAD)
test -z "$(git tag --list v0.0.5)"
git ls-remote --exit-code --tags origin refs/tags/v0.0.5 && exit 1 || test $? -eq 2
git tag v0.0.5 "$RELEASE_SOURCE"
git push origin refs/tags/v0.0.5
```

Expected: the local and remote tag both resolve to `RELEASE_SOURCE`; never move or recreate it.

- [ ] **Step 5: Wait for the protected release and download evidence**

Run:

```bash
RUN_ID=$(gh run list --workflow Release --branch v0.0.5 --event push --json databaseId,headSha,status,conclusion | jq -r --arg source "$RELEASE_SOURCE" 'map(select(.headSha == $source)) | .[0].databaseId // empty')
test -n "$RUN_ID"
gh run watch "$RUN_ID" --exit-status
EVIDENCE_DIR=$(mktemp -d /tmp/portal-v0.0.5-evidence.XXXXXX)
gh run download "$RUN_ID" --name "release-evidence-$RUN_ID-1" --dir "$EVIDENCE_DIR"
```

Expected: test, release, and promotion jobs all succeed and the evidence directory is non-empty.

- [ ] **Step 6: Review source, platform, scan, SBOM, provenance, and signature evidence**

Follow `docs/runbooks/release.md` exactly. Confirm `source-revision.txt` equals `RELEASE_SOURCE`; `image-digest.txt` matches `ghcr.io/slqzeer/homelab-portal@sha256:[a-f0-9]{64}`; both Trivy reports contain zero HIGH/CRITICAL findings; both amd64/arm64 SPDX and CycloneDX files are non-empty; the image index contains one image and linked attestation manifest for each platform; both provenance predicates name the exact repository, workflow tag, source revision, run/attempt, and BuildKit v1 build type; Cosign signature and both attestation sets verify against the workflow identity and exact digest. Any mismatch blocks pinning.

- [ ] **Step 7: Write the immutable tag-plus-digest pin only after evidence passes**

Derive and validate the one permitted image reference:

```bash
IMAGE_REF=$(tr -d '\r\n' < "$EVIDENCE_DIR/image-digest.txt")
DIGEST=${IMAGE_REF#*@}
test "$IMAGE_REF" = "ghcr.io/slqzeer/homelab-portal@$DIGEST"
printf '%s\n' "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$'
EXPECTED_IMAGE="ghcr.io/slqzeer/homelab-portal:sha-$RELEASE_SOURCE-$RUN_ID-1@$DIGEST"
```

Use `apply_patch` to replace only `spec.template.spec.containers[name=portal].image` in `deploy/overlays/homelab/image-patch.yaml` with the literal value printed by `printf '%s\n' "$EXPECTED_IMAGE"`, and update its comment to name release run `$RUN_ID`, attempt 1. Then run:

```bash
rg -F "image: $EXPECTED_IMAGE" deploy/overlays/homelab/image-patch.yaml
go test ./tests/manifest ./tests/integration -count=1
kustomize build deploy/overlays/homelab > /tmp/portal-v0.0.5-pinned.yaml
test -s /tmp/portal-v0.0.5-pinned.yaml
rg -F "image: $EXPECTED_IMAGE" /tmp/portal-v0.0.5-pinned.yaml
! rg '^kind: Namespace$' /tmp/portal-v0.0.5-pinned.yaml
git diff --check
```

Expected: tests reject tag-only, digest-only, `latest`, and zero-digest mutations; render contains the reviewed reference and no Namespace.

- [ ] **Step 8: Commit the manifest-only pin and record the handoff**

```bash
git add deploy/overlays/homelab/image-patch.yaml
git diff --cached --exit-code -- . ':(exclude)deploy/overlays/homelab/image-patch.yaml'
git commit -m "release: pin portal production image"
```

Record the release source, run URL, unique tag, exact digest, evidence review, and pin commit in the outer Phase 26 progress ledger. Resume its Task 4 using this pin commit as the Argo CD `targetRevision`; do not deploy the tag alone or mutate live resources.
