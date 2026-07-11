import { describe, expect, it } from 'vitest'

const controllerSources = import.meta.glob('../pages/asset-decisions/hooks/useAssetDecision*.ts', {
  query: '?raw',
  import: 'default',
  eager: true,
})

const EXPECTED_CONTROLLER_PATHS = [
  '../pages/asset-decisions/hooks/useAssetDecisionGroups.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRecords.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRouteState.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionTemplates.ts',
]

describe('Asset Decisions architecture contract', () => {
  it('tracks every implemented controller entry point during migration', () => {
    expect(Object.keys(controllerSources).sort()).toEqual(EXPECTED_CONTROLLER_PATHS)
  })
})
