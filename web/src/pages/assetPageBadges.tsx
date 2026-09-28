import { Badge, StatusGlyph, type BadgeTone, type HealthState } from '../components/atoms'
import type {
  SubscriptionStatus,
  IPQualitySummary,
  VPSLifecycleStatus,
  VPSRenewalDecision,
  VPSUsageStatus,
} from '../lib/types'
import {
  lifecycleLabel,
  renewalLabel,
  subscriptionStatusLabel,
  usageLabel,
} from './assetPageUtils'

function badgeToneForLabel(label: string): BadgeTone {
  if (['管理中', '在用', '承载业务', '保留', '生效中', '正常'].includes(label)) return 'normal'
  if (['测试中', '观察', '测试用途', '未评估', '未确认'].includes(label)) return 'notice'
  if (['待迁移', '迁移', '备用', '维护中', '关注'].includes(label)) return 'maintenance'
  if (
    [
      '待取消',
      '取消',
      '已取消',
      '已取消自动续费',
      '已替换',
      '已归档',
      '已暂停',
      '已过期',
      '告警',
      '严重',
    ].includes(label)
  ) {
    return 'critical'
  }
  return 'neutral'
}

function statusBadge(label: string) {
  return (
    <Badge variant="state" tone={badgeToneForLabel(label)}>
      {label}
    </Badge>
  )
}

function normalizeRiskLevel(value?: string): string {
  const risk = (value ?? '').trim().toLowerCase()
  if (risk === 'low' || risk === 'clean' || risk === 'safe') return '低风险'
  if (risk === 'medium' || risk === 'moderate') return '中风险'
  if (risk === 'high') return '高风险'
  if (risk === 'critical') return '严重风险'
  return risk || '未评级'
}

function ipQualityTone(summary?: IPQualitySummary | null): BadgeTone {
  if (!summary || summary.ambiguous || summary.stale || summary.status !== 'success') return 'notice'
  const risk = (summary.risk_level ?? '').trim().toLowerCase()
  if (risk === 'high') return 'alert'
  if (risk === 'critical') return 'critical'
  if (risk === 'medium' || risk === 'moderate') return 'notice'
  return 'normal'
}

function lifecycleGlyphState(tone: BadgeTone): HealthState {
  if (tone === 'neutral') return 'offline'
  return tone
}

function ipQualityLabel(summary: IPQualitySummary): string {
  if (summary.ambiguous) return 'IP 归属不唯一'
  if (summary.status !== 'success') return 'IP 未完成'
  const suffix = summary.use_region_code || summary.use_region_name
  return `IP ${normalizeRiskLevel(summary.risk_level)}${summary.stale ? ' · 过期' : suffix ? ` · ${suffix}` : ''}`
}

function IPQualityText({ tone, label }: { tone: BadgeTone; label: string }) {
  if (tone === 'neutral') return <span>{label}</span>
  const toneClass = tone === 'critical' || tone === 'alert' ? 'vps-lifecycle--critical' : tone === 'normal' ? '' : 'vps-lifecycle--warn'
  return (
    <span className={`vps-lifecycle ${toneClass}`}>
      <span aria-hidden="true"><StatusGlyph state={lifecycleGlyphState(tone)} size="sm" /></span>
      {label}
    </span>
  )
}

export function IPQualityBadge({
  summary,
  appearance = 'badge',
}: {
  summary?: IPQualitySummary | null
  appearance?: 'badge' | 'text'
}) {
  if (!summary) {
    return <span className="vps-quiet-fact">未采集</span>
  }
  const label = ipQualityLabel(summary)
  const tone = ipQualityTone(summary)
  if (appearance === 'text') return <IPQualityText tone={tone} label={label} />
  return (
    <Badge variant="state" tone={tone}>
      {label}
    </Badge>
  )
}

export function LifecycleBadge({ value }: { value: VPSLifecycleStatus | string }) {
  const label = lifecycleLabel(value)
  const tone = badgeToneForLabel(label)
  return (
    <span className="vps-lifecycle">
      <span aria-hidden="true"><StatusGlyph state={lifecycleGlyphState(tone)} size="sm" /></span>
      {label}
    </span>
  )
}

export function UsageBadge({ value }: { value: VPSUsageStatus | string }) {
  return statusBadge(usageLabel(value))
}

export function RenewalBadge({ value }: { value: VPSRenewalDecision | string }) {
  return statusBadge(renewalLabel(value))
}

export function SubscriptionStatusBadge({ value }: { value: SubscriptionStatus | string }) {
  return statusBadge(subscriptionStatusLabel(value))
}

export function HealthBadge({ value }: { value: string }) {
  return statusBadge(value || '未知')
}

export function AssetLabels({ labels }: { labels: string[] }) {
  if (labels.length === 0) {
    return <span className="empty-inline">无标签</span>
  }
  return (
    <span className="asset-labels">
      {labels.map((label) => (
        <Badge key={label} variant="info" tone="neutral">
          {label}
        </Badge>
      ))}
    </span>
  )
}
