import type { VPSOverviewAnomaly, VPSOverviewSectionState } from '../../lib/types'

export const HEARTBEAT_STALE_REASON = 'monitoring_heartbeat_stale'
const HEALTH_RULE = 'monitoring.health.abnormal.v1'
const INCIDENTS_RULE = 'monitoring.incidents.open.v1'

/** 概览监控分区因心跳超时而陈旧时，返回最后一次可信在线时间；否则为 null。 */
export function heartbeatLostSince(section: VPSOverviewSectionState | undefined): string | null {
  if (section?.reason_code !== HEARTBEAT_STALE_REASON) return null
  return section.last_success_at ?? section.observed_at ?? null
}

/**
 * 心跳中断时，“监控健康异常”与同源的“存在未关闭事件”说的是同一件事：
 * 合并为一条失联提示，被合并的事件项原样返回，由调用方以其自身规则渲染入口，
 * 保持动作按 rule_id 校验的允许清单不变。返回新数组，不修改入参；没有健康异常项时原样返回。
 */
export function foldHeartbeatLoss(anomalies: VPSOverviewAnomaly[]): {
  anomalies: VPSOverviewAnomaly[]
  lostRuleId: string | null
  foldedIncidents: VPSOverviewAnomaly | null
} {
  if (!anomalies.some((anomaly) => anomaly.rule_id === HEALTH_RULE)) {
    return { anomalies, lostRuleId: null, foldedIncidents: null }
  }
  return {
    anomalies: anomalies.filter((anomaly) => anomaly.rule_id !== INCIDENTS_RULE),
    lostRuleId: HEALTH_RULE,
    foldedIncidents: anomalies.find((anomaly) => anomaly.rule_id === INCIDENTS_RULE) ?? null,
  }
}
