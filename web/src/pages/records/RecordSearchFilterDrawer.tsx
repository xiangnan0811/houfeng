import { Button, Input, Modal, Select } from '../../components/atoms'
import {
  labelOptions,
  RECORD_ACTION_LABELS,
  RECORD_FOLLOW_UP_LABELS,
} from './recordLabels'
import {
  formatFilterValueList,
  parseFilterValueList,
  withFilter,
  type RecordSearchFilters,
} from './searchFilterModel'

type RecordSearchFilterDrawerProps = {
  open: boolean
  filters: RecordSearchFilters
  onChange: (next: RecordSearchFilters) => void
  onApply: () => void
  onReset: () => void
  onClose: () => void
}

/**
 * `datetime-local` speaks local wall-clock with no zone, while the filter state
 * and the server speak RFC3339 instants. These two convert between them so the
 * shared URL stays unambiguous no matter which zone the reader is in.
 */
function instantFromLocalInput(value: string): string | undefined {
  const parsed = new Date(value.trim())
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString()
}

function localInputFromInstant(value: string | undefined): string {
  const parsed = new Date(value ?? '')
  if (Number.isNaN(parsed.getTime())) return ''
  const local = new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

export function RecordSearchFilterDrawer({
  open,
  filters,
  onChange,
  onApply,
  onReset,
  onClose,
}: RecordSearchFilterDrawerProps) {
  function patchList(field: 'owner' | 'participant' | 'tag', text: string) {
    onChange(withFilter(filters, field, parseFilterValueList(text)))
  }

  function patchInstant(
    field: 'occurred_from' | 'occurred_to' | 'updated_from' | 'updated_to',
    value: string,
  ) {
    onChange(withFilter(filters, field, instantFromLocalInput(value)))
  }

  return (
    <Modal open={open} onClose={onClose} title="高级筛选" ariaLabel="记录搜索高级筛选" size="lg">
      <form
        className="events-filter-drawer record-search-drawer"
        onSubmit={(event) => {
          event.preventDefault()
          onApply()
        }}
      >
        <p className="page-sub record-search-drawer__hint">
          按参与身份、标签、跟进与待办状态，或发生与更新时间缩小范围。多个值用逗号分隔。
        </p>
        <div className="events-filter-drawer__fields record-search-drawer__fields">
          <Input
            label="负责人"
            placeholder="用户 ID，可多个"
            value={formatFilterValueList(filters.owner)}
            onChange={(event) => patchList('owner', event.target.value)}
          />
          <Input
            label="参与人"
            placeholder="用户 ID，可多个"
            value={formatFilterValueList(filters.participant)}
            onChange={(event) => patchList('participant', event.target.value)}
          />
          <Input
            label="标签"
            placeholder="标签，可多个"
            value={formatFilterValueList(filters.tag)}
            onChange={(event) => patchList('tag', event.target.value)}
          />
          <Select
            label="跟进状态"
            value={filters.follow_up ?? ''}
            options={[
              { value: '', label: '全部跟进状态' },
              ...labelOptions(RECORD_FOLLOW_UP_LABELS),
            ]}
            onChange={(event) => onChange(withFilter(
              filters,
              'follow_up',
              (event.target.value || undefined) as RecordSearchFilters['follow_up'],
            ))}
          />
          <Select
            label="待办状态"
            value={filters.action ?? ''}
            options={[
              { value: '', label: '全部待办状态' },
              ...labelOptions(RECORD_ACTION_LABELS),
            ]}
            onChange={(event) => onChange(withFilter(
              filters,
              'action',
              (event.target.value || undefined) as RecordSearchFilters['action'],
            ))}
          />
          <Input
            label="发生时间起"
            type="datetime-local"
            value={localInputFromInstant(filters.occurred_from)}
            onChange={(event) => patchInstant('occurred_from', event.target.value)}
          />
          <Input
            label="发生时间止"
            type="datetime-local"
            value={localInputFromInstant(filters.occurred_to)}
            onChange={(event) => patchInstant('occurred_to', event.target.value)}
          />
          <Input
            label="更新时间起"
            type="datetime-local"
            value={localInputFromInstant(filters.updated_from)}
            onChange={(event) => patchInstant('updated_from', event.target.value)}
          />
          <Input
            label="更新时间止"
            type="datetime-local"
            value={localInputFromInstant(filters.updated_to)}
            onChange={(event) => patchInstant('updated_to', event.target.value)}
          />
        </div>
        <div className="events-filter-drawer__actions record-search-drawer__actions">
          <Button type="button" variant="ghost" onClick={onReset}>重置高级筛选</Button>
          <Button type="button" variant="secondary" onClick={onClose}>取消</Button>
          <Button type="submit">应用高级筛选</Button>
        </div>
      </form>
    </Modal>
  )
}
