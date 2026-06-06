import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // palette, physics, glow, api: no DOM needed
    // ui: needs DOM (document.createElement)
    environmentMatchGlobs: [
      ['ui.test.js', 'jsdom'],
    ],
    environment: 'node',
    // Playwright spec files are not Vitest tests
    exclude: ['**/*.spec.ts', 'node_modules/**'],
  },
});
