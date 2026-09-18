# First-Class Light and Dark Portal Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver GitHub issue #12 by making the existing portal shell, catalogue states, and diagnostics page use accessible semantic light and dark theme tokens.

**Architecture:** Define the complete semantic token contract on `:root[data-theme]` in the shared stylesheet, with a dark default and an intentional light override. Set `data-theme="dark"` in the server-rendered and Astro document roots now; issue #13 will replace that static default before first paint with the persisted or operating-system-resolved value. Convert every portal UI colour and shadow in `app.css` to the token contract, without touching catalogue, authentication, storage, or client behavior.

**Tech Stack:** Astro 7, Tailwind CSS 4 source stylesheet, Go `html/template` BFF, Playwright 1.61 with axe-core.

**Spec:** `docs/superpowers/specs/2026-09-18-portal-theme-preferences-design.md`

## Global Constraints

- Deliver only issue #12: do not read or write local storage, listen to `prefers-color-scheme`, add a theme button, or add a theme initializer; those are #13 and #14.
- The document root must expose `data-theme="dark"` on all currently rendered portal pages as the stable initial contract for #13.
- Use semantic custom properties for page/surface, elevated surface, primary/muted text, borders, accent, focus ring, warnings, inputs, active controls, and shadows; component rules must not retain literal palette values.
- Preserve current DOM order, catalogue filtering, target links, authorization boundary, CSP, server routes, and network behavior.
- Keep a clearly visible focus indicator and meet WCAG 2.2 AA contrast for normal text and controls in the catalogue, stale, empty-filter, and diagnostics states in both themes.
- Do not add dependencies, remote resources, cookies, sessions, server APIs, or client requests.

---

## File Structure

- `web/src/styles/app.css` — declares the two semantic token palettes and makes every shared component consume them.
- `internal/http/templates.go` — gives the BFF-rendered home and admin documents the initial dark theme attribute.
- `web/src/pages/index.astro` — gives the Astro development preview the same document-root contract.
- `web/src/pages/admin.astro` — gives the Astro diagnostics preview the same document-root contract.
- `web/tests/catalog.test.mjs` — verifies the production-rendered theme contract, token-driven contrast, focus visibility, and representative states.
- `web/tests/dev-preview.test.mjs` — verifies that the development preview carries the same root attribute.
- `tests/integration/rendered_ui_test.go` — verifies that the BFF emits the root theme contract without inline theme code.

### Task 1: Specify the document-root and two-palette contract with failing tests

**Files:**
- Modify: `web/tests/catalog.test.mjs`
- Modify: `web/tests/dev-preview.test.mjs`
- Modify: `tests/integration/rendered_ui_test.go`
- Review: `internal/http/templates.go`, `web/src/pages/index.astro`, `web/src/pages/admin.astro`, `web/src/styles/app.css`

**Interfaces:**
- Consumes: the production routes `/` and `/__test/admin`, the Astro preview routes `/` and `/admin`, and the root `<html>` element.
- Produces: behavior-focused theme assertions that Task 2 satisfies without creating the resolver or switcher owned by later issues.

- [ ] **Step 1: Add a failing production browser test for the root theme attribute and both rendered palettes**

  Add the following helpers near `visibleArticles` in `web/tests/catalog.test.mjs`. They check rendered foreground/background contrast rather than a particular CSS-variable spelling.

  ```js
  const luminance = ([red, green, blue]) => {
    const channel = (value) => {
      const normalized = value / 255;
      return normalized <= 0.03928 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
    };
    return 0.2126 * channel(red) + 0.7152 * channel(green) + 0.0722 * channel(blue);
  };

  const contrastRatio = (foreground, background) => {
    const parse = (value) => value.match(/\d+(?:\.\d+)?/g).slice(0, 3).map(Number);
    const [light, dark] = [luminance(parse(foreground)), luminance(parse(background))].sort((a, b) => b - a);
    return (light + 0.05) / (dark + 0.05);
  };
  ```

  Add a test that loads `/`, expects `html` to have `data-theme="dark"`, then sets `data-theme` to `dark` and `light` in turn. For each value, assert `getComputedStyle(document.documentElement).colorScheme` equals that value and assert contrast is at least `4.5` for the body, a `.catalog-card`, `.stale-banner`, `.empty-filter-results`, a `.button`, and focused search input. Restore `dark` before the test exits. Also assert that the admin fixture respects both values for the `.watcher-status` and table text.

- [ ] **Step 2: Add the focused test for focus visibility in both palettes**

  In the same test, focus the search input after each root attribute change and assert a nontransparent outline with a width of at least `3px`. Preserve the existing keyboard/focus-order test; this new assertion covers the palette contract instead of replacing it.

- [ ] **Step 3: Add BFF and Astro root-attribute assertions**

  In `TestRenderedCatalogKeepsIdentityAndFilteringAtTheBFFBoundary`, include `<html lang="en" data-theme="dark">` in the anonymous HTML expectations. Add a focused table-driven test that fetches `/` and `/admin` from the existing handler and checks both documents contain that exact opening tag and do not contain `localStorage`, `matchMedia`, or inline `<script>` content.

  In each test in `web/tests/dev-preview.test.mjs`, assert `page.locator('html')` has `data-theme` equal to `dark` after navigation.

- [ ] **Step 4: Run the new tests and confirm the red baseline**

  Run: `npm --prefix web test -- --grep "theme contract"`

  Expected: FAIL because the documents do not declare `data-theme` and `app.css` has only the fixed dark palette.

  Run: `go test ./tests/integration -run RenderedCatalog.*Theme -count=1`

  Expected: FAIL because BFF templates lack the root attribute.

- [ ] **Step 5: Commit the red tests**

  ```bash
  git add web/tests/catalog.test.mjs web/tests/dev-preview.test.mjs tests/integration/rendered_ui_test.go
  git commit -m "test: specify portal light and dark themes"
  ```

### Task 2: Add the semantic token palettes and migrate all shared UI styles

**Files:**
- Modify: `web/src/styles/app.css`
- Test: `web/tests/catalog.test.mjs`

**Interfaces:**
- Consumes: `html[data-theme="dark"]` and `html[data-theme="light"]` as the theme-selection seam; all current classes in `app.css`.
- Produces: `--color-page`, `--color-surface`, `--color-surface-elevated`, `--color-text`, `--color-text-muted`, `--color-border`, `--color-accent`, `--color-focus`, `--color-warning-border`, `--color-warning-surface`, `--color-warning-text`, `--color-input`, `--color-active-surface`, `--color-active-text`, and `--shadow-card` for shared styles to consume.

- [ ] **Step 1: Replace the fixed `:root` palette with semantic dark and light tokens**

  In `@layer base`, make `:root` retain only shared typography and declare the dark palette on `:root[data-theme="dark"]`; declare the light palette on `:root[data-theme="light"]`. Use these values, which preserve AA contrast for the intended surfaces:

  ```css
  :root[data-theme='dark'] {
    color-scheme: dark;
    --color-page: #0f172a;
    --color-surface: #1e293b;
    --color-surface-elevated: #111827;
    --color-text: #e2e8f0;
    --color-text-muted: #cbd5e1;
    --color-border: #475569;
    --color-accent: #7dd3fc;
    --color-focus: #38bdf8;
    --color-warning-border: #fbbf24;
    --color-warning-surface: #422006;
    --color-warning-text: #fef3c7;
    --color-input: #111827;
    --color-active-surface: #155e75;
    --color-active-text: #ecfeff;
    --shadow-card: 0 0.75rem 2rem rgb(2 6 23 / 20%);
  }

  :root[data-theme='light'] {
    color-scheme: light;
    --color-page: #f8fafc;
    --color-surface: #ffffff;
    --color-surface-elevated: #e2e8f0;
    --color-text: #0f172a;
    --color-text-muted: #334155;
    --color-border: #64748b;
    --color-accent: #0369a1;
    --color-focus: #0369a1;
    --color-warning-border: #b45309;
    --color-warning-surface: #fffbeb;
    --color-warning-text: #78350f;
    --color-input: #ffffff;
    --color-active-surface: #0e7490;
    --color-active-text: #ecfeff;
    --shadow-card: 0 0.75rem 2rem rgb(15 23 42 / 12%);
  }
  ```

- [ ] **Step 2: Convert every shared component colour to the semantic contract**

  Replace every literal colour and `rgb(...)` palette value in `web/src/styles/app.css` with the appropriate token. In particular, update the body background and radial tint, links, focus outline, skip link, eyebrow/category labels, buttons, selected filters, stale/watcher status, controls, inputs, placeholders, card/empty states, tables, and the sticky controls background. Add `--color-page-tint` to each palette for the radial gradient, and use it only in the body background.

  Preserve structural values such as borders, spacing, dimensions, breakpoints, and opacity behavior. Do not make colour the only selected-filter indicator: retain `aria-pressed`, borders, and text-label behavior.

- [ ] **Step 3: Keep icon backgrounds within the two-palette visual system**

  Add CSS for `.catalog-icon` that gives the existing embedded SVG a token-backed surface treatment (for example, `background: var(--color-surface-elevated)`). Do not edit the individual icon SVG files in this issue: their supplied glyph colours remain content, while the shared card and icon surround now follows the selected palette.

- [ ] **Step 4: Run focused browser tests and adjust only token values if a contrast assertion fails**

  Run: `npm --prefix web test -- --grep "theme contract|wide catalogue keeps opaque controls|wraps maximum valid catalogue text"`

  Expected: PASS in both `data-theme` values, with the sticky controls opaque and focus visible.

  If any ratio is below `4.5`, change the offending palette token rather than adding a component-specific literal override.

- [ ] **Step 5: Commit the palette migration**

  ```bash
  git add web/src/styles/app.css web/tests/catalog.test.mjs
  git commit -m "feat: add semantic light and dark theme tokens"
  ```

### Task 3: Establish the rendered dark-default seam and verify regression coverage

**Files:**
- Modify: `internal/http/templates.go`
- Modify: `web/src/pages/index.astro`
- Modify: `web/src/pages/admin.astro`
- Test: `tests/integration/rendered_ui_test.go`, `web/tests/catalog.test.mjs`, `web/tests/dev-preview.test.mjs`

**Interfaces:**
- Consumes: the root HTML opening tags for BFF and Astro pages and the two CSS palette selectors from Task 2.
- Produces: consistent server/dev HTML that issue #13 can mutate before paint through its same-origin external initializer.

- [ ] **Step 1: Add the dark default root attribute to every current document template**

  Change both literal opening tags in `internal/http/templates.go` and the opening `<html>` tags in `web/src/pages/index.astro` and `web/src/pages/admin.astro` to:

  ```html
  <html lang="en" data-theme="dark">
  ```

  Do not add a script tag or inline JavaScript. This preserves the current dark rendering while providing #13's stable selector seam.

- [ ] **Step 2: Run HTML, browser, and development-preview checks**

  Run: `go test ./tests/integration -run RenderedCatalog -count=1`

  Expected: PASS, including the root-attribute/no-inline-theme-code assertion.

  Run: `npm --prefix web run lint`

  Expected: PASS with no Astro or TypeScript diagnostics.

  Run: `npm --prefix web test`

  Expected: PASS, including current anonymous/admin, stale, empty-result, responsive, focus-order, accessibility, and theme-contract tests.

  Run: `npm --prefix web run test:dev`

  Expected: PASS, proving both Astro preview pages use the same root contract.

- [ ] **Step 3: Run repository verification before handoff**

  Run: `go test ./...`

  Expected: PASS; no BFF, auth, catalogue, manifest, or end-to-end contract regresses.

  Run: `make lint`

  Expected: PASS, including a locked UI build, Astro checks, and Go static analysis.

  Run: `make test`

  Expected: PASS, including a fresh locked UI build and the repository's Go behavior tests.

- [ ] **Step 4: Commit the rendered default and verification coverage**

  ```bash
  git add internal/http/templates.go web/src/pages/index.astro web/src/pages/admin.astro tests/integration/rendered_ui_test.go web/tests/catalog.test.mjs web/tests/dev-preview.test.mjs
  git commit -m "feat: expose portal theme on document roots"
  ```

## Self-Review

- **Spec coverage:** Task 1 establishes browser and rendered-HTML tests for both visual palettes. Task 2 defines the required semantic token categories, covers all current page/card/control/warning/empty/diagnostic surfaces, declares matching `color-scheme`, retains focus treatment, and checks AA contrast. Task 3 supplies the root attribute on every current rendered page and runs all regression suites.
- **Issue boundaries:** The plan deliberately has no storage, media-query resolution, pre-paint asset, transition, or control. #13 owns resolution; #14 owns switching; #15 owns the full end-to-end theme behavior verification.
- **No placeholders:** Exact files, tokens, test seams, commands, expected outcomes, and commit boundaries are specified. No authorization, privacy, or network behavior changes are implied.
