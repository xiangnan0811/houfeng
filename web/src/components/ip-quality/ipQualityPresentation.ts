import type { BadgeTone } from '../atoms'
import type {
  IPQualityCoverage,
  IPQualityProviderResult,
  IPQualityServiceUnlock,
  VPSIPQualityReport,
} from '../../lib/types'

export type RiskFlagKey = 'proxy' | 'tor' | 'vpn' | 'server' | 'abuse' | 'robot'

export type RiskFlag = {
  key: RiskFlagKey
  label: string
  active: boolean | null
  negative: boolean
}

export type RiskSignalCount = {
  key: RiskFlagKey
  label: string
  yes: number
  no: number
  unknown: number
  negative: boolean
}

export type UnlockStatusKind = 'unlocked' | 'partial' | 'blocked' | 'unknown'

export type ProviderSourceGap = {
  provider: string
  label: string
  tone: BadgeTone
}

const RISK_FLAG_DEFINITIONS: Array<{
  key: RiskFlagKey
  label: string
  negative: boolean
  read: (result: IPQualityProviderResult) => boolean | null | undefined
}> = [
  { key: 'proxy', label: 'Proxy', negative: true, read: (result) => result.is_proxy },
  { key: 'tor', label: 'Tor', negative: true, read: (result) => result.is_tor },
  { key: 'vpn', label: 'VPN', negative: true, read: (result) => result.is_vpn },
  { key: 'server', label: 'Server', negative: false, read: (result) => result.is_server },
  { key: 'abuse', label: 'Abuse', negative: true, read: (result) => result.is_abuser },
  { key: 'robot', label: 'Robot', negative: true, read: (result) => result.is_robot },
]

const NEGATIVE_RISK_FLAG_DEFINITIONS = RISK_FLAG_DEFINITIONS.filter((definition) => definition.negative)

// 五类负面风险字段（proxy、tor、vpn、abuse、robot）；机房不是负面风险。
export const TOTAL_NEGATIVE_RISK_FLAGS = NEGATIVE_RISK_FLAG_DEFINITIONS.length

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
  if (risk === 'low' || risk === 'clean' || risk === 'safe') return 'normal'
  return 'neutral'
}

// 只认可比较的标准等级；其它供应商自定义文本不参与聚合。
const STANDARD_RISK_LEVEL_RANK: Readonly<Record<string, number>> = {
  low: 1,
  medium: 2,
  high: 3,
  critical: 4,
}

function standardRiskLevel(value?: string): string | null {
  const risk = (value ?? '').trim().toLowerCase()
  if (risk === 'clean' || risk === 'safe') return 'low'
  if (risk === 'moderate') return 'medium'
  return Object.hasOwn(STANDARD_RISK_LEVEL_RANK, risk) ? risk : null
}

export function riskFlags(result: IPQualityProviderResult): RiskFlag[] {
  return RISK_FLAG_DEFINITIONS.map((definition) => {
    const raw = definition.read(result)
    return {
      key: definition.key,
      label: definition.label,
      active: typeof raw === 'boolean' ? raw : null,
      negative: definition.negative,
    }
  })
}

export function providerSucceeded(result: IPQualityProviderResult): boolean {
  return sourceStatusKind(result) === 'success'
}

function sourceStatusKind(result: IPQualityProviderResult): string {
  return result.status == null ? 'success' : result.status.trim().toLowerCase()
}

function sourceTypeKind(result: IPQualityProviderResult): string {
  return result.source_type == null ? 'default' : result.source_type.trim().toLowerCase()
}

function knownNegativeFlagCount(results: IPQualityProviderResult[]): number {
  const successful = results.filter(providerSucceeded)
  return NEGATIVE_RISK_FLAG_DEFINITIONS.filter((definition) => successful.some((result) => {
    const value = definition.read(result)
    return value === true || value === false
  })).length
}

export function riskEvidenceComplete(results: IPQualityProviderResult[]): boolean {
  return knownNegativeFlagCount(results) === TOTAL_NEGATIVE_RISK_FLAGS
}

export function visibleProviderResults(results: IPQualityProviderResult[]): IPQualityProviderResult[] {
  return results.filter((result) => {
    const status = sourceStatusKind(result)
    const sourceType = sourceTypeKind(result)
    if (sourceType === 'optional' && (status === 'not_configured' || status === 'skipped')) {
      return false
    }
    return true
  })
}

function activeRiskFlags(result: IPQualityProviderResult): RiskFlag[] {
  return riskFlags(result).filter((flag) => flag.active === true)
}

// 成功来源命中的负面信号，按信号类去重：多个数据库同时判 VPN 只算一项。
export function negativeRiskSignals(results: IPQualityProviderResult[]): RiskFlag[] {
  const hits = new Set<RiskFlagKey>()
  for (const result of results.filter(providerSucceeded)) {
    for (const flag of activeRiskFlags(result)) {
      if (flag.negative) hits.add(flag.key)
    }
  }
  return RISK_FLAG_DEFINITIONS
    .filter((definition) => hits.has(definition.key))
    .map((definition) => ({ key: definition.key, label: definition.label, active: true, negative: true }))
}

// 成功来源中标准等级为高/严重的数据库，单独列出来源，不折算为信号项数。
export function highRiskProviders(results: IPQualityProviderResult[]): string[] {
  return results
    .filter(providerSucceeded)
    .filter((result) => {
      const level = standardRiskLevel(result.risk_level)
      return level === 'high' || level === 'critical'
    })
    .map((result) => result.provider)
}

export function riskSignalCounts(results: IPQualityProviderResult[]): RiskSignalCount[] {
  const successfulResults = results.filter(providerSucceeded)
  return RISK_FLAG_DEFINITIONS.map((definition) => {
    let yes = 0
    let no = 0
    let unknown = 0
    for (const result of successfulResults) {
      const value = definition.read(result)
      if (value === true) yes += 1
      else if (value === false) no += 1
      else unknown += 1
    }
    return {
      key: definition.key,
      label: definition.label,
      yes,
      no,
      unknown,
      negative: definition.negative,
    }
  })
}

function reportDiagnostics(report: VPSIPQualityReport): Record<string, unknown> | null {
  const raw = report.latest_report?.diagnostics_json
  return typeof raw === 'object' && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : null
}

// 只接受数值 revision；字符串 "2" 等不认作新规则。
export function serviceProbeRevision(report: VPSIPQualityReport): number | null {
  const revision = reportDiagnostics(report)?.service_probe_revision
  return typeof revision === 'number' && Number.isFinite(revision) ? revision : null
}

export type RiskAssessmentBlocker = 'missing_report' | 'ambiguous' | 'ip_conflict'

// 归属不唯一或来源返回不同 IP 时，数据库事实不能聚合成本 VPS 的评级。
export function riskAssessmentBlocker(report: VPSIPQualityReport): RiskAssessmentBlocker | null {
  if (!report.summary) return 'missing_report'
  if (report.summary.ambiguous) return 'ambiguous'
  if (reportDiagnostics(report)?.ip_conflict === true) return 'ip_conflict'
  return null
}

export type RiskAssessment = {
  level: string | null
  complete: boolean
  knownFlags: number
  totalFlags: number
}

// 数据库风险评级：只读成功来源的标准 risk_level 取最高；服务探测结果不参与。
export function deriveRiskAssessment(report: VPSIPQualityReport): RiskAssessment {
  const successful = report.provider_results.filter(providerSucceeded)
  const knownFlags = knownNegativeFlagCount(successful)
  if (riskAssessmentBlocker(report)) {
    return { level: null, complete: false, knownFlags, totalFlags: TOTAL_NEGATIVE_RISK_FLAGS }
  }
  let level: string | null = null
  for (const result of successful) {
    const candidate = standardRiskLevel(result.risk_level)
    if (candidate && (level == null || STANDARD_RISK_LEVEL_RANK[candidate]! > STANDARD_RISK_LEVEL_RANK[level]!)) {
      level = candidate
    }
  }
  return {
    level,
    complete: knownFlags === TOTAL_NEGATIVE_RISK_FLAGS,
    knownFlags,
    totalFlags: TOTAL_NEGATIVE_RISK_FLAGS,
  }
}

export type RiskVerdict = {
  // 大字：标准等级或 “—”。
  value: string
  headline: string
  // 证据范围说明，例如 “风险字段 4/5，结论不完整”。
  scope: string | null
  tone: BadgeTone
}

function incompleteScope(assessment: RiskAssessment): string {
  return `风险字段 ${assessment.knownFlags}/${assessment.totalFlags}，结论不完整`
}

export function riskVerdict(report: VPSIPQualityReport): RiskVerdict {
  const blocker = riskAssessmentBlocker(report)
  if (blocker === 'missing_report') {
    return { value: '—', headline: '缺少 IP 质量报告', scope: null, tone: 'neutral' }
  }
  if (blocker === 'ambiguous') {
    return { value: '—', headline: 'IP 归属不唯一，需先复核', scope: '数据库明细仍可在下方查看', tone: 'neutral' }
  }
  if (blocker === 'ip_conflict') {
    return { value: '—', headline: '来源返回了不同 IP，需复核来源', scope: '数据库明细仍可在下方查看', tone: 'neutral' }
  }

  const assessment = deriveRiskAssessment(report)
  const stale = report.summary?.stale === true
  let verdict: RiskVerdict
  if (assessment.level) {
    const label = riskLevelLabel(assessment.level)
    verdict = assessment.complete
      ? { value: label, headline: `风险字段 ${assessment.totalFlags}/${assessment.totalFlags} 已返回`, scope: null, tone: riskTone(assessment.level) }
      : {
          value: label,
          headline: `已返回来源：${label}`,
          scope: incompleteScope(assessment),
          // 证据不全时不把低风险画成整体绿色结论。
          tone: assessment.level === 'low' ? 'neutral' : riskTone(assessment.level),
        }
  } else if (negativeRiskSignals(report.provider_results).length > 0) {
    verdict = { value: '—', headline: '发现风险信号', scope: assessment.complete ? null : incompleteScope(assessment), tone: 'alert' }
  } else if (assessment.complete) {
    verdict = { value: '—', headline: '已返回风险字段未命中', scope: null, tone: 'neutral' }
  } else {
    verdict = { value: '—', headline: '证据不足', scope: incompleteScope(assessment), tone: 'neutral' }
  }
  // 过期报告保留事实，但不作为当前评级着色。
  return stale ? { ...verdict, tone: 'neutral' } : verdict
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
  const label = unlockStatusText(kind, anonymousProbe)
  const region = kind === 'unknown' ? '' : (unlock.region ?? '').trim()
  return region ? `${label} · ${region}` : label
}

// 单行可由安全 extra_json.probe_revision === 2 识别为免登录探测；旧行不套用新范围。
export function serviceRowUsesAnonymousProbe(unlock: IPQualityServiceUnlock): boolean {
  const extra = unlock.extra_json
  if (typeof extra !== 'object' || extra === null || Array.isArray(extra)) return false
  return (extra as Record<string, unknown>).probe_revision === 2
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
  catalog_sample: '抽样目录可访问',
  sample_titles_unavailable: '抽样片目不可用，未验证完整片库',
  web_and_app: 'Web 与 App 入口',
  web_only: '仅 Web 入口可用',
  app_only: '仅 App 入口可用',
  web_verified_app_unknown: 'Web 已验证，App 入口未知',
  app_verified_web_unknown: 'App 已验证，Web 入口未知',
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
}

// 只认 agent 生成的完整格式，避免把其它说明里顺带提到的状态码当作失败原因。
const HTTP_STATUS_SUMMARY = /^http status (\d{3})$/i

function httpStatusLabel(status?: string): string {
  if (!status) return '服务返回异常状态'
  const code = Number(status)
  if (code === 401 || code === 403) return `服务拒绝了探测请求（HTTP ${status}）`
  if (code === 404) return `探测地址不存在（HTTP ${status}）`
  if (code === 429) return `探测请求被限流（HTTP ${status}）`
  if (code >= 500) return `服务暂时不可用（HTTP ${status}）`
  return `服务返回异常状态（HTTP ${status}）`
}

function probeFailureLabel(errorCode: string, summary: string | null): string | null {
  const httpStatus = summary?.trim().match(HTTP_STATUS_SUMMARY)?.[1]
  if (errorCode === 'http_status') return httpStatusLabel(httpStatus)
  if (Object.hasOwn(PROBE_FAILURE_LABELS, errorCode)) return PROBE_FAILURE_LABELS[errorCode] ?? null
  // 旧数据可能只有 summary 没有 code。
  if (!errorCode && httpStatus) return httpStatusLabel(httpStatus)
  return null
}

function unknownServiceReason(unlock: IPQualityServiceUnlock): string {
  const safeError = safeDiagnosticText(unlock.error_summary)
  const errorCode = (unlock.error_code ?? '').trim().toLowerCase()
  const failureLabel = probeFailureLabel(errorCode, safeError)
  if (failureLabel) return failureLabel
  if (safeError) return safeError
  const probeStatus = (unlock.probe_status ?? '').trim().toLowerCase()
  if (probeStatus === 'skipped') return '默认探测暂不支持该服务'
  if (probeStatus === 'not_configured' || errorCode === 'not_configured') return '需要可选配置后才能检测'
  if (probeStatus === 'failure') return '探测失败，未形成可靠结论'
  return '本轮未形成可靠结论'
}

export function serviceCardDescription(unlock: IPQualityServiceUnlock): string {
  const kind = effectiveUnlockStatus(unlock)
  if (kind === 'unknown') return unknownServiceReason(unlock)
  const scope = unlockTypeLabel(unlock.unlock_type)
  if (scope) return scope
  const region = (unlock.region ?? '').trim()
  if (kind === 'unlocked') return region ? `区域 ${region} 可用` : '服务可用'
  if (kind === 'partial') return region ? `区域 ${region} 部分可用` : '部分内容可用'
  return region ? `区域 ${region} 受限` : '服务受限'
}

// 服务卡片的补充说明：状态和区域已在徽标里，已知结果只补检测范围，未知结果补原因。
export function serviceTileDetail(unlock: IPQualityServiceUnlock): string | null {
  if (effectiveUnlockStatus(unlock) !== 'unknown') return unlockTypeLabel(unlock.unlock_type)
  return unknownServiceReason(unlock)
}

function reportCoverage(report: VPSIPQualityReport): IPQualityCoverage | null {
  return report.summary?.coverage ?? report.latest_report?.coverage ?? null
}

function validCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
}

// 异常计数（负数、分子大于分母、分母为 0）返回 null，不夹到 100%。
export function coveragePercent(observed: number, expected: number): number | null {
  if (!validCount(observed) || !validCount(expected) || expected <= 0 || observed > expected) return null
  return (observed / expected) * 100
}

export type EnabledProviderCoverage = {
  successful: number
  enabled: number
  notConfigured: number
  percent: number
}

// 已启用来源成功率：分母为 expected - not_configured，只使用 API coverage，不从行数重建。
export function enabledProviderCoverage(report: VPSIPQualityReport): EnabledProviderCoverage | null {
  const coverage = reportCoverage(report)
  if (!coverage) return null
  const expected = coverage.expected_provider_count
  const successful = coverage.successful_provider_count
  const notConfigured = coverage.not_configured_provider_count
  if (!validCount(expected) || !validCount(successful) || !validCount(notConfigured)) return null
  const enabled = expected - notConfigured
  const percent = coveragePercent(successful, enabled)
  if (percent == null) return null
  return { successful, enabled, notConfigured, percent }
}

export function serviceCoverage(report: VPSIPQualityReport): number | null {
  const coverage = reportCoverage(report)
  if (!coverage) return null
  return coveragePercent(coverage.successful_service_count, coverage.expected_service_count)
}

export type DatabaseConsistency = {
  percent: number
  dimensions: number
}

function knownText(results: IPQualityProviderResult[], read: (result: IPQualityProviderResult) => string | undefined): string[] {
  return results.map((result) => normalizeComparable(read(result))).filter(Boolean)
}

// 只比较至少有两个已知值的维度；未知 bool、空字符串不参与，区域不混用代码与全名。
export function databaseConsistency(results: IPQualityProviderResult[]): DatabaseConsistency | null {
  const successful = results.filter(providerSucceeded)
  const regionCodes = knownText(successful, (result) => result.region_code)
  const dimensions: string[][] = [
    knownText(successful, (result) => result.usage_type),
    knownText(successful, (result) => result.company_type),
    knownText(successful, (result) => result.risk_level),
    regionCodes.length >= 2 ? regionCodes : knownText(successful, (result) => result.region_name),
    ...RISK_FLAG_DEFINITIONS.map((definition) => successful
      .map(definition.read)
      .filter((value): value is boolean => typeof value === 'boolean')
      .map(String)),
  ]
  const comparable = dimensions.filter((values) => values.length >= 2)
  if (comparable.length === 0) return null
  const consistent = comparable.filter((values) => new Set(values).size === 1).length
  return { percent: Math.round((consistent / comparable.length) * 100), dimensions: comparable.length }
}

function normalizeComparable(value?: string): string {
  return (value ?? '').trim().toLowerCase()
}

export type ProviderTableRows = {
  // 成功且至少给出一个判断字段的数据库，按风险相关度排序。
  rows: IPQualityProviderResult[]
  // 成功但没有任何可展示判断字段的数据库数量。
  emptyCount: number
  // 本轮未返回结果的默认/自定义来源（失败或跳过）；可选来源的未配置行只在诊断中展示。
  unavailable: ProviderSourceGap[]
}

function providerHasFacts(result: IPQualityProviderResult): boolean {
  const text = [result.usage_type, result.company_type, result.risk_level, result.risk_score, result.region_code, result.region_name]
  if (text.some((value) => (value ?? '').trim() !== '')) return true
  return riskFlags(result).some((flag) => flag.active != null)
}

function providerRelevance(result: IPQualityProviderResult): number {
  const negativeHits = activeRiskFlags(result).filter((flag) => flag.negative).length
  const level = standardRiskLevel(result.risk_level)
  const riskWeight = level ? STANDARD_RISK_LEVEL_RANK[level]! - 1 : 0
  const known = riskFlags(result).filter((flag) => flag.active != null).length
  return negativeHits * 100 + riskWeight * 10 + known
}

export function providerTableRows(results: IPQualityProviderResult[]): ProviderTableRows {
  const successful = results.filter(providerSucceeded)
  const withFacts = successful.filter(providerHasFacts)
  const rows = withFacts
    .map((result, index) => ({ result, index, relevance: providerRelevance(result) }))
    .sort((a, b) => b.relevance - a.relevance || a.index - b.index)
    .map((item) => item.result)
  const unavailable = visibleProviderResults(results)
    .filter((result) => !providerSucceeded(result))
    .map((result) => {
      const status = sourceStatusKind(result)
      const label = status === 'failure' ? '失败' : status === 'skipped' ? '未检测' : '未配置'
      return {
        provider: result.provider,
        label: `${result.provider} · ${label}`,
        tone: status === 'failure' ? 'alert' : 'neutral',
      } satisfies ProviderSourceGap
    })
  return { rows, emptyCount: successful.length - withFacts.length, unavailable }
}

// extra_json.capability 只描述该来源本次响应：geo_only 表示本次仅返回地理信息。
export function providerCapability(result: IPQualityProviderResult): 'risk' | 'geo_only' | null {
  const extra = result.extra_json
  if (typeof extra !== 'object' || extra === null || Array.isArray(extra)) return null
  const capability = (extra as Record<string, unknown>).capability
  return capability === 'risk' || capability === 'geo_only' ? capability : null
}

export type RiskSignalHit = {
  key: RiskFlagKey
  label: string
  hits: number
  total: number
  negative: boolean
}

// 只返回至少一个数据库命中的信号，total 为给出该信号判断的数据库数。
export function riskSignalHits(results: IPQualityProviderResult[]): RiskSignalHit[] {
  return riskSignalCounts(results)
    .filter((signal) => signal.yes > 0)
    .map((signal) => ({
      key: signal.key,
      label: signal.label,
      hits: signal.yes,
      total: signal.yes + signal.no,
      negative: signal.negative,
    }))
}

export const DEFAULT_SERVICE_PROBE_SOURCES: ReadonlySet<string> = new Set([
  'netflix_title_probe',
  'openai_status_probe',
  'youtube_premium_page_probe',
  'prime_video_page_probe',
  'disney_default_probe',
  'tiktok_home_probe',
  'reddit_home_probe',
])

export type ServiceProbeNotice = {
  // warning：历史停用或不可核验；muted：revision 2 的中性范围说明。
  tone: 'warning' | 'muted'
  text: string
}

export const ANONYMOUS_SERVICE_PROBE_NOTICE = '免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。'

export function defaultServiceProbeNotice(report: VPSIPQualityReport): ServiceProbeNotice | null {
  const serviceRows = report.service_unlocks
  if (!serviceRows || serviceRows.length === 0) {
    return null
  }

  const diagnostics = reportDiagnostics(report)
  const isDefaultSourceVersion = typeof diagnostics?.source_version === 'string' && diagnostics.source_version.trim() === 'v2'
  const hasDefaultSourceRow = serviceRows.some((row) => {
    const source = (row.source ?? '').trim()
    return DEFAULT_SERVICE_PROBE_SOURCES.has(source)
  })

  const isDefaultReport = isDefaultSourceVersion || hasDefaultSourceRow
  if (isDefaultReport) {
    const revision = serviceProbeRevision(report)
    if (revision === 2) {
      return { tone: 'muted', text: ANONYMOUS_SERVICE_PROBE_NOTICE }
    }

    if (revision === 1) {
      const hasSkippedRow = serviceRows.some((row) => {
        const probeStatus = (row.probe_status ?? '').trim().toLowerCase()
        const errorCode = (row.error_code ?? '').trim().toLowerCase()
        return probeStatus === 'skipped' && errorCode === 'unsupported_default_probe'
      })
      if (hasSkippedRow) {
        return { tone: 'warning', text: '默认服务解锁探测已停用：缺少可靠业务证据。未知不表示服务受阻，服务覆盖可能不足。' }
      }
      return null
    }

    return { tone: 'warning', text: '此报告使用旧版或未识别的服务探测规则，解锁结果可能不可靠；历史原始结果予以保留。' }
  }

  const hasSourceVersion = typeof diagnostics?.source_version === 'string' && diagnostics.source_version.trim() !== ''
  const allSourcesMissing = serviceRows.every((row) => (row.source ?? '').trim() === '')

  if (!hasSourceVersion && allSourcesMissing) {
    return { tone: 'warning', text: '此报告缺少服务探测来源信息，无法核验解锁结果；历史原始结果予以保留。' }
  }

  return null
}
