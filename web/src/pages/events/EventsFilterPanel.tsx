import { FilterBar, FilterChip, FilterSelect } from '../../components/filters'
import {
  EVENT_TYPE_SELECT_OPTIONS,
  INCIDENT_CLASS_OPTIONS,
  OBJECT_TYPE_OPTIONS,
  SEVERITY_OPTIONS,
  TIME_RANGE_TABS,
} from './eventsPageConstants'
import type { FilterState, TimeRange } from './types'

type EventsFilterPanelProps = {
  filters: FilterState
  hasActiveFilters: boolean
  onClearAll: () => void
  onFilterChange: <K extends keyof FilterState>(key: K, value: FilterState[K]) => void
  onTimeRangeChange: (value: TimeRange) => void
}

export function EventsFilterPanel({
  filters,
  hasActiveFilters,
  onClearAll,
  onFilterChange,
  onTimeRangeChange,
}: EventsFilterPanelProps) {
  const typeLabel = OBJECT_TYPE_OPTIONS.find((opt) => opt.value === filters.object_type)?.label
  const eventTypeLabel = EVENT_TYPE_SELECT_OPTIONS.find((opt) => opt.value === filters.event_type)?.label
  const classLabel = INCIDENT_CLASS_OPTIONS.find((opt) => opt.value === filters.incident_class)?.label
  const timeLabel = TIME_RANGE_TABS.find((tab) => tab.value === filters.time_range)?.label

  const activeChips = (
    <>
      {filters.time_range !== 'all' && timeLabel ? (
        <FilterChip label={`时间: ${timeLabel}`} onRemove={() => onTimeRangeChange('all')} />
      ) : null}
      {filters.object_type && typeLabel ? (
        <FilterChip label={`对象: ${typeLabel}`} onRemove={() => onFilterChange('object_type', '')} />
      ) : null}
      {filters.object_id ? (
        <FilterChip
          label={`对象 ID: ${filters.object_id}`}
          onRemove={() => onFilterChange('object_id', '')}
        />
      ) : null}
      {filters.severity ? (
        <FilterChip label={`严重度: ${filters.severity}`} onRemove={() => onFilterChange('severity', '')} />
      ) : null}
      {filters.event_type && eventTypeLabel ? (
        <FilterChip
          label={`类型: ${eventTypeLabel}`}
          onRemove={() => onFilterChange('event_type', '')}
        />
      ) : null}
      {filters.incident_class && classLabel ? (
        <FilterChip
          label={`异常: ${classLabel}`}
          onRemove={() => onFilterChange('incident_class', '')}
        />
      ) : null}
      {filters.keyword ? (
        <FilterChip label={`关键词: ${filters.keyword}`} onRemove={() => onFilterChange('keyword', '')} />
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
        label="时间范围"
        value={filters.time_range === 'all' ? null : filters.time_range}
        options={[
          ...TIME_RANGE_TABS.filter((tab) => tab.value !== 'all' && tab.value !== 'custom').map((tab) => ({
            value: tab.value,
            label: tab.label,
          })),
          ...(filters.time_range === 'custom' || filters.created_from || filters.created_to
            ? [{ value: 'custom', label: '自定义' }]
            : []),
        ]}
        placeholder="全部时间"
        onChange={(value) => {
          if (value === null || value === 'all') {
            onTimeRangeChange('all')
            return
          }
          if (value === '24h' || value === '7d' || value === '30d' || value === 'custom') {
            onTimeRangeChange(value)
          }
        }}
      />
      <FilterSelect
        label="对象类型"
        value={filters.object_type || null}
        options={OBJECT_TYPE_OPTIONS}
        placeholder="全部对象"
        onChange={(value) =>
          onFilterChange('object_type', value === 'monitoring_instance' || value === 'target' ? value : '')
        }
      />
      <FilterSelect
        label="严重度"
        value={filters.severity || null}
        options={SEVERITY_OPTIONS}
        placeholder="全部严重度"
        onChange={(value) =>
          onFilterChange('severity', value === '关注' || value === '告警' || value === '严重' ? value : '')
        }
      />
      <FilterSelect
        label="事件类型"
        value={filters.event_type || null}
        options={EVENT_TYPE_SELECT_OPTIONS}
        placeholder="全部类型"
        onChange={(value) => onFilterChange('event_type', value as FilterState['event_type'])}
      />
      <FilterSelect
        label="异常类别"
        value={filters.incident_class || null}
        options={INCIDENT_CLASS_OPTIONS}
        placeholder="全部类别"
        onChange={(value) => onFilterChange('incident_class', value ?? '')}
      />
      <div className="filter-select">
        <span className="filter-select__label">关键词</span>
        <input
          className="filter-select__control"
          type="text"
          placeholder="搜索摘要…"
          value={filters.keyword}
          onChange={(event) => onFilterChange('keyword', event.target.value)}
          aria-label="关键词"
        />
      </div>
    </FilterBar>
  )
}
