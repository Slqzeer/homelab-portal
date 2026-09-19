import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const baseURL = 'https://127.0.0.1:4173';

const newPage = async (browser, options = {}) => {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, ...options });
  const page = await context.newPage();
  return { context, page };
};

test('production-rendered stale catalogue exposes its freshness warning', async ({ page }) => {
  await page.goto(`${baseURL}/__test/stale`);

  await expect(page.locator('.stale-banner')).toContainText('Catalogue information is not current');
});

test('production-rendered portal resolves first visits from the operating-system theme', async ({ browser }) => {
  for (const colorScheme of ['light', 'dark']) {
    const { context, page } = await newPage(browser);
    await page.emulateMedia({ colorScheme });
    await page.goto(baseURL);
    await expect(page.locator('html')).toHaveAttribute('data-theme', colorScheme);
    await context.close();
  }
});

test('production-rendered portal retains an explicit theme across operating-system changes and reload', async ({ browser }) => {
  const { context, page } = await newPage(browser);
  await page.addInitScript(() => localStorage.setItem('portal.theme', 'light'));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(baseURL);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await context.close();
});

test('production-rendered portal falls back safely when stored theme is invalid or unreadable', async ({ browser }) => {
  const malformed = await newPage(browser);
  await malformed.page.addInitScript(() => localStorage.setItem('portal.theme', 'system'));
  await malformed.page.emulateMedia({ colorScheme: 'dark' });
  await malformed.page.goto(baseURL);
  await expect(malformed.page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await malformed.context.close();

  const unavailable = await newPage(browser);
  const pageErrors = [];
  unavailable.page.on('pageerror', (error) => pageErrors.push(error));
  await unavailable.page.addInitScript(() => {
    const getItem = Storage.prototype.getItem;
    Storage.prototype.getItem = function (key) {
      if (key === 'portal.theme') throw new DOMException('Blocked', 'SecurityError');
      return getItem.call(this, key);
    };
  });
  await unavailable.page.emulateMedia({ colorScheme: 'light' });
  await unavailable.page.goto(baseURL);
  await expect(unavailable.page.locator('html')).toHaveAttribute('data-theme', 'light');
  expect(pageErrors).toEqual([]);
  await unavailable.context.close();
});

test('production-rendered portal toggles locally without requests or cookie changes', async ({ browser }) => {
  const { context, page } = await newPage(browser);
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto(baseURL);
  const urlBefore = page.url();
  const cookiesBefore = await context.cookies();
  const requests = [];
  page.on('request', (request) => requests.push(request.url()));

  await page.getByRole('button', { name: 'Switch to dark theme' }).press('Enter');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => Object.keys(localStorage))).toEqual(['portal.theme']);
  expect(page.url()).toBe(urlBefore);
  expect(await context.cookies()).toEqual(cookiesBefore);
  expect(requests).toEqual([]);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await context.close();
});

test('production-rendered portal switches visually when theme storage cannot be written', async ({ browser }) => {
  const { context, page } = await newPage(browser);
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error));
  await page.addInitScript(() => {
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key === 'portal.theme') throw new DOMException('Blocked', 'SecurityError');
      return setItem.call(this, key, value);
    };
  });
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto(baseURL);
  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
  expect(pageErrors).toEqual([]);
  await context.close();
});

test('production-rendered portal has no active theme animation for reduced-motion users', async ({ browser }) => {
  const { context, page } = await newPage(browser);
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' });
  await page.goto(baseURL);
  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.evaluate(() => new Promise(requestAnimationFrame));
  expect(await page.evaluate(() => document.getAnimations({ subtree: true })
    .filter((animation) => ['pending', 'running'].includes(animation.playState)).length)).toBe(0);
  await context.close();
});

const states = [
  { name: 'catalogue', route: '/', empty: false, marker: '[data-catalog-grid]' },
  { name: 'stale catalogue', route: '/__test/stale', empty: false, marker: '.stale-banner' },
  { name: 'filtered empty catalogue', route: '/', empty: true, marker: '[data-empty-results]' },
  { name: 'authenticated account action', route: '/__test/admin', empty: false, marker: 'button:has-text("Sign out")' },
];

for (const colorScheme of ['light', 'dark']) {
  for (const state of states) {
    test(`${state.name} meets AA checks in ${colorScheme} theme`, async ({ browser }) => {
      const { context, page } = await newPage(browser);
      await page.emulateMedia({ colorScheme });
      await page.goto(`${baseURL}${state.route}`);
      if (state.empty) {
        await page.getByRole('searchbox', { name: 'Search catalog' }).fill('no matching catalogue item');
      }
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
