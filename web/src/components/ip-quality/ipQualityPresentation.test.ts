import { describe, expect, it } from 'vitest'

import type { IPQualityCoverage, IPQualityProviderResult, IPQualityServiceUnlock, VPSIPQualityReport } from '../../lib/types'
import {
  databaseConsistency,
  defaultServiceProbeNotice,
  deriveRiskAssessment,
  effectiveUnlockStatus,
  enabledProviderCoverage,
  negativeRiskSignals,
  observedServiceRegion,
  riskEvidenceComplete,
  riskVerdict,
  familyStateNotice,
  parseIPVersionParam,
  reportCollectionIdentity,
  selectedReportIPVersion,
  serviceAvailability,
  serviceEndpointDiagnostics,
  serviceEvidenceNotes,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusLabel,
} from './ipQualityPresentation'

function unlock(patch: Partial<IPQualityServiceUnlock>): IPQualityServiceUnlock {
  return { service: 'reddit', status: 'unknown', probe_status: 'failure', ...patch } as IPQualityServiceUnlock
}

function revision3Extra(patch: Record<string, unknown> = {}): Record<string, unknown> {
  return { probe_revision: 3, evidence_kind: 'anonymous_service_probe', ip_version: 4, ...patch }
}

describe('serviceTileDetail failure reasons', () => {
  it('reads the HTTP status integer from endpoint diagnostics, never from error_summary', () => {
    const httpFailure = (status: number) => unlock({
      error_code: 'http_status',
      error_summary: 'service returned an unsuccessful HTTP status',
      extra_json: revision3Extra({ http_status: status, endpoints: [{ endpoint: 'home', http_status: status, error_code: 'http_status', outcome: 'unknown' }] }),
    })
    expect(serviceTileDetail(httpFailure(403))).toBe('服务拒绝了探测请求（HTTP 403）')
    expect(serviceTileDetail(httpFailure(404))).toBe('探测地址不存在（HTTP 404）')
    expect(serviceTileDetail(httpFailure(429))).toBe('探测请求被限流（HTTP 429）')
    expect(serviceTileDetail(httpFailure(503))).toBe('服务暂时不可用（HTTP 503）')
    // 多入口时取最后一个 http_status 失败入口，成功入口的 200 不参与。
    expect(serviceTileDetail(unlock({
      error_code: 'http_status',
      extra_json: revision3Extra({ endpoints: [
        { endpoint: 'devices', http_status: 200, outcome: 'available', matched_rule: 'disney_device_assertion' },
        { endpoint: 'token', http_status: 401, error_code: 'http_status', outcome: 'unknown' },
      ] }),
    }))).toBe('服务拒绝了探测请求（HTTP 401）')
    // summary 里的数字不被反解析；非整数或越界状态视为缺失。
    expect(serviceTileDetail(unlock({ error_code: 'http_status', error_summary: 'http status 403' }))).toBe('服务返回异常状态')
    expect(serviceTileDetail(unlock({
      error_code: 'http_status',
      extra_json: revision3Extra({ endpoints: [{ endpoint: 'home', http_status: '403', error_code: 'http_status' }, { endpoint: 'home', http_status: 700, error_code: 'http_status' }] }),
    }))).toBe('服务返回异常状态')
    // 自定义 extra 不是匿名服务探测证据，不读取其中的状态。
    expect(serviceTileDetail(unlock({
      error_code: 'http_status',
      extra_json: { endpoints: [{ endpoint: 'home', http_status: 403, error_code: 'http_status' }] },
    }))).toBe('服务返回异常状态')
    expect(serviceTileDetail(unlock({ error_summary: 'HTTP status 403' }))).toBe('HTTP status 403')
  })

  it('maps typed transport failures to short Chinese reasons instead of raw summaries', () => {
    expect(serviceTileDetail(unlock({ error_code: 'dns_failed', error_summary: 'service DNS lookup failed' }))).toBe('域名解析失败')
    expect(serviceTileDetail(unlock({ error_code: 'connection_refused', error_summary: 'service connection was refused' }))).toBe('连接被拒绝')
    expect(serviceTileDetail(unlock({ error_code: 'network_unreachable', error_summary: 'service network is unreachable' }))).toBe('网络不可达')
    expect(serviceTileDetail(unlock({ error_code: 'tls_failed', error_summary: 'service TLS request failed' }))).toBe('TLS 连接失败')
    expect(serviceTileDetail(unlock({ error_code: 'timeout', error_summary: 'context deadline exceeded' }))).toBe('探测超时')
    expect(serviceTileDetail(unlock({ error_code: 'request_failed', error_summary: 'dial tcp: connection refused' }))).toBe('探测请求失败')
    expect(serviceTileDetail(unlock({ probe_status: 'skipped', error_code: 'address_family_unavailable', error_summary: 'no usable local address for this IP family' }))).toBe('本轮未检测到该地址族的可用本地地址')
  })

  it('describes not_configured services as not integrated rather than offering configuration', () => {
    expect(serviceTileDetail(unlock({ probe_status: 'not_configured', error_code: 'not_configured' }))).toBe('未接入该检测')
    expect(serviceTileDetail(unlock({ probe_status: 'not_configured' }))).toBe('未接入该检测')
  })
})

describe('serviceEndpointDiagnostics', () => {
  it('keeps only whitelisted endpoint fields from anonymous probe evidence', () => {
    const chatgpt = unlock({
      service: 'chatgpt',
      status: 'partial',
      probe_status: 'success',
      extra_json: revision3Extra({ endpoints: [
        { endpoint: 'web_cookie_requirements', http_status: 200, matched_rule: 'openai_cookie_requirements', outcome: 'available' },
        { endpoint: 'app_home', error_code: 'network_unreachable', outcome: 'unknown' },
        { endpoint: 'https://secret.example/?token=1', http_status: 200, outcome: 'available' },
        { endpoint: 'home', error_code: 'dial tcp 1.2.3.4: secret', matched_rule: 'Rule With Spaces', outcome: 'weird' },
      ] }),
    })
    expect(serviceEndpointDiagnostics(chatgpt)).toEqual([
      { endpoint: 'web_cookie_requirements', httpStatus: 200, matchedRule: 'openai_cookie_requirements', errorCode: null, outcome: 'available' },
      { endpoint: 'app_home', httpStatus: null, matchedRule: null, errorCode: 'network_unreachable', outcome: 'unknown' },
      { endpoint: 'home', httpStatus: null, matchedRule: null, errorCode: null, outcome: null },
    ])
    // revision 2 证据同样可追溯；未知 revision 或缺 evidence_kind 不读取。
    expect(serviceEndpointDiagnostics(unlock({ extra_json: revision3Extra({ probe_revision: 2, endpoints: [{ endpoint: 'home', outcome: 'unknown' }] }) }))).toHaveLength(1)
    expect(serviceEndpointDiagnostics(unlock({ extra_json: revision3Extra({ probe_revision: 4, endpoints: [{ endpoint: 'home' }] }) }))).toEqual([])
    expect(serviceEndpointDiagnostics(unlock({ extra_json: { probe_revision: 3, endpoints: [{ endpoint: 'home' }] } }))).toEqual([])
  })
})

describe('observedServiceRegion', () => {
  const youtube = (extra: Record<string, unknown>, patch: Partial<IPQualityServiceUnlock> = {}) => unlock({
    service: 'youtube-premium',
    source: 'youtube_premium_page_probe',
    status: 'unknown',
    probe_status: 'failure',
    error_code: 'invalid_response',
    extra_json: revision3Extra({ observed_region: 'CN', region_evidence: 'youtube_content_region', ...extra }),
    ...patch,
  })

  it('shows trusted page geography separately while availability stays unknown', () => {
    const row = youtube({})
    expect(observedServiceRegion(row, 4)).toEqual({ region: 'CN', evidence: 'youtube_content_region' })
    expect(observedServiceRegion(youtube({ region_evidence: 'youtube_context_gl', observed_region: 'US' }), 4)).toEqual({ region: 'US', evidence: 'youtube_context_gl' })
    expect(serviceEvidenceNotes(row, 4)).toEqual(['页面地区：CN；Premium 可用性未知'])
    // 页面地区不改变状态、统计或徽标区域。
    expect(effectiveUnlockStatus(row)).toBe('unknown')
    expect(unlockStatusLabel(row, true)).toBe('未知')
    expect(serviceUnlockCounts([row])).toEqual({ unlocked: 0, partial: 0, blocked: 0, unknown: 1 })
  })

  it('rejects family mismatch, older revisions, untrusted evidence kinds and malformed country codes', () => {
    expect(observedServiceRegion(youtube({}), 6)).toBeNull()
    expect(observedServiceRegion(youtube({}), null)).toBeNull()
    expect(observedServiceRegion(youtube({ ip_version: '4' }), 4)).toBeNull()
    expect(observedServiceRegion(youtube({ probe_revision: 2 }), 4)).toBeNull()
    expect(observedServiceRegion(youtube({ evidence_kind: 'custom' }), 4)).toBeNull()
    expect(observedServiceRegion(youtube({ region_evidence: 'ip_database' }), 4)).toBeNull()
    for (const observed_region of ['cn', 'CHN', 'C1', '', 7]) {
      expect(observedServiceRegion(youtube({ observed_region }), 4)).toBeNull()
    }
    // 残留 normalized region 不是页面地区证据。
    const residual = youtube({ observed_region: undefined, region_evidence: undefined }, { region: 'CN' })
    expect(observedServiceRegion(residual, 4)).toBeNull()
    expect(serviceEvidenceNotes(residual, 4)).toEqual([])
  })

  it('does not add the unknown-availability note once business evidence decided the status', () => {
    const blocked = youtube({}, { status: 'blocked', probe_status: 'success', region: 'CN', unlock_type: 'premium_offer' })
    expect(serviceEvidenceNotes(blocked, 4)).toEqual([])
    expect(unlockStatusLabel(blocked, true)).toBe('受限 · CN')
  })
})

describe('serviceEvidenceNotes capability boundaries', () => {
  it('states untested Netflix connection method and unknown ChatGPT region without inventing facts', () => {
    expect(serviceEvidenceNotes(unlock({ service: 'netflix', status: 'unlocked', probe_status: 'success', region: 'US' }), 4)).toEqual(['连接方式未检测'])
    expect(serviceEvidenceNotes(unlock({ service: 'chatgpt', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown' }), 4)).toEqual(['服务地区未知'])
    expect(serviceEvidenceNotes(unlock({ service: 'reddit', status: 'blocked', probe_status: 'success' }), 4)).toEqual([])
  })
})

describe('selectedReportIPVersion', () => {
  it('uses the selected report family and refuses contradictory identities', () => {
    expect(selectedReportIPVersion(report([], {}, { ip_version: 6 }))).toBe(6)
    expect(selectedReportIPVersion(report([], { latest_report: latestReport({ ip_version: 6 }) }, { ip_version: 4 }))).toBeNull()
    expect(selectedReportIPVersion(report([], { selected_ip_version: 6 }, { ip_version: 4 }))).toBeNull()
    expect(selectedReportIPVersion(report([], {}, { ip_version: 5 }))).toBeNull()
    // 所选族没有有效报告时仍由 selected_ip_version 确定。
    expect(selectedReportIPVersion(report([], { selected_ip_version: 6, summary: null }))).toBe(6)
  })
})

describe('parseIPVersionParam', () => {
  it('accepts exactly one 4 or 6 and treats empty, duplicate and other values as recoverable errors', () => {
    expect(parseIPVersionParam([])).toEqual({ kind: 'default' })
    expect(parseIPVersionParam(['4'])).toEqual({ kind: 'family', ipVersion: 4 })
    expect(parseIPVersionParam(['6'])).toEqual({ kind: 'family', ipVersion: 6 })
    for (const values of [[''], ['5'], ['4', '4'], ['4', '6'], [' 4'], ['v6']]) {
      expect(parseIPVersionParam(values)).toEqual({ kind: 'invalid' })
    }
  })
})

describe('reportCollectionIdentity', () => {
  it('reads the shown report round from its own diagnostics only', () => {
    expect(reportCollectionIdentity(report([], { latest_report: latestReport({ diagnostics_json: { collection_id: 'col_1', collect_request_id: 'ipqc_1' } }) })))
      .toEqual({ collectionID: 'col_1', collectRequestID: 'ipqc_1' })
    expect(reportCollectionIdentity(report([], { latest_report: latestReport({ diagnostics_json: { collection_id: 7, collect_request_id: '' } }) })))
      .toEqual({ collectionID: null, collectRequestID: null })
    expect(reportCollectionIdentity(null)).toEqual({ collectionID: null, collectRequestID: null })
  })
})

describe('familyStateNotice', () => {
  type Attempt = NonNullable<VPSIPQualityReport['families'][number]['latest_attempt']>
  const attempt = (patch: Partial<Attempt> = {}): Attempt => ({
    observed_at: '2026-06-09T12:00:00Z', stale: false, report_status: 'failure', collection_scope: 'dual_stack', collection_id: 'col_2', ...patch,
  })
  const withFamily = (family: 4 | 6, availability: string, latest_attempt: Attempt | null, hasSummary: boolean) => report([], {
    selected_ip_version: family,
    ...(hasSummary ? {} : { summary: null }),
    families: [
      { ip_version: 4, availability: family === 4 ? availability : 'available', latest_attempt: family === 4 ? latest_attempt : null, latest_report_summary: null },
      { ip_version: 6, availability: family === 6 ? availability : 'available', latest_attempt: family === 6 ? latest_attempt : null, latest_report_summary: null },
    ],
  }, { ip_version: family })

  it('separates lookup failure, fresh missing family, stale attempts and never-collected families', () => {
    expect(familyStateNotice(withFamily(4, 'available', attempt({ error_code: 'lookup_failed' }), true), 4)).toEqual({
      tone: 'warning', title: 'IPv4 出口探测失败', detail: null, lastValidAt: '2026-06-08T12:00:00Z',
    })
    expect(familyStateNotice(withFamily(6, 'unavailable', attempt({ error_code: 'address_family_unavailable' }), false), 6)).toMatchObject({
      title: '本轮未检测到 IPv6 可用本地地址', lastValidAt: null,
    })
    const stale = familyStateNotice(withFamily(6, 'unavailable', attempt({ stale: true, error_code: 'address_family_unavailable' }), true), 6)
    expect(stale?.title).toContain('已过期')
    expect(`${stale?.title}${stale?.detail}`).not.toContain('本轮未检测到')
    expect(familyStateNotice(withFamily(6, 'unknown', null, false), 6)?.title).toBe('IPv6 尚无采集结果')
    // 成功的本轮或旧 agent 只有有效报告时不额外提示，也不推断缺族。
    expect(familyStateNotice(withFamily(4, 'available', attempt({ report_status: 'success' }), true), 4)).toBeNull()
    expect(familyStateNotice(withFamily(4, 'unknown', null, true), 4)).toBeNull()
  })
  it.each([false, true])('does not promote rejected unavailable metadata to family absence (stale=%s)', (stale) => {
    const notice = familyStateNotice(withFamily(4, 'unknown', attempt({
      stale, error_code: 'address_family_unavailable',
    }), true), 4)
    expect(notice?.lastValidAt).toBe('2026-06-08T12:00:00Z')
    expect(`${notice?.title} ${notice?.detail}`).not.toMatch(/未检测到|未发现.*本地地址/)
    expect(notice?.tone).toBe(stale ? 'muted' : 'warning')
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
    selected_ip_version: summaryPatch.ip_version === 6 ? 6 : 4,
    default_ip_version: 4,
    families: [
      { ip_version: 4, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
      { ip_version: 6, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
    ],
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

function latestReport(patch: Partial<NonNullable<VPSIPQualityReport['latest_report']>> = {}): NonNullable<VPSIPQualityReport['latest_report']> {
  return {
    report_id: 'ipq_evidence', monitoring_instance_id: 'mi', observed_at: '', received_at: '', agent_version: '', fingerprint: '',
    sync_batch_id: '', ip_address: '192.0.2.1', ip_version: 4, status: 'success', is_backfilled: false, created_at: '',
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

  it('shows an evidence gap instead of a low verdict when flags are missing, but keeps found medium/high levels', () => {
    const partial = report([
      { provider: 'ipapi.is', status: 'success', risk_level: 'low', is_proxy: false, is_vpn: false, is_tor: false, is_server: true },
      { provider: 'ipquery.io', status: 'success', risk_score: '0', is_proxy: false },
    ])
    expect(deriveRiskAssessment(partial)).toEqual({ level: 'low', complete: false, knownFlags: 3, totalFlags: 5 })
    expect(riskVerdict(partial)).toEqual({ value: '证据不完整', valueKind: 'gap', headline: '已返回来源：低风险', scope: '风险字段 3/5', tone: 'neutral' })

    // 负面等级即使字段不全也必须作为已发现等级显示，同时保留缺口说明。
    for (const [level, label, tone] of [['medium', '中风险', 'notice'], ['high', '高风险', 'alert'], ['critical', '严重风险', 'critical']] as const) {
      expect(riskVerdict(report([{ provider: 'db', status: 'success', risk_level: level, is_vpn: false }]))).toEqual({
        value: label, valueKind: 'level', headline: `已返回来源：${label}`, scope: '风险字段 1/5，结论不完整', tone,
      })
    }
    expect(riskVerdict(report([{ provider: 'flags', status: 'success', risk_level: 'low', ...cleanFlags }]))).toMatchObject({ value: '低风险', valueKind: 'level', tone: 'normal' })

    expect(riskVerdict(report([{ provider: 'geo', status: 'success', region_code: 'DE' }]))).toEqual({
      value: '—',
      valueKind: 'none',
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
      latest_report: latestReport({ status: 'partial', diagnostics_json: { source_version: 'v2', ip_conflict: true } }),
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

describe('serviceAvailability', () => {
  const services = (statuses: string[]): IPQualityServiceUnlock[] => statuses.map((status, index) => ({
    service: `svc${index}`,
    status,
    probe_status: status === 'unknown' ? 'failure' : 'success',
  }))

  it('uses the full expected service count as denominator and only counts fully unlocked as available', () => {
    const rows = services(['unlocked', 'unlocked', 'partial', 'unknown', 'unknown', 'unknown', 'unknown'])
    expect(serviceAvailability(report([], { service_unlocks: rows }, { coverage: coverage() }))).toEqual({
      unlocked: 2, partial: 1, blocked: 0, unknown: 4, expected: 7,
    })
    // 覆盖声明但没有返回行的服务计为未知，而不是缩小分母。
    expect(serviceAvailability(report([], { service_unlocks: rows.slice(0, 3) }, { coverage: coverage() }))).toEqual({
      unlocked: 2, partial: 1, blocked: 0, unknown: 4, expected: 7,
    })
  })

  it('keeps status counts without fabricating a ratio when coverage is absent or invalid', () => {
    const rows = services(['unlocked', 'blocked', 'unknown'])
    for (const invalid of [null, coverage({ expected_service_count: 0 }), coverage({ expected_service_count: 2 }), coverage({ expected_service_count: -1 }), coverage({ expected_service_count: 2.5 })]) {
      expect(serviceAvailability(report([], { service_unlocks: rows, latest_report: null }, { coverage: invalid }))).toEqual({
        unlocked: 1, partial: 0, blocked: 1, unknown: 1, expected: null,
      })
    }
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
    return report([], { service_unlocks, latest_report: latestReport({ report_id: 'ipq', diagnostics_json }) })
  }

  it('uses the same neutral capability notice for revisions 2 and 3 and keeps warnings for others', () => {
    const revision2 = defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: 2 }))
    expect(revision2?.tone).toBe('muted')
    expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: 3 }))).toEqual(revision2)
    for (const service_probe_revision of ['2', '3', 4, null]) {
      expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision }))?.tone).toBe('warning')
    }
    expect(defaultServiceProbeNotice(withDiagnostics({ source_version: 'v2', service_probe_revision: 1 }, [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
    ]))).toMatchObject({ tone: 'warning', text: expect.stringContaining('已停用') })
  })
})
