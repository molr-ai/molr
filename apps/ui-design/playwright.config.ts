import { defineConfig, devices } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? process.env.MOLR_UI_DESIGN_TEST_PORT ?? '5188';
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? `http://127.0.0.1:${port}`;
export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  workers: process.env.CI ? 2 : 3,
  reporter: 'list',
  timeout: 30_000,
  use: {
    baseURL,
    locale: 'en-US',
    timezoneId: 'Asia/Shanghai',
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1360, height: 1000 }, colorScheme: 'light' } },
    { name: 'mobile', use: { ...devices['Pixel 7'], defaultBrowserType: 'chromium', colorScheme: 'dark' } },
  ],
  webServer: {
    command: `npm run dev -- --port ${port}`,
    url: baseURL,
    reuseExistingServer: false,
  },
});
