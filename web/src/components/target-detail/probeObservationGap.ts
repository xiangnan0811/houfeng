import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

export type ProbeLatencyGapKind = 'unconfigured' | 'disabled' | 'no_observation' | 'failure'

export type ProbeLatencyGap = {
  kind: ProbeLatencyGapKind
  title: string
  description: string
}

export function describeProbeLatencyGap(
  probeItems: ProbeItemRecord[],
  observations: ProbeObservation[],
  timeWindow: string,
): ProbeLatencyGap {
  const timeWindowLabel = `近 ${timeWindow}`
  if (probeItems.length === 0) {
    return {
      kind: 'unconfigured',
      title: `${timeWindowLabel} 尚未配置探测`,
      description: '添加至少一种 ProbeItem 后才会产生延迟样本。',
    }
  }

  const enabled = probeItems.filter((item) => item.enabled)
  if (enabled.length === 0) {
    return {
      kind: 'disabled',
      title: `${timeWindowLabel} 探测已停用`,
      description: '所有 ProbeItem 当前均已停用，启用后才会采集延迟。',
    }
  }

  const enabledIds = new Set(enabled.map((item) => item.probe_item_id))
  const relevant = observations.filter((observation) => enabledIds.has(observation.probe_item_id))
  if (relevant.length === 0) {
    return {
      kind: 'no_observation',
      title: `${timeWindowLabel} 尚无观测`,
      description: '已启用探测，该时间窗口内还没有观测到达。',
    }
  }

  return {
    kind: 'failure',
    title: `${timeWindowLabel} 无可用延迟样本`,
    description: '窗口内有观测，但没有带 latency_ms 的成功样本。',
  }
}

export function probeItemObservationEmptyCopy(enabled: boolean): { meta: string; body: string } {
  if (!enabled) {
    return { meta: '探测已停用', body: '探测已停用' }
  }
  return { meta: '尚无观测结果', body: '尚无观测' }
}
