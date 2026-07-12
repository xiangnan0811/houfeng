import type { FormEvent } from 'react'

import { Button, Input, Modal, Select } from '../../components/atoms'
import type { CommandAuditFilters } from './types'

const SENSITIVITY_OPTIONS = [
  { value: '', label: '全部级别' },
  { value: 'standard', label: '标准' },
  { value: 'sensitive', label: '敏感' },
]

type CommandAuditFilterDrawerProps = {
  open: boolean
  filters: CommandAuditFilters
  onChange: <K extends keyof CommandAuditFilters>(key: K, value: CommandAuditFilters[K]) => void
  onApply: () => void
  onReset: () => void
  onClose: () => void
}

export function CommandAuditFilterDrawer({
  open,
  filters,
  onChange,
  onApply,
  onReset,
  onClose,
}: CommandAuditFilterDrawerProps) {
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onApply()
  }

  return (
    <Modal open={open} onClose={onClose} title="高级筛选" ariaLabel="命令审计高级筛选" size="lg">
      <form className="events-filter-drawer command-audit-drawer" onSubmit={handleSubmit}>
        <p className="page-sub command-audit-drawer__hint">
          按敏感级别、操作者快照或稳定 Action ID 缩小审计范围。
        </p>
        <div className="events-filter-drawer__fields command-audit-drawer__fields">
          <Select
            label="敏感级别"
            value={filters.sensitivity}
            options={SENSITIVITY_OPTIONS}
            onChange={(event) => onChange('sensitivity', event.target.value as CommandAuditFilters['sensitivity'])}
          />
          <Input
            label="操作者"
            placeholder="用户 ID、用户名或显示名"
            value={filters.actor}
            onChange={(event) => onChange('actor', event.target.value)}
          />
          <Input
            label="Action ID"
            placeholder="act_…"
            value={filters.action_id}
            onChange={(event) => onChange('action_id', event.target.value)}
          />
        </div>
        <div className="events-filter-drawer__actions command-audit-drawer__actions">
          <Button type="button" variant="ghost" onClick={onReset}>重置高级筛选</Button>
          <Button type="button" variant="secondary" onClick={onClose}>取消</Button>
          <Button type="submit">应用高级筛选</Button>
        </div>
      </form>
    </Modal>
  )
}
