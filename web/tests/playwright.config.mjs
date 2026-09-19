import { defineConfig } from '@playwright/test';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  testDir: '.',
  testMatch: ['catalog.test.mjs', 'theme-preferences.test.mjs'],
  outputDir: '../.astro/test-results',
  reporter: 'line',
  webServer: {
    command: 'go run ./tests/integration/browserfixture',
    cwd: fileURLToPath(new URL('../..', import.meta.url)),
    url: 'https://127.0.0.1:4173/',
    ignoreHTTPSErrors: true,
    reuseExistingServer: !process.env.CI,
  },
  use: {
    browserName: 'chromium',
    channel: 'chrome',
    headless: true,
    ignoreHTTPSErrors: true,
  },
});
