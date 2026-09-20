import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ObservabilityNotice } from './ObservabilityNotice'
import { ObservabilityNoticeRow } from './ObservabilityNoticeRow'

describe('ObservabilityNotice', () => {
  it('renders well copy, tone class, and in-band action', () => {
    render(
      <ObservabilityNotice
        tone="critical"
        mark="严重"
        title="磁盘使用率持续超过阈值"
        detail="活跃 1"
        action={<button type="button">查看事件</button>}
        role="alert"
      />,
    )

    const well = document.querySelector('.observability-notice')
    expect(well).toHaveClass('observability-notice--critical')
    expect(well).toHaveClass('monitoring-detail-notice--critical')
    expect(screen.getByText('严重')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率持续超过阈值')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看事件' })).toBeInTheDocument()
  })
})

describe('ObservabilityNoticeRow', () => {
  it('renders compact row copy and timestamp', () => {
    render(
      <ul>
        <ObservabilityNoticeRow
          tone="alert"
          mark="告警"
          title="异常开始"
          detail="心跳超时"
          meta="心跳超时"
          time="2026-07-11T12:00:00Z"
        />
      </ul>,
    )

    const row = document.querySelector('.observability-notice-row')
    expect(row).toHaveClass('observability-notice-row--alert')
    expect(row?.tagName).toBe('LI')
    expect(screen.getByText('告警')).toBeInTheDocument()
    expect(screen.getByText('异常开始')).toBeInTheDocument()
  })
})
