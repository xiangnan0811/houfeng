import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { SubjectIdentityBar } from './SubjectIdentityBar'

describe('SubjectIdentityBar', () => {
  it('renders live identity with display name and return link', () => {
    render(
      <MemoryRouter>
        <SubjectIdentityBar
          subject={{
            kind: 'vps',
            source_id: 'vps_001',
            identity: { display_name: '东京边缘' },
            live_route: '/vps/vps_001',
            status: 'live',
          }}
          returnHref="/vps/vps_001"
          actions={<button type="button">新建记录</button>}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(screen.getByText('VPS')).toBeInTheDocument()
    expect(screen.getByText('vps_001')).toBeInTheDocument()
    expect(screen.getByText('在册')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回主体' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByRole('button', { name: '新建记录' })).toBeInTheDocument()
  })

  it('marks tombstoned subjects without inventing a live name', () => {
    render(
      <MemoryRouter>
        <SubjectIdentityBar
          subject={{
            kind: 'monitoring_instance',
            source_id: 'mi_gone',
            identity: { display_name: '已删除实例' },
            status: 'tombstoned',
          }}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('已删除主体')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '已删除实例' })).toBeInTheDocument()
  })
})

describe('SubjectIdentityBar layout', () => {
  it('puts kind and id in one identity row with a kind mark and hostname when it differs', () => {
    const { container } = render(
      <MemoryRouter>
        <SubjectIdentityBar
          subject={{
            kind: 'monitoring_instance',
            source_id: 'mi_001',
            identity: { display_name: 'alpha 主机监控', hostname: 'alpha.example.net' },
            status: 'live',
          }}
          returnHref="/monitoring/mi_001"
          returnLabel="返回详情"
        />
      </MemoryRouter>,
    )
    const identity = screen.getByLabelText('主体身份')
    expect(within(identity).getAllByRole('term').map((term) => term.textContent)).toEqual(['类型', 'ID', '主机名'])
    expect(within(identity).getByText('监控实例')).toBeInTheDocument()
    expect(container.querySelector('.subject-identity-bar__mark svg')).not.toBeNull()
    expect(screen.getByRole('link', { name: '返回详情' })).toHaveClass('btn')
  })
})
