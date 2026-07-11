import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AssetDecisionScenarioTemplateDetail } from '../../../lib/types'
import { TemplateDetailModal } from './TemplateDetailModal'

const DETAIL = {
  template_id: 'adt_001',
  builtin: false,
  status: 'active',
  scenario: 'provider_review',
  title: '服务商评估',
  goal: '复核服务商组合',
  member_count: 0,
  members: [],
} as unknown as AssetDecisionScenarioTemplateDetail

describe('TemplateDetailModal', () => {
  it('emits semantic manual-group draft patches', () => {
    const updateDraft = vi.fn()

    render(
      <TemplateDetailModal
        open
        templateDetailState={{ loading: false, error: null, detail: DETAIL }}
        templateDetailPanel="create"
        templateError={null}
        templateSaving={false}
        pendingTemplateStatus={null}
        templateManualDraft={{
          title: '服务商评估',
          goal: '复核服务商组合',
          note: '',
          renewWithinDays: 30,
        }}
        onClose={vi.fn()}
        onSetTemplateDetailPanel={vi.fn()}
        onRequestTemplateStatusUpdate={vi.fn()}
        onCancelTemplateStatusUpdate={vi.fn()}
        onUpdateTemplateStatus={vi.fn()}
        onSubmitTemplateManualGroup={vi.fn()}
        onUpdateTemplateManualDraft={updateDraft}
      />,
    )

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '自定义组合标题' } })
    expect(updateDraft).toHaveBeenCalledWith({ title: '自定义组合标题' })

    fireEvent.change(screen.getByLabelText('续费窗口'), { target: { value: '90' } })
    expect(updateDraft).toHaveBeenCalledWith({ renewWithinDays: 90 })
  })
})
