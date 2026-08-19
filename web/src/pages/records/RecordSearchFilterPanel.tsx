import { Button, Input, Select } from '../../components/atoms'
import {
  labelOptions,
  RECORD_LIFECYCLE_LABELS,
  RECORD_SORT_LABELS,
  RECORD_STATUS_GROUP_LABELS,
  RECORD_TYPE_LABELS,
} from './recordLabels'
import {
  recordSearchFilterChips,
  withFilter,
  type RecordSearchFilters,
} from './searchFilterModel'

type ListFilterField = 'type' | 'status_group' | 'lifecycle'

type RecordSearchFilterPanelProps = {
  filters: RecordSearchFilters
  onChange: (next: RecordSearchFilters) => void
  onApply: () => void
  onClear: () => void
  onOpenAdvanced: () => void
}

/**
 * Repeated filters are additive: picking a value appends it as a chip instead of
 * replacing the previous one, so a link carrying several types stays intact when
 * the reader narrows it further. The options come from the same label maps the
 * filter model validates against, so the widened type here cannot admit a value
 * the server would reject.
 */
function appendListValue(
  filters: RecordSearchFilters,
  field: ListFilterField,
  value: string,
): RecordSearchFilters {
  const values: readonly string[] = filters[field] ?? []
  if (values.includes(value)) return filters
  const appended = [...values, value] as RecordSearchFilters[ListFilterField]
  return withFilter(filters, field, appended)
}

export function RecordSearchFilterPanel({
  filters,
  onChange,
  onApply,
  onClear,
  onOpenAdvanced,
}: RecordSearchFilterPanelProps) {
  const chips = recordSearchFilterChips(filters)

  function appendControl(label: string, placeholder: string, field: ListFilterField, labels: Record<string, string>) {
    return (
      <Select
        label={label}
        value=""
        options={[{ value: '', label: placeholder }, ...labelOptions(labels)]}
        onChange={(event) => {
          if (!event.target.value) return
          onChange(appendListValue(filters, field, event.target.value))
        }}
      />
    )
  }

  return (
    <form
      className="filter-bar record-search-filter"
      aria-label="记录搜索筛选"
      onSubmit={(event) => {
        event.preventDefault()
        onApply()
      }}
    >
      <div className="filter-bar__controls">
        <div className="filter-bar__controls-row">
          <Input
            label="关键词"
            placeholder="标题或正文"
            value={filters.q ?? ''}
            onChange={(event) => onChange(withFilter(filters, 'q', event.target.value || undefined))}
          />
          {appendControl('记录类型', '全部类型', 'type', RECORD_TYPE_LABELS)}
          {appendControl('状态分组', '全部状态分组', 'status_group', RECORD_STATUS_GROUP_LABELS)}
          {appendControl('生命周期', '全部生命周期', 'lifecycle', RECORD_LIFECYCLE_LABELS)}
          <Select
            label="排序"
            value={filters.sort ?? 'updated_at_desc'}
            options={labelOptions(RECORD_SORT_LABELS)}
            onChange={(event) => onChange(withFilter(
              filters,
              'sort',
              event.target.value as RecordSearchFilters['sort'],
            ))}
          />
          <div className="section-heading__actions record-search-filter__actions">
            <Button type="submit" size="sm">搜索</Button>
            <Button type="button" size="sm" variant="secondary" onClick={onOpenAdvanced}>
              高级筛选
            </Button>
          </div>
        </div>
        {chips.length ? (
          <button type="button" className="filter-bar__clear" onClick={onClear}>清空所有</button>
        ) : null}
      </div>
      {chips.length ? (
        <div className="filter-bar__chips" aria-label="已选筛选">
          {chips.map((chip) => (
            <span className="filter-chip" key={chip.key}>
              <span className="filter-chip__label">{chip.label}</span>
              <button
                type="button"
                className="filter-chip__remove"
                aria-label={`移除筛选 ${chip.label}`}
                onClick={() => onChange(chip.next)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      ) : null}
    </form>
  )
}
