import { Link } from 'react-router-dom'

import { StatusBadge } from '../../components/StatusBadge'
import {
  type DataTableColumn,
  Hostname,
  MonoDigits,
  StatusGlyph,
  Timestamp,
} from '../../components/atoms'
import type { NodeRecord, NodeSparklinesResponse } from '../../lib/types'
import {
  isBindingConflictNode,
  NODE_BINDING_CONFLICT_STATUS,
  NODE_BINDING_CONFLICT_SUMMARY,
  nodeGlyphState,
} from './nodeHelpers'
import { NodesActionsCell } from './NodesActionsCell'
import { NodesLabelsCell } from './NodesLabelsCell'
import { NodesTrendCell } from './NodesTrendCell'
import type { NodeRuntimeAction } from './types'

type BuildNodesTableColumnsArgs = {
  compareSet: Set<string>
  sparklines: NodeSparklinesResponse | null
  editingLabelNodeId: string | null
  labelDraft: string
  groupDraft: string
  metadataBusyNodeId: string | null
  metadataErrors: Record<string, string>
  runtimeBusyNodeId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onToggleCompare: (nodeId: string) => void
  onLabelDraftChange: (value: string) => void
  onGroupDraftChange: (value: string) => void
  onSaveLabels: (node: NodeRecord) => void
  onCancelLabels: (node: NodeRecord) => void
  onStartLabelEdit: (node: NodeRecord) => void
  onRuntimeAction: (node: NodeRecord, action: NodeRuntimeAction) => void
  onQueueFocusRestore: (nodeId: string, action: NodeRuntimeAction) => void
}

export function buildNodesTableColumns({
  compareSet,
  sparklines,
  editingLabelNodeId,
  labelDraft,
  groupDraft,
  metadataBusyNodeId,
  metadataErrors,
  runtimeBusyNodeId,
  actionButtonRefs,
  onToggleCompare,
  onLabelDraftChange,
  onGroupDraftChange,
  onSaveLabels,
  onCancelLabels,
  onStartLabelEdit,
  onRuntimeAction,
  onQueueFocusRestore,
}: BuildNodesTableColumnsArgs): DataTableColumn<NodeRecord>[] {
  return [
    {
      key: 'compare',
      label: '',
      width: 28,
      align: 'center',
      render: (node) => {
        const checked = compareSet.has(node.node_id)
        const disabled = !checked && compareSet.size >= 2
        return (
          <input
            type="checkbox"
            className="nodes-table__compare-check"
            checked={checked}
            disabled={disabled}
            onChange={() => onToggleCompare(node.node_id)}
            onClick={(event) => event.stopPropagation()}
            aria-label={`选择 ${node.display_name} 进行对比`}
          />
        )
      },
    },
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (node) => (
        <StatusGlyph
          state={nodeGlyphState(node)}
          size="md"
          ariaLabel={`${node.display_name} 健康 ${node.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '节点',
      sortable: true,
      render: (node) => (
        <div className="nodes-table__identity">
          <Hostname truncate maxChars={14} className="nodes-table__id">
            {node.node_id}
          </Hostname>
          <Link
            className="text-link nodes-table__name"
            to={`/nodes/${node.node_id}`}
            onClick={(event) => event.stopPropagation()}
          >
            {node.display_name}
          </Link>
          <span className="nodes-table__freshness">
            心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" />
            {node.last_sync_at ? <> · 同步 <Timestamp value={node.last_sync_at} mode="relative" /></> : null}
          </span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置',
      sortable: true,
      render: (node) => (
        <span className="nodes-table__location">
          {[node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ') || '—'}
        </span>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (node) => (
        <NodesLabelsCell
          node={node}
          editing={editingLabelNodeId === node.node_id}
          labelDraft={labelDraft}
          groupDraft={groupDraft}
          metadataBusyNodeId={metadataBusyNodeId}
          metadataError={metadataErrors[node.node_id]}
          onLabelDraftChange={onLabelDraftChange}
          onGroupDraftChange={onGroupDraftChange}
          onSaveLabels={onSaveLabels}
          onCancelLabels={onCancelLabels}
        />
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      sortable: true,
      render: (node) => {
        const summary = isBindingConflictNode(node)
          ? NODE_BINDING_CONFLICT_SUMMARY
          : node.current_primary_issue_summary || '暂无明显异常'
        return (
          <div className="nodes-table__issue">
            <MonoDigits className="nodes-table__issue-count">
              {node.current_active_incident_count}
            </MonoDigits>
            <span className="nodes-table__issue-summary">{summary}</span>
            {isBindingConflictNode(node) ? (
              <StatusBadge label={NODE_BINDING_CONFLICT_STATUS} />
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'trends',
      label: '近 24h',
      cellClassName: 'nodes-table__trends',
      render: (node) => <NodesTrendCell node={node} sparklines={sparklines} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (node) => (
        <NodesActionsCell
          node={node}
          editingLabelNodeId={editingLabelNodeId}
          metadataBusyNodeId={metadataBusyNodeId}
          runtimeBusyNodeId={runtimeBusyNodeId}
          actionButtonRefs={actionButtonRefs}
          onStartLabelEdit={onStartLabelEdit}
          onRuntimeAction={onRuntimeAction}
          onQueueFocusRestore={onQueueFocusRestore}
        />
      ),
    },
  ]
}
