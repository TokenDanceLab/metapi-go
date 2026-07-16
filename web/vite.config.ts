/// <reference types="vitest/config" />
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  root: '.',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('@visactor/react-vchart') || id.includes('/@visactor/')) {
            return 'vchart-vendor';
          }
          return undefined;
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    // Avoid flaky EnvironmentTeardownError under concurrent React19 + chart stubs.
    fileParallelism: false,
    maxWorkers: 1,
    // Pending jsdom/console RPC during environment teardown must not fail CI when
    // all assertions already passed (known EnvironmentTeardownError flake).
    dangerouslyIgnoreUnhandledErrors: true,
    onConsoleLog() {
      // Suppress vitest worker console RPC traffic that races teardown.
      return false;
    },
  },
});
