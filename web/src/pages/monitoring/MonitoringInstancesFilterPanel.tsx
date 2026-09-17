import type { ReactNode } from 'react'

import {
  FilterBar,
  FilterChip,
  FilterMultiSelect,
  FilterSelect,
} from '../../components/filters'
import {
  MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS,
  MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS,
  MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS,
} from './monitoringHelpers'
import type { MonitoringInstanceFilterOption, MonitoringInstanceFilterState } from './types'

type MonitoringInstancesFilterPanelProps = {
  searchQuery: string
  hasActiveFilters: boolean
  filterState: MonitoringInstanceFilterState
  groupOptions: MonitoringInstanceFilterOption[]
  regionOptions: MonitoringInstanceFilterOption[]
  cityOptions: MonitoringInstanceFilterOption[]
  providerOptions: MonitoringInstanceFilterOption[]
  labelOptions: MonitoringInstanceFilterOption[]
  disabled?: boolean
  batch?: ReactNode
  onSearchChange: (value: string) => void
  onClearAll: () => void
  onSingleFilterChange: (
    key: 'group' | 'region' | 'city' | 'provider' | 'lifecycle' | 'run_status' | 'health',
    value: string | null,
  ) => void
  onMultiFilterChange: (key: 'labels', values: string[]) => void
}

export function MonitoringInstancesFilterPanel({
  searchQuery,
  hasActiveFilters,
  filterState,
  groupOptions,
  regionOptions,
  cityOptions,
  providerOptions,
  labelOptions,
  disabled = false,
  batch,
  onSearchChange,
  onClearAll,
  onSingleFilterChange,
  onMultiFilterChange,
}: MonitoringInstancesFilterPanelProps) {
  const activeChips = (
    <>
      {filterState.health ? (
        <FilterChip
          label={`健康状态: ${filterState.health}`}
          onRemove={() => onSingleFilterChange('health', null)}
        />
      ) : null}
      {filterState.runStatus ? (
        <FilterChip
          label={`运行状态: ${filterState.runStatus}`}
          onRemove={() => onSingleFilterChange('run_status', null)}
        />
      ) : null}
      {filterState.group ? (
        <FilterChip
          label={`分组: ${filterState.group}`}
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
          label={`接入阶段: ${filterState.lifecycle}`}
          onRemove={() => onSingleFilterChange('lifecycle', null)}
        />
      ) : null}
      {filterState.labels.map((label) => (
        <FilterChip
          key={`label-${label}`}
          label={`标签: ${label}`}
          onRemove={() => onMultiFilterChange('labels', filterState.labels.filter((item) => item !== label))}
        />
      ))}
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
        label="健康"
        value={filterState.health}
        options={MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS}
        placeholder="全部健康"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('health', value)}
      />
      <FilterSelect
        label="运行"
        value={filterState.runStatus}
        options={MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS}
        placeholder="全部运行"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('run_status', value)}
      />
      <FilterSelect
        label="分组"
        value={filterState.group}
        options={groupOptions}
        placeholder="全部分组"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('group', value)}
      />
      <FilterSelect
        label="地区"
        value={filterState.region}
        options={regionOptions}
        placeholder="全部地区"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('region', value)}
      />
      <FilterSelect
        label="城市"
        value={filterState.city}
        options={cityOptions}
        placeholder="全部城市"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('city', value)}
      />
      <FilterSelect
        label="供应商"
        value={filterState.provider}
        options={providerOptions}
        placeholder="全部供应商"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('provider', value)}
      />
      <FilterSelect
        label="接入阶段"
        value={filterState.lifecycle}
        options={MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS}
        placeholder="全部阶段"
        disabled={disabled}
        onChange={(value) => onSingleFilterChange('lifecycle', value)}
      />
      <FilterMultiSelect
        label="标签"
        values={filterState.labels}
        options={labelOptions}
        disabled={disabled}
        onChange={(values) => onMultiFilterChange('labels', values)}
      />
      <div className="monitoring-page__trailing-controls">
        <label className="filter-select monitoring-page__search-field">
          <span className="filter-select__label">搜索</span>
          <input
            className="filter-select__control filter-select__control--text monitoring-page__search"
            type="search"
            aria-label="搜索监控实例"
            placeholder="名称、ID、位置、标签"
            value={searchQuery}
            autoComplete="off"
            disabled={disabled}
            onChange={(event) => onSearchChange(event.target.value)}
          />
        </label>
        {batch}
      </div>
    </FilterBar>
  )
}
