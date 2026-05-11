import {
  FilterBar,
  FilterChip,
  FilterMultiSelect,
  FilterSelect,
  FilterToggle,
} from '../../components/filters'
import {
  NODE_HEALTH_STATUS_FILTER_OPTIONS,
  NODE_LIFECYCLE_FILTER_OPTIONS,
  NODE_RUN_STATUS_FILTER_OPTIONS,
} from './nodeHelpers'
import type { NodeFilterOption, NodeFilterState } from './types'

type NodesFilterPanelProps = {
  hasActiveFilters: boolean
  filterState: NodeFilterState
  groupOptions: NodeFilterOption[]
  regionOptions: NodeFilterOption[]
  cityOptions: NodeFilterOption[]
  providerOptions: NodeFilterOption[]
  labelOptions: NodeFilterOption[]
  onClearAll: () => void
  onSingleFilterChange: (
    key: 'group' | 'region' | 'city' | 'provider' | 'lifecycle' | 'run_status' | 'health',
    value: string | null,
  ) => void
  onMultiFilterChange: (key: 'labels', values: string[]) => void
  onAbnormalFilterChange: (checked: boolean) => void
  onOnboardingFilterChange: (checked: boolean) => void
}

export function NodesFilterPanel({
  hasActiveFilters,
  filterState,
  groupOptions,
  regionOptions,
  cityOptions,
  providerOptions,
  labelOptions,
  onClearAll,
  onSingleFilterChange,
  onMultiFilterChange,
  onAbnormalFilterChange,
  onOnboardingFilterChange,
}: NodesFilterPanelProps) {
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
          {filterState.region ? (
            <FilterChip
              label={`地区: ${filterState.region}`}
              onRemove={() => onSingleFilterChange('region', null)}
            />
          ) : null}
          {filterState.city ? (
            <FilterChip
              label={`城市: ${filterState.city}`}
              onRemove={() => onSingleFilterChange('city', null)}
            />
          ) : null}
          {filterState.provider ? (
            <FilterChip
              label={`供应商: ${filterState.provider}`}
              onRemove={() => onSingleFilterChange('provider', null)}
            />
          ) : null}
          {filterState.lifecycle ? (
            <FilterChip
              label={`生命周期: ${filterState.lifecycle}`}
              onRemove={() => onSingleFilterChange('lifecycle', null)}
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
          {filterState.abnormal ? (
            <FilterChip label="仅看异常" onRemove={() => onAbnormalFilterChange(false)} />
          ) : null}
          {filterState.onboardingPending ? (
            <FilterChip
              label="待接入/绑定待处理"
              onRemove={() => onOnboardingFilterChange(false)}
            />
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
        label="地区"
        value={filterState.region}
        options={regionOptions}
        onChange={(value) => onSingleFilterChange('region', value)}
      />
      <FilterSelect
        label="城市"
        value={filterState.city}
        options={cityOptions}
        onChange={(value) => onSingleFilterChange('city', value)}
      />
      <FilterSelect
        label="供应商"
        value={filterState.provider}
        options={providerOptions}
        onChange={(value) => onSingleFilterChange('provider', value)}
      />
      <FilterSelect
        label="生命周期"
        value={filterState.lifecycle}
        options={NODE_LIFECYCLE_FILTER_OPTIONS}
        onChange={(value) => onSingleFilterChange('lifecycle', value)}
      />
      <FilterSelect
        label="运行状态"
        value={filterState.runStatus}
        options={NODE_RUN_STATUS_FILTER_OPTIONS}
        onChange={(value) => onSingleFilterChange('run_status', value)}
      />
      <FilterSelect
        label="健康状态"
        value={filterState.health}
        options={NODE_HEALTH_STATUS_FILTER_OPTIONS}
        onChange={(value) => onSingleFilterChange('health', value)}
      />
      <FilterMultiSelect
        label="标签"
        values={filterState.labels}
        options={labelOptions}
        onChange={(values) => onMultiFilterChange('labels', values)}
      />
      <FilterToggle
        label="仅看异常"
        checked={filterState.abnormal}
        onChange={onAbnormalFilterChange}
      />
      <FilterToggle
        label="待接入/绑定待处理"
        checked={filterState.onboardingPending}
        onChange={onOnboardingFilterChange}
      />
    </FilterBar>
  )
}
