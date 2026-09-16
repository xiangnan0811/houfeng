import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { AssetDomainRecord, AssetServiceRecord } from '../../lib/types'
import { VPSDomainsSection } from './VPSDomainsSection'

function service(overrides: Partial<AssetServiceRecord> = {}): AssetServiceRecord {
  return {
    service_id: 'svc_001',
    vps_id: 'vps_001',
    name: 'Gateway',
    service_type: 'web',
    status: 'active',
    url: 'https://example.invalid',
    labels: [],
    note: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

function domain(overrides: Partial<AssetDomainRecord> = {}): AssetDomainRecord {
  return {
    domain_id: 'domain_001',
    vps_id: 'vps_001',
    domain_name: 'edge.example.com',
    purpose: 'gateway',
    status: 'active',
    registrar: 'Example',
    auto_renew: true,
    https_enabled: true,
    labels: [],
    note: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

function renderSection(domains: AssetDomainRecord[], services: AssetServiceRecord[] = []) {
  return render(
    <MemoryRouter>
      <VPSDomainsSection
        domains={domains}
        services={services}
        error={null}
        notice={null}
        onCreate={vi.fn()}
      />
    </MemoryRouter>,
  )
}

describe('VPSDomainsSection', () => {
  it('shows the linked service name beside the association id when metadata arrives', () => {
    renderSection([domain({ service_id: 'svc_001' })], [service()])
    const row = within(screen.getByRole('listitem'))
    expect(row.getByText('Gateway')).toBeInTheDocument()
    expect(row.getByText('svc_001')).toBeInTheDocument()
  })

  it('falls back to the association id when the linked service is missing', () => {
    renderSection([domain({ service_id: 'svc_001' })])
    const row = within(screen.getByRole('listitem'))
    expect(row.getByText('svc_001')).toBeInTheDocument()
    expect(row.queryByText('Gateway')).not.toBeInTheDocument()
    expect(row.getAllByText('svc_001')).toHaveLength(1)
  })

  it('keeps multiple domain records without inventing missing associations', () => {
    renderSection(
      [
        domain({ service_id: 'svc_001' }),
        domain({
          domain_id: 'domain_002',
          domain_name: 'very.long.unused.example.invalid',
          service_id: null,
          target_id: null,
        }),
      ],
      [service()],
    )
    const rows = screen.getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    const linked = rows[0]
    const unused = rows[1]
    if (!linked || !unused) throw new Error('missing domain row')
    expect(within(linked).getByText('Gateway')).toBeInTheDocument()
    expect(within(unused).getByText('very.long.unused.example.invalid')).toBeInTheDocument()
    expect(within(unused).queryByText('Gateway')).not.toBeInTheDocument()
    expect(within(unused).queryByText('svc_001')).not.toBeInTheDocument()
  })

  it('renders error message and service retry button when error and onRetryServices are provided', () => {
    const onRetry = vi.fn()
    render(
      <MemoryRouter>
        <VPSDomainsSection
          domains={[domain({ service_id: 'svc_001' })]}
          services={[]}
          error="加载关联服务失败"
          notice={null}
          onCreate={vi.fn()}
          onRetryServices={onRetry}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('加载关联服务失败')
    const retryBtn = screen.getByRole('button', { name: '重试加载服务' })
    expect(retryBtn).toBeInTheDocument()
    retryBtn.click()
    expect(onRetry).toHaveBeenCalledTimes(1)
    expect(screen.getByText('svc_001')).toBeInTheDocument()
  })

  it('renders notice status when notice is provided', () => {
    render(
      <MemoryRouter>
        <VPSDomainsSection
          domains={[domain()]}
          services={[]}
          error={null}
          notice="正在加载关联服务…"
          onCreate={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('status')).toHaveTextContent('正在加载关联服务…')
  })
})
