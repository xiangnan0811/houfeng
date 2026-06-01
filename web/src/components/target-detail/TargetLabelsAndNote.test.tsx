import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { TargetLabelsAndNote } from './TargetLabelsAndNote'
import type { TargetRecord } from '../../lib/types'

function targetRecord(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    execution_monitoring_instance_labels: [],
    run_status: '启用',
    group: 'prod-group',
    labels: ['公开', '生产'],
    note: '现网入口',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

const noopHandlers = {
  onGroupDraftChange: () => {},
  onLabelDraftChange: () => {},
  onNoteDraftChange: () => {},
  onStartEdit: () => {},
  onCancelEdit: () => {},
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => event.preventDefault(),
}

describe('TargetLabelsAndNote', () => {
  it('renders read-only labels, note and edit button', () => {
    render(
      <TargetLabelsAndNote
        target={targetRecord()}
        editing={false}
        groupDraft=""
        labelDraft=""
        noteDraft=""
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '标签与备注' })).toBeInTheDocument()
    expect(screen.getByText('标签：公开 · 生产')).toBeInTheDocument()
    expect(screen.getByText('备注：现网入口')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument()
  })

  it('shows "暂无备注" placeholder when note is empty', () => {
    render(
      <TargetLabelsAndNote
        target={targetRecord({ note: '' })}
        editing={false}
        groupDraft=""
        labelDraft=""
        noteDraft=""
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('备注：暂无备注')).toBeInTheDocument()
  })

  it('renders editing form and calls onSubmit when the save button is clicked', () => {
    const onSubmit = vi.fn((event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault()
    })
    render(
      <TargetLabelsAndNote
        target={targetRecord()}
        editing
        groupDraft="prod-group"
        labelDraft="edge"
        noteDraft="hello"
        submitting={false}
        error={null}
        {...noopHandlers}
        onSubmit={onSubmit}
      />,
    )

    expect(screen.getByLabelText('Group')).toHaveValue('prod-group')
    expect(screen.getByLabelText('标签')).toHaveValue('edge')
    expect(screen.getByLabelText('备注')).toHaveValue('hello')
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    expect(onSubmit).toHaveBeenCalledTimes(1)
  })

  it('renders error message via role="alert"', () => {
    render(
      <TargetLabelsAndNote
        target={targetRecord()}
        editing
        groupDraft=""
        labelDraft=""
        noteDraft=""
        submitting={false}
        error="metadata write failed"
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('metadata write failed')
  })
})
