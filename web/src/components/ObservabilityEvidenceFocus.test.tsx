import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Hostname, StatusGlyph } from './atoms'
import { ObservabilityEvidenceFocus } from './ObservabilityEvidenceFocus'

describe('ObservabilityEvidenceFocus', () => {
  it('renders priority evidence with supplied glyph, meta, and action', () => {
    render(
      <ObservabilityEvidenceFocus
        glyph={<StatusGlyph state="alert" ariaLabel="异常证据状态" />}
        eyebrow="优先核对节点"
        title="优先核对：Alerting Edge"
        description="健康状态：告警"
        meta={
          <>
            <Hostname truncate maxChars={18}>nd_alert</Hostname>
            {' · '}
            2 个活跃异常
          </>
        }
        action={<a href="/nodes/nd_alert">查看证据</a>}
      />,
    )

    expect(screen.getByLabelText('异常证据状态')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '优先核对：Alerting Edge' })).toBeInTheDocument()
    expect(screen.getByText('健康状态：告警')).toBeInTheDocument()
    expect(screen.getByText(/2 个活跃异常/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute(
      'href',
      '/nodes/nd_alert',
    )
  })

  it('marks stable evidence rows with the stable class', () => {
    const { container } = render(
      <ObservabilityEvidenceFocus
        stable
        glyph={<StatusGlyph state="normal" ariaLabel="证据稳定" />}
        eyebrow="运行证据"
        title="没有需要优先核对的 Node"
        description="当前列表没有异常对象。"
        meta="继续从 VPS 库存核对资产侧事实。"
        action={<a href="/vps">查看 VPS</a>}
      />,
    )

    expect(container.querySelector('.observability-evidence-focus')).toHaveClass(
      'observability-evidence-focus--stable',
    )
    expect(screen.getByRole('heading', { name: '没有需要优先核对的 Node' })).toBeInTheDocument()
  })
})
