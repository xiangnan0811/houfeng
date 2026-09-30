import type { BadgeTone } from '../atoms'
import type {
  IPQualityProviderResult,
  IPQualityServiceUnlock,
  IPQualitySummary,
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

export function riskFlags(result: IPQualityProviderResult): RiskFlag[] {
  return RISK_FLAG_DEFINITIONS.map((definition) => {
    const raw = definition.read(result)
    return {
      key: definition.key,
      label: definition.label,
      active: raw == null ? null : raw,
      negative: definition.negative,
    }
  })
}

export function providerSucceeded(result: IPQualityProviderResult): boolean {
  return sourceStatusKind(result) === 'success'
}

function sourceStatusKind(result: IPQualityProviderResult): string {
  return (result.status ?? 'success').trim().toLowerCase()
}

function sourceTypeKind(result: IPQualityProviderResult): string {
  return (result.source_type ?? 'default').trim().toLowerCase()
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

export function activeRiskFlags(result: IPQualityProviderResult): RiskFlag[] {
  return riskFlags(result).filter((flag) => flag.active)
}

export function strongestRiskFlags(report: VPSIPQualityReport, limit = 4): RiskFlag[] {
  const flags: RiskFlag[] = []
  const seen = new Set<RiskFlagKey>()
  // 只统计成功返回的数据库；失败/跳过来源的旧字段不能进入风险结论。
  for (const result of report.provider_results.filter(providerSucceeded)) {
    for (const flag of activeRiskFlags(result)) {
      if (!flag.negative || seen.has(flag.key)) continue
      seen.add(flag.key)
      flags.push(flag)
      if (flags.length >= limit) return flags
    }
  }
  return flags
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

export function negativeRiskSignalCount(results: IPQualityProviderResult[]): number {
  return results.filter(providerSucceeded).reduce((total, result) => {
    const flagCount = activeRiskFlags(result).filter((flag) => flag.negative).length
    const risk = (result.risk_level ?? '').trim().toLowerCase()
    const riskCount = risk === 'high' || risk === 'critical' ? 1 : 0
    return total + flagCount + riskCount
  }, 0)
}

export function unlockStatusKind(status?: string): UnlockStatusKind {
  const normalized = (status ?? '').trim().toLowerCase()
  if (normalized === 'unlocked') return 'unlocked'
  if (normalized === 'partial') return 'partial'
  if (normalized === 'blocked') return 'blocked'
  return 'unknown'
}

export function unlockTone(status?: string): BadgeTone {
  const kind = unlockStatusKind(status)
  if (kind === 'unlocked') return 'normal'
  if (kind === 'partial') return 'notice'
  if (kind === 'blocked') return 'alert'
  return 'neutral'
}

export function unlockStatusLabel(status: string, region?: string): string {
  const kind = unlockStatusKind(status)
  const label = kind === 'unlocked' ? '解锁' : kind === 'blocked' ? '受阻' : kind === 'partial' ? '部分' : '未知'
  return region ? `${label} · ${region}` : label
}

export function serviceUnlockCounts(unlocks: IPQualityServiceUnlock[]) {
  return unlocks.reduce(
    (counts, unlock) => {
      counts[unlockStatusKind(unlock.status)] += 1
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

function safeUnlockType(value?: string): string | null {
  const normalized = safeDiagnosticText(value)
  if (!normalized || normalized === 'none') return null
  return normalized
}

export function serviceCardDescription(unlock: IPQualityServiceUnlock): string {
  const unlockType = safeUnlockType(unlock.unlock_type)
  if (unlockType) {
    return `解锁类型 ${unlockType}`
  }
  const safeError = safeDiagnosticText(unlock.error_summary)
  if (safeError) return safeError
  const kind = unlockStatusKind(unlock.status)
  if (kind === 'unlocked') return unlock.region ? `区域 ${unlock.region} 可用` : '服务可用'
  if (kind === 'partial') return unlock.region ? `区域 ${unlock.region} 部分可用` : '部分内容可用'
  if (kind === 'blocked') return unlock.region ? `区域 ${unlock.region} 受阻` : '服务解锁受阻'
  const probeStatus = (unlock.probe_status ?? '').trim().toLowerCase()
  const errorCode = (unlock.error_code ?? '').trim().toLowerCase()
  if (probeStatus === 'skipped' || errorCode === 'unsupported_default_probe') return '默认探测暂不支持该服务'
  if (probeStatus === 'not_configured' || errorCode === 'not_configured') return '需要可选配置后才能检测'
  if (probeStatus === 'failure') return '探测失败，未形成可靠结论'
  return '本轮未形成可靠解锁结论'
}

export function coveragePercent(observed: number, expected: number): number | null {
  if (expected <= 0) return observed > 0 ? 100 : null
  return Math.min(100, (observed / expected) * 100)
}

export function providerCoverage(report: VPSIPQualityReport): number | null {
  const coverage = report.summary?.coverage ?? report.latest_report?.coverage
  if (coverage) return coveragePercent(coverage.successful_provider_count, coverage.expected_provider_count)
  const expected = Math.max(report.summary?.provider_count ?? 0, report.provider_results.length)
  return coveragePercent(report.provider_results.length, expected)
}

export function serviceCoverage(report: VPSIPQualityReport): number | null {
  const coverage = report.summary?.coverage ?? report.latest_report?.coverage
  if (coverage) return coveragePercent(coverage.successful_service_count, coverage.expected_service_count)
  const expected = Math.max(report.summary?.unlockable_count ?? 0, report.service_unlocks.length)
  return coveragePercent(report.service_unlocks.length, expected)
}

export function databaseConsistency(results: IPQualityProviderResult[]): number | null {
  const successfulResults = results.filter(providerSucceeded)
  if (successfulResults.length < 2) return null
  const dimensions = [
    (result: IPQualityProviderResult) => normalizeComparable(result.usage_type),
    (result: IPQualityProviderResult) => normalizeComparable(result.company_type),
    (result: IPQualityProviderResult) => normalizeComparable(result.risk_level),
    (result: IPQualityProviderResult) => normalizeComparable(result.region_code || result.region_name),
    ...RISK_FLAG_DEFINITIONS.map((definition) => (result: IPQualityProviderResult) => String(definition.read(result) ?? 'unknown')),
  ]
  const consistent = dimensions.filter((read) => {
    const values = new Set(successfulResults.map(read).filter(Boolean))
    return values.size <= 1
  }).length
  return Math.round((consistent / dimensions.length) * 100)
}

function normalizeComparable(value?: string): string {
  return (value ?? '').trim().toLowerCase()
}

export function deriveQualityScore(report: VPSIPQualityReport): number | null {
  if (!report.summary) return null
  let score = 100
  const risk = (report.summary.risk_level ?? '').trim().toLowerCase()
  if (risk === 'critical') score -= 34
  else if (risk === 'high') score -= 26
  else if (risk === 'medium' || risk === 'moderate') score -= 14

  const negativeSignals = negativeRiskSignalCount(report.provider_results)
  score -= Math.min(30, negativeSignals * 5)

  const serviceCounts = serviceUnlockCounts(report.service_unlocks)
  score -= Math.min(20, serviceCounts.blocked * 5 + serviceCounts.partial * 2)

  if (report.summary.stale) score -= 8
  if (report.summary.ambiguous) score -= 12

  return Math.max(0, Math.min(100, score))
}

export function qualityVerdict(score: number | null, summary?: IPQualitySummary | null): string {
  if (!summary) return '缺少真实 IP 质量事实'
  if (summary.ambiguous) return '归属不唯一，需先复核'
  if (summary.stale) return '报告已过期，需等待下次采集'
  if (score == null) return '证据不足，暂不评级'
  if (score >= 82) return '适合作为主力节点'
  if (score >= 68) return '可接受，建议持续观察'
  if (score >= 50) return '存在明显风险，谨慎使用'
  return '不建议作为主力节点'
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
  const risk = (result.risk_level ?? '').trim().toLowerCase()
  const riskWeight = risk === 'critical' ? 4 : risk === 'high' ? 3 : risk === 'medium' || risk === 'moderate' ? 2 : 0
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

// 服务卡片的补充说明：状态和区域已在徽标里，只补解锁类型或“未知”的原因。
export function serviceTileDetail(unlock: IPQualityServiceUnlock): string | null {
  const unlockType = safeUnlockType(unlock.unlock_type)
  if (unlockType) return `类型 ${unlockType}`
  if (unlockStatusKind(unlock.status) !== 'unknown') return null
  return serviceCardDescription(unlock)
}
