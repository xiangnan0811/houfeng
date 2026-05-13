import { DataTable, type DataTableColumn, type DataTableSortState } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import type { NodeRecord } from '../../lib/types'
import { NodesBatchPanel } from './NodesBatchPanel'
import { NodesFilterPanel } from './NodesFilterPanel'
import { NodesRuntimeOverlays } from './NodesRuntimeOverlays'
import type {
  NodeFilterOption,
  NodeFilterState,
  NodeListView,
  PendingNodeConfirmation,
} from './types'

type NodesListSectionProps = {
  nodeListView: NodeListView
  baseNodes: NodeRecord[]
  nodes: NodeRecord[]
  columns: DataTableColumn<NodeRecord>[]
  showTrends: boolean
  sortState: DataTableSortState | null
  hasActiveFilters: boolean
  filterState: NodeFilterState
  groupOptions: NodeFilterOption[]
  regionOptions: NodeFilterOption[]
  cityOptions: NodeFilterOption[]
  providerOptions: NodeFilterOption[]
  labelOptions: NodeFilterOption[]
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
  onSingleFilterChange: (
    key: 'group' | 'region' | 'city' | 'provider' | 'lifecycle' | 'run_status' | 'health',
    value: string | null,
  ) => void
  onMultiFilterChange: (key: 'labels', values: string[]) => void
  onAbnormalFilterChange: (checked: boolean) => void
  onOnboardingFilterChange: (checked: boolean) => void
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
  filterState,
  groupOptions,
  regionOptions,
  cityOptions,
  providerOptions,
  labelOptions,
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
  onSingleFilterChange,
  onMultiFilterChange,
  onAbnormalFilterChange,
  onOnboardingFilterChange,
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
  if (baseNodes.length === 0) {
    return (
      <PageState
        kind="empty"
        surface="empty"
        title={nodeListView === 'binding-conflict' ? '没有绑定异常节点' : '候风尚未接入任何节点'}
        description={
          nodeListView === 'binding-conflict'
            ? '当前没有等待绑定确认的节点。'
            : '请先创建第一个节点，完成接入后再用它支撑 VPS 运行事实。'
        }
        action={
          nodeListView === 'binding-conflict' ? null : (
            <button type="button" className="btn btn--primary btn--md" onClick={onCreateNode}>
              新建第一个节点
            </button>
          )
        }
      />
    )
  }

  const visibleColumns = showTrends
    ? columns
    : columns.filter((column) => column.key !== 'trends')

  return (
    <>
      <NodesFilterPanel
        hasActiveFilters={hasActiveFilters}
        filterState={filterState}
        groupOptions={groupOptions}
        regionOptions={regionOptions}
        cityOptions={cityOptions}
        providerOptions={providerOptions}
        labelOptions={labelOptions}
        onClearAll={onClearAllFilters}
        onSingleFilterChange={onSingleFilterChange}
        onMultiFilterChange={onMultiFilterChange}
        onAbnormalFilterChange={onAbnormalFilterChange}
        onOnboardingFilterChange={onOnboardingFilterChange}
      />

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

      {nodes.length === 0 ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有匹配当前筛选的节点"
          description="请尝试调整筛选条件，或清空筛选恢复完整列表。"
          action={
            <button type="button" className="btn btn--ghost btn--md" onClick={onClearAllFilters}>
              清空筛选
            </button>
          }
        />
      ) : (
        <div className="page-panel page-panel--scroll-x nodes-table-panel">
          <DataTable<NodeRecord>
            columns={visibleColumns}
            rows={nodes}
            rowKey={(node) => node.node_id}
            density="compact"
            className="nodes-table"
            sortState={sortState}
            onSortChange={onSortChange}
            onRowClick={onRowClick}
          />
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
