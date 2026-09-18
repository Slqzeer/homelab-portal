import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const baseURL = 'https://127.0.0.1:4173';
const longName = 'N'.repeat(80);
const longDescription = 'D'.repeat(240);
const longCategory = 'C'.repeat(40);

const visibleArticles = (page) => page.locator('article[data-catalog-item]:visible');

test('production-rendered portal declares the dark theme contract on the document root', async ({ page }) => {
  await page.goto(baseURL);

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('production-rendered portal resolves a first visit from the operating-system theme', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto(baseURL);

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
});

test('production-rendered portal switches and persists an explicit theme choice', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto(baseURL);

  const toggle = page.getByRole('button', { name: 'Switch to dark theme' });
  await toggle.click();

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(toggle).toHaveAccessibleName('Switch to light theme');
  await expect.poll(() => page.evaluate(() => window.localStorage.getItem('portal.theme'))).toBe('dark');
});

test('production-rendered portal applies the light palette when the document theme is light', async ({ page }) => {
  await page.goto(baseURL);
  await page.locator('html').evaluate((element) => element.setAttribute('data-theme', 'light'));

  await expect(page.locator('html')).toHaveCSS('color-scheme', 'light');
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(248, 250, 252)');
});

test('production-rendered portal applies light theme surfaces to catalogue controls and feedback', async ({ page }) => {
  await page.goto(baseURL);
  await page.locator('html').evaluate((element) => element.setAttribute('data-theme', 'light'));

  await expect(page.locator('.catalog-card').first()).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(page.getByRole('searchbox', { name: 'Search catalog' })).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(page.getByRole('button', { name: 'All' })).toHaveCSS('background-color', 'rgb(14, 116, 144)');
  await page.getByRole('searchbox', { name: 'Search catalog' }).fill('no matching catalogue item');
  await expect(page.locator('[data-empty-results]')).toHaveCSS('color', 'rgb(51, 65, 85)');
});

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
  await expect(page.locator('header.site-header')).toContainText('Tailnet catalogue');
  await expect(rail).not.toContainText('Tailnet catalogue');
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
  const clearSearch = page.getByRole('button', { name: 'Clear search', exact: true }).first();
  const emptyResults = page.locator('[data-empty-results]');
  const recoveryClearSearch = page.getByRole('button', { name: 'Clear search', exact: true }).last();
  const returnToAll = page.getByRole('button', { name: 'Return to All' });

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
  await expect(page).not.toHaveURL(/\?.+/);
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

  await monitoring.click();
  await search.fill('no matching catalogue item');
  await expect(visibleArticles(page)).toHaveCount(0);
  await expect(page.getByRole('status')).toHaveText('0 catalog items');
  await expect(emptyResults).toBeVisible();
  for (const control of [recoveryClearSearch, returnToAll]) {
    const box = await control.boundingBox();
    expect(box.width).toBeGreaterThanOrEqual(44);
    expect(box.height).toBeGreaterThanOrEqual(44);
  }

  await recoveryClearSearch.click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  await expect(monitoring).toHaveAttribute('aria-pressed', 'true');
  await expect(visibleArticles(page)).toHaveCount(2);

  await search.fill('keycloak');
  await expect(visibleArticles(page)).toHaveCount(0);
  await returnToAll.click();
  await expect(all).toHaveAttribute('aria-pressed', 'true');
  await expect(search).toHaveValue('keycloak');
  await expect(visibleArticles(page)).toHaveCount(1);
  await expect(emptyResults).toBeHidden();
  expect(await page.content()).not.toContain('Secret Admin');
  expect(requests).toEqual(initializationRequests);
});

test('production-rendered catalogue uses a compact desktop rail and flexible multi-column results', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(baseURL);

  const header = page.locator('header.site-header');
  const catalogueLabel = header.getByText('Tailnet catalogue', { exact: true });
  const actions = header.locator('.identity-actions');
  const rail = page.locator('aside.catalog-information-rail');
  const main = page.locator('.catalog-main');
  const grid = page.locator('[data-catalog-grid]');
  const [headerBox, labelBox, actionsBox, railBox, mainBox, gridBox, firstCard, secondCard] = await Promise.all([
    header.boundingBox(), catalogueLabel.boundingBox(), actions.boundingBox(),
    rail.boundingBox(), main.boundingBox(), grid.boundingBox(),
    page.locator('article[data-catalog-item]').nth(0).boundingBox(),
    page.locator('article[data-catalog-item]').nth(1).boundingBox(),
  ]);

  expect(labelBox.x).toBeCloseTo(headerBox.x, 1);
  expect(Math.abs((labelBox.y + (labelBox.height / 2)) - (actionsBox.y + (actionsBox.height / 2)))).toBeLessThanOrEqual(2);
  expect(actionsBox.x + actionsBox.width).toBeCloseTo(headerBox.x + headerBox.width, 1);
  await expect(rail).not.toContainText('Tailnet catalogue');
  expect(railBox.x).toBeLessThan(mainBox.x);
  expect(railBox.x).toBeCloseTo(headerBox.x, 1);
  expect(railBox.y).toBeCloseTo(mainBox.y, 1);
  expect(mainBox.width).toBeGreaterThan(railBox.width * 1.5);
  expect(gridBox.width).toBeCloseTo(mainBox.width, 1);
  expect(secondCard.y).toBeCloseTo(firstCard.y, 1);
  expect(secondCard.x).toBeGreaterThan(firstCard.x);
});

test('production-rendered catalogue keeps readable multi-column cards at tablet width', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto(baseURL);

  const grid = page.locator('[data-catalog-grid]');
  const firstCard = page.locator('article[data-catalog-item]').nth(0);
  const secondCard = page.locator('article[data-catalog-item]').nth(1);
  const [gridBox, firstBox, secondBox] = await Promise.all([
    grid.boundingBox(), firstCard.boundingBox(), secondCard.boundingBox(),
  ]);

  expect(gridBox.width).toBeGreaterThanOrEqual(560);
  expect(firstBox.width).toBeGreaterThanOrEqual(272);
  expect(secondBox.width).toBeGreaterThanOrEqual(272);
  expect(secondBox.y).toBeCloseTo(firstBox.y, 1);
  expect(secondBox.x).toBeGreaterThan(firstBox.x);
});

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

test('production-rendered catalogue stacks the information rail before one-column controls and cards at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto(baseURL);

  const rail = page.locator('aside.catalog-information-rail');
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  const firstCard = page.locator('article[data-catalog-item]').first();
  const secondCard = page.locator('article[data-catalog-item]').nth(1);
  const status = rail.locator('[data-catalog-status]');
  const categoryGroup = page.getByRole('group', { name: 'Filter by category' });
  const categoryButtons = page.locator('button[data-category-filter]');
  const [railBox, searchBox, firstCardBox, secondCardBox] = await Promise.all([
    rail.boundingBox(), search.boundingBox(), firstCard.boundingBox(), secondCard.boundingBox(),
  ]);

  expect(railBox.y).toBeLessThan(searchBox.y);
  expect(await rail.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await page.getByRole('link', { name: 'Skip to content' }).evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-status]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await status.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await search.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')?.closest('.catalog-controls')?.querySelector('.category-filters')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await search.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-grid]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await categoryGroup.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-grid]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(searchBox.y).toBeLessThan(firstCardBox.y);
  expect(secondCardBox.y).toBeGreaterThan(firstCardBox.y);
  expect(await search.evaluate((element) => element.getBoundingClientRect().width)).toBeGreaterThan(0);
  await search.focus();
  await page.keyboard.press('Tab');
  await expect(categoryButtons.first()).toBeFocused();
  const categoryCount = await categoryButtons.count();
  for (let index = 1; index < categoryCount; index += 1) {
    await page.keyboard.press('Tab');
    await expect(categoryButtons.nth(index)).toBeFocused();
  }
  await page.keyboard.press('Tab');
  await expect(firstCard.getByRole('link')).toBeFocused();

  for (const control of [search, ...await categoryButtons.all()]) {
    const box = await control.boundingBox();
    expect(box.width).toBeGreaterThanOrEqual(44);
    expect(box.height).toBeGreaterThanOrEqual(44);
  }
  await search.fill('no matching catalogue item');
  await expect(page.getByRole('status')).toHaveText('0 catalog items');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
});

test('production-rendered admin identity receives its authorized card set', async ({ page }) => {
  await page.goto(`${baseURL}/__test/admin`);

  await expect(page.locator('aside.catalog-information-rail')).toBeVisible();
  await expect(page.locator('.catalog-main')).toBeVisible();
  await expect(page.locator('aside.catalog-information-rail').getByRole('link')).toHaveCount(0);
  await expect(page.locator('header.site-header').getByRole('link', { name: 'Admin diagnostics' })).toBeVisible();
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
