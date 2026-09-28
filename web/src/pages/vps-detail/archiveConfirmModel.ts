import { formatDateTime } from '../../lib/format'
import type { ArchiveReview } from '../../lib/types'

export const NEVER_CONNECTED_EXPLANATION = '这台 VPS 从未接入过 Agent，不用等待 180 分钟安全观察。'
export const NEVER_CONNECTED_CHECKBOX_LABEL = '这台 VPS 从未接入过 Agent。我已确认它不再使用。'
export const ARCHIVE_IMPACT_NOTE = '只停止明确专属于这台 VPS 的探测；共享或归属不明确的探测会保留，并生成待核对事项。共享的服务和域名对象本身保留，只结束与这台 VPS 的关联。账单事实继续保留，服务商自动续费需要另外核对。运行异常按管理动作关闭，不发送自然恢复通知。'

const IMPACT_DUPLICATE_WARNINGS = new Set([
  '没有服务关联。',
  '没有域名关联。',
])
const NEVER_CONNECTED_WARNING_SNIPPET = '从未形成有效 Agent 会话'

type OnlineEvidence = NonNullable<ArchiveReview['online_evidence']>

export function skipsArchiveObservation(evidence: ArchiveReview['online_evidence']): boolean {
  // Never-connected objects return before the 180-minute window. earliest_archive_at
  // is null because observation does not apply, not because the archive is still blocked.
  return Boolean(evidence && (evidence.never_connected || evidence.manual_confirmation_required))
}

export function canConfirmArchive(review: ArchiveReview | null, loading: boolean): boolean {
  if (loading || !review) return false
  if ((review.blocker_details?.length ?? 0) > 0) return false
  if (review.blockers.length > 0) return false
  if (!review.eligible) return false
  return true
}

export function archiveOutcomeCopy(displayName: string) {
  const name = displayName.trim() || '这台 VPS'
  return {
    lead: `「${name}」将结束使用并归档。生命周期变为已归档，并记录归档时间。之后不会作为活跃 VPS 进入续费、迁移或成本核对。`,
    aside: '不会删除这台 VPS、订阅、监控实例关联或历史。恢复后必须重新接入监控。',
  }
}

export type ArchiveImpactRow = { label: string; value: string }

function joinNames(names: readonly string[], empty: string): string {
  const present = names.map((name) => name.trim()).filter(Boolean)
  return present.length > 0 ? present.join('、') : empty
}

export function archiveImpactRows(review: ArchiveReview): ArchiveImpactRow[] {
  const monitors = review.monitoring_instance_links
    .filter((item) => item.lifecycle_status !== '已退役')
    .map((item) => item.display_name?.trim() || item.monitoring_instance_id)
  return [
    { label: '退役当前监控', value: joinNames(monitors, '无当前实例') },
    { label: '结束服务关联', value: joinNames(review.services.map((item) => item.name), '无') },
    { label: '结束域名关联', value: joinNames(review.domains.map((item) => item.domain_name), '无') },
    { label: '涉及探测', value: joinNames(review.target_links.map((item) => item.name), '无') },
  ]
}

export function archiveInformationalWarnings(warnings: readonly string[], neverConnected: boolean): string[] {
  return warnings.filter((warning) => {
    if (IMPACT_DUPLICATE_WARNINGS.has(warning)) return false
    if (neverConnected && warning.includes(NEVER_CONNECTED_WARNING_SNIPPET)) return false
    return true
  })
}

function evidenceTime(value: string | null | undefined, empty: string): string {
  if (!value) return empty
  return formatDateTime(value)
}

export function connectedEvidenceLines(
  evidence: ArchiveReview['online_evidence'],
  links: ArchiveReview['monitoring_instance_links'],
): string[] {
  if (!evidence || skipsArchiveObservation(evidence)) return []
  const lines = [
    receiverLine(evidence),
    evidence.earliest_archive_at
      ? `最早可在 ${formatDateTime(evidence.earliest_archive_at)} 归档。`
      : '最早可归档时间尚未确定。',
  ]
  for (const instance of evidence.instances) {
    const link = links.find((item) => item.monitoring_instance_id === instance.monitoring_instance_id)
    const name = link?.display_name?.trim() || instance.monitoring_instance_id
    const session = instance.session_id ? `，会话 ${instance.session_id}` : ''
    const online = evidenceTime(instance.last_trusted_online_at, '尚未收到')
    lines.push(`${name}${session}，最后可信在线 ${online}。`)
  }
  return lines
}

function receiverLine(evidence: OnlineEvidence): string {
  if (evidence.receiver_healthy && evidence.healthy_since) {
    return `接收链路持续健康，自 ${formatDateTime(evidence.healthy_since)} 起。`
  }
  if (evidence.receiver_healthy) return '接收链路当前健康，还没有连续观察起点。'
  return '接收链路健康观察不足。'
}
