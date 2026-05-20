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
    <div className="nodes-toolbar list-command-band list-command-band--nodes" aria-label="节点列表工具栏">
      <div className="list-command-band__main">
        <p className="list-command-band__eyebrow">LIST SCAN</p>
        <h3 className="list-command-band__title">节点列表扫描</h3>
        <p className="list-command-band__description">
          筛选在这里编辑，批量操作作用于当前筛选范围；节点专属的对比、趋势和自动刷新保留在同一控制带。
        </p>
      </div>
      <div className="nodes-toolbar__primary list-command-band__controls">
        <Tabs<NodeListView>
          variant="pill"
          items={viewTabs}
          value={nodeListView}
          onChange={onNodeListViewChange}
        />
        <div className="list-command-band__meta" aria-label="节点列表当前范围">
          <span>{nodeListView === 'binding-conflict' ? '绑定异常视图' : '当前列表范围'}</span>
          <strong>
            <MonoDigits>{displayedCount}</MonoDigits>
            <small>/</small>
            <MonoDigits>{baseCount}</MonoDigits>
          </strong>
        </div>
      </div>
      <div className="nodes-toolbar__actions list-command-band__actions">
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
