import type { BadgeTone } from '../atoms'
import type {
  IPQualityBlacklistResult,
  IPQualityMailResult,
  IPQualityProductSummary,
  IPQualityProductView,
  IPQualityProviderResult,
  IPQualityScore,
  VPSIPQualityReport,
} from '../../lib/types'
import { providerSucceeded, sourceStatusKind, visibleProviderResults } from './ipQualityPresentation'

// 页面只展示服务端产出的 product view；不重算风险，不跨来源合并字段。

// 仅 revision1 且与所选报告同一 report_id 的 product 才属于本页；否则按旧版报告展示。
export function selectedProduct(report: VPSIPQualityReport): IPQualityProductView | null {
  const product = report.product
  if (!product || product.revision !== 1) return null
  const reportId = report.summary?.report_id || report.latest_report?.report_id || ''
  if (reportId && product.report_id !== reportId) return null
  return product
}

export function productEnriching(product: IPQualityProductView | null): boolean {
  return product?.state === 'pending' || product?.state === 'running'
}

// 服务端已评估的事实是否含风险命中（风险标记、文字高风险来源或黑名单命中）；与 Center ProductHasRisk 同义。
export function productSummaryHasRisk(summary: IPQualityProductSummary | null | undefined): boolean {
  return summary != null && (summary.risk_flags.length > 0 || summary.high_risk_sources.length > 0 || summary.blacklist_listed_count > 0)
}

const RISK_FLAG_LABELS: Readonly<Record<string, string>> = {
  proxy: 'Proxy',
  vpn: 'VPN',
  tor: 'Tor',
  abuse: 'Abuse',
  robot: 'Robot',
}

export function riskFlagLabel(flag: string): string {
  return RISK_FLAG_LABELS[flag] ?? flag
}

// 资产/总览/历史共用的短事实：命中的风险标记与黑名单命中数。
export function productRiskFacts(summary: IPQualityProductSummary): string[] {
  const facts = summary.risk_flags.map(riskFlagLabel)
  if (summary.high_risk_sources.length > 0) facts.push(`高风险：${summary.high_risk_sources.join('、')}`)
  if (summary.blacklist_listed_count > 0) facts.push(`黑名单 ${summary.blacklist_listed_count}`)
  return facts
}

// ---------------------------------------------------------------------------
// 来源（每个供应商一行）
// ---------------------------------------------------------------------------

export type VendorGroup = {
  provider: string
  primary: IPQualityProviderResult
  // 同一供应商的其他样本（例如匿名与认证），默认折叠。
  others: IPQualityProviderResult[]
  // 认证增强（center_api）样本失败而主行仍是成功的匿名样本：只标注，不覆盖已知事实。
  enhancementFailed: boolean
}

// 与 Center SelectedProductProviders 同一选择：同供应商认证成功样本优先，否则成功样本，否则首行。
export function vendorGroups(rows: IPQualityProviderResult[]): VendorGroup[] {
  const groups: VendorGroup[] = []
  const index: Record<string, number> = {}
  for (const row of visibleProviderResults(rows)) {
    const at = index[row.provider]
    if (at === undefined) {
      index[row.provider] = groups.length
      groups.push({ provider: row.provider, primary: row, others: [], enhancementFailed: false })
      continue
    }
    const group = groups[at]!
    const current = group.primary
    const preferred = providerSucceeded(row) && (!providerSucceeded(current) || row.details?.method === 'center_api')
    if (preferred) {
      group.others.push(current)
      group.primary = row
    } else {
      group.others.push(row)
    }
  }
  for (const group of groups) {
    group.enhancementFailed = providerSucceeded(group.primary)
      && group.primary.details?.method !== 'center_api'
      && group.others.some((row) => !providerSucceeded(row) && row.details?.method === 'center_api')
  }
  return groups
}

export function providerMethodLabel(row: IPQualityProviderResult): string | null {
  if (row.details?.method === 'center_api') return '认证'
  if (row.details?.method === 'agent_public') return '匿名'
  return null
}

export type FlagCellState = 'yes' | 'no' | 'unsupported' | 'unknown' | 'error'

export type FlagColumn = {
  key: string
  label: string
  // 机房不是负面风险：命中用中性色。
  negative: boolean
  read: (row: IPQualityProviderResult) => boolean | null | undefined
}

export const FLAG_COLUMNS: readonly FlagColumn[] = [
  { key: 'is_proxy', label: 'Proxy', negative: true, read: (row) => row.is_proxy },
  { key: 'is_vpn', label: 'VPN', negative: true, read: (row) => row.is_vpn },
  { key: 'is_tor', label: 'Tor', negative: true, read: (row) => row.is_tor },
  { key: 'is_abuser', label: 'Abuse', negative: true, read: (row) => row.is_abuser },
  { key: 'is_robot', label: 'Robot', negative: true, read: (row) => row.is_robot },
  { key: 'is_server', label: '机房', negative: false, read: (row) => row.is_server },
]

// 四态：明确布尔 → 是/否；否则读该来源 field_states（不支持/错误/未知），旧行无状态时为未知。
export function flagCellState(row: IPQualityProviderResult, column: FlagColumn): FlagCellState {
  const value = column.read(row)
  if (value === true) return 'yes'
  if (value === false) return 'no'
  const state = row.details?.field_states?.[column.key]
  if (state === 'unsupported') return 'unsupported'
  if (state === 'error') return 'error'
  return 'unknown'
}

export type FieldCellState = 'known' | 'unsupported' | 'unknown' | 'error'

// 字符串/数值字段四态：有值为已知；否则读该来源 field_states，旧行无状态时为未知。
export function fieldCellState(row: IPQualityProviderResult, key: string, value: string | null | undefined): FieldCellState {
  if ((value ?? '').trim() !== '') return 'known'
  const state = row.details?.field_states?.[key]
  if (state === 'unsupported') return 'unsupported'
  if (state === 'error') return 'error'
  return 'unknown'
}

// 使用类型/公司类型的有限规范化；只有来源明确 residential 才称“家宽”。
const USAGE_TYPE_LABELS: Readonly<Record<string, string>> = {
  hosting: '机房',
  datacenter: '机房',
  'data center': '机房',
  dch: '机房',
  isp: 'ISP',
  education: '教育',
  edu: '教育',
  government: '政府',
  gov: '政府',
  banking: '金融',
  business: '企业',
  com: '企业',
  residential: '家宽',
  'fixed line residential': '家宽',
  'fixed-line residential': '家宽',
}

export function usageTypeLabel(value: string | undefined): string | null {
  const raw = (value ?? '').trim()
  if (!raw) return null
  return USAGE_TYPE_LABELS[raw.toLowerCase()] ?? raw
}

const SCOPE_LABELS: Readonly<Record<IPQualityScore['scope'], string>> = {
  ip: '该 IP',
  company_network: '公司网段',
  asn: 'ASN',
}

export function scoreScopeLabel(scope: IPQualityScore['scope']): string {
  return SCOPE_LABELS[scope] ?? scope
}

export type ScoreDisplay = {
  text: string
  // 0–1，只在值与上下界都明确时给出；否则不画进度条。
  fraction: number | null
}

function finite(value: number | null): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

// 比例按百分比展示（0.0183 → 1.83%）；其它单位展示原数值。分数之间不排序、不平均。
export function scoreDisplay(score: IPQualityScore): ScoreDisplay {
  if (!finite(score.value)) return { text: score.raw_value.trim() || '—', fraction: null }
  const value = score.value
  const text = score.unit === 'ratio'
    ? `${Number((value * 100).toFixed(2))}%`
    : `${Number(value.toFixed(2))}${score.unit && score.unit !== 'score' ? ` ${score.unit}` : ''}`
  if (!finite(score.min) || !finite(score.max) || score.max <= score.min) return { text, fraction: null }
  const fraction = Math.min(1, Math.max(0, (value - score.min) / (score.max - score.min)))
  return { text, fraction }
}

export type ScoreLevel = {
  label: string
  tone: BadgeTone
  // 来源原文（如 "Very High"）；映射后与原文不同时页面同时展示，便于核对。
  raw: string
}

// 来源自身给出的等级标签，大小写无关的显式映射；未识别的标签保留原文、中性色，不猜测高低。
const SCORE_LEVELS: Readonly<Record<string, { label: string, tone: BadgeTone }>> = {
  'very low': { label: '很低', tone: 'normal' },
  low: { label: '低', tone: 'normal' },
  medium: { label: '中', tone: 'notice' },
  elevated: { label: '偏高', tone: 'notice' },
  high: { label: '高', tone: 'alert' },
  'very high': { label: '很高', tone: 'alert' },
  critical: { label: '严重', tone: 'critical' },
}

export function scoreLevel(sourceLabel: string): ScoreLevel | null {
  const raw = sourceLabel.trim()
  if (!raw) return null
  const known = SCORE_LEVELS[raw.toLowerCase().replace(/[\s_-]+/g, ' ')]
  return known ? { ...known, raw } : { label: raw, tone: 'neutral', raw }
}

// ---------------------------------------------------------------------------
// 多源定位
// ---------------------------------------------------------------------------

export type LocationRow = {
  key: string
  provider: string
  method: string | null
  // 展示用：来源自身给出的国家代码或名称（如 "United States"），不做查表转换。
  country: string
  // 比较用：仅该来源明确给出的国家代码（大写）；没有代码时为空，不从名称或其他来源补。
  countryCode: string
  region: string
  city: string
  coordinates: string | null
  observedAt: string | null
  // 每格的四态（来自该来源 field_states），空值不一律画成同一个“—”。
  states: { country: FieldCellState, region: FieldCellState, city: FieldCellState, coordinates: FieldCellState }
}

function coordinateState(row: IPQualityProviderResult, coordinates: string | null): FieldCellState {
  if (coordinates != null) return 'known'
  const latitude = fieldCellState(row, 'latitude', null)
  const longitude = fieldCellState(row, 'longitude', null)
  if (latitude === 'error' || longitude === 'error') return 'error'
  if (latitude === 'unsupported' && longitude === 'unsupported') return 'unsupported'
  return 'unknown'
}

export function locationRows(rows: IPQualityProviderResult[]): LocationRow[] {
  return vendorGroups(rows)
    .map((group) => group.primary)
    .filter(providerSucceeded)
    .flatMap((row) => {
      const details = row.details
      const country = (details?.country_code || row.region_code || row.region_name || '').trim()
      // revision1 行只认 typed country_code；旧行沿用其 region_code。名称永不当作可比较身份。
      const countryCode = ((details ? details.country_code : row.region_code) ?? '').trim().toUpperCase()
      const region = (details?.region ?? '').trim()
      const city = (details?.city ?? '').trim()
      const latitude = details?.latitude
      const longitude = details?.longitude
      // 0 是合法坐标，只有 null/缺失才算没有。
      const coordinates = latitude != null && longitude != null ? `${latitude}, ${longitude}` : null
      // 旧行无 details 且无定位值时不占行；revision1 来源即使全无值也保留其字段状态。
      if (!details && !country && !region && !city && coordinates == null) return []
      return [{
        key: `${row.provider}:${details?.method ?? ''}`,
        provider: row.provider,
        method: providerMethodLabel(row),
        country,
        countryCode,
        region,
        city,
        coordinates,
        observedAt: details?.observed_at ?? null,
        states: {
          country: fieldCellState(row, 'country_code', country),
          region: fieldCellState(row, 'region', region),
          city: fieldCellState(row, 'city', city),
          coordinates: coordinateState(row, coordinates),
        },
      }]
    })
}

export type AgreedValue = { value: string | null, discrepant: boolean }

// 只在各来源一致时给出单值；不同来源不同值时标“来源有分歧”，不挑选其中一个。
export function agreedValue(values: string[]): AgreedValue {
  const distinct = [...new Set(values.map((value) => value.trim()).filter(Boolean))]
  if (distinct.length === 0) return { value: null, discrepant: false }
  if (distinct.length === 1) return { value: distinct[0]!, discrepant: false }
  return { value: null, discrepant: true }
}

// 国家按来源明确给出的代码比较：显示名称（"United States"）与代码（"US"）不是分歧，缺代码的来源不参与判定。
// 有代码时一致即给出该代码；所有来源都没有代码时，仅在显示值唯一时给出，不据名称宣称分歧。
export function agreedCountry(rows: LocationRow[]): AgreedValue {
  const byCode = agreedValue(rows.map((row) => row.countryCode))
  if (byCode.value != null || byCode.discrepant) return byCode
  const byName = agreedValue(rows.map((row) => row.country))
  return { value: byName.value, discrepant: false }
}

export type IPTypeView = {
  label: string
  tone: BadgeTone
}

const IP_TYPE_LABELS: Readonly<Record<string, { label: string, tone: BadgeTone }>> = {
  native: { label: '原生 IP', tone: 'normal' },
  broadcast: { label: '广播 IP', tone: 'notice' },
  conflict: { label: '来源冲突', tone: 'notice' },
}

// 只读服务端 ip_type；旧报告没有该对象时不按 geo_status 重分类，始终给出“未知”短标记。
export function ipTypeView(summary: IPQualityProductSummary | null): IPTypeView {
  return IP_TYPE_LABELS[summary?.ip_type?.status ?? ''] ?? { label: '未知', tone: 'neutral' }
}

// 旧报告的地理一致性证据只作历史参考，不再当作 IP 类型。
export function legacyGeoStatusLabel(status: IPQualityProductSummary['geo_status']): string | null {
  if (status === 'consistent') return '注册与使用地一致'
  if (status === 'discrepant') return '注册与使用地不一致'
  if (status === 'conflict') return '来源地区分歧'
  return null
}

// 十进制字符串按千分位分组，避免 JS 数值精度丢失。
export function groupDigits(value: string | null): string | null {
  const trimmed = (value ?? '').trim()
  if (!/^\d+$/.test(trimmed)) return trimmed || null
  return trimmed.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

// ---------------------------------------------------------------------------
// 邮件与黑名单
// ---------------------------------------------------------------------------

const MAIL_STATUS: Readonly<Record<IPQualityMailResult['status'], { label: string, tone: BadgeTone }>> = {
  reachable: { label: '可连接', tone: 'normal' },
  refused: { label: '拒绝', tone: 'alert' },
  timeout: { label: '超时', tone: 'notice' },
  no_mx: { label: '无 MX', tone: 'neutral' },
  no_address: { label: '无该族地址', tone: 'neutral' },
  unknown: { label: '未知', tone: 'neutral' },
  skipped: { label: '未检测', tone: 'neutral' },
}

export function mailStatus(status: string): { label: string, tone: BadgeTone } {
  return MAIL_STATUS[status as IPQualityMailResult['status']] ?? { label: '未知', tone: 'neutral' }
}

const MAIL_ERROR_LABELS: Readonly<Record<string, string>> = {
  dns_failed: '域名解析失败',
  mx_candidates_truncated: 'MX 候选过多，未全部检查',
  non_public_target: 'MX 指向非公网地址',
  timeout: '超时',
  connection_refused: '连接被拒绝',
  network_unreachable: '网络不可达',
}

export function mailErrorLabel(code: string): string | null {
  const normalized = code.trim()
  if (!normalized) return null
  return MAIL_ERROR_LABELS[normalized] ?? normalized
}

const MAIL_PROVIDER_LABELS: Readonly<Record<string, string>> = {
  gmail: 'Gmail',
  outlook: 'Outlook',
  yahoo: 'Yahoo',
  apple: 'iCloud',
  mailru: 'Mail.ru',
  aol: 'AOL',
  gmx: 'GMX',
  mailcom: 'Mail.com',
  '163': '网易 163',
  sohu: '搜狐',
  sina: '新浪',
  qq: 'QQ 邮箱',
}

export function mailProviderLabel(provider: string): string {
  return MAIL_PROVIDER_LABELS[provider] ?? provider
}

const BLACKLIST_STATUS: Readonly<Record<IPQualityBlacklistResult['status'], { label: string, tone: BadgeTone }>> = {
  listed: { label: '已列入', tone: 'alert' },
  not_listed: { label: '未列入', tone: 'normal' },
  unknown: { label: '未知', tone: 'neutral' },
  unsupported: { label: '不支持', tone: 'neutral' },
}

export function blacklistStatus(status: string): { label: string, tone: BadgeTone } {
  return BLACKLIST_STATUS[status as IPQualityBlacklistResult['status']] ?? { label: '未知', tone: 'neutral' }
}

const BLACKLIST_CATEGORY: Readonly<Record<string, string>> = {
  spam: '垃圾邮件',
  abuse: '滥用',
  network: '恶意网络',
}

export function blacklistCategoryLabel(category: string): string {
  return BLACKLIST_CATEGORY[category] ?? category
}

const BLACKLIST_SOURCE_LABELS: Readonly<Record<string, string>> = {
  spamcop: 'SpamCop',
  dronebl: 'DroneBL',
  'spamhaus-drop': 'Spamhaus DROP',
}

export function blacklistSourceLabel(source: string): string {
  return BLACKLIST_SOURCE_LABELS[source] ?? source
}

// 外链只接受 https；其余形状不渲染为链接。
export function safeReferenceURL(value: string): string | null {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' ? url.toString() : null
  } catch {
    return null
  }
}

const PRODUCT_STATE_LABELS: Readonly<Record<IPQualityProductView['state'], string>> = {
  pending: '查询中',
  running: '查询中',
  complete: '已完成',
  partial: '部分完成',
  failed: '查询失败',
  skipped: '未查询',
}

export function productStateLabel(state: IPQualityProductView['state']): string {
  return PRODUCT_STATE_LABELS[state] ?? state
}

// 补充资料（ASN/注册/黑名单/认证来源）未到达时，模块用短状态说明而不是空白或全 unknown。
export function enrichmentGap(product: IPQualityProductView | null): string {
  if (!product) return '旧版报告无此项'
  if (productEnriching(product)) return '查询中'
  if (product.state === 'failed') return '查询失败'
  if (product.state === 'skipped') return '未查询'
  return '未获取'
}

// 来源失败行的短原因。
export function providerFailureText(row: IPQualityProviderResult): string {
  const status = sourceStatusKind(row)
  if (status === 'skipped') return '未检测'
  if (status === 'not_configured') return '未接入'
  return '查询失败'
}
