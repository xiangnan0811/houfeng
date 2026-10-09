import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { VPSObservationRows } from './VPSObservationRows'

describe('VPSObservationRows', () => {
  it('marks cells without content so the narrow stacked layout can drop label-only rows', () => {
    render(
      <VPSObservationRows
        ariaLabel="运行观测"
        rows={[
          { key: 'bare', project: 'IP 质量', conclusion: '未检测' },
          {
            key: 'full',
            project: '监控',
            conclusion: '正常',
            description: '最近一次心跳正常',
            time: '2026-10-09T08:00:00Z',
            action: <button type="button">查看实例</button>,
          },
        ]}
      />,
    )

    const bare = screen.getByRole('rowgroup', { name: 'IP 质量' })
    expect(bare.querySelector('.vps-observation__description')).toHaveClass('vps-observation__cell--empty')
    expect(bare.querySelector('.vps-observation__time')).toHaveClass('vps-observation__cell--empty')
    expect(bare.querySelector('.vps-observation__action')).toHaveClass('vps-observation__cell--empty')
    expect(bare.querySelector('.vps-observation__conclusion')).not.toHaveClass('vps-observation__cell--empty')

    const full = screen.getByRole('rowgroup', { name: '监控' })
    expect(full.querySelectorAll('.vps-observation__cell--empty')).toHaveLength(0)
  })

  it('pins every cell to its column so hidden empty cells do not shift header association', () => {
    render(<VPSObservationRows ariaLabel="运行观测" rows={[{ key: 'bare', project: 'IP 质量', conclusion: '未检测' }]} />)

    const table = screen.getByRole('table', { name: '运行观测' })
    expect(table).toHaveAttribute('aria-colcount', '5')
    const headers = new Map(screen.getAllByRole('columnheader').map((header) => [header.getAttribute('aria-colindex'), header.textContent]))
    expect(headers.size).toBe(5)
    for (const cell of screen.getByRole('rowgroup', { name: 'IP 质量' }).querySelectorAll<HTMLElement>('[role="cell"]')) {
      expect(headers.get(cell.getAttribute('aria-colindex'))).toBe(cell.dataset.label)
    }
  })

  it('treats a ready source without any time as an empty data-time slot', () => {
    render(
      <VPSObservationRows
        ariaLabel="运行观测"
        rows={[
          { key: 'pending', project: '监控', conclusion: '未接入', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
          { key: 'seen', project: 'IP 质量', conclusion: '正常', section: { state: 'ready', observed_at: '2026-10-09T08:00:00Z', last_success_at: null, reason_code: '' } },
        ]}
      />,
    )

    expect(screen.getByRole('rowgroup', { name: '监控' }).querySelector('.vps-observation__time')).toHaveClass('vps-observation__cell--empty')
    expect(screen.getByRole('rowgroup', { name: 'IP 质量' }).querySelector('.vps-observation__time')).not.toHaveClass('vps-observation__cell--empty')
  })

  it('treats a description that only repeats the conclusion as empty', () => {
    render(<VPSObservationRows ariaLabel="运行观测" rows={[{ key: 'dup', project: '综合', conclusion: '正常', description: ' 正常 ' }]} />)

    const row = screen.getByRole('rowgroup', { name: '综合' })
    expect(row.querySelector('.vps-observation__description')).toHaveClass('vps-observation__cell--empty')
  })

  it('keeps diagnostics and retry cells visible', () => {
    render(
      <VPSObservationRows
        ariaLabel="运行观测"
        rows={[{
          key: 'diag',
          project: '监控',
          conclusion: '不可用',
          diagnostics: [{ label: '原因', detail: '读取失败' }],
          section: { state: 'unavailable', observed_at: null, last_success_at: null, reason_code: 'read_failed' },
          onRetry: () => undefined,
        }]}
      />,
    )

    const row = screen.getByRole('rowgroup', { name: '监控' })
    expect(row.querySelectorAll('.vps-observation__cell--empty')).toHaveLength(0)
  })
})
