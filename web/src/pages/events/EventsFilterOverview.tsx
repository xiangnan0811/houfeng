import { Button } from '../../components/atoms'
import { DetailSection } from '../../components/DetailSection'
import { FilterBar, FilterChip } from '../../components/filters'
import { STATE_CHANGE_EVENT_TYPE_LABELS } from '../../lib/types'
import { DEFAULT_LIMIT, TIME_RANGE_LABELS } from './eventsPageConstants'
import type { FilterState } from './types'

type EventsFilterOverviewProps = {
  filters: FilterState
  hasActiveFilters: boolean
  onClearAll: () => void
  onOpenFilters: () => void
  onRemoveFilter: (key: keyof FilterState) => void
}

export function EventsFilterOverview({
  filters,
  hasActiveFilters,
  onClearAll,
  onOpenFilters,
  onRemoveFilter,
}: EventsFilterOverviewProps) {
  const objectTypeLabel =
    filters.object_type === 'node'
      ? '节点'
      : filters.object_type === 'target'
        ? '目标'
        : ''
  const timeRangeContext =
    filters.time_range !== 'custom'
      ? TIME_RANGE_LABELS[filters.time_range]
      : filters.created_from || filters.created_to
        ? '自定义时间'
        : '未限定时间'
  const limitContext = `最近 ${filters.limit || DEFAULT_LIMIT} 条`

  const activeFilterChips = (
    <>
      {filters.object_type ? (
        <FilterChip
          label={`对象类型: ${objectTypeLabel}`}
          onRemove={() => onRemoveFilter('object_type')}
        />
      ) : null}
      {filters.severity ? (
        <FilterChip
          label={`严重程度: ${filters.severity}`}
          onRemove={() => onRemoveFilter('severity')}
        />
      ) : null}
      {filters.event_type ? (
        <FilterChip
          label={`事件类型: ${STATE_CHANGE_EVENT_TYPE_LABELS[filters.event_type]}`}
          onRemove={() => onRemoveFilter('event_type')}
        />
      ) : null}
      {filters.limit !== String(DEFAULT_LIMIT) ? (
        <FilterChip
          label={`数量: ${filters.limit}`}
          onRemove={() => onRemoveFilter('limit')}
        />
      ) : null}
      {filters.time_range !== 'custom' ? (
        <FilterChip
          label={`时间范围: ${TIME_RANGE_LABELS[filters.time_range]}`}
          onRemove={() => onRemoveFilter('time_range')}
        />
      ) : null}
      {filters.created_from ? (
        <FilterChip
          label={`开始时间: ${filters.created_from}`}
          onRemove={() => onRemoveFilter('created_from')}
        />
      ) : null}
      {filters.created_to ? (
        <FilterChip
          label={`结束时间: ${filters.created_to}`}
          onRemove={() => onRemoveFilter('created_to')}
        />
      ) : null}
      {filters.label ? (
        <FilterChip label={`标签: ${filters.label}`} onRemove={() => onRemoveFilter('label')} />
      ) : null}
      {filters.notification_only ? (
        <FilterChip label="仅看通知事件" onRemove={() => onRemoveFilter('notification_only')} />
      ) : null}
      {filters.recovery_only ? (
        <FilterChip label="仅看恢复事件" onRemove={() => onRemoveFilter('recovery_only')} />
      ) : null}
      {filters.maintenance_only ? (
        <FilterChip label="仅看维护事件" onRemove={() => onRemoveFilter('maintenance_only')} />
      ) : null}
      {filters.include_backfilled ? (
        <FilterChip label="包含补传事件" onRemove={() => onRemoveFilter('include_backfilled')} />
      ) : null}
    </>
  )

  return (
    <DetailSection eyebrow="筛选条件" title="筛选条件" ribbon="offline">
      <FilterBar
        className="events-filter-overview"
        hasActiveFilters={hasActiveFilters}
        onClearAll={onClearAll}
        activeChips={activeFilterChips}
      >
        <div className="events-filter-overview__status">
          <span className="events-filter-overview__label">当前筛选</span>
          <span className="events-filter-overview__value">
            {hasActiveFilters ? `已应用筛选 · ${timeRangeContext} · ${limitContext}` : `默认：${timeRangeContext} · ${limitContext}`}
          </span>
        </div>
        <Button variant="secondary" size="sm" onClick={onOpenFilters}>
          高级筛选
        </Button>
      </FilterBar>
    </DetailSection>
  )
}
