import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const WEB_ROOT = process.cwd()
const BUDGET_PATH = resolve(WEB_ROOT, 'coverage-budget.json')
const REQUIRED_GLOBAL_METRICS = ['statements', 'branches', 'functions', 'lines'] as const
const REQUIRED_CRITICAL_PATHS = [
  'src/lib/modalStack.ts',
  'src/lib/useModalFocus.ts',
  'src/components/atoms/Modal.tsx',
  'src/pages/dashboard/dashboardRemoteState.ts',
  'src/pages/dashboard/dashboardModel.ts',
  'src/lib/apiRequest.ts',
  'src/lib/auth-client.ts',
  'src/lib/auth-context.tsx',
  'src/pages/asset-decisions/hooks/useAssetDecisionRouteState.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionTemplates.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts',
  'src/pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.ts',
] as const

type CoverageBudget = {
  global?: Record<string, unknown>
  critical?: Record<string, Record<string, unknown>>
}

function readBudget(): CoverageBudget | null {
  if (!existsSync(BUDGET_PATH)) return null
  return JSON.parse(readFileSync(BUDGET_PATH, 'utf8')) as CoverageBudget
}

describe('coverage budget contract', () => {
  it('defines an audited global baseline for every metric', () => {
    const budget = readBudget()

    expect(budget, 'coverage-budget.json must exist').not.toBeNull()
    if (!budget) return
    for (const metric of REQUIRED_GLOBAL_METRICS) {
      expect(budget.global?.[metric], `missing global ${metric}`).toEqual(expect.any(Number))
      expect(budget.global?.[metric], `global ${metric} must be a percentage`).toBeGreaterThanOrEqual(0)
      expect(budget.global?.[metric], `global ${metric} must be a percentage`).toBeLessThanOrEqual(100)
    }
  })

  it('keeps every approved critical owner present with branch coverage at least 90%', () => {
    const budget = readBudget()

    expect(budget, 'coverage-budget.json must exist').not.toBeNull()
    if (!budget) return
    for (const relativePath of REQUIRED_CRITICAL_PATHS) {
      expect(existsSync(resolve(WEB_ROOT, relativePath)), `${relativePath} must exist`).toBe(true)
      expect(
        budget.critical?.[relativePath]?.branches,
        `${relativePath} must define a branch threshold`,
      ).toEqual(expect.any(Number))
      expect(
        budget.critical?.[relativePath]?.branches,
        `${relativePath} branch threshold must be at least 90`,
      ).toBeGreaterThanOrEqual(90)
    }
  })
})
