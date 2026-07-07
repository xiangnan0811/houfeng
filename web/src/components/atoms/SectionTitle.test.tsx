import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { SectionTitle } from './SectionTitle'

describe('SectionTitle', () => {
  it('renders the title', () => {
    render(<SectionTitle title="关注" />)
    expect(screen.getByText('关注')).toHaveClass('section-title__label')
  })

  it('renders the count chip when provided', () => {
    render(<SectionTitle title="资产总览" count={8} />)
    expect(screen.getByText('8')).toHaveClass('section-count')
  })

  it('renders the action slot when provided', () => {
    render(
      <SectionTitle
        title="观测事实"
        action={<span className="panel-link">列表 →</span>}
      />,
    )
    expect(screen.getByText('列表 →')).toBeInTheDocument()
  })

  it('applies a custom className to the root', () => {
    render(<SectionTitle title="X" className="mt-4" />)
    const root = screen.getByText('X').closest('.section-title')
    expect(root).toHaveClass('mt-4')
  })
})
