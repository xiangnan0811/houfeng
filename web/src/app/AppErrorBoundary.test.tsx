import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppErrorBoundary } from './AppErrorBoundary'

let shouldThrow = true

function CrashingChild() {
  if (shouldThrow) throw new Error('private-render-token-42')
  return <p>应用已恢复</p>
}

describe('AppErrorBoundary', () => {
  beforeEach(() => {
    shouldThrow = true
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows safe recovery actions and can retry the render', () => {
    const reloadPage = vi.fn()
    render(
      <AppErrorBoundary reloadPage={reloadPage}>
        <CrashingChild />
      </AppErrorBoundary>,
    )

    expect(screen.getByRole('heading', { name: '应用暂时无法继续' })).toBeInTheDocument()
    expect(document.body).not.toHaveTextContent('private-render-token-42')
    expect(screen.getByRole('link', { name: '返回工作台' })).toHaveAttribute('href', '/')

    fireEvent.click(screen.getByRole('button', { name: '刷新页面' }))
    expect(reloadPage).toHaveBeenCalledTimes(1)

    shouldThrow = false
    fireEvent.click(screen.getByRole('button', { name: '重试渲染' }))
    expect(screen.getByText('应用已恢复')).toBeInTheDocument()
  })
})
