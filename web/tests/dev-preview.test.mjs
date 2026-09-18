import { expect, test } from '@playwright/test';

test('developer can preview and filter realistic catalogue states with live reload enabled', async ({ page }) => {
  const response = await page.goto('/');

  expect(await response.text()).toContain('/@vite/client');
  await expect(page.getByRole('heading', { name: 'Service catalogue' })).toBeVisible();
  await expect(page.getByText('Catalogue is stale.', { exact: false })).toBeVisible();
  await expect(page.locator('[data-catalog-item]')).toHaveCount(5);

  await page.getByRole('button', { name: 'Monitoring' }).click();

  await expect(page.locator('[data-catalog-status]')).toHaveText('2 catalog items');
  await expect(page.getByRole('link', { name: 'Open Grafana' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open Keycloak' })).toBeHidden();
});

test('developer can preview populated admin diagnostics', async ({ page }) => {
  await page.goto('/admin');

  await expect(page.getByRole('heading', { name: 'Admin diagnostics' })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.getByText('invalid access annotation')).toBeVisible();
});
