import { Link } from 'react-router-dom'

import { MonoDigits, Tabs, type TabItem } from '../../components/atoms'
import { AUTO_REFRESH_OPTIONS, type AutoRefreshOption } from '../../lib/useAutoRefresh'
import type { NodeListView } from './types'

type NodesToolbarProps = {
  viewTabs: TabItem<NodeListView>[]
  nodeListView: NodeListView
  displayedCount: number
  baseCount: number
  showTrends: boolean
  compareSet: Set<string>
  autoRefresh: AutoRefreshOption
  onNodeListViewChange: (value: NodeListView) => void
  onShowTrendsChange: (value: boolean) => void
  onAutoRefreshChange: (value: AutoRefreshOption) => void
}

export function NodesToolbar({
  viewTabs,
  nodeListView,
  displayedCount,
  baseCount,
  showTrends,
  compareSet,
  autoRefresh,
  onNodeListViewChange,
  onShowTrendsChange,
  onAutoRefreshChange,
}: NodesToolbarProps) {
  return (
    <div className="nodes-toolbar" aria-label="节点列表工具栏">
      <div className="nodes-toolbar__primary">
        <Tabs<NodeListView>
          variant="pill"
          items={viewTabs}
          value={nodeListView}
          onChange={onNodeListViewChange}
        />
        <span className="nodes-toolbar__result">
          当前显示 <MonoDigits>{displayedCount}</MonoDigits> / <MonoDigits>{baseCount}</MonoDigits>
        </span>
      </div>
      <div className="nodes-toolbar__actions">
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
