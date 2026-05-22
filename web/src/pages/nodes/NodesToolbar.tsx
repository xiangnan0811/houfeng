import { Link } from 'react-router-dom'

import { Button, MonoDigits, Tabs, type TabItem } from '../../components/atoms'
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
    <div className="nodes-toolbar list-command-band list-command-band--nodes" aria-label="节点列表工具栏">
      <div className="list-command-band__main">
        <p className="list-command-band__eyebrow">NODE QUICK VIEW</p>
        <h3 className="list-command-band__title">节点扫描</h3>
      </div>
      <div className="nodes-toolbar__primary list-command-band__controls">
        <Tabs<NodeQuickView>
          variant="pill"
          items={quickViewTabs}
          value={activeQuickView}
          onChange={onQuickViewChange}
        />
        <div className="list-command-band__meta" aria-label="节点列表当前范围">
          <span>{hasActiveFilters ? '当前扫描范围' : '完整 Node 库存'}</span>
          <strong>
            <MonoDigits>{displayedCount}</MonoDigits>
            <small>/</small>
            <MonoDigits>{baseCount}</MonoDigits>
          </strong>
        </div>
      </div>
      <div className="nodes-toolbar__actions list-command-band__actions" aria-label="节点次级动作">
        <Button variant="secondary" size="sm" onClick={onOpenFilters}>
          高级筛选{fieldFilterCount > 0 ? ` · ${fieldFilterCount}` : ''}
        </Button>
        <Button
          variant={batchPanelOpen ? 'secondary' : 'ghost'}
          size="sm"
          onClick={onToggleBatchPanel}
        >
          批量操作
        </Button>
        <button
          type="button"
          className={`btn btn--ghost btn--sm ${!showTrends ? 'btn--active' : ''}`}
          onClick={() => onShowTrendsChange(!showTrends)}
        >
          {showTrends ? '隐藏趋势' : '显示趋势'}
        </button>
        {compareSet.size === 2 ? (
          <Link
            className="btn btn--secondary btn--sm"
            to={`/nodes/compare?id=${[...compareSet].join('&id=')}`}
          >
            对比选中节点
          </Link>
        ) : (
          <span className="nodes-toolbar__hint">选择 2 个节点可对比</span>
        )}
        <label className="nodes-toolbar__refresh">
          <span>自动刷新</span>
          <select
            className="auto-refresh-select"
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
