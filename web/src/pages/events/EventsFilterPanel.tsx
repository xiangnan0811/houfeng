import { FilterSelect } from '../../components/filters'
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
  onFilterChange: <K extends keyof FilterState>(key: K, value: FilterState[K]) => void
  onTimeRangeChange: (value: TimeRange) => void
}

export function EventsFilterPanel({
  filters,
  onFilterChange,
  onTimeRangeChange,
}: EventsFilterPanelProps) {
  return (
    <div className="filter-panel">
      <div className="filter-bar__controls-row">
        <FilterSelect
          label="时间范围"
          value={filters.time_range}
          options={TIME_RANGE_TABS.map((t) => ({ value: t.value, label: t.label }))}
          onChange={(value) => {
            if (value === '24h' || value === '7d' || value === '30d' || value === 'custom') {
              onTimeRangeChange(value)
            }
          }}
        />
        <FilterSelect
          label="对象类型"
          value={filters.object_type || null}
          options={OBJECT_TYPE_OPTIONS}
          onChange={(value) =>
            onFilterChange('object_type', value === 'node' || value === 'target' ? value : '')
          }
        />
        <FilterSelect
          label="严重度"
          value={filters.severity || null}
          options={SEVERITY_OPTIONS}
          onChange={(value) =>
            onFilterChange('severity', value === '关注' || value === '告警' || value === '严重' ? value : '')
          }
        />
        <FilterSelect
          label="事件类型"
          value={filters.event_type || null}
          options={EVENT_TYPE_SELECT_OPTIONS}
          onChange={(value) => onFilterChange('event_type', value as FilterState['event_type'])}
        />
        <FilterSelect
          label="异常类别"
          value={filters.incident_class || null}
          options={INCIDENT_CLASS_OPTIONS}
          onChange={(value) => onFilterChange('incident_class', value ?? '')}
        />
        <div className="filter-select">
          <span className="filter-select__label">关键词</span>
          <input
            className="filter-select__control"
            type="text"
            placeholder="搜索摘要…"
            value={filters.keyword}
            onChange={(e) => onFilterChange('keyword', e.target.value)}
          />
        </div>
      </div>
    </div>
  )
}