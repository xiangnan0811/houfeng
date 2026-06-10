import type { DataTableColumn, DataTableSortState } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { MonitoringInstancesBatchPanel } from './MonitoringInstancesBatchPanel'
import type { MonitoringInstanceListView } from './types'

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

type MonitoringInstancesListSectionProps = {
  monitoringInstanceListView: MonitoringInstanceListView
  baseMonitoringInstances: MonitoringInstanceRecord[]
  monitoring: MonitoringInstanceRecord[]
  batchEligibleMonitoring: MonitoringInstanceRecord[]
  columns: DataTableColumn<MonitoringInstanceRecord>[]
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
  onClearAllFilters: () => void
  onSelectAllChange: (checked: boolean) => void
  onBatchAction: (action: string) => void
  onCommandOpenChange: (open: boolean) => void
  onCommandIDChange: (commandID: string) => void
  onExecuteBatchCommand: () => void
  onConfirmBatchPause: () => void
  onCancelBatchPause: () => void
  onSortChange: (key: string) => void
  onRowClick: (monitoringInstance: MonitoringInstanceRecord) => void
  onOpenVPSInventory: () => void
}

export function MonitoringInstancesListSection({
  monitoringInstanceListView,
  baseMonitoringInstances,
  monitoring,
  batchEligibleMonitoring,
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
  onOpenVPSInventory,
}: MonitoringInstancesListSectionProps) {
  const firstRunEmpty = baseMonitoringInstances.length === 0 && !hasActiveFilters && monitoringInstanceListView === 'all'
  const bindingConflictEmpty =
    baseMonitoringInstances.length === 0 && !hasActiveFilters && monitoringInstanceListView === 'binding-conflict'
  const runtimeAttentionEmpty =
    baseMonitoringInstances.length === 0 && !hasActiveFilters && monitoringInstanceListView === 'runtime-attention'

  const visibleColumns = showTrends
    ? columns
    : columns.filter((column) => column.key !== 'trends')

  return (
    <>
      {firstRunEmpty || !batchPanelVisible ? null : (
        <MonitoringInstancesBatchPanel
          hasActiveFilters={hasActiveFilters}
          filteredMonitoringInstanceCount={batchEligibleMonitoring.length}
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
          title="尚无观测事实"
          description="普通服务器请先创建 VPS，再从 VPS 详情页创建监控实例并接入 agent。"
          action={(
            <button type="button" className="btn md primary" onClick={onOpenVPSInventory}>
              创建第一台 VPS
            </button>
          )}
        />
      ) : bindingConflictEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有绑定异常监控实例"
          description="当前没有等待绑定确认的监控实例。"
        />
      ) : runtimeAttentionEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有维护或暂停监控实例"
          description="当前没有维护中或暂停监控的监控实例。"
        />
      ) : monitoring.length === 0 ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有匹配当前筛选的监控实例"
          description="请尝试调整筛选条件，或清空筛选恢复完整列表。"
          action={
            <button type="button" className="btn md ghost" onClick={onClearAllFilters}>
              清空筛选
            </button>
          }
        />
      ) : (
        <div className="page-panel page-panel--scroll-x monitoring-table-panel">
          <table className="table animate-in d2 monitoring-table" role="table">
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
              {monitoring.map((monitoringInstance, ri) => (
                <tr
                  key={monitoringInstance.monitoring_instance_id}
                  role="row"
                  tabIndex={0}
                  onClick={(e) => {
                    if (isInteractive(e.target)) return
                    onRowClick(monitoringInstance)
                  }}
                  onKeyDown={(e) => {
                    if (isInteractive(e.target)) return
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onRowClick(monitoringInstance)
                    }
                  }}
                >
                  {visibleColumns.map((col) => (
                    <td
                      key={col.key}
                      role="cell"
                      className={col.cellClassName || undefined}
                    >
                      {col.render(monitoringInstance, ri)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

    </>
  )
}
