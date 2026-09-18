# Responsive, Sticky, and Accessible Catalogue Interaction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete GitHub issue #9 by making the catalogue search/category area sticky on wide screens, safely normal-flow on narrow screens, and covered by behavior-focused accessibility and responsive browser tests.

**Architecture:** Preserve the current server-rendered order and local filtering in `index.astro` and `app.js`. The stylesheet alone will make `.catalog-controls` a bounded sticky surface at the desktop shell breakpoint, then reset it on smaller screens; category chips will use wrapping non-shrinking controls. Playwright tests will measure visible geometry, focus behavior, ARIA state, and overflow across wide and narrow fixtures rather than asserting CSS implementation details.

**Tech Stack:** Astro 7 static UI, Tailwind CSS 4 source stylesheet, native browser JavaScript, Playwright 1.61, axe-core Playwright.

**Spec:** `docs/superpowers/specs/2026-09-18-catalog-search-information-hierarchy-design.md`; GitHub issue #9, “Verify responsive, sticky, and accessible catalogue interaction”.

## Global Constraints

- Do not modify catalogue metadata, sorting, visibility, target navigation, authentication, or the server-rendered authorization boundary.
- Filtering remains local over already rendered authorized cards; it must not request catalogue data, navigate, or move focus.
- Preserve source and keyboard order: skip link, rail information/status, search/categories, empty-result feedback, then cards.
- Sticky controls must have an opaque portal surface and must not obscure a focused card link.
- At supported narrow widths, controls return to normal flow; chips wrap instead of shrinking labels to an unreadable size; no horizontal document overflow is allowed.
- Preserve visible labels, `aria-pressed` state, 44 by 44 CSS-pixel control targets, skip-link usability, and current stale/empty/admin behavior.
- Do not add dependencies, browser requests, cookies, session writes, or server/API changes.

---

## File Structure

- `web/src/styles/app.css` — owns desktop sticky positioning, opaque control surface, focus scroll offset, and category-chip wrapping behavior.
- `web/tests/catalog.test.mjs` — owns production-rendered browser checks for wide sticky geometry, narrow normal flow/overflow, focus visibility, and authorized/admin states.
- `web/src/pages/index.astro` and `web/public/app.js` — review-only; their current markup and filtering events supply the interfaces the layout must preserve.

### Task 1: Add failing behavior-level layout and accessibility tests

**Files:**
- Modify: `web/tests/catalog.test.mjs`
- Review: `web/src/pages/index.astro`, `web/public/app.js`, `web/src/styles/app.css`

**Interfaces:**
- Consumes: `.catalog-controls`, `.catalog-main`, `[data-catalog-grid]`, `[data-catalog-search]`, `button[data-category-filter]`, and card links named `Open <service>`.
- Produces: geometry and interaction assertions that distinguish wide sticky behavior from narrow normal flow without coupling to CSS properties.

- [ ] **Step 1: Add a failing wide-viewport sticky-control test**

  Add a test that uses a `1440 x 500` viewport, scrolls the page past the controls’ original location, and measures their rendered position and opacity. Focus a later card link and assert it is fully below the sticky surface:

  ```js
  test('production-rendered wide catalogue keeps opaque controls sticky without hiding focused cards', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 500 });
    await page.goto(baseURL);

    const controls = page.locator('.catalog-controls');
    const cardLink = page.getByRole('link', { name: 'Open Prometheus' });
    await page.evaluate(() => window.scrollTo(0, 260));
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0);

    const controlsBox = await controls.boundingBox();
    expect(controlsBox.y).toBeGreaterThanOrEqual(0);
    expect(controlsBox.y).toBeLessThanOrEqual(2);
    expect(await controls.evaluate((element) => getComputedStyle(element).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');

    await cardLink.focus();
    const [focusedBox, stickyBox] = await Promise.all([cardLink.boundingBox(), controls.boundingBox()]);
    expect(focusedBox.y).toBeGreaterThanOrEqual(stickyBox.y + stickyBox.height);
  });
  ```

- [ ] **Step 2: Add a failing narrow-viewport normal-flow and chip-wrap test**

  Add a test at `320 x 740` that verifies the controls scroll away rather than sticking, the search label remains visible, category buttons occupy readable rendered boxes, and the document has no horizontal overflow:

  ```js
  test('production-rendered narrow catalogue keeps controls in normal flow and chips readable', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 740 });
    await page.goto(baseURL);

    const controls = page.locator('.catalog-controls');
    const search = page.getByRole('searchbox', { name: 'Search catalog' });
    const buttons = page.locator('button[data-category-filter]');
    const initialY = (await controls.boundingBox()).y;
    await expect(page.getByText('Search catalog', { exact: true })).toBeVisible();
    await page.evaluate(() => window.scrollTo(0, 260));
    const scrolledY = (await controls.boundingBox()).y;

    expect(scrolledY).toBeLessThan(initialY - 100);
    for (const button of await buttons.all()) {
      const box = await button.boundingBox();
      expect(box.width).toBeGreaterThanOrEqual(44);
      expect(box.height).toBeGreaterThanOrEqual(44);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
    await search.focus();
    await expect(search).toBeFocused();
  });
  ```

- [ ] **Step 3: Extend the existing filtering test with keyboard and focus-invariance assertions**

  In `production-rendered anonymous cards filter locally and never request catalogue data`, retain the current `monitoring.focus()` / `Enter` sequence and add:

  ```js
  await expect(monitoring).toBeFocused();
  await expect(monitoring).toHaveAttribute('aria-pressed', 'true');
  await expect(page).not.toHaveURL(/\?.+/);
  ```

  The pre-existing request-array assertion remains the proof that filtering performs no network call.

- [ ] **Step 4: Run focused tests to establish the red baseline**

  Run: `npm --prefix web test -- --grep "sticky without hiding|normal flow and chips readable"`

  Expected: FAIL because `.catalog-controls` currently has no sticky positioning or opaque background surface. The narrow test should pass only after it records the current normal-flow baseline.

- [ ] **Step 5: Commit the specification tests**

  ```bash
  git add web/tests/catalog.test.mjs
  git commit -m "test: specify responsive sticky catalogue controls"
  ```

### Task 2: Implement responsive sticky controls without changing filtering

**Files:**
- Modify: `web/src/styles/app.css`
- Test: `web/tests/catalog.test.mjs`

**Interfaces:**
- Consumes: the current `.catalog-controls` wrapping search/category markup and card links in `CatalogCard.astro`.
- Produces: a desktop sticky control surface and a narrow normal-flow reset that preserve the same DOM and client-side filter behavior.

- [ ] **Step 1: Add the desktop sticky surface to the existing `.catalog-controls` rule**

  Keep the existing grid spacing, border, and margin, then add an opaque portal-colored surface and stacking context:

  ```css
  .catalog-controls {
    position: sticky;
    top: 0;
    z-index: 1;
    gap: 1rem;
    margin-block: 0 1rem;
    border-block: 1px solid #334155;
    padding-block: 0.75rem 1rem;
    background: #0f172a;
  }

  .catalog-card h4 a {
    scroll-margin-top: 10rem;
  }
  ```

  The background must stay opaque while the current dark theme is in use. `scroll-margin-top` ensures browser focus scrolling positions the primary link below the sticky surface.

- [ ] **Step 2: Prevent chip-label shrinking while preserving wrapping**

  Extend the existing filter-button rule:

  ```css
  .filter-button {
    flex: 0 0 auto;
    min-width: 0;
    max-width: 100%;
    overflow-wrap: anywhere;
    text-align: start;
  }
  ```

  Keep `.category-filters { flex-wrap: wrap; }`; do not add a horizontal scrolling region unless the narrow rendered test shows wrapping cannot fit valid labels.

- [ ] **Step 3: Reset sticky behavior at the existing mobile shell breakpoint**

  Add this inside `@media (max-width: 55.999rem)`, alongside the one-column `.catalog-shell` rule:

  ```css
  .catalog-controls {
    position: static;
    background: transparent;
  }
  ```

- [ ] **Step 4: Run focused wide and narrow browser tests**

  Run: `npm --prefix web test -- --grep "sticky without hiding|normal flow and chips readable|at 320px"`

  Expected: PASS. If geometry is within browser rounding variance, use a 2px tolerance only; do not replace geometry assertions with computed-position assertions.

- [ ] **Step 5: Commit the CSS implementation**

  ```bash
  git add web/src/styles/app.css web/tests/catalog.test.mjs
  git commit -m "feat: keep catalogue controls sticky on wide screens"
  ```

### Task 3: Verify affected catalogue states and resolve the issue

**Files:**
- Modify: `web/tests/catalog.test.mjs` only if a test needs a stable selector
- Test: `web/tests/catalog.test.mjs`, `web/tests/dev-preview.test.mjs`

**Interfaces:**
- Consumes: production fixture routes `/` and `/__test/admin`, plus the existing stale, empty-result, and admin card scenarios.
- Produces: evidence that the responsive interaction layer does not change authorization, filtering, or assistive behavior.

- [ ] **Step 1: Confirm existing state coverage remains explicit**

  Keep the current tests that prove: anonymous filtering does not expose the admin card; stale information is in the rail; empty recovery is visible for zero matches; and `/__test/admin` shows its authorized admin card and header action. Do not add new server fixtures or alter authorized card data.

- [ ] **Step 2: Run static UI checks and the full production browser suite**

  Run: `npm --prefix web run lint`

  Expected: PASS with no Astro or TypeScript diagnostics.

  Run: `npm --prefix web test`

  Expected: PASS, including filtering/no-fetch behavior, axe checks, focus order, stale/empty/admin states, wide sticky geometry, and narrow overflow behavior.

- [ ] **Step 3: Run the development-preview suite**

  Run: `npm --prefix web run test:dev`

  Expected: PASS; local Astro preview retains the same search/filter interaction.

- [ ] **Step 4: Close GitHub issue #9 with verification evidence**

  ```bash
  gh issue comment 9 --body "Added responsive sticky-control behavior and browser coverage for wide sticky geometry, narrow normal flow/chip readability, focus visibility, and existing authorized/stale/empty/admin states. Verified with npm --prefix web run lint, npm --prefix web test, and npm --prefix web run test:dev."
  gh issue close 9
  ```

## Self-Review

- **Spec coverage:** Task 1 creates observable wide sticky, narrow normal-flow, chip readability, focus, ARIA, and no-navigation test seams. Task 2 adds the smallest CSS-only implementation. Task 3 verifies the existing authorized, stale, empty, and admin state contract before resolution.
- **Scope:** No server, authentication, theme, profile, catalogue-data, or card-grid behavior changes are planned. #11 remains the next independent grid/card-density issue.
- **Type consistency:** The plan uses only existing classes, data attributes, and accessible names from the Astro components and native client script.
- **No-placeholder check:** Each requirement has an exact test, CSS rule, or verification command; conditional tolerance is limited to browser pixel rounding.
