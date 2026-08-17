import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e/component-tests',
  fullyParallel: true,
  retries: 0,
  reporter: [['list']],
  outputDir: 'test-results/playwright-components',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: 'http://127.0.0.1:4176',
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    viewport: { width: 1440, height: 1000 },
    hasTouch: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: 'vite --config vite.component-harness.config.ts --host 127.0.0.1 --port 4176 --strictPort',
    url: 'http://127.0.0.1:4176/',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
