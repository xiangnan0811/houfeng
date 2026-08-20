import type { SubjectActivityFilters } from './activityQueryState'

type Props = {
  value: SubjectActivityFilters
  onChange: (next: SubjectActivityFilters) => void
  disabled?: boolean
}

/**
 * Lightweight filter controls for subject activity. Changing any filter must
 * clear the cursor at the page layer — this component only edits filter state.
 */
export function SubjectActivityFilters({ value, onChange, disabled = false }: Props) {
  return (
    <div className="subject-activity-filters">
      <label className="subject-activity-filters__field">
        <span>来源</span>
        <select
          disabled={disabled}
          value={value.source?.[0] ?? ''}
          onChange={(event) => {
            const next = event.target.value
            if (!next) {
              const rest = { ...value }
              delete rest.source
              onChange(rest)
              return
            }
            onChange({
              ...value,
              source: [next as NonNullable<SubjectActivityFilters['source']>[number]],
            })
          }}
        >
          <option value="">全部来源</option>
          <option value="record_domain">人工记录</option>
          <option value="evidence_snapshot">证据快照</option>
          <option value="asset_history">资产事实</option>
          <option value="monitoring_event">监控事件</option>
          <option value="command_audit">命令审计</option>
        </select>
      </label>
      <label className="subject-activity-filters__field">
        <span>版本</span>
        <select
          disabled={disabled}
          value={value.versions ?? 'history'}
          onChange={(event) => {
            const next = event.target.value as 'history' | 'current'
            if (next === 'history') {
              const rest = { ...value }
              delete rest.versions
              onChange(rest)
              return
            }
            onChange({
              ...value,
              versions: next,
            })
          }}
        >
          <option value="history">完整历史</option>
          <option value="current">当前有效</option>
        </select>
      </label>
    </div>
  )
}
