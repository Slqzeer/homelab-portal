import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: 'catalog.test.mjs',
  outputDir: '../.astro/test-results',
  reporter: 'line',
  use: {
    browserName: 'chromium',
    channel: 'chrome',
    headless: true,
  },
});
