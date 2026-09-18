# Compact Catalogue Shell and Information Rail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #10 by turning the existing information-hierarchy markup into a compact responsive shell with a fixed information rail and flexible catalogue region.

**Architecture:** Keep the server-rendered `index.astro` structure and all authorization/filtering behavior unchanged. Make the layout change in `app.css`: at useful desktop widths the existing `.catalog-shell` becomes a two-column grid, while mobile retains DOM order and stacks the rail before the controls and cards. Prove the behavior through the production-rendered Playwright fixture, using geometry and visible states rather than CSS implementation assertions.

**Tech Stack:** Astro 7 static UI, Tailwind CSS 4 source stylesheet, native browser JavaScript, Playwright 1.61, axe-core Playwright.

**Spec:** `docs/superpowers/specs/2026-09-18-compact-catalog-layout-design.md`

## Global Constraints

- Do not change catalogue metadata, sorting, visibility, target navigation, or the server-rendered authorization boundary.
- Preserve source and keyboard order: skip link, rail information/status, search/categories, empty feedback, catalogue cards.
- The rail must contain no target-application links; the main region owns controls, empty feedback, and cards.
- Use a bounded comfortable viewport width, a roughly 15–18rem rail, and responsive grid tracks with a practical 16–18rem card minimum.
- At 320 CSS pixels and above, the page must have no horizontal overflow and all interactive controls must retain a 44 by 44 CSS-pixel target.
- Preserve semantic landmarks, visible focus, 200% zoom usability, and stable anonymous, authenticated, stale, empty, and admin-linked layouts.
- Do not add dependencies, client requests, cookies, session writes, or server/API changes.

---

## File Structure

- `web/src/styles/app.css` — owns viewport bounds, compact spacing, the shell’s desktop/mobile layout, and card-grid sizing.
- `web/tests/catalog.test.mjs` — production-rendered browser tests for shell geometry, source/focus order, small-screen overflow, and representative rendered states.
- `web/src/pages/index.astro` — review-only in this issue; it already establishes the required rail/main DOM order. Do not change it unless a test demonstrates a semantic defect that CSS cannot address.
- `web/src/components/CategoryFilter.astro` and `web/src/components/CatalogCard.astro` — review-only; their existing controls and card markup remain the interface consumed by the stylesheet and tests.

### Task 1: Specify the responsive shell in browser tests

**Files:**
- Modify: `web/tests/catalog.test.mjs`
- Review: `web/src/pages/index.astro`, `web/src/components/CategoryFilter.astro`, `web/src/components/CatalogCard.astro`

**Interfaces:**
- Consumes: the existing selectors `aside.catalog-information-rail`, `.catalog-main`, `[data-catalog-search]`, `[data-catalog-grid]`, and `article[data-catalog-item]`.
- Produces: behavior-focused assertions that the stylesheet in Task 2 must satisfy at desktop and narrow viewports.

- [ ] **Step 1: Add a failing desktop shell test after the anonymous catalogue test**

  Add a test that sets a `1440 x 900` viewport, loads `baseURL`, and proves that the rail and main region are adjacent columns, the main region receives most of the shell width, and the catalogue has more than one visible card column:

  ```js
  test('production-rendered catalogue uses a compact desktop rail and flexible multi-column results', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(baseURL);

    const rail = page.locator('aside.catalog-information-rail');
    const main = page.locator('.catalog-main');
    const grid = page.locator('[data-catalog-grid]');
    const [railBox, mainBox, gridBox, firstCard, secondCard] = await Promise.all([
      rail.boundingBox(), main.boundingBox(), grid.boundingBox(),
      page.locator('article[data-catalog-item]').nth(0).boundingBox(),
      page.locator('article[data-catalog-item]').nth(1).boundingBox(),
    ]);

    expect(railBox.x).toBeLessThan(mainBox.x);
    expect(railBox.y).toBeCloseTo(mainBox.y, 1);
    expect(mainBox.width).toBeGreaterThan(railBox.width * 1.5);
    expect(gridBox.width).toBeCloseTo(mainBox.width, 1);
    expect(secondCard.y).toBeCloseTo(firstCard.y, 1);
    expect(secondCard.x).toBeGreaterThan(firstCard.x);
  });
  ```

- [ ] **Step 2: Run the new desktop test and verify it fails against the current one-column layout**

  Run: `npm --prefix web test -- --grep "compact desktop rail"`

  Expected: FAIL because the current `.catalog-shell` has no desktop two-column layout and the current grid has only two fixed columns once it reaches 40rem, leaving the shell constrained to 64rem.

- [ ] **Step 3: Add a failing 320px layout/order/target-size test**

  Add a test that sets a `320 x 740` viewport, loads `baseURL`, confirms that rail content is above the search control in both DOM and screen geometry, that the cards form one column, and that the page and controls meet the narrow-screen constraints:

  ```js
  test('production-rendered catalogue stacks the information rail before one-column controls and cards at 320px', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 740 });
    await page.goto(baseURL);

    const rail = page.locator('aside.catalog-information-rail');
    const search = page.getByRole('searchbox', { name: 'Search catalog' });
    const firstCard = page.locator('article[data-catalog-item]').first();
    const secondCard = page.locator('article[data-catalog-item]').nth(1);
    const [railBox, searchBox, firstCardBox, secondCardBox] = await Promise.all([
      rail.boundingBox(), search.boundingBox(), firstCard.boundingBox(), secondCard.boundingBox(),
    ]);

    expect(railBox.y).toBeLessThan(searchBox.y);
    expect(searchBox.y).toBeLessThan(firstCardBox.y);
    expect(secondCardBox.y).toBeGreaterThan(firstCardBox.y);
    expect(await search.evaluate((element) => element.getBoundingClientRect().width)).toBeGreaterThan(0);
    await expect(page.getByRole('button', { name: 'All' })).toHaveCSS('min-height', '44px');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
  });
  ```

- [ ] **Step 4: Run the narrow test as a baseline guard for the existing mobile behavior**

  Run: `npm --prefix web test -- --grep "stacks the information rail"`

  Expected: PASS or FAIL only for the explicitly asserted geometry; either outcome records the current 320px contract before Task 2. The desktop test in Step 2 is the required red test for this task.

- [ ] **Step 5: Commit the red tests**

  ```bash
  git add web/tests/catalog.test.mjs
  git commit -m "test: specify compact catalogue shell layout"
  ```

### Task 2: Implement the compact shell and responsive catalogue grid

**Files:**
- Modify: `web/src/styles/app.css`
- Test: `web/tests/catalog.test.mjs`

**Interfaces:**
- Consumes: the existing `.catalog-shell` containing `.catalog-information-rail` followed by `.catalog-main`; `.catalog-main` contains `.catalog-controls` then `[data-catalog-grid]`.
- Produces: a CSS-only responsive layout that preserves DOM order and gives the Task 1 geometry tests their desktop and narrow contracts.

- [ ] **Step 1: Add a bounded, compact shell and desktop rail rule in `@layer components`**

  Replace the existing `64rem` shared width constraint with a viewport-efficient bound and add these desktop-oriented base rules near the `.catalog-shell` declaration. Keep `.site-header` and `.site-main` aligned by sharing the same `width` rule.

  ```css
  .site-header,
  .site-main {
    width: min(88rem, calc(100% - 2rem));
    margin-inline: auto;
  }

  .site-header {
    padding-block: 1rem 0.75rem;
  }

  .site-main {
    padding-block: 0.75rem 2rem;
  }

  .catalog-shell {
    display: grid;
    grid-template-columns: minmax(15rem, 18rem) minmax(0, 1fr);
    align-items: start;
    gap: 1.5rem;
  }

  .catalog-information-rail {
    padding-block: 0.5rem;
  }

  .catalog-main {
    min-width: 0;
  }

  .catalog-controls {
    margin-block: 0 1rem;
    padding-block: 0 1rem;
  }
  ```

- [ ] **Step 2: Replace fixed card columns with responsive tracks and compact card spacing**

  Replace the base single-column rule plus the `@media (min-width: 40rem)` two-column override with responsive tracks that use the available main region. Preserve `min-width: 0` and wrapping rules; reduce only unnecessary card whitespace.

  ```css
  .catalog-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 17rem), 1fr));
    gap: 0.75rem;
  }

  .catalog-card {
    gap: 0.75rem;
    padding: 1rem;
  }
  ```

- [ ] **Step 3: Add a mobile breakpoint that stacks without CSS reordering**

  Add this breakpoint before the existing `max-width: 39.999rem` header rule. It deliberately changes only layout, so the existing source and focus order remain intact.

  ```css
  @media (max-width: 55.999rem) {
    .catalog-shell {
      grid-template-columns: minmax(0, 1fr);
      gap: 1rem;
    }

    .catalog-information-rail {
      padding-block: 0;
    }
  }

  @media (max-width: 24rem) {
    .site-header,
    .site-main {
      width: min(100% - 1rem, 88rem);
    }

    .search-input-row {
      align-items: stretch;
      flex-direction: column;
    }

    .clear-search {
      width: 100%;
    }
  }
  ```

- [ ] **Step 4: Run the focused layout tests and make the smallest CSS corrections needed for passing geometry**

  Run: `npm --prefix web test -- --grep "compact desktop rail|stacks the information rail|wraps maximum valid catalogue text"`

  Expected: PASS. If Playwright rounding makes a `toBeCloseTo(..., 1)` assertion flaky, keep the layout and change only that assertion to use a 2px geometry tolerance; do not replace visible-geometry checks with selectors or computed grid-template assertions.

- [ ] **Step 5: Commit the implementation**

  ```bash
  git add web/src/styles/app.css web/tests/catalog.test.mjs
  git commit -m "feat: add compact responsive catalogue shell"
  ```

### Task 3: Verify the existing rendered-state contract

**Files:**
- Modify: `web/tests/catalog.test.mjs`
- Test: `web/tests/catalog.test.mjs`, `web/tests/dev-preview.test.mjs`
- Review: `tests/integration/browserfixture/main.go`, `web/src/pages/index.astro`

**Interfaces:**
- Consumes: the browser fixture routes `/`, `/__test/admin`, and existing dev fixture data with stale, long-content, and authorized-card coverage.
- Produces: an end-to-end verification suite demonstrating stable shell behavior without changes to client data or authorization boundaries.

- [ ] **Step 1: Extend the existing admin test with stable-shell assertions**

  In `production-rendered admin identity receives its authorized card set`, assert that the admin route continues to render the same shell landmarks and that the admin action remains in the header rather than the rail:

  ```js
  await expect(page.locator('aside.catalog-information-rail')).toBeVisible();
  await expect(page.locator('.catalog-main')).toBeVisible();
  await expect(page.locator('aside.catalog-information-rail').getByRole('link')).toHaveCount(0);
  await expect(page.locator('header.site-header').getByRole('link', { name: 'Admin diagnostics' })).toBeVisible();
  ```

- [ ] **Step 2: Keep issue #8's empty-result behavior out of this change**

  Do not add empty-result controls, recovery actions, or `data-empty-results` markup. #8 is open and owns that behavior. The Task 2 shell must nevertheless leave `.catalog-main` as the single region where that later component will render.

- [ ] **Step 3: Run UI static checks and the full production-rendered browser suite**

  Run: `npm --prefix web run lint`

  Expected: PASS with no Astro or TypeScript diagnostics.

  Run: `npm --prefix web test`

  Expected: PASS, including anonymous, admin, long-content, accessibility, focus-order, and new responsive-shell tests.

- [ ] **Step 4: Run the development-preview browser suite**

  Run: `npm --prefix web run test:dev`

  Expected: PASS; this confirms the compact shell is also usable under the local Astro development preview.

- [ ] **Step 5: Commit the verification coverage**

  ```bash
  git add web/tests/catalog.test.mjs
  git commit -m "test: cover catalogue shell states"
  ```

## Self-Review

- **Spec coverage:** Task 1 specifies desktop width, rail/main hierarchy, multi-column cards, source order, narrow stacking, target size, and overflow. Task 2 implements those requirements in CSS without changing authorization or data flow. Task 3 verifies anonymous and admin layouts, existing stale/long-content coverage, and explicitly keeps #8's empty-result interaction out of #10.
- **Scope:** No server, authentication, catalog visibility, theme, profile, or filtering behavior changes are planned. #11 through #16 remain separate open work.
- **Type consistency:** The plan uses only current static CSS classes and data attributes: `.catalog-shell`, `.catalog-information-rail`, `.catalog-main`, `[data-catalog-grid]`, `[data-catalog-search]`, and `article[data-catalog-item]`.
- **No-placeholder check:** The plan contains no unassigned implementation work. The one conditional empty-result assertion explicitly protects issue boundaries because #8 is currently open and owns that UI behavior.
