import { Badge, Button, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSNodeSummary } from '../../lib/types'
import { HealthBadge } from '../assetPageBadges'

type VPSNodeLinksSectionProps = {
  nodes: VPSNodeSummary[]
  unlinkingNodeId: string | null
  linkFeedback: string | null
  linkFeedbackIsError: boolean
  onOpenLink: () => void
  onUnlinkNode: (node: VPSNodeSummary) => void
}

export function VPSNodeLinksSection({
  nodes,
  unlinkingNodeId,
  linkFeedback,
  linkFeedbackIsError,
  onOpenLink,
  onUnlinkNode,
}: VPSNodeLinksSectionProps) {
  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">NODE EVIDENCE</p>
          <h2>Node 观测证据</h2>
          <p className="section-heading__description">
            关联 Node 监控用于解释续费决策，只使用 VPS detail contract 返回的 linked Node health、心跳、异常数量和主问题摘要。
          </p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{nodes.length}</MonoDigits> 个 active link
        </span>
        <Button variant="secondary" size="sm" onClick={onOpenLink}>关联 Node</Button>
      </div>
      {linkFeedback ? (
        <p
          className={[
            'asset-operation-feedback',
            linkFeedbackIsError && 'asset-operation-feedback--error',
          ].filter(Boolean).join(' ')}
          role={linkFeedbackIsError ? 'alert' : 'status'}
        >
          {linkFeedback}
        </p>
      ) : null}
      {nodes.length > 0 ? (
        <div className="vps-node-evidence-strip" aria-label="Node evidence summary">
          {nodes.map((node) => (
            <article key={node.node_id} className="vps-node-evidence-strip__item">
              <div>
                <strong>{node.display_name}</strong>
                <Hostname truncate>{node.node_id}</Hostname>
              </div>
              <HealthBadge value={node.current_health_status} />
              <span className="asset-status-stack">
                <Badge variant="info" tone="neutral">{node.monitoring_status || '未知'}</Badge>
              </span>
              <span><MonoDigits>{node.current_active_incident_count}</MonoDigits> 个活跃异常</span>
              <small>{formatOptional(node.current_primary_issue_summary)}</small>
              <div className="vps-node-evidence-strip__location">
                <span>{[node.region, node.city].filter(Boolean).join(' · ') || '—'}</span>
                <span>{formatOptional(node.provider)}</span>
              </div>
              <div className="vps-node-evidence-strip__heartbeat">
                <span>最近心跳</span>
                <Timestamp value={node.last_heartbeat_at} />
              </div>
              <div className="vps-node-evidence-strip__actions">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={unlinkingNodeId !== null}
                  onClick={() => onUnlinkNode(node)}
                >
                  {unlinkingNodeId === node.node_id ? '解除中…' : '解除关联'}
                </Button>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <p className="empty-inline">尚未关联 Node</p>
      )}
    </section>
  )
}
