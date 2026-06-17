import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  use: {
    baseURL: 'http://localhost:8083',
    launchOptions: {
      args: ['--use-angle=gl', '--enable-gpu'],
    },
  },
});
