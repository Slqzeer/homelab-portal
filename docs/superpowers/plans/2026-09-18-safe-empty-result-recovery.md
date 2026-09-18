# Safe Empty-Result Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give visitors whose local search and category filters match no authorized cards concise, accessible ways to clear the search or return to the All category.

**Architecture:** Keep filtering and recovery entirely in `web/public/app.js`, operating only on `article[data-catalog-item]` elements already rendered by the BFF. Render one initially hidden recovery region in both the Astro development page and the production Go template; JavaScript shows it only for a zero-match result caused by an active filter. The server-side empty-catalogue message remains unchanged because it represents a catalogue with no authorized items, not a client-side filter outcome.

**Tech Stack:** Go `html/template`, Astro, native browser JavaScript, Tailwind CSS, Go integration tests, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-18-catalog-search-information-hierarchy-design.md`

## Global Constraints

- Filter only authorized `article[data-catalog-item]` elements emitted by the BFF; add no API, serialized catalogue data, or client-side authorization.
- Empty-result copy must not name, count, or imply unauthorized catalogue items.
- Search and category filters retain AND semantics; clearing either constraint preserves the other.
- The recovery actions must be keyboard-operable and expose specific visible labels.
- Keep the production Go template and the Astro development page structurally equivalent.
- Do not change sticky/responsive layout (#9), shell/grid layout (#10/#11), theme behavior (#12–#15), or profile UI (#16).

---

## File Structure

| File | Change | Responsibility |
| --- | --- | --- |
| `web/src/pages/index.astro` | Modify | Render the development empty-result recovery controls in the main results section before the grid. |
| `internal/http/templates.go` | Modify | Render the same recovery region in production BFF HTML. |
| `web/public/app.js` | Modify | Determine active zero-match state; wire clear-search and return-to-All recovery actions. |
| `web/src/styles/app.css` | Modify | Give recovery feedback readable, keyboard-visible action layout without changing card/grid behavior. |
| `tests/integration/rendered_ui_test.go` | Modify | Assert production-rendered recovery markup is present and reveals no hidden catalogue data. |
| `web/tests/catalog.test.mjs` | Modify | Exercise recovery in a real production-rendered browser page and verify local-only behavior. |

### Task 1: Specify the production and development recovery markup

**Files:**
- Modify: `tests/integration/rendered_ui_test.go:30-76`
- Modify: `web/src/pages/index.astro:39-47`
- Modify: `internal/http/templates.go:28-32`

**Interfaces:**
- Consumes: Existing filter selectors `[data-catalog-search]`, `[data-category-filter]`, and result container `#catalog-items`.
- Produces: A hidden `section[data-empty-results]` containing `button[data-empty-clear-search]` labelled `Clear search` and `button[data-empty-clear-category]` labelled `Return to All`.

- [ ] **Step 1: Write the failing rendered-HTML contract test.**

  In `TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary`, add these required fragments to the anonymous `assertContainsAll` call:

  ```go
  `data-empty-results hidden`,
  `data-empty-clear-search>Clear search</button>`,
  `data-empty-clear-category>Return to All</button>`,
  `No catalog items match these filters.`,
  ```

  Retain the existing assertion that anonymous HTML excludes the admin-only card's name, category, and URL. Do not assert that static HTML excludes authorized cards: those cards are intentionally rendered.

- [ ] **Step 2: Run the narrow integration test to verify it fails.**

  Run: `go test ./tests/integration -run TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary -count=1`

  Expected: FAIL because production HTML does not yet contain `data-empty-results` or recovery actions.

- [ ] **Step 3: Add identical semantic recovery regions to both renderers.**

  In `index.astro`, add the hidden section inside the results `section`, immediately before `#catalog-items`:

  ```astro
  <section class="empty-filter-results" data-empty-results hidden aria-labelledby="empty-filter-results-heading">
    <h3 id="empty-filter-results-heading">No catalog items match these filters.</h3>
    <p>Clear a filter to see available catalog items.</p>
    <div class="empty-filter-actions">
      <button type="button" data-empty-clear-search>Clear search</button>
      <button type="button" data-empty-clear-category>Return to All</button>
    </div>
  </section>
  ```

  In `homeTemplate`, add the same element with the same data attributes, labels, copy, and element ordering after the controls and before the grid. Do not change the existing `{{else}}<p class="empty-catalog">…` branch.

- [ ] **Step 4: Run the integration test to verify it passes.**

  Run: `go test ./tests/integration -run TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary -count=1`

  Expected: PASS; the BFF output includes hidden recovery markup and retains the anonymous authorization boundary.

- [ ] **Step 5: Commit the renderer contract.**

  ```bash
  git add tests/integration/rendered_ui_test.go web/src/pages/index.astro internal/http/templates.go
  git commit -m "feat: render empty filter recovery controls"
  ```

### Task 2: Implement local recovery state and actions

**Files:**
- Modify: `web/tests/catalog.test.mjs:10-82`
- Modify: `web/public/app.js:1-58`
- Modify: `web/src/styles/app.css:310-322`

**Interfaces:**
- Consumes: `section[data-empty-results]`, `button[data-empty-clear-search]`, `button[data-empty-clear-category]`, `input[data-catalog-search]`, category buttons, and authorized cards.
- Produces: `update()` that hides the recovery region unless `visible === 0` and either a normalized query or selected category is active; local click behavior that clears exactly one constraint and immediately updates the existing polite count.

- [ ] **Step 1: Write the failing browser scenario.**

  In the first Playwright test, declare the recovery controls:

  ```js
  const emptyResults = page.locator('[data-empty-results]');
  const recoveryClearSearch = page.getByRole('button', { name: 'Clear search', exact: true }).last();
  const returnToAll = page.getByRole('button', { name: 'Return to All' });
  ```

  After selecting `Monitoring` and entering a query with no monitoring match, assert zero visible articles, `0 catalog items`, and a visible recovery section. Then:

  ```js
  await recoveryClearSearch.click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  await expect(monitoring).toHaveAttribute('aria-pressed', 'true');
  await expect(visibleArticles(page)).toHaveCount(2);

  await search.fill('graf');
  await expect(visibleArticles(page)).toHaveCount(0);
  await returnToAll.click();
  await expect(all).toHaveAttribute('aria-pressed', 'true');
  await expect(search).toHaveValue('graf');
  await expect(visibleArticles(page)).toHaveCount(1);
  await expect(emptyResults).toBeHidden();
  ```

  Keep the existing request snapshot assertion after this sequence, proving neither recovery action makes a network request.

- [ ] **Step 2: Run the targeted browser test to verify it fails.**

  Run: `npm --prefix web test -- --grep "production-rendered anonymous cards filter locally"`

  Expected: FAIL because no recovery region or actions are wired.

- [ ] **Step 3: Extend the local filtering controller.**

  In `app.js`, query the three new controls next to `clearSearch`:

  ```js
  const emptyResults = document.querySelector('[data-empty-results]');
  const emptyClearSearch = document.querySelector('[data-empty-clear-search]');
  const emptyClearCategory = document.querySelector('[data-empty-clear-category]');
  ```

  In `update()`, after cards and the status are updated, set the recovery state from active filters only:

  ```js
  const hasActiveFilter = query !== '' || selectedCategory !== '';
  if (emptyResults) {
    emptyResults.hidden = visible !== 0 || !hasActiveFilter;
  }
  ```

  Extract `selectCategory(category)` to set `selectedCategory`, update every category button's `aria-pressed` by comparing its normalized `dataset.categoryFilter`, then call `update()`. Use it in category-button handlers and in the `Return to All` handler. Wire `emptyClearSearch` to set `search.value = ''`, call `update()`, and focus `search`; wire `emptyClearCategory` to call `selectCategory('')`. Do not focus-move after `Return to All`, because the activated button remains the appropriate focus target.

- [ ] **Step 4: Style feedback without altering grid or card layout.**

  Add component rules after `.empty-catalog`:

  ```css
  .empty-filter-results {
    margin-bottom: 1rem;
    border: 1px dashed #475569;
    border-radius: 0.75rem;
    padding: 1rem;
    color: #cbd5e1;
  }

  .empty-filter-results h3 { margin-bottom: 0.5rem; }
  .empty-filter-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; }
  ```

  Apply the existing button visual declarations to `.empty-filter-actions button` by grouping it with `.button, .filter-button` and keep its 44px minimum target. Do not add live-region semantics to this message: the existing atomic polite count announces the filter outcome once.

- [ ] **Step 5: Run focused verification.**

  Run: `npm --prefix web test -- --grep "production-rendered anonymous cards filter locally"`

  Expected: PASS; recovery actions preserve the unrelated filter, count updates immediately, focus returns only after clear-search, and the request list is unchanged.

- [ ] **Step 6: Commit the local recovery behavior.**

  ```bash
  git add web/public/app.js web/src/styles/app.css web/tests/catalog.test.mjs
  git commit -m "feat: recover from empty catalogue filters"
  ```

### Task 3: Verify the complete issue boundary

**Files:**
- Modify: `tests/integration/rendered_ui_test.go:30-76` only if a missing production markup assertion is identified during verification.
- Modify: `web/tests/catalog.test.mjs:10-82` only if a missing acceptance-case assertion is identified during verification.

**Interfaces:**
- Consumes: The renderer contract and browser behavior from Tasks 1–2.
- Produces: Evidence that #8 satisfies safe recovery without exposing data or modifying server authorization.

- [ ] **Step 1: Run Go tests.**

  Run: `go test ./...`

  Expected: PASS; rendered HTML still uses escaped templates and all BFF authorization tests pass.

- [ ] **Step 2: Run web validation and the complete production-browser suite.**

  Run: `npm --prefix web run lint`

  Expected: PASS.

  Run: `npm --prefix web test`

  Expected: PASS; includes anonymous and admin authorized-card fixtures, zero-match recovery, keyboard operation, no horizontal overflow, and no filter-triggered catalogue fetch.

- [ ] **Step 3: Inspect scope and security invariants before closing the issue.**

  Run: `git diff HEAD~2 -- internal/http/templates.go web/src/pages/index.astro web/public/app.js web/src/styles/app.css tests/integration/rendered_ui_test.go web/tests/catalog.test.mjs`

  Confirm the diff adds no catalogue endpoint, `fetch`, session/cookie write, hidden metadata selector, or unauthorized-item copy; confirm it does not alter the server’s visibility decisions.

- [ ] **Step 4: Commit any verification-only test correction.**

  If Steps 1–3 required no corrections, skip this commit. Otherwise:

  ```bash
  git add tests/integration/rendered_ui_test.go web/tests/catalog.test.mjs
  git commit -m "test: cover empty filter recovery boundaries"
  ```

## Self-Review

- **Spec coverage:** Task 1 creates safe, non-disclosing recovery controls in the result region. Task 2 verifies both independent recovery paths, immediate result-count changes, accessible controls, focus behavior, and no network activity. Task 3 confirms BFF authorization and rendered HTML remain intact.
- **Scope:** The plan intentionally leaves the true server-side empty catalogue message, sticky behavior, responsive chip overflow, theme tokens, and profile controls unchanged; those are owned by other issues.
- **Type consistency:** All tasks use the same new data attributes: `data-empty-results`, `data-empty-clear-search`, and `data-empty-clear-category`.
- **Placeholder scan:** No deferred implementation or unspecified test behavior remains.
