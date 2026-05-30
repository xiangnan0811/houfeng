import type { ProbeItemRecord, ProbeObservation, TargetRecord } from '../../lib/types'

export type TargetDecisionTone = 'normal' | 'notice' | 'alert' | 'critical'

export type TargetEvidence = {
  label: string
  value: string
  meta: string
  tone: TargetDecisionTone
}

export type TargetNextAction = {
  title: string
  summary: string
  tone: TargetDecisionTone
  onAction?: () => void
  buttonLabel?: string
}

export function toneToGlyphState(tone: TargetDecisionTone) {
  if (tone === 'critical') return 'critical' as const
  if (tone === 'alert') return 'alert' as const
  if (tone === 'notice') return 'notice' as const
  return 'normal' as const
}

type BuildTargetDecisionModelInput = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  recentObservations: ProbeObservation[]
  latestObservationAt: string | null
  latencySampleCount: number
  onOpenHistory: () => void
}

export function buildTargetDecisionModel(input: BuildTargetDecisionModelInput): {
  nextAction: TargetNextAction
  evidenceItems: TargetEvidence[]
} {
  const { target, probeItems, latestObservationAt, latencySampleCount, onOpenHistory } = input
  const enabledCount = probeItems.filter((p) => p.enabled).length
  const total = probeItems.length

  const healthTone: TargetDecisionTone =
    target.current_health_status === '严重'
      ? 'critical'
      : target.current_health_status === '告警'
        ? 'alert'
        : target.current_health_status === '关注'
          ? 'notice'
          : 'normal'

  const incidentCount = target.current_active_incident_count
  const incidentTone: TargetDecisionTone =
    incidentCount > 0
      ? target.current_health_status === '严重'
        ? 'critical'
        : 'alert'
      : 'normal'

  const evidenceItems: TargetEvidence[] = [
    {
      label: '健康状态',
      value: target.current_health_status,
      meta:
        target.run_status === '维护中'
          ? '目标处于维护中，健康状态仍按后端当前判定展示。'
          : `运行状态：${target.run_status}`,
      tone: healthTone,
    },
    {
      label: 'ProbeItem 覆盖',
      value: `${enabledCount}/${total}`,
      meta:
        total === 0
          ? '尚未配置观测方式。'
          : `启用 ${enabledCount} 条，停用 ${total - enabledCount} 条。`,
      tone: total === 0 ? 'notice' : 'normal',
    },
    {
      label: '活跃异常',
      value: incidentCount > 0 ? `${incidentCount} 个` : '无',
      meta: target.current_primary_issue_summary || '当前没有活跃异常',
      tone: incidentTone,
    },
    {
      label: '最近观测',
      value: latestObservationAt ? '有观测' : '暂无观测',
      meta:
        latencySampleCount > 0
          ? `latency 样本 ${latencySampleCount} 个`
          : '暂无 latency_ms 样本',
      tone: latencySampleCount === 0 ? 'notice' : 'normal',
    },
  ]

  const nextAction = buildNextAction(target, probeItems, onOpenHistory)

  return { nextAction, evidenceItems }
}

function buildNextAction(
  target: TargetRecord,
  probeItems: ProbeItemRecord[],
  onOpenHistory: () => void,
): TargetNextAction {
  if (target.run_status === '已归档') {
    return {
      title: '目标已归档',
      summary: '已归档目标仅供追溯，如需恢复请在维护区操作。',
      tone: 'notice',
    }
  }
  if (target.current_active_incident_count > 0) {
    return {
      title: '处理活跃异常',
      summary: target.current_primary_issue_summary || '当前存在活跃异常，请核查事件历史。',
      tone: target.current_health_status === '严重' ? 'critical' : 'alert',
      onAction: onOpenHistory,
      buttonLabel: '查看事件历史',
    }
  }
  if (probeItems.length === 0) {
    return {
      title: '配置观测方式',
      summary: '尚未配置 ProbeItem，目标缺少观测证据。',
      tone: 'notice',
    }
  }
  if (target.run_status === '维护中') {
    return {
      title: '维护中',
      summary: '目标处于维护，健康判定按后端当前结果展示。',
      tone: 'notice',
    }
  }
  return {
    title: '保持观察',
    summary: '无活跃异常，观测正常，继续保持监控即可。',
    tone: 'normal',
  }
}
