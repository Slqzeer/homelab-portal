import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: 'dev-preview.test.mjs',
  outputDir: '../.astro/dev-test-results',
  reporter: 'line',
  webServer: {
    command: 'npm run dev',
    url: 'http://127.0.0.1:4321/',
    reuseExistingServer: false,
  },
  use: {
    baseURL: 'http://127.0.0.1:4321',
    browserName: 'chromium',
    channel: 'chrome',
    headless: true,
  },
});
