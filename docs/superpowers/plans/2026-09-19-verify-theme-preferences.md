# Verify Theme Accessibility, Privacy, and Browser Behavior Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete GitHub issue #15 by proving the delivered theme-preferences experience behaves safely in real browsers and retains its rendered-document, accessibility, privacy, and motion guarantees.

**Architecture:** This is a verification-only change. Keep `/theme.js` as the early resolver and `/app.js` as the manual-selection owner; extend the Playwright production fixture to expose deterministic stale and authenticated catalogue states, then add a focused theme-preferences test matrix. Use rendered-HTML integration tests for the server-side no-inline-script and control contracts, and Axe against the rendered state matrix for WCAG AA contrast.

**Tech Stack:** Go `net/http` browser fixture and integration tests, Go `html/template`, vanilla JavaScript, Astro 7 preview, Playwright 1.61, and `@axe-core/playwright` 4.11.

**Spec:** `docs/superpowers/specs/2026-09-18-portal-theme-preferences-design.md`; GitHub issue #15; dependent implementation plans `docs/superpowers/plans/2026-09-19-resolve-theme-before-paint.md` and `docs/superpowers/plans/2026-09-19-add-accessible-persistent-theme-switching.md`.

## Global Constraints

- This issue adds verification and deterministic test-fixture routing only; do not add a server-side theme preference, account synchronization, third theme, new endpoint in the production portal, CSP exception, cookie mutation, or network API.
- `portal.theme` remains the only theme storage key, and only exact `light` and `dark` values are valid.
- The production page must continue to load same-origin external `/theme.js` before styles, with no inline theme source and with the existing `script-src 'self'` CSP unchanged.
- Browser tests must use the real production renderer at `https://127.0.0.1:4173`; Astro preview tests remain only an alternate-rendering contract.
- The product has no separately rendered profile-preview component. For this issue, “profile-preview state” means the real authenticated catalogue header (the account action changes from **Sign in** to **Sign out**) served through the existing browser-fixture session route. Do not invent a profile UI solely for testing.
- Reduced-motion verification must assert no active CSS animation or transition is produced by a theme toggle when `prefers-reduced-motion: reduce` is emulated.

## Review Focus

- A fresh context with light OS preference resolves to `light`, and a fresh context with dark OS preference resolves to `dark`, without relying on state leaked from another test.
- A valid stored choice remains authoritative after an OS preference change and reload; malformed or throwing storage instead falls back cleanly to the current OS choice.
- A `localStorage.setItem` failure still changes the in-page root and button label without an uncaught page error.
- Toggling in reduced-motion mode produces no active animation or transition on the document subtree after the click.
- Axe color-contrast checks cover normal, stale, filtered-empty, and authenticated/profile-action catalogue states in both themes; the state setup itself must be visible before analysis.

---

## File Structure

- `tests/integration/browserfixture/main.go` — deterministic production-rendered stale and authenticated catalogue routes used exclusively by Playwright.
- `web/tests/playwright.config.mjs` — includes the dedicated theme-preferences production test file in the existing real-renderer suite.
- `web/tests/theme-preferences.test.mjs` — isolated browser behavior, storage-failure, privacy, reduced-motion, and Axe state-matrix tests for #15.
- `web/tests/dev-preview.test.mjs` — keeps the Astro preview’s early external initializer and theme-control markup contract explicit.
- `tests/integration/rendered_ui_test.go` — isolates the rendered-HTML contracts for external pre-paint initialization and native theme control markup.

### Task 1: Expose deterministic production states to the browser suite

**Files:**

- Modify: `tests/integration/browserfixture/main.go`

**Interfaces:**

- Consumes: the existing `publishedIngress`, `catalog.NewStore`, `portalhttp.New`, and `auth.SessionManager` helpers.
- Produces: `GET /__test/stale` rendering the normal catalogue with a snapshot older than two minutes, and `GET /__test/admin` creating the existing admin session then redirecting to `/`.

- [ ] **Step 1: Add a separate stale portal handler with a fixed stale snapshot**

  After constructing the normal `store`, construct `staleStore` using the same public fixture Ingresses, but call `Replace` with a timestamp three minutes before `now`. Build `stalePortal` with the same sessions, embedded assets, base URL, and `Now` function as `portal`:

  ```go
  staleStore := catalog.NewStore(types.NamespacedName{Namespace: "portal", Name: "portal"})
  staleStore.Replace([]networkingv1.Ingress{
      publishedIngress("grafana", "Grafana", "Dashboards and visualizations", "Monitoring", "public", "grafana"),
  }, now.Add(-3*time.Minute))
  stalePortal, err := portalhttp.New(portalhttp.Options{
      Store: staleStore, Sessions: sessions, Assets: assets.FS(), BaseURL: "https://" + address,
      Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
  })
  if err != nil {
      log.Fatal(err)
  }
  ```

- [ ] **Step 2: Route the stale fixture through the real handler without changing its document URL**

  Register `GET /__test/stale`. Clone the request, replace its path with `/`, then pass it to `stalePortal`; this keeps the tested response identical to production’s home rendering while allowing Playwright to identify the fixture route:

  ```go
  mux.HandleFunc("GET /__test/stale", func(w http.ResponseWriter, r *http.Request) {
      request := r.Clone(r.Context())
      request.URL.Path = "/"
      stalePortal.ServeHTTP(w, request)
  })
  ```

  Leave `/__test/admin` as the only fixture route that creates a session, and leave `mux.Handle("/", portal)` as the default production renderer.

- [ ] **Step 3: Run the fixture-backed stale-state probe**

  Run: `go run ./tests/integration/browserfixture`

  In another terminal, run: `curl -k https://127.0.0.1:4173/__test/stale`

  Expected: HTTP 200 HTML includes `Catalogue is stale. Catalogue information is not current.` and a same-origin `/theme.js` tag. Stop the manually started fixture after checking it.

- [ ] **Step 4: Commit the deterministic fixture state**

  ```bash
  git add tests/integration/browserfixture/main.go
  git commit -m "test: expose stale portal browser fixture"
  ```

### Task 2: Add isolated browser behavior, privacy, and motion contracts

**Files:**

- Create: `web/tests/theme-preferences.test.mjs`
- Modify: `web/tests/playwright.config.mjs`

**Interfaces:**

- Consumes: `https://127.0.0.1:4173`, `html[data-theme]`, `[data-theme-toggle]`, `[data-theme-toggle-label]`, browser-native `localStorage`, `page.emulateMedia`, and the fixture routes from Task 1.
- Produces: independently reset browser tests for first visit, explicit persistence, storage read/write failure, no request/cookie/session side effect, and reduced-motion behavior.

- [ ] **Step 1: Include the dedicated test file in the production Playwright project**

  In `web/tests/playwright.config.mjs`, replace the single-file matcher with the two explicit production files so the pre-existing catalogue suite and the new verification suite both run, while `dev-preview.test.mjs` remains excluded:

  ```js
  testMatch: ['catalog.test.mjs', 'theme-preferences.test.mjs'],
  ```

- [ ] **Step 2: Create helpers that make every theme test start from a new browser context**

  Start `web/tests/theme-preferences.test.mjs` with the existing imports, base URL, and one helper. The helper uses a fresh context so no local storage, cookie, OS preference, event listener, or request array leaks between cases:

  ```js
  import AxeBuilder from '@axe-core/playwright';
  import { expect, test } from '@playwright/test';

  const baseURL = 'https://127.0.0.1:4173';

  const newPage = async (browser, options = {}) => {
    const context = await browser.newContext({ ignoreHTTPSErrors: true, ...options });
    const page = await context.newPage();
    return { context, page };
  };
  ```

- [ ] **Step 3: Specify first-visit resolution, explicit precedence, and unreadable storage**

  Add one test that opens two new contexts, emulates `light` and `dark` before navigation, and asserts the matching `html[data-theme]`. Add a separate test that seeds `portal.theme` to `light`, loads under dark OS preference, changes the OS to light and dark again, reloads, and always expects `light`.

  Add a third test that seeds `portal.theme` to `system` under dark preference, then in a fresh context replaces `Storage.prototype.getItem` only for `portal.theme` with a throwing function under light preference. Each fallback must resolve to the OS preference and leave `pageErrors` empty:

  ```js
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error));
  await page.addInitScript(() => {
    const getItem = Storage.prototype.getItem;
    Storage.prototype.getItem = function (key) {
      if (key === 'portal.theme') throw new DOMException('Blocked', 'SecurityError');
      return getItem.call(this, key);
    };
  });
  ```

- [ ] **Step 4: Specify successful and failed manual persistence without server-visible effects**

  In a light-OS context, navigate to `/`, record the settled URL and cookies, then attach a request listener *after* navigation. Activate `[data-theme-toggle]` via keyboard and require the root, label, and only storage key to change; then reload and require the saved dark choice. The request list, URL, and cookies must remain unchanged:

  ```js
  const urlBefore = page.url();
  const cookiesBefore = await page.context().cookies();
  const requests = [];
  page.on('request', (request) => requests.push(request.url()));

  await page.getByRole('button', { name: 'Switch to dark theme' }).press('Enter');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => Object.keys(localStorage))).toEqual(['portal.theme']);
  expect(page.url()).toBe(urlBefore);
  expect(await page.context().cookies()).toEqual(cookiesBefore);
  expect(requests).toEqual([]);
  ```

  In a second new context, inject a `Storage.prototype.setItem` implementation that throws only for `portal.theme`, collect `pageerror`, click the button, and assert the root and label still switch while `pageErrors` remains empty. This is the test for unavailable write storage; do not change `/app.js` unless that test exposes a regression.

- [ ] **Step 5: Specify reduced-motion behavior at the observable page boundary**

  Use a fresh context and emulate reduced motion before navigation. Click the control, wait one animation frame, then inspect the whole document’s active animations. It must switch immediately and have no running or pending animation:

  ```js
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' });
  await page.goto(baseURL);
  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.evaluate(() => new Promise(requestAnimationFrame));
  expect(await page.evaluate(() => document.getAnimations({ subtree: true })
    .filter((animation) => ['pending', 'running'].includes(animation.playState)).length)).toBe(0);
  ```

- [ ] **Step 6: Run the new focused behavior tests and verify the current implementation passes**

  Run: `npm --prefix web test -- --grep "theme preference|theme storage|reduced motion"`

  Expected: PASS. The suite proves both first-visit OS choices, explicit persistence precedence, malformed/read-failed fallback, failed-write resilience, no post-load request/cookie/navigation side effect, and no reduced-motion animation.

- [ ] **Step 7: Commit the browser behavior coverage**

  ```bash
  git add web/tests/playwright.config.mjs web/tests/theme-preferences.test.mjs
  git commit -m "test: verify theme browser behavior and privacy"
  ```

### Task 3: Add the two-theme accessibility state matrix

**Files:**

- Modify: `web/tests/theme-preferences.test.mjs`

**Interfaces:**

- Consumes: normal home `/`, stale home `/__test/stale`, authenticated/profile-action home `/__test/admin`, the existing searchbox and empty-results controls, and `AxeBuilder`.
- Produces: WCAG AA automated contrast evidence for catalogue, stale, filtered-empty, and authenticated/profile-action states under both `light` and `dark` themes.

- [ ] **Step 1: Add a state setup function that uses visible application states**

  Add the following helper below `newPage`. It loads the supplied route under the requested OS preference and creates the empty state through the real search interaction rather than modifying DOM classes directly:

  ```js
  const prepareState = async (page, route, colorScheme, empty) => {
    await page.emulateMedia({ colorScheme });
    await page.goto(`${baseURL}${route}`);
    if (empty) {
      await page.getByRole('searchbox', { name: 'Search catalog' }).fill('no matching catalogue item');
      await expect(page.locator('[data-empty-results]')).toBeVisible();
    }
  };
  ```

- [ ] **Step 2: Add the eight-case Axe matrix**

  Parameterize four real page states across both themes: catalogue (`/`), stale (`/__test/stale`), filtered empty (`/` with `empty: true`), and authenticated/profile-action (`/__test/admin`). For every row, assert the expected state marker before running Axe, then analyze only WCAG 2.x AA tags including color contrast:

  ```js
  const states = [
    { name: 'catalogue', route: '/', empty: false, marker: '[data-catalog-grid]' },
    { name: 'stale catalogue', route: '/__test/stale', empty: false, marker: '.stale-banner' },
    { name: 'filtered empty catalogue', route: '/', empty: true, marker: '[data-empty-results]' },
    { name: 'authenticated profile action', route: '/__test/admin', empty: false, marker: 'button:has-text("Sign out")' },
  ];

  for (const colorScheme of ['light', 'dark']) {
    for (const state of states) {
      test(`${state.name} meets AA checks in ${colorScheme} theme`, async ({ browser }) => {
        const { context, page } = await newPage(browser);
        await prepareState(page, state.route, colorScheme, state.empty);
        await expect(page.locator('html')).toHaveAttribute('data-theme', colorScheme);
        await expect(page.locator(state.marker)).toBeVisible();
        const results = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
          .analyze();
        expect(results.violations).toEqual([]);
        await context.close();
      });
    }
  }
  ```

  Keep the existing narrow-layout Axe assertion in `catalog.test.mjs`; it continues to own the long-content and layout-specific accessibility regression.

- [ ] **Step 3: Run the accessibility matrix**

  Run: `npm --prefix web test -- --grep "meets AA checks"`

  Expected: PASS for eight tests. Failures must identify the specific state and theme in the Playwright test name; correct a real token/markup contrast defect only if one is exposed.

- [ ] **Step 4: Commit the accessibility matrix**

  ```bash
  git add web/tests/theme-preferences.test.mjs
  git commit -m "test: cover theme contrast state matrix"
  ```

### Task 4: Make rendered-document and preview contracts explicit

**Files:**

- Modify: `tests/integration/rendered_ui_test.go`
- Modify: `web/tests/dev-preview.test.mjs`

**Interfaces:**

- Consumes: production `homeTemplate` and `adminTemplate`, Astro `/` and `/admin`, the exact external asset `/theme.js`, and catalogue `[data-theme-toggle]` markup.
- Produces: regression-proof contracts that theme initialization is same-origin and before styles without inline script, and that the development preview retains the native labelled theme control.

- [ ] **Step 1: Extract the rendered theme contract into its own Go test**

  Add `TestRenderedThemePreferenceContracts` in `tests/integration/rendered_ui_test.go`. Reuse `renderHome`, `serve`, `sessionCookie`, and `assertThemeInitializer`, but construct the normal and admin documents in this test. Require the home page’s native control and child markers; require the admin page to have the early initializer but *not* a catalogue theme control:

  ```go
  assertThemeInitializer(t, anonymous)
  assertContainsAll(t, anonymous,
      `<button class="button button-secondary theme-toggle" type="button" data-theme-toggle>`,
      `data-theme-toggle-icon aria-hidden="true"`,
      `data-theme-toggle-label>Switch to light theme</span>`,
  )
  assertThemeInitializer(t, adminPage)
  assertContainsNone(t, adminPage, `data-theme-toggle`, `<script>`, "localStorage", "matchMedia")
  ```

  Remove only duplicated theme-specific assertions from `TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary`; retain its catalogue visibility, identity, asset-origin, and filtering assertions.

- [ ] **Step 2: Strengthen the preview contract without duplicating production behavior**

  In `web/tests/dev-preview.test.mjs`, keep the existing light-OS initialization checks for `/` and `/admin`. In the labelled theme-control test, also assert exactly one external initializer and that the button is a native `button`, while retaining the decorative icon and visible-label checks:

  ```js
  await expect(page.locator('script[src="/theme.js"]')).toHaveCount(1);
  await expect(page.locator('button[data-theme-toggle]')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Switch to dark theme' })).toBeVisible();
  ```

  Do not add persistence, network, cookie, stale, or Axe tests to Astro preview; those are owned by the real-renderer suite.

- [ ] **Step 3: Run rendered-document and preview contracts**

  Run: `go test ./tests/integration -run 'Rendered(Catalog|Theme)' -count=1`

  Expected: PASS; the test reports exactly one `/theme.js` in each document head before styles, no inline theme source, and a home-only native theme button.

  Run: `npm --prefix web run test:dev`

  Expected: PASS; both Astro entries retain the same early external initializer and the catalogue preview has the labelled button.

- [ ] **Step 4: Commit the rendering contracts**

  ```bash
  git add tests/integration/rendered_ui_test.go web/tests/dev-preview.test.mjs
  git commit -m "test: isolate rendered theme preference contracts"
  ```

### Task 5: Run regressions and close issue #15 with evidence

**Files:**

- Modify: `docs/superpowers/plans/2026-09-19-verify-theme-preferences.md` only if a command, exact route, or expected result above proves incorrect during execution.

**Interfaces:**

- Consumes: Tasks 1–4, the production browser fixture, theme assets, rendered templates, and existing complete test suites.
- Produces: reproducible verification evidence for issue #15, with no product behavior change beyond a test-only stale fixture route.

- [ ] **Step 1: Run static and full production browser verification**

  Run: `npm --prefix web run lint`

  Expected: PASS.

  Run: `npm --prefix web test`

  Expected: PASS, including all existing catalogue checks plus the #15 first-visit, persistence, storage failure, reduced-motion, privacy, and eight-state contrast coverage.

- [ ] **Step 2: Run preview and Go integration regressions**

  Run: `npm --prefix web run test:dev`

  Expected: PASS.

  Run: `go test ./internal/http ./tests/integration -count=1`

  Expected: PASS; rendered documents retain their same-origin early initializer and no server-side preference behavior is introduced.

- [ ] **Step 3: Inspect the issue boundary before closure**

  Run: `git diff HEAD~4..HEAD -- tests/integration/browserfixture/main.go web/tests tests/integration/rendered_ui_test.go web/public/theme.js web/public/app.js internal/http`

  Expected: implementation changes are limited to the browser test fixture and test files. `/theme.js`, `/app.js`, production templates, CSP, session handling, cookies, and request paths have no product-behavior change.

- [ ] **Step 4: Commit a verification-only plan correction only if required**

  ```bash
  git add docs/superpowers/plans/2026-09-19-verify-theme-preferences.md
  git commit -m "docs: finalize theme verification plan"
  ```

- [ ] **Step 5: Close GitHub issue #15 with the command evidence**

  After every command passes, comment and close the issue:

  ```bash
  gh issue comment 15 --body "Verified theme preferences with npm --prefix web run lint, npm --prefix web test, npm --prefix web run test:dev, and go test ./internal/http ./tests/integration -count=1. Production browser coverage verifies first-visit OS resolution, explicit persistence and OS-change precedence, malformed/read-write-failed storage safety, reduced motion, and no post-load request/cookie/session side effect. Axe passes for normal, stale, filtered-empty, and authenticated account-action states in light and dark themes. Rendered HTML retains the same-origin external pre-paint initializer and native catalogue theme control without inline script."
  gh issue close 15
  ```

## Self-Review

- **Spec coverage:** Task 2 covers both OS first-visit outcomes, explicit persistence, later OS changes, malformed/read-unavailable storage, write-unavailable storage, no uncaught errors, reduced motion, and no request/cookie/session effects. Task 4 covers the same-origin, pre-style, no-inline-script initialization and control contracts. Task 3 supplies AA evidence for catalogue, stale, empty, and the current authenticated profile-action equivalent in both themes.
- **Scope:** The only non-test production-facing code is a test binary route that exists solely under the local Playwright fixture; production `internal/http` behavior, theme logic, CSP, cookies, and sessions are intentionally untouched.
- **Consistency:** Every task uses the established `portal.theme`, `light`, `dark`, `/theme.js`, `html[data-theme]`, `[data-theme-toggle]`, `baseURL`, and `__test` route names. The “profile-preview” terminology is resolved once in Global Constraints against the repository’s actual authenticated header rather than inventing a UI state.
- **Review Focus:** Each of the five listed risky inputs/conditions is assigned to Task 2 or Task 3 with an executable test design. No placeholders, undefined test seams, or unowned acceptance criteria remain.
