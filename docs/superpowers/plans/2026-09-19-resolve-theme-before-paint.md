# Resolve Theme Before Paint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver GitHub issue #13 by resolving the portal theme from a valid saved preference or the operating-system preference before styles can produce a meaningful paint, and by following later OS changes while no explicit preference exists.

**Architecture:** Keep a small, same-origin `/theme.js` as the sole early resolver. It reads only `portal.theme`, validates its two permitted values, assigns the resolved value to `document.documentElement.dataset.theme`, and subscribes to the `prefers-color-scheme` media query only when storage has no valid explicit value. Every rendered document loads that asset in its `<head>` before stylesheet links; the later `/app.js` toggle remains outside this issue's scope.

**Tech Stack:** Go `html/template` server rendering, Astro 7 static preview, browser-native `matchMedia`/`localStorage`, Playwright, Go integration tests.

**Spec:** `docs/superpowers/specs/2026-09-18-portal-theme-preferences-design.md`; GitHub issue #13.

## Global Constraints

- Implement issue #13 only; do not change the manual toggle, persistence-on-click, labels, icon, network, or session behavior owned by #14.
- `portal.theme` accepts exactly `light` and `dark`; every other value means no explicit preference.
- All storage reads and writes may fail without preventing rendering; this issue only reads storage.
- The initializer must remain a same-origin external `/theme.js` asset and must run before theme CSS; do not loosen `script-src 'self'` or add inline script.
- No third theme or persistent `system` value is introduced. Clearing storage restores OS following.

---

## File Structure

- `web/public/theme.js` — synchronous, guarded preference resolver and OS-change listener shared by all page variants.
- `internal/http/templates.go` — production home and admin `<head>` markup, with `/theme.js` preceding all emitted stylesheet links.
- `web/src/pages/admin.astro` — Astro development-preview admin `<head>` markup matching the production asset ordering.
- `web/tests/catalog.test.mjs` — production browser tests for first-load resolution, OS following, invalid/blocked storage fallback, and explicit-preference precedence.
- `web/tests/dev-preview.test.mjs` — preview-page coverage that both Astro entry pages load the resolver and honor OS preference.
- `tests/integration/rendered_ui_test.go` — rendered HTML contract for same-origin external early theme initialization on home and admin routes.

### Task 1: Specify the resolver’s browser behavior with failing tests

**Files:**
- Modify: `web/tests/catalog.test.mjs`
- Modify: `web/tests/dev-preview.test.mjs`

**Interfaces:**
- Consumes: a document with `/theme.js` loaded in the head; `window.matchMedia('(prefers-color-scheme: dark)')`; optional `window.localStorage.getItem('portal.theme')`.
- Produces: executable assertions for `html[data-theme]` resolution and its runtime OS-following contract.

- [ ] **Step 1: Replace the static-dark first-load assertion with independent OS-resolution cases**

  In `web/tests/catalog.test.mjs`, make each test start from a clean browser storage state, emulate `light` and then `dark`, navigate to `baseURL`, and assert the root resolves respectively:

  ```js
  test('production-rendered portal resolves a first visit from the operating-system theme', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' });
    await page.goto(baseURL);
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

    await page.context().clearCookies();
    await page.evaluate(() => window.localStorage.clear());
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
  ```

- [ ] **Step 2: Add failing OS-following and valid-preference-precedence tests**

  Add one test that visits under `light`, changes the emulated scheme to `dark`, and expects `data-theme="dark"`; then changes back to `light` and expects `light`. Add a separate test that uses `page.addInitScript` to seed `localStorage` with `portal.theme = 'light'` before navigation, emulates dark, and asserts the root remains light after an OS change. Use a fresh page/context for the seeded-preference test so state cannot leak from the follower test.

  ```js
  await page.addInitScript(() => localStorage.setItem('portal.theme', 'light'));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(baseURL);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  ```

- [ ] **Step 3: Add failing malformed and unavailable-storage fallback tests**

  For malformed storage, seed `portal.theme` with `system`, emulate dark, and expect dark. For unavailable storage, inject a `Storage.prototype.getItem` replacement that throws only for `portal.theme`, emulate light, navigate, and expect light without `pageerror`:

  ```js
  await page.addInitScript(() => {
    const getItem = Storage.prototype.getItem;
    Storage.prototype.getItem = function (key) {
      if (key === 'portal.theme') throw new DOMException('Blocked', 'SecurityError');
      return getItem.call(this, key);
    };
  });
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error));
  await page.goto(baseURL);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  expect(pageErrors).toEqual([]);
  ```

- [ ] **Step 4: Add development-preview coverage for both page entry points**

  In `web/tests/dev-preview.test.mjs`, emulate light before navigation and change the existing root assertions on `/` and `/admin` to expect `data-theme="light"`. Add an assertion in each test that the page includes `script[src="/theme.js"]`; this guards the Astro rendering path without duplicating production behavior tests.

- [ ] **Step 5: Run the focused browser tests and confirm the missing behavior fails**

  Run: `npm --prefix web test -- --grep "operating-system theme|OS changes|unavailable storage|explicit preference"`

  Expected: FAIL because the current initializer has no media-query change listener and the production/admin and Astro/admin documents do not consistently load `/theme.js`.

- [ ] **Step 6: Commit the test specification**

  ```bash
  git add web/tests/catalog.test.mjs web/tests/dev-preview.test.mjs
  git commit -m "test: specify portal theme resolution"
  ```

### Task 2: Make the external resolver follow OS changes safely

**Files:**
- Modify: `web/public/theme.js`

**Interfaces:**
- Consumes: `window.matchMedia('(prefers-color-scheme: dark)')`, `document.documentElement`, and best-effort `localStorage.getItem('portal.theme')`.
- Produces: `data-theme` equal to `'light' | 'dark'` synchronously during script evaluation, plus a media-query listener only when no valid stored choice exists.

- [ ] **Step 1: Implement validation and one resolver function**

  Replace the current body with a resolver that returns only the two allowed strings. Keep the storage read inside `try`/`catch`, use `null` for every unavailable or invalid value, and set the root before registering any listener:

  ```js
  (() => {
    const root = document.documentElement;
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    let preference = null;

    try {
      const stored = window.localStorage.getItem('portal.theme');
      if (stored === 'light' || stored === 'dark') preference = stored;
    } catch {
      // Storage is optional; use the operating-system preference instead.
    }

    const systemTheme = () => (media.matches ? 'dark' : 'light');
    root.dataset.theme = preference ?? systemTheme();
  })();
  ```

- [ ] **Step 2: Subscribe only when no explicit valid preference exists**

  Immediately after setting the initial value, add the listener inside `if (preference === null)`. Its callback must set the dataset directly from `event.matches` and must not read, write, or clear storage:

  ```js
  if (preference === null) {
    media.addEventListener('change', (event) => {
      root.dataset.theme = event.matches ? 'dark' : 'light';
    });
  }
  ```

- [ ] **Step 3: Run focused resolver tests and confirm they pass**

  Run: `npm --prefix web test -- --grep "operating-system theme|OS changes|unavailable storage|explicit preference"`

  Expected: OS-derived cases update on emulation changes, stored `light` wins over dark OS, malformed/throwing storage falls back without a page error.

- [ ] **Step 4: Commit the resolver**

  ```bash
  git add web/public/theme.js
  git commit -m "feat: follow system theme preference"
  ```

### Task 3: Load the resolver early on every rendered portal page

**Files:**
- Modify: `internal/http/templates.go`
- Modify: `web/src/pages/admin.astro`
- Modify: `tests/integration/rendered_ui_test.go`

**Interfaces:**
- Consumes: externally served `/theme.js`, the existing CSP `script-src 'self'`, and generated CSS URLs in `pageData.Styles` / Astro’s stylesheet output.
- Produces: `/theme.js` as a same-origin external `<script src="/theme.js"></script>` in each document head before CSS, with no inline theme source.

- [ ] **Step 1: Add failing rendered-HTML ordering tests**

  Add a helper that verifies the exact external script marker is present once, has no `http:`/`https:` URL, and appears before the first stylesheet link. Apply it to the anonymous home page and `/admin` page returned by the existing test handler. Also assert neither page contains an inline script whose text mentions `localStorage` or `matchMedia`.

  ```go
  assertThemeInitializer(t, anonymous)
  assertThemeInitializer(t, adminPage)
  ```

  ```go
  func assertThemeInitializer(t *testing.T, html string) {
      t.Helper()
      script := `<script src="/theme.js"></script>`
      scriptAt := strings.Index(html, script)
      stylesheetAt := strings.Index(html, `<link rel="stylesheet"`)
      if scriptAt < 0 || (stylesheetAt >= 0 && scriptAt > stylesheetAt) {
          t.Fatalf("theme initializer must precede styles: %s", html)
      }
      assertContainsNone(t, html, `<script>`, "localStorage", "matchMedia")
  }
  ```

- [ ] **Step 2: Add `/theme.js` before styles to the production admin template**

  In `adminTemplate`’s `<head>`, insert the same external tag used by `homeTemplate` immediately before `{{range .Styles}}`. Do not alter `headers.go`: the existing `script-src 'self'` already authorizes this asset.

- [ ] **Step 3: Add the external initializer to the Astro admin head**

  In `web/src/pages/admin.astro`, add the same source-tag form used by `index.astro`:

  ```astro
  <script is:inline src="/theme.js"></script>
  ```

  Keep it in `<head>`, after metadata and before Astro emits the imported stylesheet. Do not inline the JavaScript contents.

- [ ] **Step 4: Run rendered-HTML and preview contract tests**

  Run: `go test ./tests/integration -run RenderedCatalog -count=1`

  Expected: PASS with the home and admin heads declaring the same-origin external initializer before styles.

  Run: `npm --prefix web run test:dev -- --grep "developer can preview"`

  Expected: PASS for the home and admin preview pages under an emulated light OS preference.

- [ ] **Step 5: Commit the document integration**

  ```bash
  git add internal/http/templates.go web/src/pages/admin.astro tests/integration/rendered_ui_test.go
  git commit -m "feat: initialize portal theme before styles"
  ```

### Task 4: Run regression verification and record the issue boundary

**Files:**
- Modify: `docs/superpowers/plans/2026-09-19-resolve-theme-before-paint.md` only if verification reveals an omitted command or expectation.

**Interfaces:**
- Consumes: the resolver, document heads, and existing theme-toggle code.
- Produces: evidence that #13 works without changing #14 manual-choice behavior or CSP.

- [ ] **Step 1: Run static checks and the complete browser suite**

  Run: `npm --prefix web run lint`

  Expected: PASS.

  Run: `npm --prefix web test`

  Expected: PASS, including existing palette, accessible toggle, filtering, responsive-layout, and new resolver tests.

- [ ] **Step 2: Run Go tests that cover rendering and headers**

  Run: `go test ./internal/http ./tests/integration -count=1`

  Expected: PASS; CSP remains `script-src 'self'` and all rendered documents serve the external asset contract.

- [ ] **Step 3: Inspect the final diff for scope boundaries**

  Run: `git diff HEAD~4..HEAD -- web/public/app.js web/public/theme.js internal/http/templates.go web/src/pages web/tests tests/integration`

  Expected: #13 changes are limited to initial resolution, OS following, early asset loading, and tests; no new theme-toggle behavior, storage writes, session/cookie logic, or CSP relaxation is introduced.

- [ ] **Step 4: Commit any verification-only plan correction, if needed**

  ```bash
  git add docs/superpowers/plans/2026-09-19-resolve-theme-before-paint.md
  git commit -m "docs: finalize theme resolution plan"
  ```

## Self-Review

- **Spec coverage:** Task 1 verifies default OS resolution, valid-value-only storage handling, blocked/malformed storage fallback, and ongoing OS changes. Task 2 implements those rules synchronously in the external resolver. Task 3 covers pre-styles ordering, same-origin source, every current production and preview document, and preserved CSP. Task 4 runs the relevant regressions and enforces the #13/#14 boundary.
- **Scope:** Manual selection, persistence writes, control accessibility, transitions, and network/cookie assertions remain #14 work. The existing code for them is not expanded by this plan.
- **Consistency:** `portal.theme`, `light`, `dark`, `/theme.js`, and `data-theme` are the only preference API names used throughout. No placeholder text or undefined interfaces remain.
