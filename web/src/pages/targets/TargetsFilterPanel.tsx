import type { ReactNode } from 'react'

import { FilterBar, FilterChip, FilterSelect } from '../../components/filters'
import {
  TARGET_HEALTH_STATUS_FILTER_OPTIONS,
  TARGET_RUN_STATUS_FILTER_OPTIONS,
  TARGET_TYPE_OPTIONS,
  targetTypeLabel,
} from './targetHelpers'
import type { TargetFilterOption, TargetFilterState } from './types'

type TargetsFilterPanelProps = {
  filterState: TargetFilterState
  groupOptions: TargetFilterOption[]
  hasActiveFilters: boolean
  batch?: ReactNode
  onClearAll: () => void
  onSingleFilterChange: (
    key: 'group' | 'type' | 'run_status' | 'health',
    value: string | null,
  ) => void
}

export function TargetsFilterPanel({
  filterState,
  groupOptions,
  hasActiveFilters,
  batch,
  onClearAll,
  onSingleFilterChange,
}: TargetsFilterPanelProps) {
  const activeChips = (
    <>
      {filterState.type ? (
        <FilterChip
          label={`类型: ${targetTypeLabel(filterState.type)}`}
          onRemove={() => onSingleFilterChange('type', null)}
        />
      ) : null}
      {filterState.health ? (
        <FilterChip
          label={`健康: ${filterState.health}`}
          onRemove={() => onSingleFilterChange('health', null)}
        />
      ) : null}
      {filterState.runStatus ? (
        <FilterChip
          label={`运行: ${filterState.runStatus}`}
          onRemove={() => onSingleFilterChange('run_status', null)}
        />
      ) : null}
      {filterState.group ? (
        <FilterChip
          label={`分组: ${filterState.group}`}
          onRemove={() => onSingleFilterChange('group', null)}
        />
      ) : null}
    </>
  )

  return (
    <FilterBar
      className="monitoring-page__filters"
      hasActiveFilters={hasActiveFilters}
      onClearAll={onClearAll}
      activeChips={activeChips}
    >
      <FilterSelect
        label="类型"
        value={filterState.type}
        options={TARGET_TYPE_OPTIONS.map((opt) => ({ value: opt.value, label: opt.label }))}
        onChange={(value) => onSingleFilterChange('type', value)}
      />
      <FilterSelect
        label="健康"
        value={filterState.health}
        options={[...TARGET_HEALTH_STATUS_FILTER_OPTIONS]}
        onChange={(value) => onSingleFilterChange('health', value)}
      />
      <FilterSelect
        label="运行"
        value={filterState.runStatus}
        options={[...TARGET_RUN_STATUS_FILTER_OPTIONS]}
        onChange={(value) => onSingleFilterChange('run_status', value)}
      />
      <FilterSelect
        label="分组"
        value={filterState.group}
        options={groupOptions}
        onChange={(value) => onSingleFilterChange('group', value)}
      />
      <div className="monitoring-page__trailing-controls">{batch}</div>
    </FilterBar>
  )
}
