import { Button } from '../../components/atoms'
import { FilterSelect } from '../../components/filters'
import { COMMAND_OPTIONS } from '../../config/commands'
import type { CommandAuditFilters } from './types'

const WINDOW_OPTIONS = [
  { value: '24h', label: '最近 24 小时' },
  { value: '7d', label: '最近 7 天' },
  { value: '30d', label: '最近 30 天' },
  { value: 'all', label: '全部时间' },
  { value: 'custom', label: '自定义时间' },
]

const OUTCOME_OPTIONS = [
  { value: 'rejected', label: '已拒绝' },
  { value: 'queued', label: '已排队' },
  { value: 'dispatched', label: '已派发' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
]

type CommandAuditFilterPanelProps = {
  filters: CommandAuditFilters
  onChange: <K extends keyof CommandAuditFilters>(key: K, value: CommandAuditFilters[K]) => void
  onApply: () => void
  onOpenAdvanced: () => void
}

export function CommandAuditFilterPanel({
  filters,
  onChange,
  onApply,
  onOpenAdvanced,
}: CommandAuditFilterPanelProps) {
  const commandOptions = COMMAND_OPTIONS.filter((option) => option.value !== '')

  return (
    <form
      className="filter-bar filter-bar--stacked monitoring-page__filters command-audit-filter"
      aria-label="命令审计筛选"
      onSubmit={(event) => {
        event.preventDefault()
        onApply()
      }}
    >
      <div className="filter-bar__controls">
        <div className="filter-bar__controls-row">
          <FilterSelect
            label="时间范围"
            value={filters.window}
            options={WINDOW_OPTIONS}
            placeholder="最近 30 天"
            onChange={(value) => {
              if (
                value === '24h'
                || value === '7d'
                || value === '30d'
                || value === 'all'
                || value === 'custom'
              ) {
                onChange('window', value)
              }
            }}
          />
          <label className="filter-select">
            <span className="filter-select__label">监控实例</span>
            <input
              className="filter-select__control"
              placeholder="名称或稳定 ID"
              value={filters.monitoring_instance}
              onChange={(event) => onChange('monitoring_instance', event.target.value)}
            />
          </label>
          <FilterSelect
            label="命令"
            value={filters.command_id || null}
            options={commandOptions}
            placeholder="全部命令"
            onChange={(value) => onChange('command_id', value ?? '')}
          />
          <FilterSelect
            label="结果"
            value={filters.outcome || null}
            options={OUTCOME_OPTIONS}
            placeholder="全部结果"
            onChange={(value) => onChange('outcome', (value ?? '') as CommandAuditFilters['outcome'])}
          />
          <div className="monitoring-page__trailing-controls command-audit-filter__actions">
            <Button type="submit" size="sm">应用筛选</Button>
            <Button type="button" size="sm" variant="secondary" onClick={onOpenAdvanced}>
              高级筛选
            </Button>
          </div>
        </div>
      </div>
      {filters.window === 'custom' ? (
        <div className="filter-bar__controls-row command-audit-filter__custom">
          <label className="filter-select">
            <span className="filter-select__label">开始时间</span>
            <input
              className="filter-select__control"
              type="datetime-local"
              value={filters.started_from}
              onChange={(event) => onChange('started_from', event.target.value)}
            />
          </label>
          <label className="filter-select">
            <span className="filter-select__label">结束时间</span>
            <input
              className="filter-select__control"
              type="datetime-local"
              value={filters.started_to}
              onChange={(event) => onChange('started_to', event.target.value)}
            />
          </label>
        </div>
      ) : null}
    </form>
  )
}
