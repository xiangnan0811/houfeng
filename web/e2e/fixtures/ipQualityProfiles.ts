import type {
  IPQualityCollectionScope,
  IPQualityCollectRequest,
  IPQualityCollectStatus,
  IPQualityCoverage,
  IPQualityFamilyAttempt,
  IPQualityFamilyAvailability,
  IPQualityFamilyStatus,
  IPQualityIPVersion,
  IPQualityProductState,
  IPQualityProductSummary,
  IPQualityProductView,
  IPQualityProviderDetails,
  IPQualityProviderResult,
  IPQualityReport,
  IPQualityServiceUnlock,
  IPQualitySummary,
  VPSIPQualityReport,
} from '../../src/lib/types'
import { apiRouteKey, type ApiFixtureProfile, type ApiFixtureResponse, type ApiRouteKey } from './contracts'
import { authenticatedProfile } from './profiles'

// 浏览器 fixture 数据：只驱动页面行为契约，不是 center/PG/agent 的运行证据。
// 新双栈数据使用 RFC 5737 / RFC 3849 文档地址与 RFC 5398 文档 ASN，组织名带 Fixture 标签。

export const IPQ_VPS_ID = 'vps_001'
export const IPQ_PAGE_PATH = `/vps/${IPQ_VPS_ID}/ip-quality`
export const IPQ_REPORT_PATH = `/api/vps/${IPQ_VPS_ID}/ip-quality`
export const IPQ_COLLECT_PATH = `/api/vps/${IPQ_VPS_ID}/ip-quality/collect`
export const IPQ_COLLECT_REQUEST_ID = 'ipqc_e2e'

/** 显式族查询；未传族时就是默认 GET（无 query）。 */
export function ipqReportPath(ipVersion?: IPQualityIPVersion): string {
  return ipVersion ? `${IPQ_REPORT_PATH}?ip_version=${ipVersion}` : IPQ_REPORT_PATH
}

/** 历史详情 API 只带报告 ID，永远不带 ip_version。 */
export function ipqDetailPath(reportId: string): string {
  return `${IPQ_REPORT_PATH}/reports/${reportId}`
}

export function ipqPageURL(params: { ipVersion?: IPQualityIPVersion | string; reportId?: string } = {}): string {
  const search = new URLSearchParams()
  if (params.ipVersion !== undefined) search.set('ip_version', String(params.ipVersion))
  if (params.reportId) search.set('report_id', params.reportId)
  const query = search.toString()
  return query ? `${IPQ_PAGE_PATH}?${query}` : IPQ_PAGE_PATH
}

// ---------------------------------------------------------------------------
// 共享来源/服务形状
// ---------------------------------------------------------------------------

// 默认采集固定启用的五个数据库；其余十三个只作为未接入（wire not_configured）诊断行存在，不是失败请求。
export const NOT_CONFIGURED_PROVIDERS: readonly IPQualityProviderResult[] = [
  'maxmind', 'ipinfo', 'ipregistry', 'ipdata', 'ipqualityscore', 'abuseipdb', 'scamalytics',
  'ipapi.co', 'ip-api.com', 'db-ip', 'ippure', 'meowvps', 'ip.net.coffee',
].map((provider) => ({
  provider,
  status: 'not_configured',
  source_type: 'optional',
  error_code: 'not_configured',
  error_summary: 'optional IP quality source requires configuration or is not enabled for default agent collection',
}))

// 原始覆盖保持 18 个来源（13 个未接入）与 7 项服务的计数合同。
export const RAW_COVERAGE_BASE = {
  expected_provider_count: 18,
  skipped_provider_count: 0,
  not_configured_provider_count: 13,
  expected_service_count: 7,
  not_configured_service_count: 0,
} as const

export const SERVICE_SOURCES: ReadonlyArray<readonly [string, string]> = [
  ['netflix', 'netflix_title_probe'],
  ['chatgpt', 'openai_status_probe'],
  ['youtube-premium', 'youtube_premium_page_probe'],
  ['amazon-prime-video', 'prime_video_page_probe'],
  ['disney-plus', 'disney_default_probe'],
  ['tiktok', 'tiktok_home_probe'],
  ['reddit', 'reddit_home_probe'],
]

type SummaryIdentity = {
  ipAddress: string
  ipVersion: IPQualityIPVersion
  regionCode: string
  regionName: string
  asn: string
  organization: string
}

function summaryFor(
  reportId: string,
  observedAt: string,
  status: string,
  riskLevel: string,
  reportCoverage: IPQualityCoverage,
  providerCount: number,
  identity: SummaryIdentity,
  stale = false,
): IPQualitySummary {
  return {
    report_id: reportId,
    vps_id: IPQ_VPS_ID,
    observed_at: observedAt,
    ip_address: identity.ipAddress,
    ip_version: identity.ipVersion,
    status,
    risk_level: riskLevel,
    use_region_code: identity.regionCode,
    use_region_name: identity.regionName,
    asn: identity.asn,
    organization: identity.organization,
    stale,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: providerCount,
    unlockable_count: 7,
    coverage: reportCoverage,
  }
}

function latestFor(
  summary: IPQualitySummary,
  receivedAt: string,
  diagnostics: Record<string, unknown>,
  raw: unknown,
  agentVersion = 'v1.1.0',
): IPQualityReport {
  return {
    report_id: summary.report_id!,
    monitoring_instance_id: 'mi_001',
    observed_at: summary.observed_at,
    received_at: receivedAt,
    agent_version: agentVersion,
    fingerprint: `fixture-fp-${summary.report_id}`,
    sync_batch_id: `fixture-sync-${summary.report_id}`,
    ip_address: summary.ip_address,
    ip_version: summary.ip_version,
    status: summary.status,
    ...(summary.asn ? { asn: summary.asn } : {}),
    ...(summary.organization ? { organization: summary.organization } : {}),
    ...(summary.use_region_code ? { use_region_code: summary.use_region_code } : {}),
    ...(summary.use_region_name ? { use_region_name: summary.use_region_name } : {}),
    is_backfilled: false,
    created_at: receivedAt,
    ...(summary.coverage ? { coverage: summary.coverage } : {}),
    diagnostics_json: diagnostics,
    raw_json: raw,
  }
}

// 原始报告与 agent rawEnvelopeV2 同形：只有脱敏状态与安全证据，没有 HTML 采样。
function rawEnvelope(ipAddress: string, providers: IPQualityProviderResult[], services: IPQualityServiceUnlock[], diagnostics: Record<string, unknown>) {
  return {
    providers: Object.fromEntries(providers.map((row) => [row.provider, {
      status: row.status,
      ...(row.error_code ? { error_code: row.error_code } : {}),
      ...(row.error_summary ? { error_summary: row.error_summary } : {}),
      ...(row.status === 'success' ? { raw: { ip: ipAddress, ...(row.extra_json as Record<string, unknown>) } } : {}),
    }])),
    services: Object.fromEntries(services.map((row) => [row.service, {
      status: row.probe_status,
      ...(row.error_code ? { error_code: row.error_code } : {}),
      ...(row.error_summary ? { error_summary: row.error_summary } : {}),
      raw: row.extra_json,
    }])),
    diagnostics,
  }
}

// ---------------------------------------------------------------------------
// 既有单族修订夹具（collection_revision ≤ 3，旧 agent）：保留 revision/legacy 行为回归。
// ---------------------------------------------------------------------------

const LEGACY_IDENTITY: SummaryIdentity = {
  ipAddress: '95.169.166.229',
  ipVersion: 4,
  regionCode: 'DE',
  regionName: 'Germany',
  asn: 'AS63150',
  organization: 'BAGE CLOUD LLC',
}

// 与 revision3 agent 实际输出一致的五个启用来源：ip2location.io 本轮失败，其余四个成功。
// 失败行故意残留 VPN/高风险字段，页面不得把它们算进风险结论。
function revision2Providers(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', latency_ms: 412,
      company_type: 'hosting', region_code: 'DE', region_name: 'Germany',
      extra_json: { company: { name: 'BAGE CLOUD LLC', type: 'hosting' }, asn: { asn: 63150, org: 'BAGE CLOUD LLC', type: 'hosting' }, capability: 'geo_only' },
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', latency_ms: 268,
      risk_level: 'low', risk_score: '0', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true,
      extra_json: { isp: { asn: 'AS63150', org: 'BAGE CLOUD LLC' }, capability: 'risk' },
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', latency_ms: 531,
      usage_type: 'Hosting', risk_level: 'low', risk_score: '33', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true, is_robot: false,
      extra_json: { api_version: '24-June-2026', compromised: false, anonymous: false, confidence: 0, capability: 'risk' },
    },
    {
      provider: 'ipwho.is', status: 'success', source_type: 'default', latency_ms: 377,
      region_code: 'DE', region_name: 'Germany',
      extra_json: { connection: { asn: 63150, org: 'BAGE CLOUD LLC', isp: 'BAGE CLOUD LLC' }, capability: 'geo_only' },
    },
    {
      provider: 'ip2location.io', status: 'failure', source_type: 'default', latency_ms: 5000,
      error_code: 'http_status', error_summary: 'http status 429',
      is_vpn: true, risk_level: 'high', risk_score: '91',
    },
    ...NOT_CONFIGURED_PROVIDERS,
  ]
}

function revision2Evidence(matchedRule: string, region: string, endpoints: Array<[string, number]>) {
  return {
    probe_revision: 2,
    evidence_kind: 'anonymous_service_probe',
    ...(matchedRule ? { matched_rule: matchedRule } : {}),
    http_status: endpoints[endpoints.length - 1]?.[1],
    ...(region ? { region } : {}),
    ip_version: 4,
    endpoints: endpoints.map(([endpoint, httpStatus]) => ({ endpoint, http_status: httpStatus })),
  }
}

// 免登录服务探测 revision 2：五项可用、ChatGPT 仅 Web 已验证、Disney+ 遇到人机验证。
// Disney+ 失败行故意残留 blocked/US/范围，页面只能显示未知。
function revision2Services(): IPQualityServiceUnlock[] {
  return [
    { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1840, region: 'DE', unlock_type: 'catalog_sample', extra_json: revision2Evidence('netflix_title_catalog', 'DE', [['title_81280792', 200], ['title_70143836', 200]]) },
    { service: 'chatgpt', source: 'openai_status_probe', status: 'partial', probe_status: 'success', latency_ms: 960, unlock_type: 'web_verified_app_unknown', extra_json: revision2Evidence('openai_web_positive_app_unknown', '', [['web_cookie_requirements', 200], ['app_home', 403]]) },
    { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1210, region: 'DE', unlock_type: 'premium_offer', extra_json: revision2Evidence('youtube_premium_region_offer', 'DE', [['premium', 200]]) },
    { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1490, region: 'DE', unlock_type: 'territory_offer', extra_json: revision2Evidence('prime_current_territory', 'DE', [['home', 200]]) },
    { service: 'disney-plus', source: 'disney_default_probe', status: 'blocked', probe_status: 'failure', latency_ms: 2210, region: 'US', unlock_type: 'supported_location', error_code: 'challenge_detected', error_summary: 'service challenge detected', extra_json: revision2Evidence('', '', [['devices', 403]]) },
    { service: 'tiktok', source: 'tiktok_home_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1720, region: 'DE', unlock_type: 'web_region', extra_json: revision2Evidence('tiktok_script_region', 'DE', [['home', 200]]) },
    { service: 'reddit', source: 'reddit_home_probe', status: 'unlocked', probe_status: 'success', latency_ms: 880, region: 'DE', unlock_type: 'anonymous_web', extra_json: revision2Evidence('reddit_shreddit_country', 'DE', [['anonymous_chat', 200]]) },
  ]
}

// 来源不明修订的旧报告：多数数据库只给地区或什么都不给，原始报告里带大段 HTML 采样。
function legacyProviders(): IPQualityProviderResult[] {
  const empty = ['abuseipdb', 'db-ip', 'ipapi.co', 'ip-api.com', 'ipdata', 'ipgeolocation', 'ipregistry', 'iplocation', 'ipwho.is', 'ipquery.io', 'ip2proxy', 'ipwhois']
  return [
    { provider: 'ip2location.io', status: 'success', source_type: 'default', region_code: 'DE', is_proxy: false },
    { provider: 'ipapi.is', status: 'success', source_type: 'default', company_type: 'BAGE CLOUD LLC', region_name: 'Germany', is_proxy: true, is_vpn: true, is_server: true, is_tor: false, is_abuser: false },
    { provider: 'proxycheck.io', status: 'success', source_type: 'default', usage_type: 'hosting', risk_level: 'low', risk_score: '12', is_proxy: false, is_vpn: false, is_server: true },
    { provider: 'scamalytics', status: 'failure', source_type: 'default', error_code: 'http_status', error_summary: 'http status 429' },
    { provider: 'maxmind', status: 'not_configured', source_type: 'optional', error_code: 'not_configured' },
    ...empty.map((provider) => ({ provider, status: 'success', source_type: 'default' }) satisfies IPQualityProviderResult),
  ]
}

const HTML_SAMPLE = `<!DOCTYPE html><html lang="en"><head>${'<meta name="viewport" content="width=device-width">'.repeat(200)}</head></html>`

// revision 1 历史报告：服务探测停用（七项 skipped），数据库矩阵完整且有供应商高风险，
// 摘要却仍是旧规则留下的 low，页面必须以所选报告自己的来源事实为准。
function revision1Providers(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', company_type: 'hosting', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: true, is_tor: false, is_server: true, is_abuser: false, is_robot: false,
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', usage_type: 'Business', risk_level: 'high', risk_score: '82', region_code: 'DE',
      is_proxy: true, is_vpn: true, is_tor: false, is_server: true,
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', risk_level: 'low', risk_score: '0', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true,
    },
    { provider: 'ipwho.is', status: 'success', source_type: 'default', region_code: 'DE', region_name: 'Germany' },
    { provider: 'ip2location.io', status: 'success', source_type: 'default', region_code: 'DE', region_name: 'Germany' },
    ...NOT_CONFIGURED_PROVIDERS,
  ]
}

const UNSUPPORTED_DEFAULT_PROBE = 'safe default probe is not available without verified business evidence'

const LEGACY_SUMMARY = summaryFor('ipq_hist_legacy', '2026-09-30T06:56:00Z', 'success', 'low', {
  expected_provider_count: 17,
  successful_provider_count: 15,
  failed_provider_count: 1,
  skipped_provider_count: 0,
  not_configured_provider_count: 1,
  expected_service_count: 7,
  successful_service_count: 5,
  failed_service_count: 1,
  skipped_service_count: 1,
  not_configured_service_count: 0,
}, 17, LEGACY_IDENTITY)

const REVISION1_SUMMARY = summaryFor('ipq_hist_rev1', '2026-10-01T12:00:00Z', 'success', 'low', {
  ...RAW_COVERAGE_BASE,
  successful_provider_count: 5,
  failed_provider_count: 0,
  successful_service_count: 0,
  failed_service_count: 0,
  skipped_service_count: 7,
}, 18, LEGACY_IDENTITY)

const REVISION2_SUMMARY = summaryFor('ipq_0b581f3fc74fc31f', '2026-10-03T12:00:00Z', 'partial', 'low', {
  ...RAW_COVERAGE_BASE,
  successful_provider_count: 4,
  failed_provider_count: 1,
  successful_service_count: 6,
  failed_service_count: 1,
  skipped_service_count: 0,
}, 18, LEGACY_IDENTITY)

// 旧 agent（单族）对应的族状态：IPv4 有 legacy 尝试，IPv6 没有任何记录 → unknown 而不是 unavailable。
function legacyAgentFamilies(): IPQualityFamilyStatus[] {
  return [
    {
      ip_version: 4,
      availability: 'unknown',
      latest_attempt: { observed_at: REVISION2_SUMMARY.observed_at, stale: false, report_status: 'partial', collection_scope: 'legacy_single_family' },
      latest_report_summary: REVISION2_SUMMARY,
    },
    { ip_version: 6, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
  ]
}

function legacyAgentHistory(): IPQualitySummary[] {
  return [
    REVISION2_SUMMARY,
    REVISION1_SUMMARY,
    LEGACY_SUMMARY,
    ...Array.from({ length: 5 }, (_, index) => ({
      ...REVISION1_SUMMARY,
      report_id: `ipq_hist_${index + 3}`,
      observed_at: new Date(Date.parse(LEGACY_SUMMARY.observed_at) - (index + 1) * 86_400_000).toISOString(),
      risk_level: index % 2 === 0 ? 'low' : 'medium',
    })),
  ]
}

export function legacyReportFixture(): VPSIPQualityReport {
  return {
    selected_ip_version: 4,
    default_ip_version: 4,
    families: legacyAgentFamilies(),
    summary: LEGACY_SUMMARY,
    latest_report: latestFor(LEGACY_SUMMARY, '2026-09-30T06:56:05Z', { source_version: 'v2' }, {
      services: { reddit: { raw: { body_sample: HTML_SAMPLE, http_status: 200 } }, tiktok: { raw: { body_sample: HTML_SAMPLE } } },
    }),
    provider_results: legacyProviders(),
    service_unlocks: [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', region: 'DE', unlock_type: 'originals' },
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'DE' },
      { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unlocked', probe_status: 'success', region: 'DE' },
      { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'blocked', probe_status: 'success' },
      { service: 'disney-plus', source: 'disney_default_probe', status: 'blocked', probe_status: 'success' },
      { service: 'tiktok', source: 'tiktok_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
      { service: 'reddit', source: 'reddit_home_probe', status: 'unknown', probe_status: 'failure', error_code: 'http_status', error_summary: 'http status 403' },
    ],
    history: legacyAgentHistory(),
  }
}

export function revision1ReportFixture(): VPSIPQualityReport {
  const providers = revision1Providers()
  const services = SERVICE_SOURCES.map(([service, source]) => ({
    service, source, status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: UNSUPPORTED_DEFAULT_PROBE,
  }) satisfies IPQualityServiceUnlock)
  const diagnostics = { source_version: 'v2', service_probe_revision: 1 }
  return {
    selected_ip_version: 4,
    default_ip_version: 4,
    families: legacyAgentFamilies(),
    summary: REVISION1_SUMMARY,
    latest_report: latestFor(REVISION1_SUMMARY, '2026-10-01T12:00:05Z', diagnostics, rawEnvelope(LEGACY_IDENTITY.ipAddress, providers, services, diagnostics)),
    provider_results: providers,
    service_unlocks: services,
    history: legacyAgentHistory(),
  }
}

/** 旧 agent 的 revision2 单族最新报告（IPv4），IPv6 族无记录。 */
export function revision2ReportFixture(): VPSIPQualityReport {
  const providers = revision2Providers()
  const services = revision2Services()
  const diagnostics = {
    source_version: 'v2',
    collection_revision: 3,
    service_probe_revision: 2,
    elapsed_ms: 6840,
    ip_address_family: 4,
    discovery_ip_candidates: { 'ipapi.is': '95.169.166.229', 'ipquery.io': '2a0e:97c0:3e3:4bc::1' },
    ip_candidates: { 'ipapi.is': '95.169.166.229', 'ipquery.io': '95.169.166.229', 'proxycheck.io': '95.169.166.229', 'ipwho.is': '95.169.166.229' },
  }
  return {
    selected_ip_version: 4,
    default_ip_version: 4,
    families: legacyAgentFamilies(),
    summary: REVISION2_SUMMARY,
    latest_report: latestFor(REVISION2_SUMMARY, '2026-10-03T12:00:05Z', diagnostics, rawEnvelope(LEGACY_IDENTITY.ipAddress, providers, services, diagnostics)),
    provider_results: providers,
    service_unlocks: services,
    history: legacyAgentHistory(),
  }
}

/** 旧 agent VPS：默认 GET、显式 IPv4 与两个历史详情；IPv6 显式查询为空事实 + unknown 族。 */
export function legacyAgentProfile(collect: IPQualityCollectStatus = COLLECT_AVAILABLE, extra: ApiFixtureProfile = {}): ApiFixtureProfile {
  const latest = revision2ReportFixture()
  return authenticatedProfile({
    [apiRouteKey('GET', ipqReportPath())]: ok(latest),
    [apiRouteKey('GET', ipqReportPath(4))]: ok(latest),
    [apiRouteKey('GET', ipqReportPath(6))]: ok(emptyFamilyView(6, 4, legacyAgentFamilies())),
    [apiRouteKey('GET', ipqDetailPath('ipq_hist_rev1'))]: ok(revision1ReportFixture()),
    [apiRouteKey('GET', ipqDetailPath('ipq_hist_legacy'))]: ok(legacyReportFixture()),
    [apiRouteKey('GET', IPQ_COLLECT_PATH)]: ok(collect),
    ...extra,
  })
}

// ---------------------------------------------------------------------------
// revision 4 双栈夹具
// ---------------------------------------------------------------------------

export const IPV4_IDENTITY: SummaryIdentity = {
  ipAddress: '198.51.100.10',
  ipVersion: 4,
  regionCode: 'US',
  regionName: 'United States',
  asn: 'AS64500',
  organization: 'Fixture Cable West',
}

// 最长形式的 IPv6 文档地址，用于检查页头/历史不被撑开。
export const IPV6_IDENTITY: SummaryIdentity = {
  ipAddress: '2001:db8:ffff:ffff:ffff:ffff:ffff:1234',
  ipVersion: 6,
  regionCode: 'JP',
  regionName: 'Japan',
  asn: 'AS64501',
  organization: 'Fixture Fiber East',
}

const IPV4_ROUND2_IDENTITY: SummaryIdentity = { ...IPV4_IDENTITY, ipAddress: '198.51.100.20' }
const IPV6_ROUND2_IDENTITY: SummaryIdentity = { ...IPV6_IDENTITY, ipAddress: '2001:db8:ffff:ffff:ffff:ffff:ffff:5678' }

export const ROUND1 = { collectionId: 'ipqcol_e2e_round1', observedAt: '2026-10-03T12:00:00Z' } as const
export const ROUND2 = { collectionId: 'ipqcol_e2e_round2', observedAt: '2026-10-04T12:00:00Z' } as const
const IPV4_PREVIOUS_AT = '2026-10-02T12:00:00Z'
const IPV6_PREVIOUS_AT = '2026-09-28T08:30:00Z'
const STALE_ATTEMPT_AT = '2026-08-20T02:00:00Z'

function revision4Diagnostics(ipVersion: IPQualityIPVersion, collectionId: string, availability: IPQualityFamilyAvailability, collectRequestId?: string) {
  return {
    source_version: 'v2',
    collection_revision: 4,
    service_probe_revision: 3,
    collection_id: collectionId,
    expected_ip_versions: [4, 6],
    ip_address_family: ipVersion,
    family_availability: availability,
    ...(collectRequestId ? { collect_request_id: collectRequestId } : {}),
  }
}

type EndpointEvidence = { endpoint: string, http_status?: number, error_code?: string, outcome?: 'available' | 'restricted' | 'unknown' }

function revision3Evidence(ipVersion: IPQualityIPVersion, matchedRule: string, endpoints: EndpointEvidence[], region?: { observed: string, evidence: 'youtube_content_region' | 'youtube_context_gl' }) {
  const last = endpoints[endpoints.length - 1]
  return {
    probe_revision: 3,
    evidence_kind: 'anonymous_service_probe',
    ...(matchedRule ? { matched_rule: matchedRule } : {}),
    ...(last?.http_status !== undefined ? { http_status: last.http_status } : {}),
    ...(region ? { observed_region: region.observed, region_evidence: region.evidence } : {}),
    ip_version: ipVersion,
    endpoints,
  }
}

function service(row: Omit<IPQualityServiceUnlock, 'source'>): IPQualityServiceUnlock {
  return { source: SERVICE_SOURCES.find(([name]) => name === row.service)?.[1] ?? '', ...row }
}

// IPv4：五字段完整的低风险，6 项可用、TikTok 业务受限。
function ipv4Providers(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', latency_ms: 301,
      company_type: 'isp', region_code: 'US', region_name: 'United States',
      is_proxy: false, is_vpn: false, is_tor: false, is_abuser: false, is_server: false,
      extra_json: { company: { name: 'Fixture Cable West', type: 'isp' }, asn: { asn: 64500, org: 'Fixture Cable West', type: 'isp' }, capability: 'risk' },
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', latency_ms: 233,
      risk_level: 'low', risk_score: '0', region_code: 'US', region_name: 'United States',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: false,
      extra_json: { isp: { asn: 'AS64500', org: 'Fixture Cable West' }, capability: 'risk' },
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', latency_ms: 498,
      usage_type: 'Residential', risk_level: 'low', risk_score: '4', region_code: 'US', region_name: 'United States',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: false, is_robot: false,
      extra_json: { capability: 'risk' },
    },
    {
      provider: 'ipwho.is', status: 'success', source_type: 'default', latency_ms: 352,
      region_code: 'US', region_name: 'United States',
      extra_json: { connection: { asn: 64500, org: 'Fixture Cable West' }, capability: 'geo_only' },
    },
    {
      provider: 'ip2location.io', status: 'success', source_type: 'default', latency_ms: 610,
      region_code: 'US', region_name: 'United States', is_proxy: false,
      extra_json: { capability: 'geo_only' },
    },
  ]
}

function ipv4Services(): IPQualityServiceUnlock[] {
  return [
    service({ service: 'netflix', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'catalog_sample', extra_json: revision3Evidence(4, 'netflix_title_catalog', [{ endpoint: 'title_81280792', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'chatgpt', status: 'unlocked', probe_status: 'success', unlock_type: 'web_and_app', extra_json: revision3Evidence(4, 'openai_web_and_app', [{ endpoint: 'web_cookie_requirements', http_status: 200, outcome: 'available' }, { endpoint: 'app_home', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'youtube-premium', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'premium_offer', extra_json: revision3Evidence(4, 'youtube_premium_region_offer', [{ endpoint: 'premium', http_status: 200, outcome: 'available' }], { observed: 'US', evidence: 'youtube_content_region' }) }),
    service({ service: 'amazon-prime-video', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'territory_offer', extra_json: revision3Evidence(4, 'prime_current_territory', [{ endpoint: 'home', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'disney-plus', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'supported_location', extra_json: revision3Evidence(4, 'disney_supported_location', [{ endpoint: 'devices', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'tiktok', status: 'blocked', probe_status: 'success', unlock_type: 'web_region', extra_json: revision3Evidence(4, 'tiktok_region_restricted', [{ endpoint: 'home', http_status: 200, outcome: 'restricted' }]) }),
    service({ service: 'reddit', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'anonymous_web', extra_json: revision3Evidence(4, 'reddit_shreddit_country', [{ endpoint: 'anonymous_chat', http_status: 200, outcome: 'available' }]) }),
  ]
}

// IPv6：proxycheck 判中风险但 abuse 字段缺失（4/5）；2 可用、1 部分、4 未知。
// YouTube 只有可信页面地区 CN 而无 offer → 未知；Prime/Reddit 普通 403 → 未知而非受限；TikTok DNS 失败。
function ipv6Providers(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', latency_ms: 344,
      company_type: 'isp', region_code: 'JP', region_name: 'Japan',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: false,
      extra_json: { company: { name: 'Fixture Fiber East', type: 'isp' }, asn: { asn: 64501, org: 'Fixture Fiber East', type: 'isp' }, capability: 'risk' },
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', latency_ms: 251,
      risk_level: 'low', risk_score: '0', region_code: 'JP', region_name: 'Japan',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: false,
      extra_json: { isp: { asn: 'AS64501', org: 'Fixture Fiber East' }, capability: 'risk' },
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', latency_ms: 512,
      usage_type: 'Residential', risk_level: 'medium', risk_score: '48', region_code: 'JP', region_name: 'Japan',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: false, is_robot: false,
      extra_json: { capability: 'risk' },
    },
    {
      provider: 'ipwho.is', status: 'success', source_type: 'default', latency_ms: 366,
      region_code: 'JP', region_name: 'Japan',
      extra_json: { connection: { asn: 64501, org: 'Fixture Fiber East' }, capability: 'geo_only' },
    },
    {
      provider: 'ip2location.io', status: 'failure', source_type: 'default', latency_ms: 5000,
      error_code: 'timeout', error_summary: 'request timed out',
    },
  ]
}

function ipv6Services(): IPQualityServiceUnlock[] {
  return [
    service({ service: 'netflix', status: 'unlocked', probe_status: 'success', region: 'JP', unlock_type: 'catalog_sample', extra_json: revision3Evidence(6, 'netflix_title_catalog', [{ endpoint: 'title_81280792', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'chatgpt', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown', extra_json: revision3Evidence(6, 'openai_web_positive_app_unknown', [{ endpoint: 'web_cookie_requirements', http_status: 200, outcome: 'available' }, { endpoint: 'app_home', error_code: 'connection_refused', outcome: 'unknown' }]) }),
    // 无 offer 的有效页面：生产分类 invalid_response（行与 endpoint 同码）；失败行故意残留 normalized region=US，页面只能用 observed_region=CN 证据。
    service({ service: 'youtube-premium', status: 'unknown', probe_status: 'failure', region: 'US', error_code: 'invalid_response', error_summary: 'invalid service response', extra_json: revision3Evidence(6, '', [{ endpoint: 'premium', http_status: 200, error_code: 'invalid_response', outcome: 'unknown' }], { observed: 'CN', evidence: 'youtube_content_region' }) }),
    service({ service: 'amazon-prime-video', status: 'unknown', probe_status: 'failure', error_code: 'http_status', error_summary: 'service returned an unsuccessful HTTP status', extra_json: revision3Evidence(6, '', [{ endpoint: 'home', http_status: 403, error_code: 'http_status', outcome: 'unknown' }]) }),
    service({ service: 'disney-plus', status: 'unlocked', probe_status: 'success', region: 'JP', unlock_type: 'supported_location', extra_json: revision3Evidence(6, 'disney_supported_location', [{ endpoint: 'devices', http_status: 200, outcome: 'available' }]) }),
    service({ service: 'tiktok', status: 'unknown', probe_status: 'failure', error_code: 'dns_failed', error_summary: 'service DNS lookup failed', extra_json: revision3Evidence(6, '', [{ endpoint: 'home', error_code: 'dns_failed', outcome: 'unknown' }]) }),
    service({ service: 'reddit', status: 'unknown', probe_status: 'failure', error_code: 'http_status', error_summary: 'service returned an unsuccessful HTTP status', extra_json: revision3Evidence(6, '', [{ endpoint: 'anonymous_chat', http_status: 403, error_code: 'http_status', outcome: 'unknown' }]) }),
  ]
}

type FamilyFacts = {
  summary: IPQualitySummary
  latest: IPQualityReport
  providers: IPQualityProviderResult[]
  services: IPQualityServiceUnlock[]
  product: IPQualityProductView | null
}

// revision1 product：五个真实默认来源（无 18 来源占位分母）、12 邮局、三个黑名单来源与 RDAP/RIS 资料。
const MAIL_DOMAINS: ReadonlyArray<readonly [string, string]> = [
  ['gmail', 'gmail.com'], ['outlook', 'outlook.com'], ['yahoo', 'yahoo.com'], ['apple', 'me.com'], ['mailru', 'mail.ru'], ['aol', 'aol.com'],
  ['gmx', 'gmx.com'], ['mailcom', 'mail.com'], ['163', '163.com'], ['sohu', 'sohu.com'], ['sina', 'sina.com'], ['qq', 'qq.com'],
]

function providerDetails(row: IPQualityProviderResult, ipAddress: string, observedAt: string, city: string): IPQualityProviderResult {
  if (row.status !== 'success') return row
  const details: IPQualityProviderDetails = {
    queried_ip: ipAddress, observed_at: observedAt, method: 'agent_public', api_tier: 'anonymous',
    country_code: row.region_code ?? '', region: '', city, postal_code: '', timezone: '',
    latitude: null, longitude: null, registered_country_code: '', asn: '', as_name: '', as_domain: '', as_country_code: '', route: '',
    company_name: '', company_domain: '', is_mobile: null, is_anycast: null, is_bogon: null,
    scores: row.risk_score ? [{ id: 'risk_score', label: '风险分', raw_value: row.risk_score, unit: 'score', scope: 'ip', direction: 'higher_risk', source_label: '', value: Number(row.risk_score), min: 0, max: 100 }] : [],
    field_states: {},
  }
  // 新报告不再携带旧综合评级；来源原始分数在 typed scores 中。
  const rest: IPQualityProviderResult = { ...row }
  delete rest.risk_level
  return { ...rest, details }
}

function productFor(ipVersion: IPQualityIPVersion, reportId: string, identity: SummaryIdentity, observedAt: string, providers: IPQualityProviderResult[], state: IPQualityProductState = 'complete'): IPQualityProductView {
  const summary: IPQualityProductSummary = ipVersion === 4
    ? {
        risk_flags: [], high_risk_sources: [], restricted_services: ['tiktok'], blacklist_listed_count: 0, blacklist_checked_count: 3, service_available_count: 6, service_partial_count: 0, service_restricted_count: 1, service_unknown_count: 0, geo_status: 'consistent',
        // 无 Key 默认路径：IPinfo 滥用联系国家与使用国比较（不是官方注册国）。
        ip_type: { status: 'native', basis: 'abuse_contact_country', provider: 'ipinfo', method: 'agent_public', country_code: 'US', comparison_country_code: 'US', upstream_commit: '2384a67c756eb35231f5982b34731e522be3653e', fork_revision: 1 },
      }
    : { risk_flags: [], high_risk_sources: [], restricted_services: [], blacklist_listed_count: 0, blacklist_checked_count: 1, service_available_count: 2, service_partial_count: 1, service_restricted_count: 0, service_unknown_count: 4, geo_status: 'consistent' }
  const completedAt = new Date(Date.parse(observedAt) + 30_000).toISOString().replace('.000Z', 'Z')
  const dnsbl = (source: string, category: 'spam' | 'abuse') => ({
    source, category, status: ipVersion === 4 ? 'not_listed' as const : 'unsupported' as const, query_method: 'dnsbl' as const,
    error_code: '', reference_url: '', matched_range: '', codes: [], observed_at: completedAt, dataset_at: null,
  })
  return {
    revision: 1,
    report_id: reportId,
    ip_address: identity.ipAddress,
    ip_version: ipVersion,
    state,
    agent_observed_at: observedAt,
    completed_at: state === 'complete' ? completedAt : null,
    ptr_names: [ipVersion === 4 ? 'edge-10.fixture.example' : 'edge-v6.fixture.example'],
    providers,
    mail: MAIL_DOMAINS.map(([provider, domain], index) => ({
      provider, domain,
      status: ipVersion === 4 ? (index === 11 ? 'timeout' as const : 'reachable' as const) : (index < 3 ? 'reachable' as const : 'no_address' as const),
      mx_host: `mx.${domain}`, target_ip: ipVersion === 4 ? `192.0.2.${index + 1}` : `2001:db8::${index + 1}`,
      error_code: '', mx_preference: 10, greeting_code: index === 11 && ipVersion === 4 ? null : 220, latency_ms: 40 + index, observed_at: observedAt,
    })),
    blacklists: [
      dnsbl('spamcop', 'spam'),
      dnsbl('dronebl', 'abuse'),
      { source: 'spamhaus-drop', category: 'network', status: 'not_listed', query_method: 'dataset', error_code: '', reference_url: 'https://www.spamhaus.org/drop/', matched_range: '', codes: [], observed_at: completedAt, dataset_at: completedAt },
    ],
    registration: {
      status: 'success', rir: 'arin', network_handle: `NET-FIXTURE-${ipVersion}`, start_address: '', end_address: '', network_name: `FIXTURE-NET-${ipVersion}`,
      network_country_code: identity.regionCode, registrant_country_code: identity.regionCode, registrant_name: identity.organization, source_url: '',
      registered_at: '2020-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
    asn: {
      status: 'success', number: identity.asn, name: identity.organization, domain: 'fixture.example', type: 'isp', country_code: identity.regionCode, rir: 'arin',
      source_url: '', route: '', registered_at: '2020-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', observed_at: completedAt,
      ipv4_prefixes: '12', ipv4_addresses: '3072', ipv6_prefixes: '2', ipv6_48s: '131072', origins: [identity.asn],
    },
    summary,
  }
}

function familyFacts(
  ipVersion: IPQualityIPVersion,
  reportId: string,
  observedAt: string,
  collectionId: string,
  identity: SummaryIdentity,
  options: { collectRequestId?: string, stale?: boolean } = {},
): FamilyFacts {
  const city = ipVersion === 4 ? 'Los Angeles' : 'Tokyo'
  const providers = (ipVersion === 4 ? ipv4Providers() : ipv6Providers()).map((row) => providerDetails(row, identity.ipAddress, observedAt, city))
  const services = ipVersion === 4 ? ipv4Services() : ipv6Services()
  const coverageBase = { expected_provider_count: 5, skipped_provider_count: 0, not_configured_provider_count: 0, expected_service_count: 7, not_configured_service_count: 0 }
  const coverage: IPQualityCoverage = ipVersion === 4
    ? { ...coverageBase, successful_provider_count: 5, failed_provider_count: 0, successful_service_count: 7, failed_service_count: 0, skipped_service_count: 0 }
    : { ...coverageBase, successful_provider_count: 4, failed_provider_count: 1, successful_service_count: 3, failed_service_count: 4, skipped_service_count: 0 }
  const status = ipVersion === 4 ? 'success' : 'partial'
  const product = productFor(ipVersion, reportId, identity, observedAt, providers)
  const summary: IPQualitySummary = {
    ...summaryFor(reportId, observedAt, status, '', coverage, 5, identity, options.stale ?? false),
    product_revision: 1,
    intelligence_state: product.state,
    product_summary: product.summary,
  }
  const diagnostics = { ...revision4Diagnostics(ipVersion, collectionId, 'available', options.collectRequestId), service_probe_revision: 4 }
  const receivedAt = new Date(Date.parse(observedAt) + 5_000).toISOString().replace('.000Z', 'Z')
  return {
    summary,
    latest: latestFor(summary, receivedAt, diagnostics, rawEnvelope(identity.ipAddress, providers, services, diagnostics), 'fixture-agent-rev4'),
    providers,
    services,
    product,
  }
}

/** 同一报告的 Center 补充资料仍在进行：Agent 事实齐全，ASN/注册/黑名单尚未到达。 */
export function enrichingView(view: VPSIPQualityReport): VPSIPQualityReport {
  const product = view.product!
  return {
    ...view,
    product: {
      ...product,
      state: 'running',
      completed_at: null,
      blacklists: [],
      registration: { ...product.registration, status: 'unknown' },
      asn: { ...product.asn, status: 'unknown' },
      summary: { ...product.summary, blacklist_checked_count: 0 },
    },
  }
}

function viewOf(facts: FamilyFacts, defaultIPVersion: IPQualityIPVersion, families: IPQualityFamilyStatus[], history: IPQualitySummary[]): VPSIPQualityReport {
  return {
    selected_ip_version: facts.summary.ip_version as IPQualityIPVersion,
    default_ip_version: defaultIPVersion,
    families,
    summary: facts.summary,
    latest_report: facts.latest,
    provider_results: facts.providers,
    service_unlocks: facts.services,
    history,
    product: facts.product,
  }
}

/** 所选族没有有效报告：空 facts + 族状态，不 404、不回退另一族。 */
export function emptyFamilyView(selected: IPQualityIPVersion, defaultIPVersion: IPQualityIPVersion, families: IPQualityFamilyStatus[]): VPSIPQualityReport {
  return {
    selected_ip_version: selected,
    default_ip_version: defaultIPVersion,
    families,
    summary: null,
    latest_report: null,
    provider_results: [],
    service_unlocks: [],
    history: [],
  }
}

function attempt(observedAt: string, reportStatus: string, options: { stale?: boolean, errorCode?: string, collectionId?: string, scope?: IPQualityCollectionScope } = {}): IPQualityFamilyAttempt {
  return {
    observed_at: observedAt,
    stale: options.stale ?? false,
    report_status: reportStatus,
    ...(options.errorCode ? { error_code: options.errorCode } : {}),
    collection_scope: options.scope ?? 'dual_stack',
    ...(options.collectionId ? { collection_id: options.collectionId } : {}),
  }
}

function family(ipVersion: IPQualityIPVersion, availability: IPQualityFamilyAvailability, latestAttempt: IPQualityFamilyAttempt | null, latestSummary: IPQualitySummary | null): IPQualityFamilyStatus {
  return { ip_version: ipVersion, availability, latest_attempt: latestAttempt, latest_report_summary: latestSummary }
}

const unavailableAttempt = (observedAt: string, collectionId: string, stale = false) => attempt(observedAt, 'failure', { errorCode: 'address_family_unavailable', collectionId, stale })

export type IPQualityScenarioName =
  | 'dual_stack'
  | 'ipv4_only'
  | 'ipv6_only'
  | 'ipv4_lookup_failure'
  | 'ipv6_unavailable_with_history'
  | 'ipv6_unavailable_stale'

/** 一个场景的全部可达 API 响应：默认 GET、显式族 GET 与各历史详情。 */
export type IPQualityScenario = {
  defaultIPVersion: IPQualityIPVersion
  families: IPQualityFamilyStatus[]
  views: Record<IPQualityIPVersion, VPSIPQualityReport>
  details: Record<string, VPSIPQualityReport>
}

type FamilyTimeline = {
  current: FamilyFacts | null
  history: FamilyFacts[]
}

function buildScenario(defaultIPVersion: IPQualityIPVersion, families: IPQualityFamilyStatus[], timelines: Record<IPQualityIPVersion, FamilyTimeline>): IPQualityScenario {
  const views = {} as Record<IPQualityIPVersion, VPSIPQualityReport>
  const details: Record<string, VPSIPQualityReport> = {}
  for (const ipVersion of [4, 6] as const) {
    const timeline = timelines[ipVersion]
    const history = timeline.history.map((facts) => facts.summary)
    views[ipVersion] = timeline.current ? viewOf(timeline.current, defaultIPVersion, families, history) : emptyFamilyView(ipVersion, defaultIPVersion, families)
    if (!timeline.current) views[ipVersion].history = history
    for (const facts of timeline.history) details[facts.summary.report_id!] = viewOf(facts, defaultIPVersion, families, history)
  }
  return { defaultIPVersion, families, views, details }
}

const v4Round1 = () => familyFacts(4, 'ipq_v4_r1', ROUND1.observedAt, ROUND1.collectionId, IPV4_IDENTITY)
const v6Round1 = () => familyFacts(6, 'ipq_v6_r1', ROUND1.observedAt, ROUND1.collectionId, IPV6_IDENTITY)
const v4Previous = () => familyFacts(4, 'ipq_v4_prev', IPV4_PREVIOUS_AT, 'ipqcol_e2e_prev4', IPV4_IDENTITY)
const v6Previous = () => familyFacts(6, 'ipq_v6_prev', IPV6_PREVIOUS_AT, 'ipqcol_e2e_prev6', IPV6_IDENTITY)

export function ipQualityScenario(name: IPQualityScenarioName): IPQualityScenario {
  switch (name) {
    case 'dual_stack': {
      const v4 = v4Round1(); const v6 = v6Round1()
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND1.observedAt, 'success', { collectionId: ROUND1.collectionId }), v4.summary),
        family(6, 'available', attempt(ROUND1.observedAt, 'partial', { collectionId: ROUND1.collectionId }), v6.summary),
      ], { 4: { current: v4, history: [v4, v4Previous()] }, 6: { current: v6, history: [v6, v6Previous()] } })
    }
    case 'ipv4_only': {
      const v4 = v4Round1()
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND1.observedAt, 'success', { collectionId: ROUND1.collectionId }), v4.summary),
        family(6, 'unavailable', unavailableAttempt(ROUND1.observedAt, ROUND1.collectionId), null),
      ], { 4: { current: v4, history: [v4, v4Previous()] }, 6: { current: null, history: [] } })
    }
    case 'ipv6_only': {
      const v6 = v6Round1()
      return buildScenario(6, [
        family(4, 'unavailable', unavailableAttempt(ROUND1.observedAt, ROUND1.collectionId), null),
        family(6, 'available', attempt(ROUND1.observedAt, 'partial', { collectionId: ROUND1.collectionId }), v6.summary),
      ], { 4: { current: null, history: [] }, 6: { current: v6, history: [v6, v6Previous()] } })
    }
    case 'ipv4_lookup_failure': {
      // 本轮（ROUND2）IPv4 本地地址可用但出口探测失败，IPv6 成功；IPv4 仍是默认，展示上次有效结果。
      const v4 = v4Round1()
      const v6 = familyFacts(6, 'ipq_v6_r2', ROUND2.observedAt, ROUND2.collectionId, IPV6_ROUND2_IDENTITY)
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND2.observedAt, 'failure', { errorCode: 'lookup_failed', collectionId: ROUND2.collectionId }), v4.summary),
        family(6, 'available', attempt(ROUND2.observedAt, 'partial', { collectionId: ROUND2.collectionId }), v6.summary),
      ], { 4: { current: v4, history: [v4, v4Previous()] }, 6: { current: v6, history: [v6, v6Round1(), v6Previous()] } })
    }
    case 'ipv6_unavailable_with_history': {
      // 新鲜 unavailable + 旧同族有效报告：不自动首选 IPv6，但显式查看仍展示上次结果。
      const v4 = v4Round1(); const v6 = v6Previous()
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND1.observedAt, 'success', { collectionId: ROUND1.collectionId }), v4.summary),
        family(6, 'unavailable', unavailableAttempt(ROUND1.observedAt, ROUND1.collectionId), v6.summary),
      ], { 4: { current: v4, history: [v4, v4Previous()] }, 6: { current: v6, history: [v6] } })
    }
    case 'ipv6_unavailable_stale': {
      // 过期 unavailable：不能宣称当前单栈，只能注明过期。
      const v4 = v4Round1()
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND1.observedAt, 'success', { collectionId: ROUND1.collectionId }), v4.summary),
        family(6, 'unknown', unavailableAttempt(STALE_ATTEMPT_AT, 'ipqcol_e2e_stale', true), null),
      ], { 4: { current: v4, history: [v4, v4Previous()] }, 6: { current: null, history: [] } })
    }
  }
}

// ---------------------------------------------------------------------------
// 立即采集轮次
// ---------------------------------------------------------------------------

export const COLLECT_AVAILABLE: IPQualityCollectStatus = { enabled: true, available: true, monitoring_instance_id: 'mi_001' }

export type CollectOutcome = 'both_success' | 'ipv4_failure_ipv6_success' | 'ipv6_unavailable' | 'legacy_single_family'

export function collectRequest(status: 'pending' | 'dispatched' | 'completed', outcome: CollectOutcome = 'both_success'): IPQualityCollectRequest {
  const now = Date.now()
  const base: IPQualityCollectRequest = {
    request_id: IPQ_COLLECT_REQUEST_ID,
    monitoring_instance_id: 'mi_001',
    status,
    requested_at: new Date(now - 5_000).toISOString(),
    expires_at: new Date(now + 600_000).toISOString(),
    ...(status !== 'pending' ? { dispatched_at: new Date(now - 3_000).toISOString() } : {}),
  }
  if (status !== 'completed') return base
  const completed = { ...base, completed_at: new Date(now).toISOString() }
  switch (outcome) {
    case 'both_success':
      return { ...completed, report_status: 'success', collection_scope: 'dual_stack', collection_id: ROUND2.collectionId, family_results: [
        { ip_version: 4, availability: 'available', report_status: 'success' },
        { ip_version: 6, availability: 'available', report_status: 'success' },
      ] }
    case 'ipv4_failure_ipv6_success':
      return { ...completed, report_status: 'partial', collection_scope: 'dual_stack', collection_id: ROUND2.collectionId, family_results: [
        { ip_version: 4, availability: 'available', report_status: 'failure', error_code: 'lookup_failed' },
        { ip_version: 6, availability: 'available', report_status: 'success' },
      ] }
    case 'ipv6_unavailable':
      return { ...completed, report_status: 'success', collection_scope: 'dual_stack', collection_id: ROUND2.collectionId, family_results: [
        { ip_version: 4, availability: 'available', report_status: 'success' },
        { ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' },
      ] }
    case 'legacy_single_family':
      return { ...completed, report_status: 'success', collection_scope: 'legacy_single_family' }
  }
}

/** 一轮立即采集结束后 center 会返回的场景（从 dual_stack ROUND1 出发）。 */
export function scenarioAfterCollect(outcome: CollectOutcome): IPQualityScenario {
  const v4Old = v4Round1(); const v6Old = v6Round1()
  const v4New = familyFacts(4, 'ipq_v4_r2', ROUND2.observedAt, ROUND2.collectionId, IPV4_ROUND2_IDENTITY, { collectRequestId: IPQ_COLLECT_REQUEST_ID })
  const v6New = familyFacts(6, 'ipq_v6_r2', ROUND2.observedAt, ROUND2.collectionId, IPV6_ROUND2_IDENTITY, { collectRequestId: IPQ_COLLECT_REQUEST_ID })
  switch (outcome) {
    case 'both_success':
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND2.observedAt, 'success', { collectionId: ROUND2.collectionId }), v4New.summary),
        family(6, 'available', attempt(ROUND2.observedAt, 'partial', { collectionId: ROUND2.collectionId }), v6New.summary),
      ], { 4: { current: v4New, history: [v4New, v4Old, v4Previous()] }, 6: { current: v6New, history: [v6New, v6Old, v6Previous()] } })
    case 'ipv4_failure_ipv6_success':
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND2.observedAt, 'failure', { errorCode: 'lookup_failed', collectionId: ROUND2.collectionId }), v4Old.summary),
        family(6, 'available', attempt(ROUND2.observedAt, 'partial', { collectionId: ROUND2.collectionId }), v6New.summary),
      ], { 4: { current: v4Old, history: [v4Old, v4Previous()] }, 6: { current: v6New, history: [v6New, v6Old, v6Previous()] } })
    case 'ipv6_unavailable':
      return buildScenario(4, [
        family(4, 'available', attempt(ROUND2.observedAt, 'success', { collectionId: ROUND2.collectionId }), v4New.summary),
        family(6, 'unavailable', unavailableAttempt(ROUND2.observedAt, ROUND2.collectionId), v6Old.summary),
      ], { 4: { current: v4New, history: [v4New, v4Old, v4Previous()] }, 6: { current: v6Old, history: [v6Old, v6Previous()] } })
    case 'legacy_single_family': {
      // 旧 agent：单份 IPv4 报告只带 collect_request_id，没有 collection_id/族元数据。
      const summary: IPQualitySummary = { ...v4New.summary, report_id: 'ipq_v4_legacy_r2', risk_level: 'low' }
      delete summary.product_revision
      delete summary.intelligence_state
      delete summary.product_summary
      const diagnostics = { source_version: 'v2', collection_revision: 3, service_probe_revision: 3, ip_address_family: 4, collect_request_id: IPQ_COLLECT_REQUEST_ID }
      const legacy: FamilyFacts = { ...v4New, summary, product: null, latest: { ...v4New.latest, report_id: summary.report_id!, diagnostics_json: diagnostics, raw_json: rawEnvelope(summary.ip_address, v4New.providers, v4New.services, diagnostics) } }
      return buildScenario(4, [
        family(4, 'unknown', attempt(ROUND2.observedAt, 'success', { scope: 'legacy_single_family' }), summary),
        family(6, 'available', attempt(ROUND1.observedAt, 'partial', { collectionId: ROUND1.collectionId }), v6Old.summary),
      ], { 4: { current: legacy, history: [legacy, v4Old, v4Previous()] }, 6: { current: v6Old, history: [v6Old, v6Previous()] } })
    }
  }
}

/** 首次进入时完全没有报告：两族都 unknown 且无尝试。 */
export function emptyScenario(): IPQualityScenario {
  return buildScenario(4, [family(4, 'unknown', null, null), family(6, 'unknown', null, null)], {
    4: { current: null, history: [] },
    6: { current: null, history: [] },
  })
}

// ---------------------------------------------------------------------------
// fail-closed profile：只注册精确 method/path/query
// ---------------------------------------------------------------------------

function ok(body: unknown): ApiFixtureResponse {
  return { status: 200, body }
}

/** 场景的报告路由：默认 GET、?ip_version=4|6、每个历史详情（无 query）。 */
export function scenarioReportRoutes(scenario: IPQualityScenario, overrides: Partial<Record<'default' | IPQualityIPVersion, ApiFixtureResponse>> = {}): Record<ApiRouteKey, ApiFixtureResponse> {
  const routes: Record<ApiRouteKey, ApiFixtureResponse> = {
    [apiRouteKey('GET', ipqReportPath())]: overrides.default ?? ok(scenario.views[scenario.defaultIPVersion]),
    [apiRouteKey('GET', ipqReportPath(4))]: overrides[4] ?? ok(scenario.views[4]),
    [apiRouteKey('GET', ipqReportPath(6))]: overrides[6] ?? ok(scenario.views[6]),
  }
  for (const [reportId, detail] of Object.entries(scenario.details)) routes[apiRouteKey('GET', ipqDetailPath(reportId))] = ok(detail)
  return routes
}

export function ipQualityScenarioProfile(
  scenario: IPQualityScenario | IPQualityScenarioName,
  options: { collect?: IPQualityCollectStatus, extra?: ApiFixtureProfile, overrides?: Partial<Record<'default' | IPQualityIPVersion, ApiFixtureResponse>> } = {},
): ApiFixtureProfile {
  const resolved = typeof scenario === 'string' ? ipQualityScenario(scenario) : scenario
  return authenticatedProfile({
    ...scenarioReportRoutes(resolved, options.overrides),
    [apiRouteKey('GET', IPQ_COLLECT_PATH)]: ok(options.collect ?? COLLECT_AVAILABLE),
    ...options.extra,
  })
}

export function collectPostRoute(outcome: CollectOutcome = 'both_success'): ApiFixtureProfile {
  return {
    [apiRouteKey('POST', IPQ_COLLECT_PATH)]: { status: 202, body: { ...COLLECT_AVAILABLE, request: collectRequest('pending', outcome) }, expectNoBody: true as const },
  }
}
