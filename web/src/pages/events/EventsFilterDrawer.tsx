import type { FormEvent } from 'react'

import { Modal, Tabs } from '../../components/atoms'
import { FilterSelect, FilterToggle } from '../../components/filters'
import type { StateChangeEventType } from '../../lib/types'
import {
  ALLOWED_EVENT_TYPES,
  EVENT_TYPE_SELECT_OPTIONS,
  INCIDENT_CLASS_OPTIONS,
  OBJECT_TYPE_OPTIONS,
  SEVERITY_OPTIONS,
  TIME_RANGE_TABS,
} from './eventsPageConstants'
import type { FilterState, TimeRange } from './types'

type EventsFilterDrawerProps = {
  open: boolean
  filters: FilterState
  onClose: () => void
  onApply: () => void
  onReset: () => void
  onTimeRangeChange: (value: TimeRange) => void
  onFilterChange: <K extends keyof FilterState>(key: K, value: FilterState[K]) => void
}

function isEventTypeValue(value: string | null): value is StateChangeEventType {
  return value !== null && ALLOWED_EVENT_TYPES.has(value as StateChangeEventType)
}

export function EventsFilterDrawer({
  open,
  filters,
  onClose,
  onApply,
  onReset,
  onTimeRangeChange,
  onFilterChange,
}: EventsFilterDrawerProps) {
  const customRange = filters.time_range === 'custom'

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onApply()
  }

  return (
    <Modal open={open} onClose={onClose} title="事件筛选" ariaLabel="事件高级筛选">
      <form className="events-filter-drawer" onSubmit={handleSubmit}>
        <div className="events-filter-drawer__group">
          <span className="events-filter-drawer__label">时间范围</span>
          <Tabs<TimeRange>
            variant="pill"
            value={filters.time_range}
            onChange={onTimeRangeChange}
            items={TIME_RANGE_TABS}
          />
        </div>

        <div className="events-filter-drawer__grid">
          <FilterSelect
            label="对象类型"
            value={filters.object_type || null}
            options={OBJECT_TYPE_OPTIONS}
            onChange={(value) =>
              onFilterChange('object_type', value === 'node' || value === 'target' ? value : '')
            }
          />
          <FilterSelect
            label="严重程度"
            value={filters.severity || null}
            options={SEVERITY_OPTIONS}
            onChange={(value) =>
              onFilterChange(
                'severity',
                value === '关注' || value === '告警' || value === '严重' ? value : '',
              )
            }
          />
          <FilterSelect
            label="事件类型"
            value={filters.event_type || null}
            options={EVENT_TYPE_SELECT_OPTIONS}
            onChange={(value) => onFilterChange('event_type', isEventTypeValue(value) ? value : '')}
          />
          <FilterSelect
            label="异常类别"
            value={filters.incident_class || null}
            options={INCIDENT_CLASS_OPTIONS}
            onChange={(value) => onFilterChange('incident_class', value ?? '')}
          />
          <FilterToggle
            label="仅看通知事件"
            checked={filters.notification_only}
            onChange={(checked) => onFilterChange('notification_only', checked)}
          />
          <FilterToggle
            label="仅看恢复事件"
            checked={filters.recovery_only}
            onChange={(checked) => onFilterChange('recovery_only', checked)}
          />
          <FilterToggle
            label="仅看维护事件"
            checked={filters.maintenance_only}
            onChange={(checked) => onFilterChange('maintenance_only', checked)}
          />
          <FilterToggle
            label="包含补传事件"
            checked={filters.include_backfilled}
            onChange={(checked) => onFilterChange('include_backfilled', checked)}
          />
        </div>

        <div className="events-filter-drawer__fields">
          <label className="events-filter-drawer__field">
            <span className="events-filter-drawer__label">关键词</span>
            <input
              aria-label="关键词"
              placeholder="搜索摘要或异常类别…"
              value={filters.keyword}
              onChange={(e) => onFilterChange('keyword', e.target.value)}
            />
          </label>

          <label className="events-filter-drawer__field">
            <span className="events-filter-drawer__label">标签</span>
            <input
              aria-label="标签"
              placeholder="edge"
              value={filters.label}
              onChange={(e) => onFilterChange('label', e.target.value)}
            />
          </label>

          <label className="events-filter-drawer__field">
            <span className="events-filter-drawer__label">开始时间</span>
            <input
              aria-label="开始时间"
              placeholder="2026-04-25T00:00:00Z"
              value={filters.created_from}
              disabled={!customRange}
              onChange={(e) => onFilterChange('created_from', e.target.value)}
            />
          </label>

          <label className="events-filter-drawer__field">
            <span className="events-filter-drawer__label">结束时间</span>
            <input
              aria-label="结束时间"
              placeholder="2026-04-26T00:00:00Z"
              value={filters.created_to}
              disabled={!customRange}
              onChange={(e) => onFilterChange('created_to', e.target.value)}
            />
          </label>
        </div>

        <div className="events-filter-drawer__actions">
          <button type="submit" className="btn sm primary">应用筛选</button>
          <button type="button" className="btn sm secondary" onClick={onReset}>重置筛选</button>
          <button type="button" className="btn sm ghost" onClick={onClose}>关闭</button>
        </div>
      </form>
    </Modal>
  )
}