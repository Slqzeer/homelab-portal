# Reorganize Catalogue Information Hierarchy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate descriptive catalogue information from actionable cards by rendering portal identity, the visible result count, and stale-catalogue information in an information rail before the search-and-results region.

**Architecture:** Keep the BFF as the authorization boundary: it continues to compute `pageData.Items` with `catalog.Visible` before rendering HTML. Restructure the equivalent Astro development page and Go production template into a `catalog-shell` containing a semantic `aside.catalog-information-rail` followed by a `div.catalog-main`; the existing local script continues to update the same `data-catalog-status` element, now located in the rail.

**Tech Stack:** Go `net/http` and `html/template`, Astro, Tailwind CSS, native browser JavaScript, Go integration tests, Playwright.

**Spec:** GitHub issue [#6](https://github.com/Slqzeer/homelab-portal/issues/6); `docs/superpowers/specs/2026-09-18-catalog-search-information-hierarchy-design.md`; `docs/superpowers/specs/2026-09-18-compact-catalog-layout-design.md`.

## Global Constraints

- Filtering and result counts operate only on `article[data-catalog-item]` elements already emitted by the BFF; no catalogue API, serialized catalogue, or client-side authorization is added.
- The browser must not receive unauthorized catalogue names, descriptions, categories, or target URLs.
- Stale status describes catalogue freshness only; it must not imply target health.
- Preserve HTML escaping, same-origin local assets, existing security headers, and the anonymous/member/admin server-rendered states.
- Preserve source and keyboard order: portal information, count/stale state, search/categories, empty feedback, then cards.
- Do not implement clear-search recovery, sticky controls, responsive shell/grid changes, themes, or profile UI; they belong to dependent issues #7–#16.

---

## Planned File Structure

| File | Change | Responsibility |
| --- | --- | --- |
| `web/src/pages/index.astro` | Modify | Keep the development preview structurally equivalent to production: information rail first, then controls and cards. |
| `web/src/components/CategoryFilter.astro` | Modify | Render only search and category controls; retain existing filter data attributes. |
| `web/src/styles/app.css` | Modify | Add scoped rail/main structural classes without introducing the later responsive shell or dense-grid work. |
| `internal/http/templates.go` | Modify | Render the same information-first structure from authorized `pageData`. |
| `tests/integration/rendered_ui_test.go` | Modify | Assert ordering, unique status rendering, stale placement, and absence of unauthorized content in rendered BFF HTML. |
| `web/tests/catalog.test.mjs` | Modify | Assert the visible production page places the rail before the search and cards in DOM/focus order. |

### Task 1: Prove the required hierarchy at the BFF boundary

**Files:**
- Modify: `tests/integration/rendered_ui_test.go`

**Interfaces:**
- Consumes: `GET /` rendered by `portalhttp.New` using `catalog.Visible`.
- Produces: `TestRenderedCatalogSeparatesInformationFromActions`, which protects the semantic/ordering contract for anonymous, authenticated, stale, and admin-linked page states.

- [ ] **Step 1: Write a failing rendered-HTML hierarchy test.**

  Add a test that builds a store with one public item and one admin item, renders anonymous, authenticated, and exact-`portal-admin` requests, and asserts the required markers occur in this order:

  ```go
  func assertInOrder(t *testing.T, html string, markers ...string) {
      t.Helper()
      offset := 0
      for _, marker := range markers {
          index := strings.Index(html[offset:], marker)
          if index < 0 {
              t.Fatalf("missing %q in %s", marker, html)
          }
          offset += index + len(marker)
      }
  }

  assertInOrder(t, anonymous,
      `<aside class="catalog-information-rail"`,
      `data-catalog-status role="status" aria-live="polite" aria-atomic="true"`,
      `<section class="catalog-controls"`,
      `<div id="catalog-items" class="catalog-grid" data-catalog-grid>`,
  )
  assert.Equal(t, 1, strings.Count(anonymous, `data-catalog-status`))
  assertContainsNone(t, anonymous, "Secret Admin", "secret-admin.tail.example", `href="/admin"`)
  assertContainsAll(t, authenticated, `>Sign out<`)
  assertContainsAll(t, admin, `href="/admin"`, "Secret Admin")
  ```

  Create a stale snapshot as in `TestRenderedStaleStateDescribesCatalogueFreshnessOnly`, then assert the stale `aside` occurs after `catalog-information-rail` begins and before `catalog-controls` begins. Retain the existing assertion that stale copy contains no health claim.

- [ ] **Step 2: Run the focused test to verify the current templates fail.**

  Run: `go test ./tests/integration -run 'TestRenderedCatalogSeparatesInformationFromActions|TestRenderedStaleStateDescribesCatalogueFreshnessOnly' -count=1`

  Expected: FAIL because `catalog-information-rail` is absent and the current status remains inside `catalog-controls`.

- [ ] **Step 3: Commit the red test separately.**

  ```bash
  git add tests/integration/rendered_ui_test.go
  git commit -m "test: specify catalogue information rail"
  ```

### Task 2: Render the information rail in development and production HTML

**Files:**
- Modify: `web/src/pages/index.astro`
- Modify: `web/src/components/CategoryFilter.astro`
- Modify: `internal/http/templates.go`

**Interfaces:**
- Consumes: `items`, `categories`, and development-only `stale` in `index.astro`; `pageData{Items, Categories, Stale, Authenticated, IsAdmin, CSRFToken, Styles}` in `templates.go`.
- Produces: A single `aside.catalog-information-rail` before `div.catalog-main`; `CategoryFilter` accepts only `categories: string[]`; the existing `data-catalog-status` remains one atomic polite live region.

- [ ] **Step 1: Update the Astro component contract and page structure.**

  Remove `count` from `CategoryFilter.astro` and delete its `catalog-status` paragraph. In `index.astro`, pass `<CategoryFilter categories={categories} />` and replace the heading/banner/controls/results sequence with this structure inside `main`:

  ```astro
  <div class="catalog-shell">
    <aside class="catalog-information-rail" aria-labelledby="portal-title">
      <p class="eyebrow">Tailnet catalogue</p>
      <h1 id="portal-title">Homelab Portal</h1>
      <p class="eyebrow">Available to you</p>
      <h2>Service catalogue</h2>
      <p>Open an intentionally published service. Each target application manages its own access.</p>
      <p class="catalog-status" data-catalog-status role="status" aria-live="polite" aria-atomic="true">
        {items.length} catalog {items.length === 1 ? 'item' : 'items'}
      </p>
      <StaleBanner stale={import.meta.env.DEV} />
    </aside>
    <div class="catalog-main">
      <CategoryFilter categories={categories} />
      <!-- retain the existing labelled results section and data-catalog-grid -->
    </div>
  </div>
  ```

  Keep account/admin actions in `header.site-header` and do not add any target-application link to the rail.

- [ ] **Step 2: Mirror that structure in `homeTemplate`.**

  Keep the existing authenticated sign-out/sign-in and conditional admin link unchanged. In the main element, render the rail first with the exact same portal copy, one `data-catalog-status` paragraph using `{{len .Items}}`, and the conditional stale `aside`; then render `catalog-controls` and the current results section inside `div.catalog-main`.

  ```gotemplate
  <div class="catalog-shell"><aside class="catalog-information-rail" aria-labelledby="portal-title">
  <p class="eyebrow">Tailnet catalogue</p><h1 id="portal-title">Homelab Portal</h1>
  <p class="eyebrow">Available to you</p><h2>Service catalogue</h2>
  <p>Open an intentionally published service. Each target application manages its own access.</p>
  <p class="catalog-status" data-catalog-status role="status" aria-live="polite" aria-atomic="true">{{len .Items}} catalog {{if eq (len .Items) 1}}item{{else}}items{{end}}</p>
  {{if .Stale}}<aside class="stale-banner" role="status" aria-live="polite"><strong>Catalogue is stale. Catalogue information is not current.</strong> Showing the last known catalogue links.</aside>{{end}}
  </aside><div class="catalog-main">
  ```

  Close `catalog-main` and `catalog-shell` after the unmodified results section. Do not add fields to `pageData`, change `home`, or change `web/public/app.js`: its `data-catalog-status` query continues to work after relocation.

- [ ] **Step 3: Add minimal structural CSS.**

  In `web/src/styles/app.css`, add classes that keep the regions visually distinct while deferring the desktop rail/grid layout to #10 and #11:

  ```css
  .catalog-shell,
  .catalog-main,
  .catalog-information-rail {
    min-width: 0;
  }

  .catalog-information-rail > :last-child {
    margin-bottom: 0;
  }

  .catalog-information-rail .stale-banner {
    margin-block: 1.5rem 0;
  }
  ```

  Remove selectors made obsolete by moving `h1` and `h2` from their previous locations only if no admin-page selector depends on them. Preserve the current 44px button/input targets, focus style, card styling, and narrow-width header behavior.

- [ ] **Step 4: Run the focused Go tests and Astro checks.**

  Run: `go test ./tests/integration -run 'TestRenderedCatalogSeparatesInformationFromActions|TestRenderedStaleStateDescribesCatalogueFreshnessOnly' -count=1`

  Expected: PASS.

  Run: `npm --prefix web run lint`

  Expected: Astro check exits 0.

- [ ] **Step 5: Commit the rendering change.**

  ```bash
  git add web/src/pages/index.astro web/src/components/CategoryFilter.astro web/src/styles/app.css internal/http/templates.go
  git commit -m "feat: separate catalogue information rail"
  ```

### Task 3: Verify the rendered browser order without expanding later-ticket scope

**Files:**
- Modify: `web/tests/catalog.test.mjs`

**Interfaces:**
- Consumes: The production BFF fixture at `https://127.0.0.1:4173` and its anonymous/admin routes.
- Produces: Browser evidence that the rail is present before the search/results region and that the count remains a single status element while filtering authorized cards locally.

- [ ] **Step 1: Add a failing browser assertion to the anonymous production test.**

  Immediately after the existing heading/card checks, add:

  ```js
  const rail = page.locator('aside.catalog-information-rail');
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  const results = page.locator('[data-catalog-grid]');

  await expect(rail).toContainText('Tailnet catalogue');
  await expect(rail).toContainText('Homelab Portal');
  await expect(rail.getByRole('status')).toHaveText('4 catalog items');
  await expect(page.locator('[data-catalog-status]')).toHaveCount(1);
  expect(await rail.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await search.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-grid]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  ```

  Keep the existing request list equality assertion after filtering: it remains the browser proof that moving the status did not add a catalogue-data request.

- [ ] **Step 2: Run the browser test to verify it fails before Task 2 is applied.**

  Run: `npm --prefix web test -- --grep 'production-rendered anonymous cards filter locally'`

  Expected: FAIL because `aside.catalog-information-rail` does not exist.

- [ ] **Step 3: Run the browser test after Task 2 and add stale/admin coverage.**

  Extend the existing admin test only to assert that the authorized admin link remains visible and the rail contains no `a[href^="https://"]` target-application links. Add a stale-route fixture assertion if the local test server exposes one; otherwise keep stale placement as the BFF integration contract in Task 1 rather than inventing a new browser fixture.

  Run: `npm --prefix web test -- --grep 'production-rendered anonymous cards filter locally|production-rendered admin identity receives its authorized card set'`

  Expected: PASS.

- [ ] **Step 4: Run the complete verification set.**

  Run: `go test ./...`

  Expected: PASS.

  Run: `npm --prefix web run lint`

  Expected: PASS.

  Run: `npm --prefix web test`

  Expected: PASS.

- [ ] **Step 5: Commit browser coverage.**

  ```bash
  git add web/tests/catalog.test.mjs tests/integration/rendered_ui_test.go
  git commit -m "test: cover catalogue information hierarchy"
  ```

## Self-Review

- **Spec coverage:** Task 1 proves the rail contains portal information, count, and stale state; Task 2 moves those elements in both renderer paths while preserving BFF authorization; Task 3 verifies the visible production hierarchy and the existing local filtering/no-fetch behavior. Anonymous, authenticated, stale, empty, and admin-linked output is covered by the combined BFF fixture cases and existing empty rendering.
- **Scope:** The plan intentionally does not change filtering behavior (#7), empty-result recovery (#8), sticky/responsive interaction (#9), the compact desktop shell (#10), or grid/card density (#11).
- **Placeholder scan:** No unfinished markers or unspecified implementation steps remain.
- **Type consistency:** No Go public interface changes are introduced. `CategoryFilter` changes from `{ categories, count }` to `{ categories }`, and both documented callers use that signature.
