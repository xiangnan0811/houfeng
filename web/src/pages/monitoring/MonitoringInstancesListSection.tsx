import { Fragment } from 'react'
import { ColumnResizeHandle, isInteractiveRowTarget, type DataTableColumn, type DataTableSortState } from '../../components/atoms'
import { useColumnWidths } from '../../lib/useColumnWidths'
import { PageState } from '../../components/PageState'
import type { MonitoringInstanceRecord } from '../../lib/types'
import type { MonitoringInstanceQuickView } from './types'
const CHECKBOX_WIDTH = 40
const RESIZABLE_DATA_DEFAULTS = [180, 150, 160, 100, 140] as const
const STORAGE_KEY = 'monitoring-data-cols-v4'


type MonitoringInstancesListSectionProps = {
  quickView: MonitoringInstanceQuickView
  baseMonitoringInstances: MonitoringInstanceRecord[]
  monitoring: MonitoringInstanceRecord[]
  columns: DataTableColumn<MonitoringInstanceRecord>[]
  sortState: DataTableSortState | null
  hasActiveFilters: boolean
  hasSearchQuery: boolean
  navigationLocked: boolean
  refreshing: boolean
  onClearAllFilters: () => void
  onSortChange: (key: string) => void
  onRowClick: (monitoringInstance: MonitoringInstanceRecord) => void
  onOpenVPSInventory: () => void
}

export function MonitoringInstancesListSection({
  quickView,
  baseMonitoringInstances,
  monitoring,
  columns,
  sortState,
  hasActiveFilters,
  hasSearchQuery,
  navigationLocked,
  refreshing,
  onClearAllFilters,
  onSortChange,
  onRowClick,
  onOpenVPSInventory,
}: MonitoringInstancesListSectionProps) {
  const scopedEmpty = !hasActiveFilters && !hasSearchQuery && baseMonitoringInstances.length === 0
  const firstRunEmpty = scopedEmpty && quickView === 'all'
  const bindingConflictEmpty = scopedEmpty && quickView === 'binding-conflict'
  const runtimeAttentionEmpty = scopedEmpty && quickView === 'runtime-attention'
  const abnormalEmpty = scopedEmpty && quickView === 'abnormal'
  const onboardingEmpty = scopedEmpty && quickView === 'onboarding'

  const { widths: resizableWidths, startResize } = useColumnWidths(STORAGE_KEY, RESIZABLE_DATA_DEFAULTS)
  return (
    <>
      {firstRunEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="尚无观测事实"
          description="从未关联 VPS 中选择一台，创建监控实例并接入 agent。"
          action={(
            <button type="button" className="btn md primary" onClick={onOpenVPSInventory}>
              选择未关联 VPS
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
      ) : abnormalEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有已知异常监控实例"
          description="当前没有健康为关注、告警或严重的监控实例。"
        />
      ) : onboardingEmpty ? (
        <PageState
          kind="empty"
          surface="empty"
          title="没有待接入监控实例"
          description="当前没有待接入或绑定待处理的监控实例。"
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
        <div
          className="monitoring-page__table-scroll"
          role="region"
          aria-label="监控实例表"
          tabIndex={0}
          aria-busy={refreshing || undefined}
        >
          <table className="table table--resizable monitoring-table" role="table">
            <colgroup>
              <col width={CHECKBOX_WIDTH} />
              <col width={resizableWidths[0] ?? RESIZABLE_DATA_DEFAULTS[0]} />
              <col width={resizableWidths[1] ?? RESIZABLE_DATA_DEFAULTS[1]} />
              <col width={resizableWidths[2] ?? RESIZABLE_DATA_DEFAULTS[2]} />
              <col width={resizableWidths[3] ?? RESIZABLE_DATA_DEFAULTS[3]} />
              <col width={resizableWidths[4] ?? RESIZABLE_DATA_DEFAULTS[4]} />
              <col />
            </colgroup>
            <thead>
              <tr role="row">
                {columns.map((col, index) => {
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
                          disabled={navigationLocked}
                        >
                          {col.label}
                          <span className="data-table__sort-indicator" aria-hidden="true">
                            {isActive && dir === 'asc' ? ' ↑' : isActive && dir === 'desc' ? ' ↓' : ' ↕'}
                          </span>
                        </button>
                      ) : (
                        col.label
                      )}
                      {index >= 1 && index <= 5 ? (
                        <ColumnResizeHandle onDragStart={(clientX) => startResize(index - 1, clientX)} />
                      ) : null}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {monitoring.map((monitoringInstance, ri) => (
                <Fragment key={monitoringInstance.monitoring_instance_id}>
                  {/* a11y-allow-nonsemantic-click: keyboard-complete-row */}
                  <tr
                    role="row"
                    tabIndex={0}
                    onClick={(e) => {
                      if (isInteractiveRowTarget(e.target)) return
                      onRowClick(monitoringInstance)
                    }}
                    onKeyDown={(e) => {
                      if (isInteractiveRowTarget(e.target)) return
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        onRowClick(monitoringInstance)
                      }
                    }}
                  >
                    {columns.map((col) => (
                      <td
                        key={col.key}
                        role="cell"
                        className={col.cellClassName || undefined}
                      >
                        {col.render(monitoringInstance, ri)}
                      </td>
                    ))}
                  </tr>
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
