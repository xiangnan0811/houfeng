import { describe, expect, it } from 'vitest'

import type { AssetDecisionRecordDetail } from '../../lib/types'
import { readbackCountSummary, recordCoverMeta } from './formatters'
import { decisionRecord, recordReadback } from './testFixtures'

describe('asset decision readback wording', () => {
  it('uses the same vocabulary as the overview risk subtitle and waits for results when readback is missing', () => {
    const withoutReadback = { ...decisionRecord(), execution_readback: undefined } as unknown as AssetDecisionRecordDetail
    expect(recordCoverMeta(withoutReadback)).toMatch(/· 等待核对结果$/)
    expect(readbackCountSummary(undefined)).toBe('等待核对结果')
    expect(readbackCountSummary(recordReadback({ drift_count: 1, blocked_count: 2, needs_evidence_count: 3, open_count: 4 }) as never))
      .toBe('与决定不符 1 · 受阻 2 · 缺资料 3 · 待核对结果 4')
  })
})
