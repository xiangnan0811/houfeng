import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required for local-live acceptance`)
  return value
}
const loopbackHosts: Record<string, true> = { '127.0.0.1': true, localhost: true, '::1': true, '[::1]': true }


function centerBaseURL(): string {
  const configured = requiredEnv('LOCAL_LIVE_CENTER_URL')
  let url: URL
  try {
    url = new URL(configured)
  } catch {
    throw new Error('LOCAL_LIVE_CENTER_URL must be a loopback HTTP origin')
  }

  if (
    url.protocol !== 'http:' ||
    loopbackHosts[url.hostname] !== true ||
    url.username !== '' ||
    url.password !== '' ||
    (url.pathname !== '' && url.pathname !== '/') ||
    url.search !== '' ||
    url.hash !== ''
  ) {
    throw new Error('LOCAL_LIVE_CENTER_URL must be a loopback HTTP origin without credentials or URL suffixes')
  }
  return url.origin
}

const webDir = dirname(fileURLToPath(import.meta.url))
const outputDir = resolve(webDir, '../tmp/vps-state-local-live/playwright-results')

requiredEnv('LOCAL_LIVE_USERNAME')
requiredEnv('LOCAL_LIVE_PASSWORD')

export default defineConfig({
  testDir: './e2e/local-live',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  timeout: 240_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['json', { outputFile: resolve(outputDir, 'report.json') }]],
  outputDir,
  use: {
    ...devices['Desktop Chrome'],
    baseURL: centerBaseURL(),
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    viewport: { width: 1440, height: 1000 },
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
  },
  projects: [{ name: 'local-live-chromium', use: { browserName: 'chromium' } }],
})
