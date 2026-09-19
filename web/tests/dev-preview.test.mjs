import { expect, test } from '@playwright/test';

test('developer preview exposes a labelled decorative theme control', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto('/');

  const toggle = page.getByRole('button', { name: 'Switch to dark theme' });
  await expect(toggle).toBeVisible();
  await expect(toggle.locator('[data-theme-toggle-icon]')).toHaveAttribute('aria-hidden', 'true');
  await expect(toggle.locator('[data-theme-toggle-label]')).toHaveText('Switch to dark theme');
});

test('developer can preview and filter realistic catalogue states with live reload enabled', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  const response = await page.goto('/');

  expect(await response.text()).toContain('/@vite/client');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('script[src="/theme.js"]')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Switch to dark theme' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Service catalogue' })).toBeVisible();
  await expect(page.getByText('Catalogue is stale.', { exact: false })).toBeVisible();
  await expect(page.locator('[data-catalog-item]')).toHaveCount(5);

  const rail = page.locator('aside.catalog-information-rail');
  const search = page.getByRole('searchbox', { name: 'Search catalog' });
  const emptyResults = page.locator('[data-empty-results]');
  await expect(page.locator('header.site-header')).toContainText('Homelab Portal');
  await expect(rail).not.toContainText('Homelab Portal');
  await expect(rail.locator('[data-catalog-status]')).toHaveText('5 catalog items');
  await expect(page.locator('[data-catalog-status]')).toHaveCount(1);
  await expect(emptyResults).toBeHidden();
  expect(await rail.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-search]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  expect(await search.evaluate((element) => Boolean(element.compareDocumentPosition(document.querySelector('[data-catalog-grid]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);

  await page.getByRole('button', { name: 'Monitoring' }).click();

  await expect(page.locator('[data-catalog-status]')).toHaveText('2 catalog items');
  await expect(page.getByRole('link', { name: 'Open Grafana' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open Keycloak' })).toBeHidden();

  const clearSearch = page.getByRole('button', { name: 'Clear search' });
  await expect(clearSearch).toBeHidden();
  await search.fill('grafana');
  await expect(clearSearch).toBeVisible();
  await clearSearch.click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  await expect(page.locator('[data-catalog-status]')).toHaveText('2 catalog items');
  await expect(clearSearch).toBeHidden();

  await search.fill('no matching catalogue item');
  await expect(emptyResults).toBeVisible();
  await expect(page.getByRole('button', { name: 'Return to All' })).toBeVisible();
});

test('developer can preview populated admin diagnostics', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto('/admin');

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('script[src="/theme.js"]')).toHaveCount(1);
  await expect(page.getByRole('heading', { name: 'Admin diagnostics' })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.getByText('invalid access annotation')).toBeVisible();
});
