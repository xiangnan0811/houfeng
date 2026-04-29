import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ActionConfirmationCard } from './ActionConfirmationCard'

describe('ActionConfirmationCard', () => {
  it('shows the confirmation eyebrow in Chinese-first copy', () => {
    render(
      <ActionConfirmationCard
        title="确认切换"
        current="当前：A"
        result="结果：B"
        impact="影响：立即生效"
        unchanged="不变：其他配置"
        confirmLabel="确认"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(screen.getByText('操作确认')).toBeInTheDocument()
  })
})
