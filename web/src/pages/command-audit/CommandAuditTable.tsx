import { Link } from 'react-router-dom'

import { Badge, DataTable, MonoDigits, Timestamp, type DataTableColumn } from '../../components/atoms'
import { COMMAND_LABELS } from '../../config/commands'
import type { CommandAuditAction, CommandAuditOutcome } from '../../lib/types'
import { CommandAuditEventTimeline } from './CommandAuditEventTimeline'

const OUTCOME_PRESENTATION: Record<CommandAuditOutcome, { label: string; tone: 'normal' | 'notice' | 'alert' | 'critical' | 'neutral' }> = {
  rejected: { label: '已拒绝', tone: 'alert' },
  queued: { label: '已排队', tone: 'neutral' },
  dispatched: { label: '已派发', tone: 'notice' },
  succeeded: { label: '成功', tone: 'normal' },
  failed: { label: '失败', tone: 'critical' },
}

function actorName(row: CommandAuditAction): string {
  return row.actor?.display_name || row.actor?.username || row.actor?.user_id || '系统'
}

type CommandAuditTableProps = {
  rows: CommandAuditAction[]
  expandedIDs: Set<string>
  onToggle: (id: string) => void
}

export function CommandAuditTable({ rows, expandedIDs, onToggle }: CommandAuditTableProps) {
  const columns: DataTableColumn<CommandAuditAction>[] = [
    {
      key: 'started_at',
      label: '时间',
      width: 150,
      render: (row) => <Timestamp value={row.started_at} mode="absolute" />,
    },
    {
      key: 'monitoring_instance',
      label: '实例',
      width: 190,
      render: (row) => {
        const name = row.monitoring_instance.name || row.monitoring_instance.id
        return (
          <div className="command-audit-table__identity">
            <div className="command-audit-table__identity-main">
              {row.monitoring_instance.deleted
                ? <span>{name}</span>
                : <Link to={`/monitoring/${encodeURIComponent(row.monitoring_instance.id)}`}>{name}</Link>}
              {row.monitoring_instance.deleted ? <Badge tone="notice">已删除</Badge> : null}
            </div>
            <div><MonoDigits>{row.monitoring_instance.id}</MonoDigits></div>
          </div>
        )
      },
    },
    {
      key: 'command_id',
      label: '命令',
      width: 160,
      render: (row) => (
        <div className="command-audit-table__stack">
          <span>{COMMAND_LABELS[row.command_id] ?? row.command_id}</span>
          <div><MonoDigits>{row.command_id}</MonoDigits></div>
        </div>
      ),
    },
    {
      key: 'sensitivity',
      label: '敏感级别',
      width: 100,
      render: (row) => (
        <Badge tone={row.sensitivity === 'sensitive' ? 'notice' : 'neutral'}>
          {row.sensitivity === 'sensitive' ? '敏感' : '标准'}
        </Badge>
      ),
    },
    {
      key: 'actor',
      label: '操作者',
      width: 160,
      render: (row) => (
        <div className="command-audit-table__stack">
          <span>{actorName(row)}</span>
          {row.actor ? <div><MonoDigits>{row.actor.user_id}</MonoDigits></div> : null}
        </div>
      ),
    },
    {
      key: 'outcome',
      label: '结果',
      width: 100,
      render: (row) => {
        const presentation = OUTCOME_PRESENTATION[row.outcome]
        return <Badge variant="state" tone={presentation.tone} withDot>{presentation.label}</Badge>
      },
    },
    {
      key: 'action_id',
      label: 'Action ID',
      width: 170,
      render: (row) => <MonoDigits>{row.action_id || '—'}</MonoDigits>,
    },
    {
      key: 'events',
      label: '事件',
      width: 330,
      render: (row) => {
        const expanded = expandedIDs.has(row.id)
        const count = row.events.length
        return (
          <div className="command-audit-table__events">
            {count > 0 ? (
              <button
                type="button"
                className="btn sm ghost command-audit-table__toggle"
                aria-expanded={expanded}
                onClick={() => onToggle(row.id)}
              >
                {expanded ? `收起 ${count} 个事件` : `展开 ${count} 个事件`}
              </button>
            ) : <span className="empty-inline">无事件</span>}
            {expanded && count > 0
              ? <CommandAuditEventTimeline actionID={row.id} events={row.events} />
              : null}
          </div>
        )
      },
    },
  ]

  return (
    <>
      <div className="section-heading">
        <h2 className="section-heading__title" id="command-audit-table-heading">审计记录</h2>
      </div>
      <p className="events-table-scroll-hint" id="command-audit-table-scroll-hint">
        可横向滚动；聚焦表格区域后可使用方向键浏览。
      </p>
      <div
        className="events-table-scroll command-audit-table-scroll"
        role="region"
        aria-labelledby="command-audit-table-heading"
        aria-describedby="command-audit-table-scroll-hint"
        tabIndex={0}
      >
        <DataTable
          className="command-audit-table"
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          density="compact"
        />
      </div>
    </>
  )
}
