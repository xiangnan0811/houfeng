import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { NodeLabelsAndNote } from './NodeLabelsAndNote'
import type { NodeRecord } from '../../lib/types'

function nodeRecord(overrides: Partial<NodeRecord> = {}): NodeRecord {
  return {
    node_id: 'nd_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: ['edge', 'core'],
    note: 'keep me',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

const noopHandlers = {
  onLabelDraftChange: () => {},
  onNoteDraftChange: () => {},
  onStartEdit: () => {},
  onCancelEdit: () => {},
  onSave: () => {},
}

describe('NodeLabelsAndNote', () => {
  it('renders read-only labels, note and edit button', () => {
    render(
      <NodeLabelsAndNote
        node={nodeRecord()}
        editing={false}
        labelDraft=""
        noteDraft=""
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '标签与备注' })).toBeInTheDocument()
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
    expect(screen.getByText('备注：keep me')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument()
  })

  it('shows "暂无备注" placeholder when note is empty', () => {
    render(
      <NodeLabelsAndNote
        node={nodeRecord({ note: '' })}
        editing={false}
        labelDraft=""
        noteDraft=""
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('备注：暂无备注')).toBeInTheDocument()
  })

  it('renders editing form with submit/cancel buttons and reflects drafts', () => {
    const onSave = vi.fn()
    render(
      <NodeLabelsAndNote
        node={nodeRecord()}
        editing
        labelDraft="edge"
        noteDraft="hello"
        submitting={false}
        error={null}
        {...noopHandlers}
        onSave={onSave}
      />,
    )

    expect(screen.getByRole('textbox', { name: '标签' })).toHaveValue('edge')
    expect(screen.getByRole('textbox', { name: '备注' })).toHaveValue('hello')
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('renders error message via role="alert"', () => {
    render(
      <NodeLabelsAndNote
        node={nodeRecord()}
        editing
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
