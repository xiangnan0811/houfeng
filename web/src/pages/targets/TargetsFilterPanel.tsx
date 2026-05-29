import {
  TARGET_HEALTH_STATUS_FILTER_OPTIONS,
  TARGET_RUN_STATUS_FILTER_OPTIONS,
  TARGET_TYPE_OPTIONS,
} from './targetHelpers'
import type { TargetFilterOption, TargetFilterState } from './types'

type TargetsFilterPanelProps = {
  filterState: TargetFilterState
  groupOptions: TargetFilterOption[]
  onSingleFilterChange: (
    key: 'group' | 'type' | 'run_status' | 'health',
    value: string | null,
  ) => void
}

export function TargetsFilterPanel({
  filterState,
  groupOptions,
  onSingleFilterChange,
}: TargetsFilterPanelProps) {
  return (
    <div className="filter-bar">
      <span className="filter-bar__label">筛选</span>
      <select
        className="filter-select"
        aria-label="类型"
        value={filterState.type ?? ''}
        onChange={(e) => onSingleFilterChange('type', e.target.value || null)}
      >
        <option value="">探测类型: 全部</option>
        {TARGET_TYPE_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
      <select
        className="filter-select"
        aria-label="健康状态"
        value={filterState.health ?? ''}
        onChange={(e) => onSingleFilterChange('health', e.target.value || null)}
      >
        <option value="">健康状态: 全部</option>
        {TARGET_HEALTH_STATUS_FILTER_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
      <select
        className="filter-select"
        aria-label="运行状态"
        value={filterState.runStatus ?? ''}
        onChange={(e) => onSingleFilterChange('run_status', e.target.value || null)}
      >
        <option value="">运行状态: 全部</option>
        {TARGET_RUN_STATUS_FILTER_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
      <select
        className="filter-select"
        aria-label="Group"
        value={filterState.group ?? ''}
        onChange={(e) => onSingleFilterChange('group', e.target.value || null)}
      >
        <option value="">Group: 全部</option>
        {groupOptions.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
    </div>
  )
}
