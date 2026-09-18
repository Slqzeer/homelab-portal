import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const baseURL = 'https://127.0.0.1:4173';
const longName = 'N'.repeat(80);
const longDescription = 'D'.repeat(240);
const longCategory = 'C'.repeat(40);

const visibleArticles = (page) => page.locator('article[data-catalog-item]:visible');

test('production-rendered anonymous cards filter locally and never request catalogue data', async ({ page }) => {
  const requests = [];
  page.on('request', (request) => requests.push(request.url()));

  await page.goto(baseURL);
  await expect(page.getByRole('heading', { level: 1, name: 'Homelab Portal' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Service catalogue' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Homelab Portal' })).toHaveCSS('margin-bottom', '0px');
  await expect(page.getByRole('heading', { level: 1, name: 'Homelab Portal' })).toHaveCSS('letter-spacing', '-0.4px');
  await expect(page.locator('article.catalog-card img.catalog-icon')).toHaveCount(4);
  await expect(visibleArticles(page)).toHaveCount(4);
  const rail = page.locator('aside.catalog-information-rail');
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  await expect(rail).toContainText('Tailnet catalogue');
  await expect(rail.locator('[data-catalog-status]')).toHaveText('4 catalog items');
  await expect(page.locator('[data-catalog-status]')).toHaveCount(1);
  expect(await rail.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await search.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-grid]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  const header = await page.locator('header.site-header').boundingBox();
  const actions = await page.locator('header.site-header .identity-actions').boundingBox();
  expect(actions.x + actions.width).toBeCloseTo(header.x + header.width, 1);

  const anonymousHTML = await page.content();
  expect(anonymousHTML).not.toContain('Secret Admin');
  expect(anonymousHTML).not.toContain('secret-admin.tail.example');

  for (const requestURL of requests) {
    const requested = new URL(requestURL);
    expect(requested.origin).toBe(baseURL);
    expect(requested.pathname).toMatch(/^\/$|^\/app\.js$|^\/_astro\/.+\.css$|^\/icons\/.+\.svg$/);
  }
  const initializationRequests = [...requests];

  const monitoring = page.getByRole('button', { name: 'Monitoring' });
  const all = page.getByRole('button', { name: 'All' });
  const clearSearch = page.getByRole('button', { name: 'Clear search' });

  await expect(clearSearch).toBeHidden();

  await search.fill('gRaF');
  await expect(visibleArticles(page)).toHaveCount(1);
  await expect(page.getByRole('article').filter({ hasText: 'Grafana' })).toBeVisible();

  await search.fill('iDENTity');
  await expect(visibleArticles(page)).toHaveCount(1);
  await expect(page.getByRole('article').filter({ hasText: 'Keycloak' })).toBeVisible();

  await search.fill('TIME-series');
  await monitoring.focus();
  await page.keyboard.press('Enter');
  await expect(monitoring).toBeFocused();
  await expect(monitoring).toHaveAttribute('aria-pressed', 'true');
  await expect(all).toHaveAttribute('aria-pressed', 'false');
  await expect(visibleArticles(page)).toHaveCount(1);
  await expect(page.getByRole('article').filter({ hasText: 'Prometheus' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('1 catalog item');
  await expect(clearSearch).toBeVisible();

  await clearSearch.click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  await expect(visibleArticles(page)).toHaveCount(2);
  await expect(page.getByRole('status')).toHaveText('2 catalog items');
  await expect(clearSearch).toBeHidden();

  await all.click();
  await expect(visibleArticles(page)).toHaveCount(4);
  await expect(page.getByRole('status')).toHaveText('4 catalog items');
  expect(await page.content()).not.toContain('Secret Admin');
  expect(requests).toEqual(initializationRequests);
});

test('production-rendered admin identity receives its authorized card set', async ({ page }) => {
  await page.goto(`${baseURL}/__test/admin`);

  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin diagnostics' })).toBeVisible();
  await expect(visibleArticles(page)).toHaveCount(5);
  await expect(page.getByRole('article').filter({ hasText: 'Secret Admin' })).toBeVisible();
  await expect(page.locator('a[href="https://secret-admin.tail.example"]')).toHaveCount(1);
  await expect(page.locator('img[src="/icons/generic.svg"]')).toHaveCount(2);
  await expect(page.locator('aside.catalog-information-rail a[href^="https://"]')).toHaveCount(0);
});

test('compiled production layout wraps maximum valid catalogue text at a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 740 });
  await page.goto(baseURL);

  const accessibility = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  expect(accessibility.violations).toEqual([]);

  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#main-content')).toBeFocused();
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

  await expect(page.getByRole('link', { name: `Open ${longName}` })).toBeVisible();
  await expect(page.getByText(longDescription, { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: longCategory })).toBeVisible();
  await expect(page.locator('[data-catalog-grid]')).toHaveCSS('display', 'grid');

  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(dimensions).toEqual({ clientWidth: 360, scrollWidth: 360 });
});
