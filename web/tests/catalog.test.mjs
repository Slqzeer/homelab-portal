import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const catalogScript = fileURLToPath(new URL('../src/scripts/catalog.js', import.meta.url));
const catalogStyles = fileURLToPath(new URL('../src/styles/app.css', import.meta.url));

const renderedCatalog = `<!doctype html>
<html lang="en">
  <head><meta charset="utf-8"><title>Catalog fixture</title></head>
  <body>
    <main>
      <h1>Service catalogue</h1>
      <section class="catalog-controls" aria-labelledby="catalog-filters-heading">
        <h2 id="catalog-filters-heading">Find a catalog item</h2>
        <label for="catalog-search">Search catalog</label>
        <input id="catalog-search" type="search" data-catalog-search>
        <div role="group" aria-label="Filter by category">
          <button type="button" data-category-filter="" aria-pressed="true">All</button>
          <button type="button" data-category-filter="Monitoring" aria-pressed="false">Monitoring</button>
          <button type="button" data-category-filter="Identity" aria-pressed="false">Identity</button>
        </div>
        <p data-catalog-status role="status" aria-live="polite">3 catalog items</p>
      </section>
      <section aria-labelledby="catalog-results-heading">
        <h2 id="catalog-results-heading">Catalog items</h2>
        <div class="catalog-grid" data-catalog-grid>
          <article data-catalog-item>
            <h3 data-catalog-name><a href="https://grafana.tail.example">Grafana</a></h3>
            <p data-catalog-description>Metrics and dashboards</p>
            <p data-catalog-category>Monitoring</p>
          </article>
          <article data-catalog-item>
            <h3 data-catalog-name><a href="https://prometheus.tail.example">Prometheus</a></h3>
            <p data-catalog-description>Time-series metrics</p>
            <p data-catalog-category>Monitoring</p>
          </article>
          <article data-catalog-item>
            <h3 data-catalog-name><a href="https://keycloak.tail.example">Keycloak</a></h3>
            <p data-catalog-description>Single sign-on</p>
            <p data-catalog-category>Identity</p>
          </article>
        </div>
      </section>
    </main>
  </body>
</html>`;

async function loadCatalog(page) {
  await page.setContent(renderedCatalog);
  await page.addScriptTag({ path: catalogScript });
}

async function loadCatalogWithStyles(page) {
  await loadCatalog(page);
  const css = (await readFile(catalogStyles, 'utf8'))
    .replace(/^@config.*$/gm, '')
    .replace(/^@import.*$/gm, '');
  await page.addStyleTag({ content: css });
}

test('search filters the delivered cards by visible name without requesting data', async ({ page }) => {
  await loadCatalog(page);
  const requests = [];
  page.on('request', (request) => requests.push(request.url()));

  await page.getByRole('searchbox', { name: 'Search catalog' }).fill('gRaF');

  await expect(page.getByRole('article').filter({ hasText: 'Grafana' })).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'Prometheus' })).toBeHidden();
  await expect(page.getByRole('article').filter({ hasText: 'Keycloak' })).toBeHidden();
  await expect(page.getByRole('status')).toHaveText('1 catalog item');
  expect(requests).toEqual([]);
});

test('description and category search combine with keyboard-operable category buttons', async ({ page }) => {
  await loadCatalog(page);
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  const monitoring = page.getByRole('button', { name: 'Monitoring' });
  const all = page.getByRole('button', { name: 'All' });

  await page.keyboard.press('Tab');
  await expect(search).toBeFocused();
  await search.fill('TIME-series');
  await expect(page.getByRole('article').filter({ hasText: 'Prometheus' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('1 catalog item');

  await search.fill('iDENTity');
  await expect(page.getByRole('article').filter({ hasText: 'Keycloak' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('1 catalog item');

  await search.fill('');
  await monitoring.focus();
  await page.keyboard.press('Enter');
  await expect(monitoring).toBeFocused();
  await expect(monitoring).toHaveAttribute('aria-pressed', 'true');
  await expect(all).toHaveAttribute('aria-pressed', 'false');
  await expect(page.getByRole('article').filter({ hasText: 'Grafana' })).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'Prometheus' })).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'Keycloak' })).toBeHidden();

  await all.click();
  await expect(page.getByRole('article')).toHaveCount(3);
  for (const article of await page.getByRole('article').all()) {
    await expect(article).toBeVisible();
  }
  await expect(page.getByRole('status')).toHaveText('3 catalog items');
});

test('the narrow catalogue is accessible and exposes visible keyboard focus', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 740 });
  await loadCatalogWithStyles(page);

  const accessibility = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  expect(accessibility.violations).toEqual([]);

  await page.keyboard.press('Tab');
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  await expect(search).toBeFocused();
  const focusStyle = await search.evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      outlineColor: style.outlineColor,
      outlineStyle: style.outlineStyle,
      outlineWidth: style.outlineWidth,
    };
  });
  expect(focusStyle).toEqual({
    outlineColor: 'rgb(56, 189, 248)',
    outlineStyle: 'solid',
    outlineWidth: '3px',
  });

  const layout = await page.locator('[data-catalog-grid]').evaluate((element) => {
    const style = getComputedStyle(element);
    return { columns: style.gridTemplateColumns.split(' ').length, display: style.display };
  });
  expect(layout).toEqual({ columns: 1, display: 'grid' });
  const horizontallyScrollable = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(horizontallyScrollable).toBe(false);
});
