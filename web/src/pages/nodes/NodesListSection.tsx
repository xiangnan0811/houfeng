import type { DataTableColumn, DataTableSortState } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import type { NodeRecord } from '../../lib/types'
import { NodesBatchPanel } from './NodesBatchPanel'
import { NodesRuntimeOverlays } from './NodesRuntimeOverlays'
import type {
  NodeListView,
  PendingNodeConfirmation,
} from './types'

const INTERACTIVE_SELECTOR = [
  'a[href]',
  'button',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="link"]',
].join(',')

function isInteractive(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(INTERACTIVE_SELECTOR) != null
}

type NodesListSectionProps = {
  nodeListView: NodeListView
  baseNodes: NodeRecord[]
  nodes: NodeRecord[]
  columns: DataTableColumn<NodeRecord>[]
  showTrends: boolean
  sortState: DataTableSortState | null
  hasActiveFilters: boolean
  batchPanelVisible: boolean
  selectAll: boolean
  batchSubmitting: boolean
  batchError: string | null
  commandOpen: boolean
  commandID: string
  pendingBatchAction: string | null
  runtimeErrors: Record<string, string>
  pendingConfirmation: PendingNodeConfirmation | null
  runtimeBusyNodeId: string | null
  onClearAllFilters: () => void
  onSelectAllChange: (checked: boolean) => void
  onBatchAction: (action: string) => void
  onCommandOpenChange: (open: boolean) => void
  onCommandIDChange: (commandID: string) => void
  onExecuteBatchCommand: () => void
  onConfirmBatchPause: () => void
  onCancelBatchPause: () => void
  onSortChange: (key: string) => void
  onRowClick: (node: NodeRecord) => void
  onConfirmPause: (node: NodeRecord) => void
  onCancelPause: (node: NodeRecord) => void
  onCreateNode: () => void
}

export function NodesListSection({
  nodeListView,
  baseNodes,
  nodes,
  columns,
  showTrends,
  sortState,
  hasActiveFilters,
  batchPanelVisible,
  selectAll,
  batchSubmitting,
  batchError,
  commandOpen,
  commandID,
  pendingBatchAction,
  runtimeErrors,
  pendingConfirmation,
  runtimeBusyNodeId,
  onClearAllFilters,
  onSelectAllChange,
  onBatchAction,
  onCommandOpenChange,
  onCommandIDChange,
  onExecuteBatchCommand,
  onConfirmBatchPause,
  onCancelBatchPause,
  onSortChange,
  onRowClick,
  onConfirmPause,
  onCancelPause,
  onCreateNode,
}: NodesListSectionProps) {
  const firstRunEmpty = baseNodes.length === 0 && !hasActiveFilters && nodeListView === 'all'
  const bindingConflictEmpty =
    baseNodes.length === 0 && !hasActiveFilters && nodeListView === 'binding-conflict'
  const runtimeAttentionEmpty =
    baseNodes.length === 0 && !hasActiveFilters && nodeListView === 'runtime-attention'

  const visibleColumns = showTrends
    ? columns
    : columns.filter((column) => column.key !== 'trends')

  return (
    <>
      {firstRunEmpty || !batchPanelVisible ? null : (
        <NodesBatchPanel
          hasActiveFilters={hasActiveFilters}
          filteredNodeCount={nodes.length}
          selectAll={selectAll}
          batchSubmitting={batchSubmitting}
          batchError={batchError}
          commandOpen={commandOpen}
          commandID={commandID}
          pendingBatchAction={pendingBatchAction}
          onSelectAllChange={onSelectAllChange}
          onBatchAction={onBatchAction}
          onCommandOpenChange={onCommandOpenChange}
          onCommandIDChange={onCommandIDChange}
          onExecuteBatchCommand={onExecuteBatchCommand}
          onConfirmBatchPause={onConfirmBatchPause}
          onCancelBatchPause={onCancelBatchPause}
        />
      )}

      {firstRunEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="候风尚未接入任何节点"
          description="请先创建第一个节点，完成接入后再用它支撑 VPS 运行事实。"
          action={(
            <button type="button" className="btn md primary" onClick={onCreateNode}>
              新建第一个节点
            </button>
          )}
        />
      ) : bindingConflictEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有绑定异常节点"
          description="当前没有等待绑定确认的节点。"
        />
      ) : runtimeAttentionEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有维护或暂停节点"
          description="当前没有维护中或暂停监控的节点。"
        />
      ) : nodes.length === 0 ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有匹配当前筛选的节点"
          description="请尝试调整筛选条件，或清空筛选恢复完整列表。"
          action={
            <button type="button" className="btn md ghost" onClick={onClearAllFilters}>
              清空筛选
            </button>
          }
        />
      ) : (
        <div className="page-panel page-panel--scroll-x nodes-table-panel">
          <table className="table animate-in d2 nodes-table" role="table">
            <colgroup>
              {visibleColumns.map((col) => (
                <col
                  key={col.key}
                  style={col.width ? { width: typeof col.width === 'number' ? `${col.width}px` : col.width } : undefined}
                />
              ))}
            </colgroup>
            <thead>
              <tr role="row">
                {visibleColumns.map((col) => {
                  const isSortable = col.sortable && onSortChange
                  const sortKey = col.sortKey ?? col.key
                  const isActive = sortState?.key === sortKey
                  const dir = isActive ? sortState?.direction : null
                  return (
                    <th
                      key={col.key}
                      role="columnheader"
                      scope="col"
                      aria-sort={isActive ? (dir === 'asc' ? 'ascending' : 'descending') : undefined}
                    >
                      {isSortable ? (
                        <button
                          type="button"
                          className="data-table__sort-btn"
                          onClick={() => onSortChange(sortKey)}
                        >
                          {col.label}
                          <span className="data-table__sort-indicator" aria-hidden="true">
                            {isActive && dir === 'asc' ? ' ↑' : isActive && dir === 'desc' ? ' ↓' : ' ↕'}
                          </span>
                        </button>
                      ) : (
                        col.label
                      )}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {nodes.map((node, ri) => (
                <tr
                  key={node.node_id}
                  role="row"
                  tabIndex={0}
                  onClick={(e) => {
                    if (isInteractive(e.target)) return
                    onRowClick(node)
                  }}
                  onKeyDown={(e) => {
                    if (isInteractive(e.target)) return
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onRowClick(node)
                    }
                  }}
                >
                  {visibleColumns.map((col) => (
                    <td
                      key={col.key}
                      role="cell"
                      className={col.cellClassName || undefined}
                    >
                      {col.render(node, ri)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <NodesRuntimeOverlays
        nodes={nodes}
        runtimeErrors={runtimeErrors}
        pendingConfirmation={pendingConfirmation}
        runtimeBusyNodeId={runtimeBusyNodeId}
        onConfirmPause={onConfirmPause}
        onCancelPause={onCancelPause}
      />
    </>
  )
}
