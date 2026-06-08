import { Link } from 'react-router-dom'

import type { MonitoringInstanceFilterState } from './types'

type MonitoringToolbarProps = {
  filterState: MonitoringInstanceFilterState
  healthOptions: string[]
  lifecycleOptions: string[]
  runStatusOptions: string[]
  regionOptions: { value: string; label: string }[]
  providerOptions: { value: string; label: string }[]
  compareSet: Set<string>
  onFilterChange: (key: string, value: string | null) => void
  onAbnormalChange: (checked: boolean) => void
  onOpenBatchPanel: () => void
}

export function MonitoringToolbar({
  filterState,
  healthOptions,
  lifecycleOptions,
  runStatusOptions,
  regionOptions,
  providerOptions,
  compareSet,
  onFilterChange,
  onAbnormalChange,
  onOpenBatchPanel,
}: MonitoringToolbarProps) {
  return (
    <>
      <div className="filter-panel animate-in d1">
        <div className="filter-bar">
          <span className="filter-bar__label">筛选</span>
          <select
            className="filter-select"
            value={filterState.health ?? ''}
            onChange={(e) => onFilterChange('health', e.target.value || null)}
          >
            <option value="">健康状态: 全部</option>
            {healthOptions.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
          {lifecycleOptions.length > 0 ? (
            <select
              className="filter-select"
              value={filterState.lifecycle ?? ''}
              onChange={(e) => onFilterChange('lifecycle', e.target.value || null)}
            >
              <option value="">接入阶段: 全部</option>
              {lifecycleOptions.map((v) => (
                <option key={v} value={v}>{v}</option>
              ))}
            </select>
          ) : null}
          <select
            className="filter-select"
            value={filterState.runStatus ?? ''}
            onChange={(e) => onFilterChange('run_status', e.target.value || null)}
          >
            <option value="">运行状态: 全部</option>
            {runStatusOptions.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
          <select
            className="filter-select"
            value={filterState.region ?? ''}
            onChange={(e) => onFilterChange('region', e.target.value || null)}
          >
            <option value="">地区: 全部</option>
            {regionOptions.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          <select
            className="filter-select"
            value={filterState.provider ?? ''}
            onChange={(e) => onFilterChange('provider', e.target.value || null)}
          >
            <option value="">供应商: 全部</option>
            {providerOptions.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          <label>
            <input
              type="checkbox"
              checked={filterState.abnormal}
              onChange={(e) => onAbnormalChange(e.target.checked)}
            />
            仅看异常
          </label>
        </div>
      </div>
      <div className="monitoring-toolbar__secondary-row">
        {compareSet.size === 2 ? (
          <Link
            className="btn sm secondary"
            to={`/monitoring/compare?id=${[...compareSet].join('&id=')}`}
          >
            对比选中监控实例
          </Link>
        ) : (
          <button type="button" className="btn sm secondary" disabled>
            对比选中 ({compareSet.size}/2)
          </button>
        )}
        <span className="text-sm text-muted">
          勾选 2 个监控实例可进入对比视图
        </span>
        <button type="button" className="btn sm secondary" onClick={onOpenBatchPanel}>
          批量操作
        </button>
      </div>
    </>
  )
}
