import { describe, expect, it } from 'vitest'

const controllerSources = import.meta.glob('../pages/asset-decisions/hooks/useAssetDecision*.ts', {
  query: '?raw',
  import: 'default',
  eager: true,
})

const EXPECTED_CONTROLLER_PATHS = [
  '../pages/asset-decisions/hooks/useAssetDecisionRouteState.ts',
]

describe('Asset Decisions architecture contract', () => {
  it('tracks every implemented controller entry point during migration', () => {
    expect(Object.keys(controllerSources).sort()).toEqual(EXPECTED_CONTROLLER_PATHS)
  })
})
