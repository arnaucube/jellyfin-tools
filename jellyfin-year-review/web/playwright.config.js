import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  use: { baseURL: 'http://127.0.0.1:18180', browserName: 'chromium', viewport: { width: 390, height: 844 }, reducedMotion: 'reduce' },
  webServer: { command: 'cd .. && go run ./scripts/e2e-server.go', url: 'http://127.0.0.1:18180', reuseExistingServer: false, timeout: 120000 }
});
