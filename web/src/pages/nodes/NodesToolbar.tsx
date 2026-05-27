import { Link } from 'react-router-dom'

import { MonoDigits, Tabs, type TabItem } from '../../components/atoms'
import { AUTO_REFRESH_OPTIONS, type AutoRefreshOption } from '../../lib/useAutoRefresh'
import type { NodeQuickView } from './types'

type NodesToolbarProps = {
  quickViewTabs: TabItem<NodeQuickView>[]
  activeQuickView: NodeQuickView
  displayedCount: number
  baseCount: number
  fieldFilterCount: number
  hasActiveFilters: boolean
  batchPanelOpen: boolean
  showTrends: boolean
  compareSet: Set<string>
  autoRefresh: AutoRefreshOption
  onQuickViewChange: (value: NodeQuickView) => void
  onOpenFilters: () => void
  onToggleBatchPanel: () => void
  onShowTrendsChange: (value: boolean) => void
  onAutoRefreshChange: (value: AutoRefreshOption) => void
}

export function NodesToolbar({
  quickViewTabs,
  activeQuickView,
  displayedCount,
  baseCount,
  fieldFilterCount,
  hasActiveFilters,
  batchPanelOpen,
  showTrends,
  compareSet,
  autoRefresh,
  onQuickViewChange,
  onOpenFilters,
  onToggleBatchPanel,
  onShowTrendsChange,
  onAutoRefreshChange,
}: NodesToolbarProps) {
  return (
    <div className="filter-panel animate-in d1" aria-label="节点列表工具栏">
      <Tabs<NodeQuickView>
        variant="pill"
        items={quickViewTabs}
        value={activeQuickView}
        onChange={onQuickViewChange}
      />
      <div className="filter-bar">
        <span className="filter-bar__scope">
          {hasActiveFilters ? '当前范围' : '全部'}
          {' '}
          <MonoDigits>{displayedCount}</MonoDigits>/<MonoDigits>{baseCount}</MonoDigits>
        </span>
        <button type="button" className="btn sm secondary" onClick={onOpenFilters}>
          高级筛选{fieldFilterCount > 0 ? ` · ${fieldFilterCount}` : ''}
        </button>
        <button
          type="button"
          className={`btn sm ${batchPanelOpen ? 'secondary' : 'ghost'}`}
          onClick={onToggleBatchPanel}
        >
          批量操作
        </button>
        <button
          type="button"
          className={`btn sm ghost`}
          onClick={() => onShowTrendsChange(!showTrends)}
        >
          {showTrends ? '隐藏趋势' : '显示趋势'}
        </button>
        {compareSet.size === 2 ? (
          <Link
            className="btn sm secondary"
            to={`/nodes/compare?id=${[...compareSet].join('&id=')}`}
          >
            对比选中节点
          </Link>
        ) : (
          <span style={{ fontSize: '11px', color: 'var(--t4)' }}>选择 2 个节点可对比</span>
        )}
        <label style={{ fontSize: '11px', color: 'var(--t3)', display: 'flex', alignItems: 'center', gap: '4px', marginLeft: '8px' }}>
          <span>自动刷新</span>
          <select
            className="filter-select"
            value={autoRefresh == null ? '' : String(autoRefresh)}
            onChange={(event) => {
              const value = event.target.value
              onAutoRefreshChange(value === '' ? null : Number(value))
            }}
            aria-label="自动刷新间隔"
          >
            {AUTO_REFRESH_OPTIONS.map((option) => (
              <option key={option.label} value={option.value == null ? '' : String(option.value)}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>
    </div>
  )
}
