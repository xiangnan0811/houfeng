import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { CommandAuditAction } from '../../lib/types'
import { CommandAuditTable } from './CommandAuditTable'

function action(outcome: CommandAuditAction['outcome'], id: string): CommandAuditAction {
  return {
    id,
    ...(outcome === 'rejected' ? {} : { action_id: id }),
    monitoring_instance: { id: `mi_${id}`, name: `实例 ${id}`, deleted: false },
    command_id: outcome === 'rejected' ? 'systemctl_status' : 'uptime',
    sensitivity: outcome === 'rejected' ? 'sensitive' : 'standard',
    outcome,
    actor: { user_id: 'usr_001', username: 'admin', display_name: '管理员' },
    started_at: '2026-07-12T10:00:00Z',
    events: [],
  }
}

describe('CommandAuditTable', () => {
  it('renders five outcomes, shared command labels, and current/deleted identities', () => {
    const rows = [
      action('rejected', 'cmd_aud_rejected'),
      action('queued', 'act_queued'),
      action('dispatched', 'act_dispatched'),
      action('succeeded', 'act_succeeded'),
      action('failed', 'act_failed'),
    ]
    rows[0]!.monitoring_instance = { id: 'mi_deleted', name: 'Osaka Relay', deleted: true }
    rows[0]!.actor = { user_id: 'usr_deleted', username: '', display_name: '' }

    render(
      <MemoryRouter>
        <CommandAuditTable rows={rows} expandedIDs={new Set()} onToggle={vi.fn()} />
      </MemoryRouter>,
    )

    for (const label of ['已拒绝', '已排队', '已派发', '成功', '失败']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    expect(screen.getAllByText('uptime').length).toBeGreaterThan(0)
    expect(screen.getByText('systemctl status')).toBeInTheDocument()
    expect(screen.getAllByText('usr_deleted')).toHaveLength(2)
    expect(screen.getByText('已删除')).toBeInTheDocument()
    expect(screen.getByText('Osaka Relay').closest('a')).toBeNull()
    expect(screen.getByRole('link', { name: '实例 act_queued' })).toHaveAttribute(
      'href',
      '/monitoring/mi_act_queued',
    )
  })

  it('uses a semantic expansion button and renders allowlisted events in stable order', () => {
    const row = action('succeeded', 'act_001') as CommandAuditAction & {
      stdout?: string
      stderr?: string
      details?: Record<string, unknown>
    }
    row.stdout = 'TOP LEVEL SECRET'
    row.stderr = 'TOP LEVEL ERROR'
    row.details = { stdout: 'NESTED SECRET' }
    row.events = [
      {
        audit_id: 'cmd_aud_completed',
        event_type: 'completed',
        source: 'agent_sync',
        occurred_at: '2026-07-12T10:00:02Z',
        exit_code: 0,
        ...({ stdout: 'EVENT SECRET', details: { stderr: 'EVENT ERROR' } } as object),
      },
      {
        audit_id: 'cmd_aud_queued',
        event_type: 'queued',
        source: 'web',
        occurred_at: '2026-07-12T10:00:00Z',
      },
    ]
    const onToggle = vi.fn()
    const { rerender } = render(
      <MemoryRouter>
        <CommandAuditTable rows={[row]} expandedIDs={new Set()} onToggle={onToggle} />
      </MemoryRouter>,
    )

    const expand = screen.getByRole('button', { name: '展开 2 个事件' })
    expect(expand).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(expand)
    expect(onToggle).toHaveBeenCalledWith('act_001')

    rerender(
      <MemoryRouter>
        <CommandAuditTable rows={[row]} expandedIDs={new Set(['act_001'])} onToggle={onToggle} />
      </MemoryRouter>,
    )
    const timeline = screen.getByRole('region', { name: 'act_001 原始审计事件' })
    expect(screen.getByRole('button', { name: '收起 2 个事件' })).toHaveAttribute('aria-expanded', 'true')
    expect(within(timeline).getAllByTestId('command-audit-event-type').map((node) => node.textContent)).toEqual([
      '已排队',
      '已完成',
    ])
    expect(within(timeline).getByText(/退出码/)).toHaveTextContent('退出码 0')
    for (const secret of ['TOP LEVEL SECRET', 'TOP LEVEL ERROR', 'NESTED SECRET', 'EVENT SECRET', 'EVENT ERROR']) {
      expect(screen.queryByText(secret)).not.toBeInTheDocument()
    }
  })

  it('owns horizontal overflow in a named keyboard-focusable region', () => {
    render(
      <MemoryRouter>
        <CommandAuditTable rows={[action('queued', 'act_001')]} expandedIDs={new Set()} onToggle={vi.fn()} />
      </MemoryRouter>,
    )

    expect(screen.getByRole('region', { name: '命令审计结果，可横向滚动' })).toHaveAttribute('tabindex', '0')
  })
})
