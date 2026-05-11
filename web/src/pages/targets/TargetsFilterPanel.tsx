import {
  FilterBar,
  FilterChip,
  FilterMultiSelect,
  FilterSelect,
  FilterToggle,
} from '../../components/filters'
import {
  TARGET_HEALTH_STATUS_FILTER_OPTIONS,
  TARGET_RUN_STATUS_FILTER_OPTIONS,
  TARGET_TYPE_OPTIONS,
} from './targetHelpers'
import type { TargetFilterOption, TargetFilterState } from './types'

type TargetsFilterPanelProps = {
  hasActiveFilters: boolean
  filterState: TargetFilterState
  groupOptions: TargetFilterOption[]
  labelOptions: TargetFilterOption[]
  executionLabelOptions: TargetFilterOption[]
  onClearAll: () => void
  onSingleFilterChange: (
    key: 'group' | 'type' | 'run_status' | 'health',
    value: string | null,
  ) => void
  onMultiFilterChange: (key: 'labels' | 'execution_labels', values: string[]) => void
  onAbnormalFilterChange: (checked: boolean) => void
}

export function TargetsFilterPanel({
  hasActiveFilters,
  filterState,
  groupOptions,
  labelOptions,
  executionLabelOptions,
  onClearAll,
  onSingleFilterChange,
  onMultiFilterChange,
  onAbnormalFilterChange,
}: TargetsFilterPanelProps) {
  return (
    <FilterBar
      hasActiveFilters={hasActiveFilters}
      onClearAll={onClearAll}
      activeChips={
        <>
          {filterState.group ? (
            <FilterChip
              label={`Group: ${filterState.group}`}
              onRemove={() => onSingleFilterChange('group', null)}
            />
          ) : null}
          {filterState.type ? (
            <FilterChip
              label={`类型: ${filterState.type}`}
              onRemove={() => onSingleFilterChange('type', null)}
            />
          ) : null}
          {filterState.runStatus ? (
            <FilterChip
              label={`运行状态: ${filterState.runStatus}`}
              onRemove={() => onSingleFilterChange('run_status', null)}
            />
          ) : null}
          {filterState.health ? (
            <FilterChip
              label={`健康状态: ${filterState.health}`}
              onRemove={() => onSingleFilterChange('health', null)}
            />
          ) : null}
          {filterState.labels.map((label) => (
            <FilterChip
              key={`label-${label}`}
              label={`标签: ${label}`}
              onRemove={() =>
                onMultiFilterChange(
                  'labels',
                  filterState.labels.filter((item) => item !== label),
                )
              }
            />
          ))}
          {filterState.executionLabels.map((label) => (
            <FilterChip
              key={`execution-${label}`}
              label={`执行节点标签: ${label}`}
              onRemove={() =>
                onMultiFilterChange(
                  'execution_labels',
                  filterState.executionLabels.filter((item) => item !== label),
                )
              }
            />
          ))}
          {filterState.abnormal ? (
            <FilterChip label="仅看异常" onRemove={() => onAbnormalFilterChange(false)} />
          ) : null}
        </>
      }
    >
      <FilterSelect
        label="Group"
        value={filterState.group}
        options={groupOptions}
        onChange={(value) => onSingleFilterChange('group', value)}
      />
      <FilterSelect
        label="类型"
        value={filterState.type}
        options={TARGET_TYPE_OPTIONS}
        onChange={(value) => onSingleFilterChange('type', value)}
      />
      <FilterSelect
        label="运行状态"
        value={filterState.runStatus}
        options={TARGET_RUN_STATUS_FILTER_OPTIONS}
        onChange={(value) => onSingleFilterChange('run_status', value)}
      />
      <FilterSelect
        label="健康状态"
        value={filterState.health}
        options={TARGET_HEALTH_STATUS_FILTER_OPTIONS}
        onChange={(value) => onSingleFilterChange('health', value)}
      />
      <FilterMultiSelect
        label="标签"
        values={filterState.labels}
        options={labelOptions}
        onChange={(values) => onMultiFilterChange('labels', values)}
      />
      <FilterMultiSelect
        label="执行节点标签"
        values={filterState.executionLabels}
        options={executionLabelOptions}
        onChange={(values) => onMultiFilterChange('execution_labels', values)}
      />
      <FilterToggle
        label="仅看异常"
        checked={filterState.abnormal}
        onChange={onAbnormalFilterChange}
      />
    </FilterBar>
  )
}
