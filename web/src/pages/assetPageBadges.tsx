import { Badge, type BadgeTone } from '../components/atoms'
import type {
  SubscriptionStatus,
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
  if (['在用', '承载业务', '保留', '生效中', '正常'].includes(label)) return 'normal'
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

export function LifecycleBadge({ value }: { value: VPSLifecycleStatus | string }) {
  return statusBadge(lifecycleLabel(value))
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
