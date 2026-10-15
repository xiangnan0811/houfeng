import type { BadgeTone } from '../atoms'
import type {
  IPQualityProviderResult,
  IPQualityServiceCheck,
  IPQualityServiceUnlock,
  VPSIPQualityReport,
} from '../../lib/types'

export type UnlockStatusKind = 'unlocked' | 'partial' | 'blocked' | 'unknown'

export function serviceLabel(value: string): string {
  const normalized = value.toLowerCase()
  if (normalized === 'chatgpt' || normalized === 'openai') return 'ChatGPT'
  if (normalized === 'netflix') return 'Netflix'
  if (normalized === 'youtube-premium') return 'YouTube Premium'
  if (normalized === 'amazon-prime-video') return 'Amazon Prime Video'
  if (normalized === 'disney-plus') return 'Disney+'
  if (normalized === 'tiktok') return 'TikTok'
  if (normalized === 'reddit') return 'Reddit'
  return value
}

// 旧版报告（无 product）的服务端/来源评级，只作为“旧版评级”标注展示。
export function riskLevelLabel(value?: string): string {
  const risk = (value ?? '').trim().toLowerCase()
  if (risk === 'low' || risk === 'clean' || risk === 'safe') return '低风险'
  if (risk === 'medium' || risk === 'moderate') return '中风险'
  if (risk === 'high') return '高风险'
  if (risk === 'critical') return '严重风险'
  return risk || '未评级'
}

export function riskTone(value?: string): BadgeTone {
  const risk = (value ?? '').trim().toLowerCase()
  if (risk === 'critical') return 'critical'
  if (risk === 'high') return 'alert'
  if (risk === 'medium' || risk === 'moderate') return 'notice'
  return 'neutral'
}

export function providerSucceeded(result: IPQualityProviderResult): boolean {
  return sourceStatusKind(result) === 'success'
}

export function sourceStatusKind(result: IPQualityProviderResult): string {
  return result.status == null ? 'success' : result.status.trim().toLowerCase()
}

function sourceTypeKind(result: IPQualityProviderResult): string {
  return result.source_type == null ? 'default' : result.source_type.trim().toLowerCase()
}

// 历史报告里无 adapter 的可选来源占位（not_configured/skipped）只在诊断出现。
export function visibleProviderResults(results: IPQualityProviderResult[]): IPQualityProviderResult[] {
  return results.filter((result) => {
    const status = sourceStatusKind(result)
    return !(sourceTypeKind(result) === 'optional' && (status === 'not_configured' || status === 'skipped'))
  })
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null
}

function reportDiagnostics(report: VPSIPQualityReport): Record<string, unknown> | null {
  return objectRecord(report.latest_report?.diagnostics_json)
}

// 只接受数值 revision；字符串 "2" 等不认作新规则。
export function serviceProbeRevision(report: VPSIPQualityReport): number | null {
  const revision = reportDiagnostics(report)?.service_probe_revision
  return typeof revision === 'number' && Number.isFinite(revision) ? revision : null
}

// 免登录服务探测的报告级/行级 revision：2、3、4 同属业务证据探测；5 为固定 IPQuality 脚本与其余免登录探测的混合实现。
// 行级优先读各自 extra_json.probe_revision：新报告里 TikTok/Disney+/Reddit 仍为 4，四项脚本服务为 5。
export function isAnonymousServiceProbeRevision(revision: unknown): boolean {
  return revision === 2 || revision === 3 || revision === 4 || revision === 5
}

function ipFamily(value: unknown): 4 | 6 | null {
  return value === 4 || value === 6 ? value : null
}

// 所选报告自身的地址族：selected_ip_version、latest_report 与 summary 描述同一报告，任意两者矛盾时不认任何一族。
export function selectedReportIPVersion(report: VPSIPQualityReport): 4 | 6 | null {
  const candidates = [report.selected_ip_version, report.latest_report?.ip_version, report.summary?.ip_version]
    .filter((value) => value != null)
  const families = candidates.map(ipFamily)
  if (families.length === 0 || families.some((value) => value == null)) return null
  return families.every((value) => value === families[0]) ? families[0]! : null
}

export type IPVersionParam =
  | { kind: 'default' }
  | { kind: 'family', ipVersion: 4 | 6 }
  | { kind: 'invalid' }

// 页面 URL 的 ip_version：缺省走服务端默认；只接受恰好一个 “4” 或 “6”，空值/重复/其他值都是可恢复的参数错误。
export function parseIPVersionParam(values: readonly string[]): IPVersionParam {
  if (values.length === 0) return { kind: 'default' }
  if (values.length === 1 && (values[0] === '4' || values[0] === '6')) return { kind: 'family', ipVersion: values[0] === '4' ? 4 : 6 }
  return { kind: 'invalid' }
}

export type ReportCollectionIdentity = {
  collectionID: string | null
  collectRequestID: string | null
}

function diagnosticsIdentifier(value: unknown): string | null {
  return typeof value === 'string' && value.trim() !== '' && value.length <= 128 ? value : null
}

// 当前展示报告属于哪一轮：双栈看 collection_id，旧 agent 看 collect_request_id；用于区分“轮次结束”和“报告已更新”。
export function reportCollectionIdentity(report: VPSIPQualityReport | null): ReportCollectionIdentity {
  const diagnostics = report ? reportDiagnostics(report) : null
  return {
    collectionID: diagnosticsIdentifier(diagnostics?.collection_id),
    collectRequestID: diagnosticsIdentifier(diagnostics?.collect_request_id),
  }
}

export type FamilyStateNotice = {
  tone: 'warning' | 'muted'
  title: string
  detail: string | null
  // 所选族仍有旧有效报告时，其独立时间；页面写明“上次有效结果”。
  lastValidAt: string | null
}

// 所选地址族最新尝试的说明：失败尝试不冒充出口事实，缺族不冒充失败，过期记录不宣称当前缺族。
export function familyStateNotice(report: VPSIPQualityReport, family: 4 | 6): FamilyStateNotice | null {
  const status = report.families.find((item) => item.ip_version === family)
  const attempt = status?.latest_attempt ?? null
  const summary = report.summary ?? null
  const label = `IPv${family}`
  if (!attempt) {
    return summary ? null : { tone: 'muted', title: `${label} 尚无采集结果`, detail: null, lastValidAt: null }
  }
  const unavailable = status?.availability === 'unavailable'
  const failed = !unavailable && attempt.report_status !== 'success' && attempt.report_status !== 'partial'
  const lastValidAt = summary ? summary.observed_at : null
  if (attempt.stale && (unavailable || failed || !summary)) {
    return {
      tone: 'muted',
      title: `${label} 最近一次采集记录已过期`,
      detail: unavailable ? '当时未发现该地址族的本地地址，不代表当前状态' : failed ? '当时出口探测失败，不代表当前状态' : null,
      lastValidAt,
    }
  }
  if (unavailable) {
    return { tone: 'muted', title: `本轮未检测到 ${label} 可用本地地址`, detail: null, lastValidAt }
  }
  if (failed) {
    return { tone: 'warning', title: `${label} 出口探测失败`, detail: null, lastValidAt }
  }
  return null
}

export type ReportAttributionBlocker = 'ambiguous' | 'ip_conflict'

// 归属不唯一或来源返回不同 IP：事实仍展示，但页头给出复核标记。
export function reportAttributionBlocker(report: VPSIPQualityReport): ReportAttributionBlocker | null {
  if (report.summary?.ambiguous) return 'ambiguous'
  if (reportDiagnostics(report)?.ip_conflict === true) return 'ip_conflict'
  return null
}

function unlockStatusKind(status?: string): UnlockStatusKind {
  const normalized = (status ?? '').trim().toLowerCase()
  if (normalized === 'unlocked') return 'unlocked'
  if (normalized === 'partial') return 'partial'
  if (normalized === 'blocked') return 'blocked'
  return 'unknown'
}

function isKnownSuccessfulService(unlock: IPQualityServiceUnlock): boolean {
  const status = unlockStatusKind(unlock.status)
  if (status !== 'unlocked' && status !== 'partial' && status !== 'blocked') {
    return false
  }
  if (unlock.probe_status == null) {
    return true
  }
  return unlock.probe_status.trim().toLowerCase() === 'success'
}

// 服务状态的唯一入口：只有探测成功且业务状态已知才算已知，失败行残留的 blocked/region/type 一律视为未知。
export function effectiveUnlockStatus(unlock: IPQualityServiceUnlock): UnlockStatusKind {
  return isKnownSuccessfulService(unlock) ? unlockStatusKind(unlock.status) : 'unknown'
}

export function unlockTone(kind: UnlockStatusKind): BadgeTone {
  if (kind === 'unlocked') return 'normal'
  if (kind === 'partial') return 'notice'
  if (kind === 'blocked') return 'alert'
  return 'neutral'
}

const ANONYMOUS_PROBE_STATUS_LABELS: Readonly<Record<UnlockStatusKind, string>> = {
  unlocked: '可用',
  partial: '部分可用',
  blocked: '受限',
  unknown: '未知',
}

const LEGACY_UNLOCK_STATUS_LABELS: Readonly<Record<UnlockStatusKind, string>> = {
  unlocked: '解锁',
  partial: '部分',
  blocked: '受阻',
  unknown: '未知',
}

export function unlockStatusText(kind: UnlockStatusKind, anonymousProbe: boolean): string {
  return (anonymousProbe ? ANONYMOUS_PROBE_STATUS_LABELS : LEGACY_UNLOCK_STATUS_LABELS)[kind]
}

export function unlockStatusLabel(unlock: IPQualityServiceUnlock, anonymousProbe: boolean): string {
  const kind = effectiveUnlockStatus(unlock)
  // Netflix 仅自制剧可访问是独立的部分结论，用短标签而不是笼统的“部分可用”。
  const label = kind === 'partial' && (unlock.unlock_type ?? '').trim().toLowerCase() === 'originals_only'
    ? '仅自制剧'
    : unlockStatusText(kind, anonymousProbe)
  const region = kind === 'unknown' ? '' : (unlock.region ?? '').trim()
  return region ? `${label} · ${region}` : label
}

// 单行可由安全 extra_json.probe_revision 识别为免登录探测；旧行不套用新范围。
export function serviceRowUsesAnonymousProbe(unlock: IPQualityServiceUnlock): boolean {
  return isAnonymousServiceProbeRevision(objectRecord(unlock.extra_json)?.probe_revision)
}

export function serviceUnlockCounts(unlocks: IPQualityServiceUnlock[]) {
  return unlocks.reduce(
    (counts, unlock) => {
      counts[effectiveUnlockStatus(unlock)] += 1
      return counts
    },
    { unlocked: 0, partial: 0, blocked: 0, unknown: 0 } satisfies Record<UnlockStatusKind, number>,
  )
}

function safeDiagnosticText(value?: string): string | null {
  const normalized = (value ?? '').trim()
  if (!normalized) return null
  const lowered = normalized.toLowerCase()
  if (
    lowered.includes('default_probe') ||
    lowered.includes('not_configured') ||
    lowered.includes('optional_service_probe') ||
    lowered.includes('optional ip quality source requires configuration') ||
    lowered.includes('unsupported_default_probe') ||
    lowered.includes('safe default probe is not available without optional service configuration') ||
    lowered.includes('optional service configuration')
  ) {
    return null
  }
  return normalized
}

// agent 的 UnlockType 只描述检测范围，页面显示中文范围，不显示内部枚举。
const UNLOCK_TYPE_LABELS: Readonly<Record<string, string>> = {
  catalog_sample: '样片可访问',
  sample_titles_unavailable: '样片均不可用',
  originals_only: '仅自制剧可访问',
  web_and_app: 'Web 与 App',
  web_only: '仅 Web',
  app_only: '仅 App',
  web_verified_app_unknown: 'Web 可用，App 未知',
  app_verified_web_unknown: 'App 可用，Web 未知',
  premium_offer: 'Premium 地区展示',
  territory_offer: 'Prime 地区展示',
  supported_location: '地区支持检测',
  web_region: '网页地区检测',
  anonymous_web: '匿名网页入口',
}

function unlockTypeLabel(value?: string): string | null {
  const normalized = safeDiagnosticText(value)
  if (!normalized || normalized === 'none') return null
  const key = normalized.toLowerCase()
  if (Object.hasOwn(UNLOCK_TYPE_LABELS, key)) return UNLOCK_TYPE_LABELS[key] ?? null
  // 自定义/历史来源的类型不在 agent 范围表内，保留原有“类型 X”说明。
  return `类型 ${normalized}`
}

// agent 服务探测的 error_code → 中文短句；原始 error_summary 留在折叠的采集诊断里。
const PROBE_FAILURE_LABELS: Readonly<Record<string, string>> = {
  timeout: '探测超时',
  dns_failed: '域名解析失败',
  connection_refused: '连接被拒绝',
  network_unreachable: '网络不可达',
  tls_failed: 'TLS 连接失败',
  non_json_response: '服务响应无法识别',
  request_failed: '探测请求失败',
  read_failed: '读取服务响应失败',
  invalid_request: '探测请求无法发出',
  probe_failed: '探测失败，未形成可靠结论',
  unsupported_service: '默认探测暂不支持该服务',
  unsupported_default_probe: '默认探测暂不支持该服务',
  invalid_response: '响应未形成可靠业务结论',
  response_too_large: '服务响应过大，未形成结论',
  unexpected_redirect: '探测被重定向到非预期入口',
  challenge_detected: '服务要求人机验证',
  egress_ambiguous: '当前出口不唯一，无法确认探测归属',
  ip_mismatch: '来源返回了不同IP',
  address_family_unavailable: '本轮未检测到该地址族的可用本地地址',
  not_configured: '未接入该检测',
  unauthorized: '凭据被拒绝',
  settings_changed: '凭据已变更，本次结果作废',
  detector_dependency_missing: '检测组件缺少运行依赖',
  detector_fetch_failed: '检测请求失败',
  detector_invalid_output: '检测结果无法识别',
  detector_output_limit: '检测输出超出上限',
  missing_target_ip: '缺少已确认的出口 IP',
  client_access_denied: '入口拒绝客户端访问',
}

// 固定安全错误码的中文短句；未知码返回 null，由调用方决定是否展示原码。
export function probeErrorLabel(errorCode?: string, httpStatus: number | null = null): string | null {
  const code = (errorCode ?? '').trim().toLowerCase()
  if (code === 'http_status') return httpStatusLabel(httpStatus)
  return Object.hasOwn(PROBE_FAILURE_LABELS, code) ? (PROBE_FAILURE_LABELS[code] ?? null) : null
}

function httpStatusLabel(status: number | null): string {
  if (status == null) return '服务返回异常状态'
  if (status === 401 || status === 403) return `服务拒绝了探测请求（HTTP ${status}）`
  if (status === 404) return `探测地址不存在（HTTP ${status}）`
  if (status === 429) return `探测请求被限流（HTTP ${status}）`
  if (status >= 500) return `服务暂时不可用（HTTP ${status}）`
  return `服务返回异常状态（HTTP ${status}）`
}

// agent 只写固定 snake_case 标签/规则/错误码；不符合的值视为不可信，不展示。
const SAFE_EVIDENCE_TOKEN = /^[a-z0-9_]{1,64}$/
// 脚本端点 ID 是固定的连字符标识（netflix-81280792、openai-ios 等）。
const SAFE_ENDPOINT_TOKEN = /^[a-z0-9][a-z0-9_-]{0,63}$/

function safeEvidenceToken(value: unknown): string | null {
  return typeof value === 'string' && SAFE_EVIDENCE_TOKEN.test(value) ? value : null
}

export type ServiceEvidenceKind = 'anonymous_service_probe' | 'ipquality_script'

// 服务行自身的证据类型：evidence_kind 与 probe_revision 必须配对（免登录探测 2–4，固定脚本 5）。
export function serviceEvidenceKind(unlock: IPQualityServiceUnlock): ServiceEvidenceKind | null {
  const extra = objectRecord(unlock.extra_json)
  if (!extra) return null
  const revision = extra.probe_revision
  if (extra.evidence_kind === 'anonymous_service_probe' && (revision === 2 || revision === 3 || revision === 4)) return 'anonymous_service_probe'
  if (extra.evidence_kind === 'ipquality_script' && revision === 5) return 'ipquality_script'
  return null
}

export type ServiceEndpointOutcome = 'available' | 'restricted' | 'unknown'

export type ServiceEndpointDiagnostic = {
  endpoint: string
  httpStatus: number | null
  matchedRule: string | null
  errorCode: string | null
  outcome: ServiceEndpointOutcome | null
}

function validHTTPStatus(value: unknown): number | null {
  return typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599 ? value : null
}

// 各入口的固定标签、整数 HTTP 状态、规则与错误码；任何非白名单形状的条目整体丢弃。
// 免登录探测读 endpoints（outcome=available），固定脚本读 diagnostics（outcome=positive），两者统一成同一结构。
export function serviceEndpointDiagnostics(unlock: IPQualityServiceUnlock): ServiceEndpointDiagnostic[] {
  const kind = serviceEvidenceKind(unlock)
  if (!kind) return []
  const extra = objectRecord(unlock.extra_json)
  const entries = kind === 'ipquality_script' ? extra?.diagnostics : extra?.endpoints
  if (!Array.isArray(entries)) return []
  const positive = kind === 'ipquality_script' ? 'positive' : 'available'
  return entries.flatMap((raw) => {
    const entry = objectRecord(raw)
    const endpoint = typeof entry?.endpoint === 'string' && SAFE_ENDPOINT_TOKEN.test(entry.endpoint) ? entry.endpoint : null
    if (!entry || !endpoint) return []
    const outcome = entry.outcome === positive ? 'available' : entry.outcome === 'restricted' || entry.outcome === 'unknown' ? entry.outcome : null
    return [{
      endpoint,
      httpStatus: validHTTPStatus(entry.http_status),
      matchedRule: safeEvidenceToken(entry.matched_rule),
      errorCode: safeEvidenceToken(entry.error_code),
      outcome,
    }]
  })
}

// 固定脚本行的判定规则（安全 snake_case）；其余证据类型没有行级规则。
export function serviceScriptRule(unlock: IPQualityServiceUnlock): string | null {
  return serviceEvidenceKind(unlock) === 'ipquality_script' ? safeEvidenceToken(objectRecord(unlock.extra_json)?.rule) : null
}

// http_status 失败的整数状态只取 endpoint 安全诊断（最后一个 http_status 入口），不从 error_summary 反解析。
function serviceFailureHTTPStatus(unlock: IPQualityServiceUnlock): number | null {
  const failed = serviceEndpointDiagnostics(unlock).filter((endpoint) => endpoint.errorCode === 'http_status' && endpoint.httpStatus != null)
  return failed.at(-1)?.httpStatus ?? null
}

function unknownServiceReason(unlock: IPQualityServiceUnlock): string {
  const errorCode = (unlock.error_code ?? '').trim().toLowerCase()
  const failureLabel = probeErrorLabel(errorCode, errorCode === 'http_status' ? serviceFailureHTTPStatus(unlock) : null)
  if (failureLabel) return failureLabel
  const safeError = safeDiagnosticText(unlock.error_summary)
  if (safeError) return safeError
  const probeStatus = (unlock.probe_status ?? '').trim().toLowerCase()
  if (probeStatus === 'skipped') return '默认探测暂不支持该服务'
  if (probeStatus === 'not_configured') return '未接入该检测'
  if (probeStatus === 'failure') return '探测失败，未形成可靠结论'
  return '本轮未形成可靠结论'
}

// 服务卡片的一行补充：已知结果只补检测范围，未知结果补原因。
export function serviceTileDetail(unlock: IPQualityServiceUnlock): string | null {
  if (effectiveUnlockStatus(unlock) !== 'unknown') return unlockTypeLabel(unlock.unlock_type)
  return unknownServiceReason(unlock)
}

const COUNTRY_CODE = /^[A-Z]{2}$/

export type ObservedServiceRegion = {
  region: string
  evidence: 'youtube_content_region' | 'youtube_context_gl'
}

// 可信“页面地区”：所选报告族、probe_revision、evidence_kind、证据类型与国家码全部匹配才采用。
// 它只描述页面展示的地区，不改变 effectiveUnlockStatus 或计数。
export function observedServiceRegion(unlock: IPQualityServiceUnlock, selectedIPVersion: 4 | 6 | null): ObservedServiceRegion | null {
  const extra = objectRecord(unlock.extra_json)
  if (!extra || selectedIPVersion == null) return null
  if ((extra.probe_revision !== 3 && extra.probe_revision !== 4) || extra.evidence_kind !== 'anonymous_service_probe' || extra.ip_version !== selectedIPVersion) return null
  const evidence = extra.region_evidence
  const region = extra.observed_region
  if (evidence !== 'youtube_content_region' && evidence !== 'youtube_context_gl') return null
  if (typeof region !== 'string' || !COUNTRY_CODE.test(region)) return null
  return { region, evidence }
}

export type ServiceCheckStatus = IPQualityServiceCheck['status']

// typed checks 只接受固定枚举与安全 ID；未知形状整条忽略，不被当作结论。
export function serviceChecks(unlock: IPQualityServiceUnlock): IPQualityServiceCheck[] {
  if (!Array.isArray(unlock.checks)) return []
  return unlock.checks.filter((check) => (
    check != null
    && safeEvidenceToken(check.id) != null
    && (check.status === 'available' || check.status === 'restricted' || check.status === 'unknown')
  ))
}

export function serviceCheckStatusText(status: ServiceCheckStatus): string {
  if (status === 'available') return '可用'
  if (status === 'restricted') return '受限'
  return '未知'
}

const DEFAULT_SERVICE_PROBE_SOURCES: Readonly<Record<string, true>> = {
  netflix_title_probe: true,
  openai_status_probe: true,
  youtube_premium_page_probe: true,
  prime_video_page_probe: true,
  disney_default_probe: true,
  tiktok_home_probe: true,
  reddit_home_probe: true,
}

export type ServiceRuleMark = {
  label: string
  // 鼠标/读屏可读的单句说明，不作为常驻段落。
  title: string
}

// 所选报告服务探测规则的短标记：当前免登录规则不加标记；旧规则、停用规则与来源缺失各给一个短标记。
export function serviceRuleMark(report: VPSIPQualityReport): ServiceRuleMark | null {
  const serviceRows = report.service_unlocks
  if (!serviceRows || serviceRows.length === 0) return null

  const diagnostics = reportDiagnostics(report)
  const isDefaultSourceVersion = typeof diagnostics?.source_version === 'string' && diagnostics.source_version.trim() === 'v2'
  const hasDefaultSourceRow = serviceRows.some((row) => Object.hasOwn(DEFAULT_SERVICE_PROBE_SOURCES, (row.source ?? '').trim()))

  if (isDefaultSourceVersion || hasDefaultSourceRow) {
    const revision = serviceProbeRevision(report)
    if (isAnonymousServiceProbeRevision(revision)) return null
    if (revision === 1) {
      const hasSkippedRow = serviceRows.some((row) => (
        (row.probe_status ?? '').trim().toLowerCase() === 'skipped'
        && (row.error_code ?? '').trim().toLowerCase() === 'unsupported_default_probe'
      ))
      return hasSkippedRow ? { label: '探测已停用', title: '该报告的默认服务探测缺少可靠业务证据，已停用；未知不表示受阻。' } : null
    }
    return { label: '旧版规则', title: '该报告使用旧版或未识别的服务探测规则，结果仅作历史参考。' }
  }

  const hasSourceVersion = typeof diagnostics?.source_version === 'string' && diagnostics.source_version.trim() !== ''
  const allSourcesMissing = serviceRows.every((row) => (row.source ?? '').trim() === '')
  if (!hasSourceVersion && allSourcesMissing) {
    return { label: '来源未知', title: '该报告缺少服务探测来源信息，无法核验结果。' }
  }
  return null
}
