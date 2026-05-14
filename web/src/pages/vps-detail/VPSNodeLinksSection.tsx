import { Badge, Button, DataTable, Hostname, MonoDigits, Timestamp, type DataTableColumn } from '../../components/atoms'
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
  const nodeColumns: DataTableColumn<VPSNodeSummary>[] = [
    {
      key: 'node',
      label: 'Node',
      render: (node) => (
        <div className="asset-table__identity">
          <strong>{node.display_name}</strong>
          <span>{node.node_id}</span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置 / Provider Hint',
      render: (node) => (
        <div className="asset-table__stack">
          <strong>{[node.region, node.city].filter(Boolean).join(' · ') || '—'}</strong>
          <span>{formatOptional(node.provider)}</span>
        </div>
      ),
    },
    {
      key: 'health',
      label: '监控状态',
      render: (node) => (
        <span className="asset-status-stack">
          <HealthBadge value={node.current_health_status} />
          <Badge variant="info" tone="neutral">{node.monitoring_status || '未知'}</Badge>
        </span>
      ),
    },
    {
      key: 'issue',
      label: '异常摘要',
      render: (node) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{node.current_active_incident_count}</MonoDigits> 个活跃异常</strong>
          <span>{formatOptional(node.current_primary_issue_summary)}</span>
        </div>
      ),
    },
    {
      key: 'heartbeat',
      label: '最近心跳',
      render: (node) => <Timestamp value={node.last_heartbeat_at} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (node) => (
        <Button
          variant="ghost"
          size="sm"
          disabled={unlinkingNodeId !== null}
          onClick={() => onUnlinkNode(node)}
        >
          {unlinkingNodeId === node.node_id ? '解除中…' : '解除关联'}
        </Button>
      ),
    },
  ]

  return (
    <section className="page-panel page-panel--scroll-x">
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
      {nodes.length > 0 ? (
        <div className="vps-node-evidence-strip" aria-label="Node evidence summary">
          {nodes.slice(0, 3).map((node) => (
            <article key={node.node_id} className="vps-node-evidence-strip__item">
              <div>
                <strong>{node.display_name}</strong>
                <Hostname truncate>{node.node_id}</Hostname>
              </div>
              <HealthBadge value={node.current_health_status} />
              <span><MonoDigits>{node.current_active_incident_count}</MonoDigits> 个活跃异常</span>
              <small>{formatOptional(node.current_primary_issue_summary)}</small>
            </article>
          ))}
        </div>
      ) : null}
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
      <DataTable
        className="asset-table vps-node-table"
        columns={nodeColumns}
        rows={nodes}
        rowKey={(node) => node.node_id}
        emptyContent={<span className="empty-inline">尚未关联 Node</span>}
      />
    </section>
  )
}
