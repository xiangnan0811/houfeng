import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testIgnore: ['staging/**'],
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  ...(process.env.CI ? { workers: 2 } : {}),
  reporter: process.env.CI
    ? [['html', { open: 'never' }], ['list']]
    : [['list']],
  outputDir: 'test-results/playwright',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: 'http://127.0.0.1:4175',
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    viewport: { width: 1440, height: 1000 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: 'npm run preview -- --host 127.0.0.1 --port 4175 --strictPort',
    url: 'http://127.0.0.1:4175/login',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
