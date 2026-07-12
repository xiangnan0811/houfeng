import { Button, Input, Select } from '../../components/atoms'
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
  { value: '', label: '全部结果' },
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
  return (
    <form
      className="filter-bar command-audit-filter"
      aria-label="命令审计筛选"
      onSubmit={(event) => {
        event.preventDefault()
        onApply()
      }}
    >
      <div className="events-filter-drawer__fields command-audit-filter__primary">
        <Select
          label="时间范围"
          value={filters.window}
          options={WINDOW_OPTIONS}
          onChange={(event) => onChange('window', event.target.value as CommandAuditFilters['window'])}
        />
        <Input
          label="监控实例"
          placeholder="名称或稳定 ID"
          value={filters.monitoring_instance}
          onChange={(event) => onChange('monitoring_instance', event.target.value)}
        />
        <Select
          label="命令"
          value={filters.command_id}
          options={COMMAND_OPTIONS}
          onChange={(event) => onChange('command_id', event.target.value)}
        />
        <Select
          label="结果"
          value={filters.outcome}
          options={OUTCOME_OPTIONS}
          onChange={(event) => onChange('outcome', event.target.value as CommandAuditFilters['outcome'])}
        />
        <div className="section-heading__actions command-audit-filter__actions">
          <Button type="submit" size="sm">应用筛选</Button>
          <Button type="button" size="sm" variant="secondary" onClick={onOpenAdvanced}>
            高级筛选
          </Button>
        </div>
      </div>
      {filters.window === 'custom' ? (
        <div className="events-filter-drawer__fields command-audit-filter__custom">
          <Input
            label="开始时间"
            type="datetime-local"
            value={filters.started_from}
            onChange={(event) => onChange('started_from', event.target.value)}
          />
          <Input
            label="结束时间"
            type="datetime-local"
            value={filters.started_to}
            onChange={(event) => onChange('started_to', event.target.value)}
          />
        </div>
      ) : null}
    </form>
  )
}
