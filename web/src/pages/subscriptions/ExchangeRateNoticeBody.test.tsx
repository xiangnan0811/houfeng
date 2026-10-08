import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ExchangeRateNoticeBody } from './ExchangeRateNoticeBody'

describe('ExchangeRateNoticeBody', () => {
  it('keeps the raw provider summary inside a closed diagnostic', () => {
    render(
      <ExchangeRateNoticeBody
        notice={{
          tone: 'error',
          text: '补取失败：JPY',
          diagnostics: [{ label: 'JPY', detail: 'timeout' }],
        }}
      />,
    )

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('补取失败：JPY')
    const summary = screen.getByText('补取失败：JPY')
    expect(summary.closest('details')).toBeNull()
    const details = screen.getByText('timeout').closest('details')
    expect(details).not.toHaveAttribute('open')
    expect(details).toHaveTextContent('JPY')
  })

  it('renders a status without diagnostics when the refresh has no error summary', () => {
    render(<ExchangeRateNoticeBody notice={{ tone: 'status', text: '汇率已更新' }} />)
    expect(screen.getByRole('status')).toHaveTextContent('汇率已更新')
    expect(screen.queryByText('补取诊断')).not.toBeInTheDocument()
  })
})
