import { describe, expect, it } from 'vitest'

import type {
  IPQualityProductView,
  IPQualityProviderDetails,
  IPQualityProviderResult,
  IPQualityScore,
  IPQualityServiceUnlock,
  VPSIPQualityReport,
} from '../../lib/types'
import {
  effectiveUnlockStatus,
  observedServiceRegion,
  probeDiagnosticText,
  familyStateNotice,
  parseIPVersionParam,
  reportCollectionIdentity,
  selectedReportIPVersion,
  serviceEndpointDiagnostics,
  serviceRuleMark,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusLabel,
} from './ipQualityPresentation'
import {
  agreedCountry,
  agreedValue,
  fieldCellState,
  flagCellState,
  FLAG_COLUMNS,
  groupDigits,
  locationRows,
  scoreDisplay,
  selectedProduct,
  usageTypeLabel,
  vendorGroups,
} from './ipQualityProductPresentation'

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
    // revision 2/3/4 证据同样可追溯；未知 revision 或缺 evidence_kind 不读取。
    expect(serviceEndpointDiagnostics(unlock({ extra_json: revision3Extra({ probe_revision: 2, endpoints: [{ endpoint: 'home', outcome: 'unknown' }] }) }))).toHaveLength(1)
    expect(serviceEndpointDiagnostics(unlock({ extra_json: revision3Extra({ probe_revision: 4, endpoints: [{ endpoint: 'home', outcome: 'unknown' }] }) }))).toHaveLength(1)
    expect(serviceEndpointDiagnostics(unlock({ extra_json: revision3Extra({ probe_revision: 5, endpoints: [{ endpoint: 'home' }] }) }))).toEqual([])
    expect(serviceEndpointDiagnostics(unlock({ extra_json: { probe_revision: 3, endpoints: [{ endpoint: 'home' }] } }))).toEqual([])
    // 固定脚本 revision5：读 diagnostics，positive 统一为可用；连字符端点 ID 合法，非白名单形状整体丢弃。
    const script = (patch: Record<string, unknown>) => unlock({ extra_json: { evidence_kind: 'ipquality_script', probe_revision: 5, ip_version: 4, rule: 'netflix_originals_only', ...patch } })
    expect(serviceEndpointDiagnostics(script({ diagnostics: [
      { endpoint: 'netflix-80057281', http_status: 200, outcome: 'positive' },
      { endpoint: 'openai-ios', http_status: 403, outcome: 'unknown', error_code: 'client_access_denied' },
      { endpoint: 'Bad Endpoint', outcome: 'positive' },
    ] }))).toEqual([
      { endpoint: 'netflix-80057281', httpStatus: 200, matchedRule: null, errorCode: null, outcome: 'available' },
      { endpoint: 'openai-ios', httpStatus: 403, matchedRule: null, errorCode: 'client_access_denied', outcome: 'unknown' },
    ])
    // 脚本证据必须是 revision5；evidence_kind 与 revision 不配对时不读取。
    expect(serviceEndpointDiagnostics(script({ probe_revision: 4, diagnostics: [{ endpoint: 'netflix-80057281', outcome: 'positive' }] }))).toEqual([])
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
    expect(observedServiceRegion(youtube({ probe_revision: 4 }), 4)).toEqual({ region: 'CN', evidence: 'youtube_content_region' })
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
    expect(observedServiceRegion(youtube({}, { status: 'blocked', probe_status: 'success' }), 6)).toBeNull()
  })

  it('keeps a business-decided status label independent of page geography', () => {
    const blocked = youtube({}, { status: 'blocked', probe_status: 'success', region: 'CN', unlock_type: 'premium_offer' })
    expect(unlockStatusLabel(blocked, true)).toBe('受限 · CN')
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
      kind: 'failed', tone: 'warning', title: 'IPv4 出口探测失败', detail: null, lastValidAt: '2026-06-08T12:00:00Z', attemptedAt: '2026-06-09T12:00:00Z',
    })
    expect(familyStateNotice(withFamily(6, 'unavailable', attempt({ error_code: 'address_family_unavailable' }), false), 6)).toMatchObject({
      kind: 'unavailable', title: '本轮未检测到 IPv6 可用本地地址', lastValidAt: null, attemptedAt: '2026-06-09T12:00:00Z',
    })
    const stale = familyStateNotice(withFamily(6, 'unavailable', attempt({ stale: true, error_code: 'address_family_unavailable' }), true), 6)
    expect(stale?.kind).toBe('stale_unavailable')
    expect(stale?.title).toContain('已过期')
    expect(`${stale?.title}${stale?.detail}`).not.toContain('本轮未检测到')
    expect(familyStateNotice(withFamily(4, 'available', attempt({ stale: true }), false), 4)?.kind).toBe('stale_failed')
    expect(familyStateNotice(withFamily(4, 'available', attempt({ stale: true, report_status: 'success' }), false), 4)?.kind).toBe('stale_attempt')
    expect(familyStateNotice(withFamily(6, 'unknown', null, false), 6)).toMatchObject({ kind: 'never', title: 'IPv6 尚无采集结果', attemptedAt: null })
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

describe('probeDiagnosticText', () => {
  it('states the HTTP status and safe reason once per diagnostic', () => {
    // http_status 的短句已含状态码，不再前置独立的 HTTP N；401/403/429/5xx 语义不变，403 不变成地区封锁。
    expect(probeDiagnosticText('http_status', 403)).toBe('服务拒绝了探测请求（HTTP 403）')
    expect(probeDiagnosticText('HTTP_STATUS', 429)).toBe('探测请求被限流（HTTP 429）')
    expect(probeDiagnosticText('http_status', 503)).toBe('服务暂时不可用（HTTP 503）')
    expect(probeDiagnosticText('http_status', null)).toBe('服务返回异常状态')
    // 其他码：合法状态在前、中文原因在后；未知但已过白名单的码原样显示。
    expect(probeDiagnosticText('client_access_denied', 403)).toBe('HTTP 403 · 入口拒绝客户端访问')
    expect(probeDiagnosticText('future_code', 418)).toBe('HTTP 418 · future_code')
    // 可用：只有 HTTP；越界/非整数状态视为缺失；两者都无时为 null。
    expect(probeDiagnosticText(undefined, 200)).toBe('HTTP 200')
    expect(probeDiagnosticText('client_access_denied', 99)).toBe('入口拒绝客户端访问')
    expect(probeDiagnosticText('http_status', 200.5)).toBe('服务返回异常状态')
    expect(probeDiagnosticText(' ', null)).toBeNull()
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
    expect(serviceTileDetail(netflix)).toBe('样片均不可用')
    const chatgpt = unlock({ service: 'chatgpt', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown' })
    expect(serviceTileDetail(chatgpt)).toBe('Web 可用，App 未知')
    expect(unlockStatusLabel(unlock({ status: 'blocked', probe_status: 'success' }), true)).toBe('受限')
    expect(unlockStatusLabel(unlock({ status: 'blocked', probe_status: 'success' }), false)).toBe('受阻')
    // Netflix 仅自制剧：短标签替代笼统“部分可用”，范围在详情里。
    const originals = unlock({ service: 'netflix', status: 'partial', probe_status: 'success', region: 'US', unlock_type: 'originals_only' })
    expect(unlockStatusLabel(originals, true)).toBe('仅自制剧 · US')
    expect(serviceTileDetail(originals)).toBe('仅自制剧可访问')
    // 失败行残留的 originals_only 不生效。
    expect(unlockStatusLabel(unlock({ ...originals, probe_status: 'failure' }), true)).toBe('未知')
  })
})

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


describe('serviceRuleMark', () => {
  const rows: IPQualityServiceUnlock[] = [{ service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success' }]
  const withDiagnostics = (diagnostics_json: unknown, service_unlocks = rows) => report([], { service_unlocks, latest_report: latestReport({ diagnostics_json }) })

  it('adds no mark for current anonymous revisions and short marks for old or unverifiable rules', () => {
    for (const service_probe_revision of [2, 3, 4]) {
      expect(serviceRuleMark(withDiagnostics({ source_version: 'v2', service_probe_revision }))).toBeNull()
    }
    for (const service_probe_revision of ['2', '4', 0, null]) {
      expect(serviceRuleMark(withDiagnostics({ source_version: 'v2', service_probe_revision }))?.label).toBe('旧版规则')
    }
    expect(serviceRuleMark(withDiagnostics({ source_version: 'v2', service_probe_revision: 1 }, [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
    ]))?.label).toBe('探测已停用')
    expect(serviceRuleMark(withDiagnostics({ source_version: 'v2', service_probe_revision: 1 }))).toBeNull()
    expect(serviceRuleMark(withDiagnostics({}, [{ service: 'custom', status: 'unlocked' }]))?.label).toBe('来源未知')
    expect(serviceRuleMark(withDiagnostics({}, []))).toBeNull()
  })
})

function score(patch: Partial<IPQualityScore>): IPQualityScore {
  return { id: 's', label: 's', raw_value: '', unit: 'score', scope: 'ip', direction: 'higher_risk', source_label: '', value: null, min: null, max: null, ...patch }
}

describe('product presentation', () => {
  it('shows ratio scores as percentages and only draws bars with explicit bounds', () => {
    expect(scoreDisplay(score({ unit: 'ratio', value: 0.0183, min: 0, max: 1, scope: 'company_network' }))).toEqual({ text: '1.83%', fraction: 0.0183 })
    expect(scoreDisplay(score({ value: 20, min: 0, max: 100 }))).toEqual({ text: '20', fraction: 0.2 })
    expect(scoreDisplay(score({ value: 0, min: 0, max: 100 }))).toEqual({ text: '0', fraction: 0 })
    // 未得到明确数字就不画进度，也不构造 0。
    expect(scoreDisplay(score({ raw_value: 'n/a' }))).toEqual({ text: 'n/a', fraction: null })
    expect(scoreDisplay(score({ value: 33 }))).toEqual({ text: '33', fraction: null })
  })

  it('selects one sample per vendor without merging fields across vendors', () => {
    const agent: IPQualityProviderResult = { provider: 'ipapi.is', status: 'success', details: { method: 'agent_public' } as IPQualityProviderDetails }
    const center: IPQualityProviderResult = { provider: 'ipapi.is', status: 'success', details: { method: 'center_api' } as IPQualityProviderDetails }
    const failedCenter: IPQualityProviderResult = { provider: 'abuseipdb', status: 'failure' }
    expect(vendorGroups([agent, center, failedCenter]).map((group) => [group.provider, group.primary, group.others])).toEqual([
      ['ipapi.is', center, [agent]],
      ['abuseipdb', failedCenter, []],
    ])
    // 认证失败时保留匿名成功样本为主行，并标注增强查询失败；仅有失败认证行的供应商照实显示失败。
    const failedEnhancement: IPQualityProviderResult = { provider: 'ipapi.is', status: 'failure', error_code: 'rate_limited', details: { method: 'center_api' } as IPQualityProviderDetails }
    const [withFailure] = vendorGroups([agent, failedEnhancement])
    expect(withFailure?.primary).toBe(agent)
    expect(withFailure?.enhancementFailed).toBe(true)
    expect(vendorGroups([agent, center])[0]?.enhancementFailed).toBe(false)
    const [failureOnly] = vendorGroups([{ ...failedEnhancement, provider: 'abuseipdb' }])
    expect(failureOnly?.primary.status).toBe('failure')
    expect(failureOnly?.enhancementFailed).toBe(false)
    // 历史可选占位不进入主表。
    expect(vendorGroups([{ provider: 'maxmind', status: 'not_configured', source_type: 'optional' }])).toEqual([])
  })

  it('separates known negatives from unsupported, error and unknown cells', () => {
    const vpn = FLAG_COLUMNS.find((column) => column.key === 'is_vpn')!
    const row = (patch: Partial<IPQualityProviderResult>, states: Record<string, 'known' | 'unsupported' | 'unknown' | 'error'> = {}): IPQualityProviderResult => ({
      provider: 'p', status: 'success', ...patch, details: { field_states: states } as IPQualityProviderDetails,
    })
    expect(flagCellState(row({ is_vpn: true }), vpn)).toBe('yes')
    expect(flagCellState(row({ is_vpn: false }), vpn)).toBe('no')
    expect(flagCellState(row({}, { is_vpn: 'unsupported' }), vpn)).toBe('unsupported')
    expect(flagCellState(row({}, { is_vpn: 'error' }), vpn)).toBe('error')
    expect(flagCellState(row({}), vpn)).toBe('unknown')
    expect(flagCellState({ provider: 'legacy', status: 'success' }, vpn)).toBe('unknown')
  })

  it('keeps zero coordinates and per-source locations, and only agrees on identical values', () => {
    const rows = locationRows([
      { provider: 'a', status: 'success', details: { method: 'agent_public', country_code: 'US', region: '', city: 'LA', latitude: 0, longitude: 0, observed_at: 't' } as IPQualityProviderDetails },
      { provider: 'b', status: 'success', details: { method: 'agent_public', country_code: 'US', region: '', city: 'SJ', latitude: null, longitude: null, observed_at: 't' } as IPQualityProviderDetails },
      { provider: 'c', status: 'failure', region_code: 'JP' },
    ])
    expect(rows.map((row) => [row.provider, row.city, row.coordinates])).toEqual([['a', 'LA', '0, 0'], ['b', 'SJ', null]])
    expect(agreedValue(rows.map((row) => row.country))).toEqual({ value: 'US', discrepant: false })
    expect(agreedValue(rows.map((row) => row.city))).toEqual({ value: null, discrepant: true })
  })

  it('compares countries by typed code only, keeping name-only sources visible and unfilled', () => {
    const typed = (provider: string, code: string, regionName = ''): IPQualityProviderResult => ({
      provider, status: 'success', region_name: regionName,
      details: { method: 'agent_public', country_code: code, region: 'Wyoming', city: 'Buffalo', latitude: null, longitude: null, observed_at: 't' } as IPQualityProviderDetails,
    })
    const rows = locationRows([typed('a', 'US'), typed('b', '', 'United States'), typed('c', 'us')])
    expect(rows.map((row) => [row.country, row.countryCode])).toEqual([['US', 'US'], ['United States', ''], ['us', 'US']])
    expect(agreedCountry(rows)).toEqual({ value: 'US', discrepant: false })
    expect(agreedCountry(locationRows([typed('a', 'US'), typed('b', 'JP')]))).toEqual({ value: null, discrepant: true })
    // 全部只有名称时不据名称宣称分歧。
    expect(agreedCountry(locationRows([typed('a', '', 'United States'), typed('b', '', 'United States of America')]))).toEqual({ value: null, discrepant: false })
  })

  it('keeps per-source field states for type and location cells instead of one missing dash', () => {
    const row: IPQualityProviderResult = {
      provider: 'ipapi.is', status: 'success', usage_type: 'hosting',
      details: {
        method: 'agent_public', country_code: 'US', region: '', city: '', latitude: null, longitude: null, observed_at: 't',
        field_states: { usage_type: 'known', company_type: 'unsupported', region: 'error', latitude: 'unsupported', longitude: 'unsupported' },
      } as unknown as IPQualityProviderDetails,
    }
    expect(fieldCellState(row, 'usage_type', row.usage_type)).toBe('known')
    expect(fieldCellState(row, 'company_type', row.company_type)).toBe('unsupported')
    expect(fieldCellState({ provider: 'legacy', status: 'success' }, 'company_type', '')).toBe('unknown')
    const [location] = locationRows([row])
    expect(location?.states).toEqual({ country: 'known', region: 'error', city: 'unknown', coordinates: 'unsupported' })
    // revision1 来源即使没有任何定位值也保留一行以显示其字段状态；无 details 的旧行不占行。
    const empty = { ...row, details: { ...row.details!, country_code: '', field_states: { country_code: 'unsupported' } } } as IPQualityProviderResult
    expect(locationRows([empty])[0]?.states.country).toBe('unsupported')
    expect(locationRows([{ provider: 'legacy', status: 'success' }])).toEqual([])
    const badCoordinates = { ...row, details: { ...row.details!, field_states: { latitude: 'error', longitude: 'unknown' } } } as IPQualityProviderResult
    expect(locationRows([badCoordinates])[0]?.states.coordinates).toBe('error')
  })

  it('labels residential only when a source says so and groups large decimal strings', () => {
    expect(usageTypeLabel('hosting')).toBe('机房')
    expect(usageTypeLabel('Residential')).toBe('家宽')
    expect(usageTypeLabel('isp')).toBe('ISP')
    expect(usageTypeLabel('MOB')).toBe('MOB')
    expect(usageTypeLabel('')).toBeNull()
    expect(groupDigits('79228162514264337593543950336')).toBe('79,228,162,514,264,337,593,543,950,336')
    expect(groupDigits(null)).toBeNull()
  })

  it('uses only a revision1 product that belongs to the selected report', () => {
    const product = { revision: 1, report_id: 'ipq_evidence' } as IPQualityProductView
    expect(selectedProduct(report([], { product }))).toBe(product)
    expect(selectedProduct(report([], { product: { ...product, report_id: 'other' } }))).toBeNull()
    expect(selectedProduct(report([], { product: { ...product, revision: 2 } }))).toBeNull()
  })
})
