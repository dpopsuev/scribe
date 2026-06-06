import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environmentMatchGlobs: [
      ['ui.test.js',   'jsdom'],
      ['perf.test.js', 'jsdom'],  // canvas cache tests need document
    ],
    environment: 'node',
    exclude: ['**/*.spec.ts', 'node_modules/**'],
  },
});
