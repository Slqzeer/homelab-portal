# Authenticated Profile Entry and Profile Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a privacy-preserving, authenticated profile entry and read-only `/profile` page to the portal.

**Architecture:** Extract and normalize only the verified OIDC `name` claim, carry it through the already encrypted portal session, and expose it as presentation-only data on `catalog.Identity`. The public HTTP handler renders the top-right profile link and `/profile` server-side; the browser receives neither identity APIs nor client-side profile storage.

**Tech Stack:** Go 1.25, `coreos/go-oidc`, encrypted AES-GCM cookie sessions, `html/template`, Tailwind CSS v4, Playwright, Go `httptest`.

**Spec:** `docs/superpowers/specs/2026-09-18-profile-entry-and-page-design.md`

## Global Constraints

- Depend on the closed compact catalogue shell issue #10; keep the profile entry in its existing top-right `.identity-actions` group.
- Retain only a trimmed `name` JSON string that has no Unicode control code points and is at most 120 Unicode code points; invalid or absent values do not fail login.
- Display name is presentation-only: it must not affect catalogue/admin authorization, logs, metrics, keys, URLs, or browser storage.
- Do not render email, groups, subject ID, provider data, raw claims, tokens, or session details in HTML, client scripts, cookies outside the encrypted payload, logs, or metrics.
- `/profile` is public-listener-only, GET-only, server-rendered, and receives the existing security-header and encrypted-session-refresh middleware.
- Anonymous or expired `/profile` access begins login with the server-defined destination `/profile`; never accept a browser-supplied return URL.
- Profile previews contain no interactive descendants, profile links and all account actions retain a minimum 44×44 CSS-pixel target, and display name output relies on `html/template` escaping.

## Review Focus

- OIDC `name` that is a non-string, whitespace-only, has a control character, or is 121 Unicode code points: login succeeds and output is exactly `Signed-in user`.
- A valid display name after previous-key rotation and repeated idle refresh: it persists while the four-hour absolute expiry does not move.
- An anonymous or expired `/profile` request with arbitrary `next`, `return`, or `redirect` parameters: its authentication flow returns only to `/profile`.
- A malicious display name containing markup, quotes, or accessibility-context-sensitive text: it is escaped in page text and attribute values and no forbidden identity field is rendered.
- Touch and keyboard profile interactions: touch follows `/profile`, keyboard focus shows/dismisses the non-interactive preview, and neither leaves a focus/hover trap.

---

### Task 1: Carry a normalized verified display name through portal sessions

**Files:**
- Modify: `internal/auth/oidc.go`
- Modify: `internal/auth/session.go`
- Modify: `internal/catalog/model.go`
- Modify: `internal/auth/oidc_test.go`
- Modify: `internal/auth/session_test.go`

**Interfaces:**
- Produces: `auth.Claims{Groups []string, DisplayName string}` where `DisplayName` is either a normalized valid name or empty.
- Produces: `catalog.Identity{Authenticated bool, Groups map[string]struct{}, IsAdmin bool, DisplayName string}` with display name independent of authorization fields.
- Consumes: verified ID-token payload already decoded by `OIDC.Callback` and the existing encrypted session write/read/refresh path.

- [ ] **Step 1: Add the failing OIDC claim-normalization tests**

Extend the existing signed-token fixture so each test can set `name`, then table-test accepted and rejected values. Assert the group result remains unchanged and invalid `name` values return a successful callback with an empty display name.

```go
tests := []struct { name string; claim any; want string }{
    {"trimmed string", "  Ada Lovelace  ", "Ada Lovelace"},
    {"missing", nil, ""}, {"non-string", 17, ""},
    {"control character", "Ada\\nLovelace", ""},
    {"over unicode limit", strings.Repeat("界", 121), ""},
}
```

- [ ] **Step 2: Run the focused OIDC test and verify it fails**

Run: `go test ./internal/auth -run TestOIDCCallbackNormalizesDisplayName -count=1`

Expected: FAIL because `Claims` has no display-name field and callback discards `name`.

- [ ] **Step 3: Implement the narrow claim parser and session propagation**

Add `DisplayName string` to `Claims`, `session`, and `catalog.Identity`; decode only `payload["name"]` as `json.RawMessage`. A missing or non-string claim is neutral, `strings.TrimSpace` normalizes strings, and a helper rejects `unicode.IsControl(r)` and a rune count above 120. Preserve the existing group-validation failure behavior; do not add any claim to logging or metrics. Copy `s.DisplayName` into the loaded identity and preserve it naturally when `Refresh` rewrites the decrypted session.

```go
func normalizedDisplayName(raw json.RawMessage) string {
    var value string
    if len(raw) == 0 || json.Unmarshal(raw, &value) != nil { return "" }
    value = strings.TrimSpace(value)
    if value == "" || utf8.RuneCountInString(value) > 120 { return "" }
    for _, r := range value { if unicode.IsControl(r) { return "" } }
    return value
}
```

- [ ] **Step 4: Add session round-trip, rotation, refresh, and authorization-independence tests**

Create sessions with `DisplayName: "Ada Lovelace"`; assert encrypted cookie values never contain it in plaintext, `Load` returns it, previous-key sessions load and refreshed cookies use only the current key, and idle refresh retains it without extending `Expires`. Pair identities with the same groups but different names and assert `catalog.Visible` yields identical item IDs and identical `IsAdmin` values.

- [ ] **Step 5: Run the auth and catalog tests**

Run: `go test ./internal/auth ./internal/catalog -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the identity contract**

```bash
git add internal/auth/oidc.go internal/auth/session.go internal/catalog/model.go internal/auth/oidc_test.go internal/auth/session_test.go
git commit -m "feat: retain normalized profile display name"
```

### Task 2: Make profile login return destinations server-controlled

**Files:**
- Modify: `internal/http/login.go`
- Modify: `internal/http/login_test.go`

**Interfaces:**
- Produces: `loginTransaction{State, Nonce, Verifier, ReturnTo, Expires}` where `ReturnTo` is assigned only by the server.
- Consumes: `GET /auth/login` and the new internal profile-login entry point.
- Produces: successful callbacks redirect to the transaction’s server-assigned `ReturnTo` (`/` or `/profile`).

- [ ] **Step 1: Write failing route-return tests**

Test normal `GET /auth/login?return=https://attacker.example` still creates a transaction whose callback redirects to `/`. Test the profile route’s login initiation creates a transaction whose callback redirects to `/profile`, even when the original request includes `next`, `return`, or `redirect` query values.

```go
if callback.Header().Get("Location") != "/profile" {
    t.Fatalf("profile callback location = %q", callback.Header().Get("Location"))
}
```

- [ ] **Step 2: Run the focused return-flow tests and verify they fail**

Run: `go test ./internal/http -run 'Test(ProfileLoginReturn|LoginRejectsBrowserReturn)' -count=1`

Expected: FAIL because transactions have no destination and every callback currently redirects to `/`.

- [ ] **Step 3: Add an internal destination parameter without parsing browser destinations**

Add `ReturnTo string` to the encrypted login transaction. Change `begin` to accept a server constant/argument (`"/"` for `login`, `"/profile"` for the profile handler) and include it in the transaction before encryption. Keep the public login handler ignorant of `r.URL.Query`; after session creation redirect to `tx.ReturnTo`. Never use user input to select, validate, or concatenate the destination.

```go
const homeReturnTo = "/"
const profileReturnTo = "/profile"
```

- [ ] **Step 4: Run login and logout regression tests**

Run: `go test ./internal/http -run 'Test(Login|Callback|Logout|ProfileLoginReturn|LoginRejectsBrowserReturn)' -count=1`

Expected: PASS, including existing replay and safe-failure cases.

- [ ] **Step 5: Commit the controlled return flow**

```bash
git add internal/http/login.go internal/http/login_test.go
git commit -m "feat: retain server-defined profile return path"
```

### Task 3: Render the authenticated profile entry and `/profile` page

**Files:**
- Modify: `internal/http/server.go`
- Modify: `internal/http/templates.go`
- Modify: `internal/http/server_test.go`
- Create: `internal/http/profile_test.go`

**Interfaces:**
- Produces: public `GET /profile`, registered alongside `GET /` and excluded from the operations mux.
- Produces: shared `accountData`/page data containing `DisplayName`, fallback label, `IsAdmin`, and existing CSRF token.
- Consumes: `catalog.Identity.DisplayName`, existing `identity` session-refresh behavior, and existing `POST /auth/logout` contract.

- [ ] **Step 1: Add failing HTTP tests for catalogue and profile identity states**

Cover anonymous `/` (sign-in only; no `/profile`, initials, preview, display name, or forbidden fields), authenticated `/` (profile link, accessible label, non-interactive preview, no inline sign-out), valid authenticated `/profile`, fallback profile, admin-only diagnostics link, and anonymous/expired `/profile` starting profile login. Assert `GET /profile` on `Handlers.Operations` is 404 and public profile responses have the same CSP, HSTS, no-store, nosniff, and referrer headers as `/`.

```go
for _, forbidden := range []string{"private-subject", "private-access-token", "private-refresh-token", "portal-admin"} {
    require.NotContains(t, body, forbidden)
}
```

- [ ] **Step 2: Run focused profile tests and verify they fail**

Run: `go test ./internal/http -run 'Test(Profile|Home.*Profile|OperationsListenerHasNoPages)' -count=1`

Expected: FAIL because `/profile` is unregistered and templates do not receive a display name.

- [ ] **Step 3: Register and implement the protected profile route**

Register `GET /profile` only on the public mux. In `profile`, call the existing `identity` helper first so valid requests refresh sessions and invalid cookies are cleared. If the result is absent or unauthenticated, call the internal profile-login start path; otherwise create/reuse the CSRF token and render `profileTemplate`. The profile template must contain the neutral label when `DisplayName == ""`, signed-in state, catalogue link, existing CSRF form, and `/admin` only for `IsAdmin`.

```go
if identity == nil || !identity.Authenticated {
    s.beginLogin(w, r, profileReturnTo)
    return
}
```

- [ ] **Step 4: Replace the authenticated home action with a single profile link**

Extend the home data with presentation-safe `DisplayName` and a helper/fallback label. When authenticated, render one `/profile` link containing local initials, an accessible name that includes the display label, and an `aria-describedby` relationship to a tooltip/preview that says only `{{display name}}` and `Signed in.`. Do not render the logout form in the catalogue header for authenticated users. Keep anonymous sign-in and current admin diagnostics visibility intact; use `html/template` fields/functions rather than concatenating HTML.

- [ ] **Step 5: Run the complete HTTP package**

Run: `go test ./internal/http -count=1`

Expected: PASS, including security, logout, login, and catalog-visibility tests.

- [ ] **Step 6: Commit the server-rendered profile experience**

```bash
git add internal/http/server.go internal/http/templates.go internal/http/server_test.go internal/http/profile_test.go
git commit -m "feat: add authenticated profile page"
```

### Task 4: Add responsive, accessible profile-entry presentation without client identity logic

**Files:**
- Modify: `web/src/styles/app.css`
- Modify: `web/src/pages/index.astro`
- Create: `web/src/pages/profile.astro`
- Modify: `web/tests/dev-preview.test.mjs`

**Interfaces:**
- Produces: `.profile-entry`, `.profile-initials`, and `.profile-preview` styles shared by production embedded CSS and Astro development previews.
- Consumes: server-rendered `data-profile-entry`/`data-profile-preview` markup; no JavaScript state or fetch endpoint.

- [ ] **Step 1: Add failing developer-preview tests**

Add a deterministic authenticated preview fixture route/state, then verify the profile link has a 44px minimum width and height, the preview contains no `a`, `button`, `input`, or `[tabindex]` descendants, focus exposes it, pointer leave and Escape hide it, and `/profile` is directly reachable by keyboard and touch.

- [ ] **Step 2: Run the preview test and verify it fails**

Run: `npm --prefix web run test:dev -- --grep "profile"`

Expected: FAIL because Astro preview has no profile fixture or profile markup.

- [ ] **Step 3: Build the CSS-only interaction contract**

Use `:hover` and `:focus-within` on the entry wrapper to reveal an absolutely-positioned non-interactive preview; use an Escape-capable minimal progressive enhancement only if necessary to remove focus/hover persistence, without reading or retaining identity values. Style the local initials circle and link as a 2.75rem minimum target, prevent narrow-screen overflow, and leave touch activation as ordinary link navigation. Add a `prefers-reduced-motion` rule so reveal/hide has no required animation.

```css
.profile-entry:hover .profile-preview,
.profile-entry:focus-within .profile-preview { opacity: 1; pointer-events: none; }
.profile-link { min-width: 2.75rem; min-height: 2.75rem; }
```

- [ ] **Step 4: Mirror only non-sensitive development fixtures in Astro**

Update `index.astro` and add `profile.astro` so local `make dev` shows a labelled fake profile using a harmless fixture display name. Keep production identity rendering in Go templates and do not add browser storage, request calls, tokens, groups, or provider data.

- [ ] **Step 5: Run lint, build, and development-preview tests**

Run: `npm --prefix web run lint; npm --prefix web run build; npm --prefix web run test:dev`

Expected: PASS.

- [ ] **Step 6: Commit the profile interaction styling**

```bash
git add web/src/styles/app.css web/src/pages/index.astro web/src/pages/profile.astro web/tests/dev-preview.test.mjs
git commit -m "feat: style accessible profile entry"
```

### Task 5: Exercise the real rendered browser boundary and full release verification

**Files:**
- Modify: `tests/integration/browserfixture/main.go`
- Modify: `web/tests/catalog.test.mjs`
- Modify: `web/tests/theme-preferences.test.mjs`
- Modify: `internal/http/profile_test.go`

**Interfaces:**
- Produces: `/__test/profile` and `/__test/profile-fallback` fixture entry points that create local encrypted sessions with no real OIDC/provider data.
- Consumes: the real Go public handler over TLS, Playwright browser contexts, and existing authenticated `/__test/admin` fixture behavior.

- [ ] **Step 1: Add failing production-browser coverage**

In Playwright, verify anonymous production `/` has sign-in and no profile DOM/text; authenticated fixture `/` has the name-labelled link, correct initials, minimum target, no client identity requests/storage, non-interactive hover/focus preview, Escape dismissal, and no forbidden identity text. Verify keyboard Enter and a `hasTouch: true` context reach `/profile`; on the profile page verify fallback/valid names, sign-out form, catalogue return link, conditional admin link, and successful sign-out returns an anonymous home state. Run axe against anonymous catalogue, authenticated catalogue, profile, and fallback profile states.

- [ ] **Step 2: Run the focused production-browser tests and verify they fail**

Run: `npm --prefix web test -- --grep "profile"`

Expected: FAIL because the fixture routes and production profile elements do not yet exist.

- [ ] **Step 3: Add browserfixture profile states without weakening production routes**

Add test-only handlers before the catch-all portal handler that create sessions with `auth.Claims{DisplayName: "Ada Lovelace"}` or no display name, then redirect to `/` or `/profile`. Do not add those routes to `internal/http`; preserve their test-only scope in `tests/integration/browserfixture`.

- [ ] **Step 4: Add HTTP escape and privacy regressions**

In `profile_test.go`, create an authenticated session with a valid display name such as `<Ada "Lovelace">` and assert text and attribute contexts are escaped. Check rendered home/profile bodies, response headers, cookie strings, collected metrics, and captured logs exclude fixture subject, email, groups, raw token data, and unapproved display-name uses. Keep the encrypted session cookie as the only permitted persistence location.

- [ ] **Step 5: Run the complete verification suite**

Run: `make test`

Expected: PASS.

Run: `npm --prefix web run lint; go vet ./...`

Expected: PASS.

- [ ] **Step 6: Commit the release coverage**

```bash
git add tests/integration/browserfixture/main.go web/tests/catalog.test.mjs web/tests/theme-preferences.test.mjs internal/http/profile_test.go
git commit -m "test: cover authenticated profile experience"
```

## Plan Self-Review

- **Spec coverage:** Tasks 1–2 cover verified-name validation, encrypted session survival, and fixed login return destinations; Task 3 covers the public route, approved actions, headers, and authorization; Task 4 covers accessible layout and interaction; Task 5 covers browser, privacy, sign-out, and regression verification.
- **Placeholder scan:** No incomplete or unspecified validation/testing steps remain.
- **Type consistency:** `auth.Claims.DisplayName` flows to `catalog.Identity.DisplayName`, then into HTTP template data; `loginTransaction.ReturnTo` is set only by server route constants.
- **Review focus:** Each listed failure mode has a named test in Tasks 1, 2, 3, 4, or 5.
