import { defineConfig, devices } from '@playwright/test'

function stagingBaseURL(): string {
  const raw = process.env.HOUFENG_STAGING_BASE_URL?.trim()
  if (!raw) throw new Error('missing HOUFENG_STAGING_BASE_URL')
  const url = new URL(raw)
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new Error('HOUFENG_STAGING_BASE_URL must use http or https')
  }
  return url.toString().replace(/\/$/, '')
}

export default defineConfig({
  testDir: './e2e/staging',
  testMatch: 'staging-smoke.spec.ts',
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  workers: 1,
  timeout: 40 * 60 * 1000,
  reporter: [['list']],
  outputDir: 'test-results/staging-playwright-private',
  preserveOutput: 'never',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: stagingBaseURL(),
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
})
