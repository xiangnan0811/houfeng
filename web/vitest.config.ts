import { readFileSync } from 'node:fs'
import { configDefaults, defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

type CoverageMetrics = {
  statements: number
  branches: number
  functions: number
  lines: number
}

type CoverageBudget = {
  global: CoverageMetrics
  critical: Record<string, Partial<CoverageMetrics>>
}

const coverageBudget = JSON.parse(
  readFileSync(new URL('./coverage-budget.json', import.meta.url), 'utf8'),
) as CoverageBudget

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    unstubGlobals: true,
    setupFiles: './src/test/setup.ts',
    exclude: [...configDefaults.exclude, 'e2e/**'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/**/*.d.ts',
        'src/**/*TestFixtures.{ts,tsx}',
        'src/**/*testFixtures.{ts,tsx}',
      ],
      reporter: ['text', 'json-summary', 'lcov'],
      reportsDirectory: 'coverage',
      thresholds: {
        ...coverageBudget.global,
        ...coverageBudget.critical,
        autoUpdate: false,
      },
    },
  },
})
