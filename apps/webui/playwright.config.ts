import { defineConfig, devices } from '@playwright/test';
const baseURL = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? '5193'}`;
export default defineConfig({
  testDir: './tests', timeout: 45_000, expect: { timeout: 10_000 }, fullyParallel: false, workers: 1,
  use: { baseURL, locale: 'en-US', timezoneId: 'Asia/Shanghai', screenshot: 'only-on-failure', trace: 'retain-on-failure', launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined } },
  projects: [{ name: 'desktop', use: { viewport: { width: 1440, height: 1000 } } }, { name: 'mobile', use: { ...devices['Pixel 7'], defaultBrowserType: 'chromium', colorScheme: 'dark' } }],
  webServer: { command: 'node tests/server.mjs', gracefulShutdown: { signal: 'SIGTERM', timeout: 10000 }, url: baseURL, timeout: 120_000, reuseExistingServer: false },
});
