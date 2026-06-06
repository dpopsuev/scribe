import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  timeout: 40_000,
  retries: 0,
  workers: 1,
  reporter: 'list',

  // Serve static files from this directory — no Go server needed.
  webServer: {
    command: 'python3 -m http.server 4321',
    url: 'http://localhost:4321',
    reuseExistingServer: true,
    timeout: 10_000,
  },

  use: {
    baseURL: 'http://localhost:4321',
    headless: true,
  },

  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox',  use: { ...devices['Desktop Firefox'] } },
  ],
});
