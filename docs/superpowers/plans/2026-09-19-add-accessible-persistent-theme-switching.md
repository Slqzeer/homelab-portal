# Add Accessible Persistent Theme Switching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete GitHub issue #14 by making the Portal's manual theme control accessible, locally persistent, and isolated from navigation, network activity, cookies, and the Portal session.

**Architecture:** Keep `/theme.js` as the early, read-only resolver and make `/app.js` the only manual-selection owner. The rendered catalogue header supplies a native button with an inline decorative icon and visible label; the client handler derives the opposite current `data-theme`, applies it immediately, attempts one `portal.theme` write, and then updates the icon and accessible label. Browser tests own behavior and isolation assertions, while the Go rendered-HTML test prevents the server template from losing the control.

**Tech Stack:** Go `html/template` server rendering, Astro 7 static preview, browser-native `localStorage`, vanilla JavaScript, CSS, Playwright, Go integration tests.

**Spec:** `docs/superpowers/specs/2026-09-18-portal-theme-preferences-design.md`; GitHub issue #14.

## Global Constraints

- Implement issue #14 only; preserve #13's same-origin external `/theme.js`, its before-styles placement, and its OS-following behavior when no valid preference exists.
- `portal.theme` is the only storage key written by theme code, and its only persisted values are exactly `light` and `dark`.
- A blocked or unavailable `localStorage` write must not prevent the immediate in-page theme change.
- The control must be a native `button`, remain in the top-right `identity-actions` group, expose an updated accessible name, and have a target of at least 44 by 44 CSS pixels.
- The icon is decorative (`aria-hidden="true"`); the visible label supplies the button's accessible name. Do not add an icon request, third-party resource, cookie, session mutation, URL mutation, or reload.
- Clearing site storage is the only way to return to operating-system following; do not introduce a persistent `system` value or a third control state.

---

## File Structure

- `internal/http/templates.go` — production catalogue-header theme-button markup beside the profile action.
- `web/src/pages/index.astro` — development-preview catalogue-header markup matching production.
- `web/public/app.js` — one focused manual-selection controller that synchronizes `data-theme`, icon, label, and best-effort local persistence.
- `web/src/styles/app.css` — theme-control icon alignment and explicit 44px minimum target without changing the shared button behavior.
- `web/tests/catalog.test.mjs` — real-browser acceptance tests for keyboard/touch activation, persistence precedence, storage clearing, storage failure, and no side effects.
- `web/tests/dev-preview.test.mjs` — Astro-preview control contract, so the alternate rendering path cannot omit the control.
- `tests/integration/rendered_ui_test.go` — production rendered-HTML contract for native control semantics and placement.

### Task 1: Specify the manual-selection contract with failing browser tests

**Files:**

- Modify: `web/tests/catalog.test.mjs`
- Modify: `web/tests/dev-preview.test.mjs`
- Modify: `tests/integration/rendered_ui_test.go`

**Interfaces:**

- Consumes: `[data-theme-toggle]`, `[data-theme-toggle-icon]`, `[data-theme-toggle-label]`, `html[data-theme]`, and `localStorage['portal.theme']`.
- Produces: executable coverage that a manual choice wins over OS changes/reloads, clearing storage restores OS following, and activation cannot change browser or Portal state outside the DOM and `portal.theme`.

- [ ] **Step 1: Add failing rendered-document checks for the button and its placement**

  In `TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary`, require the exact control markup and assert it appears before the sign-in/sign-out profile action inside `identity-actions`:

  ```go
  assertContainsAll(t, anonymous,
      `<button class="button button-secondary theme-toggle" type="button" data-theme-toggle>`,
      `data-theme-toggle-icon aria-hidden="true"`,
      `data-theme-toggle-label>Switch to light theme</span>`,
  )
  assertHTMLOrder(t, anonymous,
      `<nav class="identity-actions" aria-label="Account">`,
      `data-theme-toggle`,
      `href="/auth/login"`,
  )
  ```

  Keep the assertions specific to the catalogue header: the issue does not add a theme control to the admin diagnostics page.

- [ ] **Step 2: Replace the narrow toggle test with keyboard, touch, size, and label cases**

  In `web/tests/catalog.test.mjs`, start from a light OS theme and clean local storage. Assert the initial accessible name, then use keyboard activation and a touch activation separately. Check the bounding box after the page has rendered:

  ```js
  const toggle = page.getByRole('button', { name: 'Switch to dark theme' });
  const box = await toggle.boundingBox();
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.height).toBeGreaterThanOrEqual(44);

  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(toggle).toHaveAccessibleName('Switch to light theme');

  await page.tap('[data-theme-toggle]');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.getByRole('button', { name: 'Switch to dark theme' })).toBeVisible();
  ```

  Also assert the icon has `aria-hidden="true"`; this prevents it from becoming a duplicate screen-reader name.

- [ ] **Step 3: Add failing persistence, precedence, and clearing cases**

  After keyboard activation selects dark, verify the stored key and reload under a light OS preference. Then clear storage, reload, and change the emulated OS theme to prove that following resumes:

  ```js
  await expect.poll(() => page.evaluate(() => window.localStorage.getItem('portal.theme'))).toBe('dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  ```

  In the same clean context, assert `Object.keys(localStorage)` is exactly `['portal.theme']` immediately after a successful manual selection. This enforces the single-key acceptance criterion.

- [ ] **Step 4: Add failing no-side-effect and failed-write cases**

  Record the URL, all cookies visible to the browser context, and requests after initial navigation. Click once; the root changes and the label updates, but the URL, cookies, and request list remain unchanged:

  ```js
  const urlBefore = page.url();
  const cookiesBefore = await page.context().cookies();
  const requests = [];
  page.on('request', (request) => requests.push(request.url()));

  await toggle.click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(page.url()).toBe(urlBefore);
  expect(await page.context().cookies()).toEqual(cookiesBefore);
  expect(requests).toEqual([]);
  ```

  Use a fresh page with an init script that throws only when `Storage.prototype.setItem` receives `portal.theme`. Assert no `pageerror`, a switched root, and the new label. This ensures persistence is best-effort without broadening the storage API surface.

- [ ] **Step 5: Add the development-preview markup contract**

  In `web/tests/dev-preview.test.mjs`, load `/` under a light OS theme and assert one `button[data-theme-toggle]`, the initial `Switch to dark theme` accessible name, and the decorative icon and label data attributes. Do not duplicate the production persistence and request tests here.

- [ ] **Step 6: Run focused tests to confirm the uncovered contract fails**

  Run: `npm --prefix web test -- --grep "manual theme|theme choice|storage clearing|theme control"`

  Expected: FAIL because the current control has no icon/label elements and the current suite does not yet specify reload, cleared-storage, failed-write, touch, size, or side-effect behavior.

  Run: `go test ./tests/integration -run RenderedCatalogKeepsIdentity -count=1`

  Expected: FAIL because the rendered button does not yet include the planned icon and label markup.

- [ ] **Step 7: Commit the failing specification**

  ```bash
  git add web/tests/catalog.test.mjs web/tests/dev-preview.test.mjs tests/integration/rendered_ui_test.go
  git commit -m "test: specify persistent theme control"
  ```

### Task 2: Render an accessible labelled theme control in both catalogue entry points

**Files:**

- Modify: `internal/http/templates.go`
- Modify: `web/src/pages/index.astro`
- Modify: `web/src/styles/app.css`

**Interfaces:**

- Consumes: `.identity-actions`, shared `.button` styling, and `data-theme-toggle` queried by `/app.js`.
- Produces: one catalogue-only native button with a decorative icon target and a visible label target for the controller to update.

- [ ] **Step 1: Replace the production button text with stable child targets**

  In `homeTemplate`, replace the existing single-text button with this markup immediately before the auth profile control:

  ```html
  <button class="button button-secondary theme-toggle" type="button" data-theme-toggle>
    <span data-theme-toggle-icon aria-hidden="true">☾</span>
    <span data-theme-toggle-label>Switch to light theme</span>
  </button>
  ```

  Keep the initial label aligned with the server's `data-theme="dark"` fallback. The early `/theme.js` and `/app.js` will synchronize it to the resolved theme after they load.

- [ ] **Step 2: Mirror the exact public control contract in Astro**

  Make the equivalent change in `web/src/pages/index.astro`. Use the same class and data attributes, initial dark-fallback label, and decorative-icon semantics so production and preview exercise the same client interface.

- [ ] **Step 3: Add only the local layout rules needed by this control**

  Add focused CSS under the component layer:

  ```css
  .theme-toggle {
    min-width: 2.75rem;
    gap: 0.5rem;
  }

  .theme-toggle [data-theme-toggle-icon] {
    display: inline-grid;
    width: 1rem;
    place-items: center;
  }
  ```

  `2.75rem` is 44 CSS pixels at the Portal's default root size and complements the existing shared `.button { min-height: 2.75rem; }`. Do not add animation or a media-query rule; #14 requires immediate switching and already meets the target with the shared component behavior.

- [ ] **Step 4: Run the rendered and preview contracts**

  Run: `go test ./tests/integration -run RenderedCatalogKeepsIdentity -count=1`

  Expected: PASS with exactly one real catalogue-header button, decorative icon, visible label, and profile-adjacent order.

  Run: `npm --prefix web run test:dev -- --grep "theme control"`

  Expected: PASS with Astro emitting the same control contract.

- [ ] **Step 5: Commit the markup and styles**

  ```bash
  git add internal/http/templates.go web/src/pages/index.astro web/src/styles/app.css
  git commit -m "feat: label portal theme control"
  ```

### Task 3: Make the manual controller synchronize the label, icon, and best-effort explicit choice

**Files:**

- Modify: `web/public/app.js`

**Interfaces:**

- Consumes: `document.documentElement.dataset.theme`, `[data-theme-toggle]`, `[data-theme-toggle-icon]`, `[data-theme-toggle-label]`, and `localStorage.setItem('portal.theme', 'light' | 'dark')`.
- Produces: an immediate manual choice with matching decorative icon and accessible name; no navigation, fetch, cookie, or session behavior.

- [ ] **Step 1: Query the stable child targets and centralize rendering**

  Beside the existing toggle query, add:

  ```js
  const themeToggleIcon = document.querySelector('[data-theme-toggle-icon]');
  const themeToggleLabel = document.querySelector('[data-theme-toggle-label]');
  ```

  Replace `updateThemeToggle` with a function that derives the opposite action from the current resolved root theme and updates only these child elements:

  ```js
  const updateThemeToggle = () => {
    const isDark = document.documentElement.dataset.theme === 'dark';
    if (themeToggleIcon) themeToggleIcon.textContent = isDark ? '☾' : '☀';
    if (themeToggleLabel) themeToggleLabel.textContent = isDark
      ? 'Switch to light theme'
      : 'Switch to dark theme';
  };
  ```

  Do not set an `aria-label` on the button: its visible label is its accessible name.

- [ ] **Step 2: Preserve the minimal event behavior**

  Keep the listener only on the native button. On click, calculate `nextTheme` from the root's currently resolved value, assign the root dataset before attempting storage, retain the existing narrow `try`/`catch`, and call `updateThemeToggle()` afterward:

  ```js
  themeToggle?.addEventListener('click', () => {
    const nextTheme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = nextTheme;
    try {
      window.localStorage.setItem('portal.theme', nextTheme);
    } catch {
      // The visual preference still applies for this page when storage is unavailable.
    }
    updateThemeToggle();
  });
  ```

  Do not import a package or add `fetch`, form submission, history manipulation, cookie access, session endpoint, OS media listener, or reload. `/theme.js` remains the only OS-following owner.

- [ ] **Step 3: Run focused browser behavior tests**

  Run: `npm --prefix web test -- --grep "manual theme|theme choice|storage clearing|theme control"`

  Expected: PASS. Keyboard and touch switch immediately; the selected value survives reload and ignores later OS changes; clearing storage resumes OS following; a failed write remains error-free; activation produces no request, navigation, or cookie mutation.

- [ ] **Step 4: Commit the controller update**

  ```bash
  git add web/public/app.js
  git commit -m "feat: synchronize theme control state"
  ```

### Task 4: Run complete regression verification and close the issue only with evidence

**Files:**

- Modify: `docs/superpowers/plans/2026-09-19-add-accessible-persistent-theme-switching.md` only if verification exposes an omitted command or an incorrect expected result.

**Interfaces:**

- Consumes: the production and preview pages, early resolver, manual controller, header markup, and complete browser suite.
- Produces: objective evidence that #14 is complete without regression or scope leakage into Portal identity/session behavior.

- [ ] **Step 1: Run static and production browser checks**

  Run: `npm --prefix web run lint`

  Expected: PASS.

  Run: `npm --prefix web test`

  Expected: PASS, including OS resolution (#13), manual-toggle accessibility and persistence (#14), catalogue interaction, responsive layout, palette, and Axe checks.

- [ ] **Step 2: Run the preview and Go render regressions**

  Run: `npm --prefix web run test:dev`

  Expected: PASS for both Astro page entry points, including the catalogue theme-control contract.

  Run: `go test ./internal/http ./tests/integration -count=1`

  Expected: PASS; rendered HTML retains the same-origin external initializer, CSP remains untouched, and the control appears only in the intended catalogue header.

- [ ] **Step 3: Inspect the final issue scope**

  Run: `git diff HEAD~3..HEAD -- internal/http/templates.go web/src/pages/index.astro web/src/styles/app.css web/public/app.js web/tests tests/integration`

  Expected: Changes are limited to control markup/style, client-side root dataset and `portal.theme` behavior, and tests. There is no server endpoint, cookie/session write, navigation, network API call, or new preference value.

- [ ] **Step 4: Commit a verification-only plan correction if needed**

  ```bash
  git add docs/superpowers/plans/2026-09-19-add-accessible-persistent-theme-switching.md
  git commit -m "docs: finalize theme switching plan"
  ```

- [ ] **Step 5: Close GitHub issue #14 with the verification summary**

  After all commands pass, comment with the browser, preview, and Go test results, then close the issue:

  ```bash
  gh issue comment 14 --body "Implemented accessible persistent theme switching. Verified with npm --prefix web run lint, npm --prefix web test, npm --prefix web run test:dev, and go test ./internal/http ./tests/integration -count=1. The control switches immediately, persists only portal.theme, follows OS again after storage is cleared, and produces no navigation, network, cookie, or Portal-session side effects."
  gh issue close 14
  ```

## Self-Review

- **Spec coverage:** Task 1 specifies every #14 acceptance criterion, including native accessibility, target size, manual precedence over OS changes, reload persistence, storage clearing, valid-value-only persistence, and isolation from navigation/network/cookies/session. Task 2 supplies matching production and preview control markup. Task 3 implements the focused DOM/storage behavior. Task 4 performs end-to-end regression and closure verification.
- **Scope:** #13 retains initial resolution, early document loading, and OS-following without an explicit choice. This plan adds no server-side preference, third theme, CSP change, account synchronization, or unrelated visual refactor.
- **Consistency:** `portal.theme`, `light`, `dark`, `data-theme-toggle`, `data-theme-toggle-icon`, and `data-theme-toggle-label` are the only theme-control API names used throughout. All interfaces are defined before consumers, and no task contains a placeholder.
