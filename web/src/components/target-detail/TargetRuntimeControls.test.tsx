import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { TargetRuntimeControls } from './TargetRuntimeControls'
import type { TargetRecord } from '../../lib/types'

function targetRecord(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    execution_node_labels: [],
    run_status: '启用',
    group: '',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

const noopHandlers = {
  onAction: () => {},
  onConfirm: () => {},
  onCancelConfirmation: () => {},
  registerActionButtonRef: () => {},
}

describe('TargetRuntimeControls', () => {
  it('renders the action buttons that match the target run_status', () => {
    render(
      <TargetRuntimeControls
        target={targetRecord({ run_status: '启用' })}
        disabled={false}
        submitting={false}
        error={null}
        pendingConfirmation={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('button', { name: '进入维护' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '归档' })).toBeInTheDocument()
  })

  it('invokes onAction with the corresponding action when a button is clicked', () => {
    const onAction = vi.fn()
    render(
      <TargetRuntimeControls
        target={targetRecord({ run_status: '已归档' })}
        disabled={false}
        submitting={false}
        error={null}
        pendingConfirmation={null}
        {...noopHandlers}
        onAction={onAction}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '恢复到暂停' }))
    expect(onAction).toHaveBeenCalledWith('restore-to-paused')
  })

  it('renders the pause confirmation card when pendingConfirmation is pause', () => {
    render(
      <TargetRuntimeControls
        target={targetRecord()}
        disabled={false}
        submitting={false}
        error={null}
        pendingConfirmation={{ action: 'pause' }}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停目标' })).toBeInTheDocument()
  })

  it('renders the runtime error message inline', () => {
    render(
      <TargetRuntimeControls
        target={targetRecord()}
        disabled={false}
        submitting={false}
        error="目标运行控制操作失败"
        pendingConfirmation={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('目标运行控制操作失败')).toBeInTheDocument()
  })
})
