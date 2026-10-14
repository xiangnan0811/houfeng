import { describe, expect, it } from 'vitest'

import type { IPQualityCoverage, IPQualityProviderResult, IPQualityServiceUnlock, VPSIPQualityReport } from '../../lib/types'
import {
  databaseConsistency,
  defaultServiceProbeNotice,
  deriveRiskAssessment,
  effectiveUnlockStatus,
  enabledProviderCoverage,
  negativeRiskSignals,
  riskEvidenceComplete,
  riskVerdict,
  serviceCardDescription,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusLabel,
} from './ipQualityPresentation'

function unlock(patch: Partial<IPQualityServiceUnlock>): IPQualityServiceUnlock {
  return { service: 'reddit', status: 'unknown', probe_status: 'failure', ...patch } as IPQualityServiceUnlock
}

describe('serviceCardDescription', () => {
  it('turns HTTP probe failures into a short Chinese judgement instead of the raw agent text', () => {
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 403' }))).toBe('服务拒绝了探测请求（HTTP 403）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 401' }))).toBe('服务拒绝了探测请求（HTTP 401）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 404' }))).toBe('探测地址不存在（HTTP 404）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 429' }))).toBe('探测请求被限流（HTTP 429）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 503' }))).toBe('服务暂时不可用（HTTP 503）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 418' }))).toBe('服务返回异常状态（HTTP 418）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: '' }))).toBe('服务返回异常状态')
    // 旧数据可能只有 summary 没有 code。
    expect(serviceCardDescription(unlock({ error_summary: 'HTTP status 403' }))).toBe('服务拒绝了探测请求（HTTP 403）')
  })

  it('labels every error code the agent service probes emit, preferring the code over summary text', () => {
    expect(serviceCardDescription(unlock({ error_code: 'timeout', error_summary: 'context deadline exceeded' }))).toBe('探测超时')
    // agent 真实文本里带有 http status 200，也必须按 error_code 归类为响应无法识别。
    expect(serviceCardDescription(unlock({ error_code: 'non_json_response', error_summary: 'non_json_response: http status 200 content-type "text/html"' }))).toBe('服务响应无法识别')
    expect(serviceCardDescription(unlock({ error_code: 'request_failed', error_summary: 'dial tcp: connection refused' }))).toBe('探测请求失败')
    expect(serviceCardDescription(unlock({ error_code: 'read_failed', error_summary: 'unexpected EOF' }))).toBe('读取服务响应失败')
    expect(serviceCardDescription(unlock({ error_code: 'invalid_request', error_summary: 'parse "%%": invalid URL escape' }))).toBe('探测请求无法发出')
    expect(serviceCardDescription(unlock({ error_code: 'probe_failed', error_summary: 'panic recovered' }))).toBe('探测失败，未形成可靠结论')
    expect(serviceCardDescription(unlock({ probe_status: 'skipped', error_code: 'unsupported_service', error_summary: 'service is not supported by default IP quality probes' }))).toBe('默认探测暂不支持该服务')
    expect(serviceCardDescription(unlock({ error_code: 'response_too_large', error_summary: 'service response exceeds size limit' }))).toBe('服务响应过大，未形成结论')
    expect(serviceCardDescription(unlock({ error_code: 'unexpected_redirect', error_summary: 'service redirect is not allowed' }))).toBe('探测被重定向到非预期入口')
    expect(serviceCardDescription(unlock({ error_code: 'challenge_detected', error_summary: 'service challenge detected' }))).toBe('服务要求人机验证')
    expect(serviceCardDescription(unlock({ error_code: 'egress_ambiguous', error_summary: 'service egress is ambiguous' }))).toBe('当前出口不唯一，无法确认探测归属')
    expect(serviceCardDescription(unlock({ error_code: 'ip_mismatch', error_summary: 'provider returned a different IP' }))).toBe('来源返回了不同IP')
  })

  it('does not rewrite summaries that merely mention an HTTP status', () => {
    expect(serviceCardDescription(unlock({ error_summary: '上次 http status 403 已恢复，等待复测' }))).toBe('上次 http status 403 已恢复，等待复测')
    expect(serviceCardDescription(unlock({ error_code: 'custom_check', error_summary: 'http status 403' }))).toBe('http status 403')
  })
})

describe('effectiveUnlockStatus', () => {
  it('only trusts a known business status from a successful probe', () => {
    expect(effectiveUnlockStatus(unlock({ status: 'blocked', probe_status: 'success' }))).toBe('blocked')
    expect(effectiveUnlockStatus(unlock({ status: 'partial', probe_status: 'success' }))).toBe('partial')
    // 历史数据缺 probe_status 时兼容旧语义。
    expect(effectiveUnlockStatus({ service: 'reddit', status: 'unlocked' })).toBe('unlocked')
    for (const probe_status of ['failure', '', 'skipped', 'challenge']) {
      expect(effectiveUnlockStatus(unlock({ status: 'blocked', probe_status }))).toBe('unknown')
    }
    expect(effectiveUnlockStatus(unlock({ status: 'unknown', probe_status: 'success' }))).toBe('unknown')
  })

  it('hides residual region and scope on failed rows and counts them as unknown', () => {
    const residual = unlock({
      status: 'blocked',
      probe_status: 'failure',
      region: 'US',
      unlock_type: 'catalog_sample',
      error_code: 'challenge_detected',
    })
    expect(unlockStatusLabel(residual, true)).toBe('未知')
    expect(serviceTileDetail(residual)).toBe('服务要求人机验证')
    expect(serviceUnlockCounts([residual])).toEqual({ unlocked: 0, partial: 0, blocked: 0, unknown: 1 })
  })

  it('maps scope enums to Chinese wording and revision-specific status words', () => {
    const netflix = unlock({ service: 'netflix', status: 'partial', probe_status: 'success', region: 'DE', unlock_type: 'sample_titles_unavailable' })
    expect(unlockStatusLabel(netflix, true)).toBe('部分可用 · DE')
    expect(unlockStatusLabel(netflix, false)).toBe('部分 · DE')
    expect(serviceTileDetail(netflix)).toBe('抽样片目不可用，未验证完整片库')
    const chatgpt = unlock({ service: 'chatgpt', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown' })
    expect(serviceTileDetail(chatgpt)).toBe('Web 已验证，App 入口未知')
    expect(unlockStatusLabel(unlock({ status: 'blocked', probe_status: 'success' }), true)).toBe('受限')
    expect(unlockStatusLabel(unlock({ status: 'blocked', probe_status: 'success' }), false)).toBe('受阻')
  })
})

function coverage(patch: Partial<IPQualityCoverage> = {}): IPQualityCoverage {
  return {
    expected_provider_count: 18,
    successful_provider_count: 5,
    failed_provider_count: 0,
    skipped_provider_count: 0,
    not_configured_provider_count: 13,
    expected_service_count: 7,
    successful_service_count: 6,
    failed_service_count: 1,
    skipped_service_count: 0,
    not_configured_service_count: 0,
    ...patch,
  }
}

function report(
  provider_results: IPQualityProviderResult[],
  patch: Partial<VPSIPQualityReport> = {},
  summaryPatch: Partial<NonNullable<VPSIPQualityReport['summary']>> = {},
): VPSIPQualityReport {
  return {
    summary: {
      report_id: 'ipq_evidence',
      vps_id: 'vps_evidence',
      observed_at: '2026-06-08T12:00:00Z',
      ip_address: '192.0.2.1',
      ip_version: 4,
      status: 'success',
      stale: false,
      ambiguous: false,
      provider_count: provider_results.length,
      unlockable_count: 0,
      ...summaryPatch,
    },
    provider_results,
    service_unlocks: [],
    history: [],
    ...patch,
  }
}

const cleanFlags = { is_proxy: false, is_tor: false, is_vpn: false, is_abuser: false, is_robot: false }

describe('deriveRiskAssessment', () => {
  it('is independent of service probe results', () => {
    const providers: IPQualityProviderResult[] = [
      { provider: 'ipapi.is', status: 'success', ...cleanFlags, is_vpn: true },
      { provider: 'proxycheck.io', status: 'success', risk_level: 'medium', is_vpn: true },
    ]
    const unknownServices = report(providers, {
      service_unlocks: [{ service: 'netflix', status: 'unknown', probe_status: 'failure', error_code: 'timeout' }],
    })
    const knownServices = report(providers, {
      service_unlocks: [
        { service: 'netflix', status: 'blocked', probe_status: 'success' },
        { service: 'reddit', status: 'unlocked', probe_status: 'success' },
      ],
    })
    expect(deriveRiskAssessment(unknownServices)).toEqual(deriveRiskAssessment(knownServices))
    expect(riskVerdict(unknownServices)).toEqual(riskVerdict(knownServices))
    expect(deriveRiskAssessment(knownServices)).toEqual({ level: 'medium', complete: true, knownFlags: 5, totalFlags: 5 })
  })

  it('takes the highest successful provider level and ignores summary low and failed residue', () => {
    const assessed = report([
      { provider: 'ipapi.is', status: 'success', risk_level: 'low', ...cleanFlags },
      { provider: 'proxycheck.io', status: 'success', risk_level: 'high' },
      { provider: 'ip2location.io', status: 'failure', risk_level: 'critical', is_tor: true },
    ], {}, { risk_level: 'low' })
    expect(deriveRiskAssessment(assessed).level).toBe('high')
    expect(riskVerdict(assessed)).toMatchObject({ value: '高风险', tone: 'alert' })
    expect(negativeRiskSignals(assessed.provider_results)).toEqual([])
  })

  it('reports missing flags as incomplete instead of a clean low verdict', () => {
    const partial = report([
      { provider: 'ipapi.is', status: 'success', risk_level: 'low', is_proxy: false, is_vpn: false, is_tor: false, is_server: true },
      { provider: 'ipquery.io', status: 'success', risk_score: '0', is_proxy: false },
    ])
    expect(deriveRiskAssessment(partial)).toEqual({ level: 'low', complete: false, knownFlags: 3, totalFlags: 5 })
    expect(riskVerdict(partial)).toEqual({ value: '低风险', headline: '已返回来源：低风险', scope: '风险字段 3/5，结论不完整', tone: 'neutral' })

    expect(riskVerdict(report([{ provider: 'geo', status: 'success', region_code: 'DE' }]))).toEqual({
      value: '—',
      headline: '证据不足',
      scope: '风险字段 0/5，结论不完整',
      tone: 'neutral',
    })
    expect(riskVerdict(report([{ provider: 'vpn-db', status: 'success', is_vpn: true }]))).toMatchObject({ value: '—', headline: '发现风险信号', tone: 'alert' })
    expect(riskVerdict(report([{ provider: 'flags', status: 'success', ...cleanFlags }]))).toMatchObject({ value: '—', headline: '已返回风险字段未命中' })
    expect(riskEvidenceComplete([{ provider: 'flags', status: 'success', ...cleanFlags, is_robot: null }])).toBe(false)
  })

  it('withholds aggregation for ambiguous attribution or source IP conflict, and neutralises stale reports', () => {
    const providers: IPQualityProviderResult[] = [{ provider: 'ipapi.is', status: 'success', risk_level: 'high', ...cleanFlags }]
    expect(deriveRiskAssessment(report(providers, {}, { ambiguous: true }))).toMatchObject({ level: null, complete: false })
    expect(riskVerdict(report(providers, {}, { ambiguous: true })).headline).toBe('IP 归属不唯一，需先复核')

    const conflict = report(providers, {
      latest_report: {
        report_id: 'ipq_evidence', monitoring_instance_id: 'mi', observed_at: '', received_at: '', agent_version: '', fingerprint: '',
        sync_batch_id: '', ip_address: '192.0.2.1', ip_version: 4, status: 'partial', is_backfilled: false, created_at: '',
        diagnostics_json: { source_version: 'v2', ip_conflict: true },
      },
    })
    expect(deriveRiskAssessment(conflict).level).toBeNull()
    expect(riskVerdict(conflict)).toMatchObject({ value: '—', headline: '来源返回了不同 IP，需复核来源' })

    expect(riskVerdict(report(providers, {}, { stale: true }))).toMatchObject({ value: '高风险', tone: 'neutral' })
    expect(deriveRiskAssessment({ ...report(providers), summary: null })).toMatchObject({ level: null, complete: false })
  })
})

describe('enabledProviderCoverage', () => {
  it('divides by enabled sources only and refuses to clamp abnormal counts', () => {
    expect(enabledProviderCoverage(report([], { latest_report: null }, { coverage: coverage() }))).toEqual({ successful: 5, enabled: 5, notConfigured: 13, percent: 100 })
    expect(enabledProviderCoverage(report([], {}, { coverage: coverage({ successful_provider_count: 4, failed_provider_count: 1 }) }))?.percent).toBe(80)
    expect(enabledProviderCoverage(report([], {}, { coverage: coverage({ successful_provider_count: 6 }) }))).toBeNull()
    expect(enabledProviderCoverage(report([], {}, { coverage: coverage({ not_configured_provider_count: 18 }) }))).toBeNull()
    expect(enabledProviderCoverage(report([], {}, { coverage: coverage({ successful_provider_count: -1 }) }))).toBeNull()
    expect(enabledProviderCoverage(report([], {}, { coverage: null }))).toBeNull()
  })
})

describe('databaseConsistency', () => {
  it('compares only dimensions with at least two known values and never mixes region names with codes', () => {
    expect(databaseConsistency([
      { provider: 'ipapi.is', status: 'success', region_name: 'Singapore', is_vpn: false, is_proxy: null },
      { provider: 'ipquery.io', status: 'success', region_code: 'JP', is_vpn: false },
    ])).toEqual({ percent: 100, dimensions: 1 })
    expect(databaseConsistency([
      { provider: 'a', status: 'success', region_code: 'SG', is_vpn: true },
      { provider: 'b', status: 'success', region_code: 'JP', is_vpn: true },
      { provider: 'c', status: 'failure', region_code: 'US', is_vpn: false },
    ])).toEqual({ percent: 50, dimensions: 2 })
    expect(databaseConsistency([
      { provider: 'a', status: 'success', region_code: 'SG' },
      { provider: 'b', status: 'success', usage_type: 'hosting' },
    ])).toBeNull()
  })
})

describe('defaultServiceProbeNotice', () => {
  const rows: IPQualityServiceUnlock[] = [{ service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success' }]
  function withDiagnostics(diagnostics_json: unknown, service_unlocks = rows): VPSIPQualityReport {
    return report([], {
      service_unlocks,
      latest_report: {
        report_id: 'ipq', monitoring_instance_id: 'mi', observed_at: '', received_at: '', agent_version: '', fingerprint: '',
        sync_batch_id: '', ip_address: '192.0.2.1', ip_version: 4, status: 'success', is_backfilled: false, created_at: '',
        diagnostics_json,
      },
    })
  }

  it('uses a neutral scope notice for revision 2 and keeps history warnings for other revisions', () => {
    expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: 2 }))).toEqual({
      tone: 'muted',
      text: '免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。',
    })
    expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: '2' }))?.text).toContain('旧版或未识别')
    expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: 1 }, [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
    ]))).toMatchObject({ tone: 'warning', text: expect.stringContaining('已停用') })
  })
})
