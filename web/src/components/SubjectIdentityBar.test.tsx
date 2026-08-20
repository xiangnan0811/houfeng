import { render, screen } from '@testing-library/react'
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
