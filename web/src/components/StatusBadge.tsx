import { Badge, type BadgeTone } from './atoms'

export type StatusBadgeTone = 'cyan' | 'green' | 'yellow' | 'red' | 'slate'

const TONE_MAP: Record<StatusBadgeTone, BadgeTone> = {
  cyan: 'neutral',
  green: 'normal',
  yellow: 'notice',
  red: 'critical',
  slate: 'offline',
}

type StatusBadgeProps = {
  label: string
  tone?: StatusBadgeTone
}

function inferTone(label: string): StatusBadgeTone {
  if (['正常', '启用', '已绑定', '在用'].includes(label)) return 'green'
  if (['异常', '严重', 'P1', 'P2', 'critical', 'severe'].includes(label)) return 'red'
  if (['维护中', '观察中', '待接入'].includes(label)) return 'yellow'
  if (['暂停', '已归档', '已退役', '不续费', '指纹变更待确认'].includes(label)) {
    return 'red'
  }
  if (['service', 'china_reference'].includes(label)) return 'cyan'
  return 'slate'
}

/**
 * v2: Thin wrapper around the Badge atom. The legacy `tone` API
 * (cyan/green/yellow/red/slate) is preserved for callers; internally
 * mapped to the BadgeTone vocabulary so the v2 visual language applies.
 */
export function StatusBadge({ label, tone = inferTone(label) }: StatusBadgeProps) {
  const mapped = TONE_MAP[tone]
  return (
    <Badge variant="state" tone={mapped} className="status-badge">
      {label}
    </Badge>
  )
}
