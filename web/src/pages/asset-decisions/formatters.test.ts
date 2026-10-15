import { describe, expect, it } from 'vitest'

import type { AssetDecisionExecutionCurrentFacts, AssetDecisionRecordDetail, IPQualityProductSummary, IPQualitySummary } from '../../lib/types'
import { currentFactsIPQualityLabel, readbackCountSummary, recordCoverMeta } from './formatters'
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

describe('current facts IP quality label', () => {
  const product = (patch: Partial<IPQualityProductSummary> = {}): IPQualityProductSummary => ({
    risk_flags: [], high_risk_sources: [], restricted_services: [],
    blacklist_listed_count: 0, blacklist_checked_count: 2,
    service_available_count: 5, service_partial_count: 0, service_restricted_count: 0, service_unknown_count: 2,
    geo_status: 'unknown',
    ...patch,
  })
  const facts = (summary: Partial<IPQualitySummary>): AssetDecisionExecutionCurrentFacts => ({
    ip_quality_summary: { ip_address: '192.0.2.1', status: 'success', risk_level: 'low', use_region_code: 'US', ...summary },
  }) as unknown as AssetDecisionExecutionCurrentFacts

  it('reads revision1 product facts for partial and success, never the legacy rating', () => {
    const partialRisk = facts({ status: 'partial', risk_level: 'low', product_revision: 1, product_summary: product({ risk_flags: ['vpn'], high_risk_sources: ['ipquery.io'], blacklist_listed_count: 1 }) })
    const label = currentFactsIPQualityLabel(partialRisk)
    expect(label).toContain('风险命中')
    expect(label).toContain('VPN')
    expect(label).toContain('ipquery.io')
    expect(label).toContain('黑名单 1')
    expect(label).not.toContain('低风险')

    const clean = currentFactsIPQualityLabel(facts({ status: 'success', risk_level: 'low', product_revision: 1, product_summary: product() }))
    expect(clean).toBe('IP 192.0.2.1 采集成功 US')
    expect(currentFactsIPQualityLabel(facts({ status: 'partial', risk_level: 'high', product_revision: 1, product_summary: product() }))).toBe('IP 192.0.2.1 部分采集')
  })

  it('keeps the legacy rating only for revision0 reports', () => {
    expect(currentFactsIPQualityLabel(facts({ status: 'success', risk_level: 'high' }))).toBe('IP 192.0.2.1 高风险 US')
  })
})
