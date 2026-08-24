import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { VPSOverviewRelation } from '../../lib/types'
import { VPSOverviewRelations } from './VPSOverviewRelations'

const READY = { state: 'ready' as const, observed_at: null, last_success_at: null, reason_code: '' }

function relation(overrides: Partial<VPSOverviewRelation>): VPSOverviewRelation {
  return {
    kind: 'monitoring_instances',
    count: 1,
    label: '监控实例',
    section: READY,
    ...overrides,
  }
}

describe('VPSOverviewRelations', () => {
  it('renders the subscription route and exact VPS-scoped relation commands', () => {
    const onCommand = vi.fn()
    render(
      <MemoryRouter>
        <VPSOverviewRelations
          vpsId="vps /东京"
          relations={[
            relation({
              kind: 'monitoring_instances',
              label: '监控实例',
            }),
            relation({
              kind: 'subscriptions',
              label: '订阅',
              route: '/subscriptions?vps_id=vps+%2F%E4%B8%9C%E4%BA%AC',
            }),
            relation({ kind: 'services', label: '服务' }),
            relation({ kind: 'domains', label: '域名' }),
          ]}
          onCommand={onCommand}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: /订阅/ })).toHaveAttribute(
      'href',
      '/subscriptions?vps_id=vps+%2F%E4%B8%9C%E4%BA%AC',
    )
    fireEvent.click(screen.getByRole('button', { name: /监控实例/ }))
    fireEvent.click(screen.getByRole('button', { name: /服务/ }))
    fireEvent.click(screen.getByRole('button', { name: /域名/ }))
    expect(onCommand.mock.calls).toEqual([
      ['open_monitoring_instances'],
      ['open_services'],
      ['open_domains'],
    ])
  })

  it('keeps freshness retry as a sibling of the relation trigger', () => {
    const onRefresh = vi.fn()
    render(
      <MemoryRouter>
        <VPSOverviewRelations
          vpsId="vps_001"
          relations={[relation({
            kind: 'services',
            label: '服务',
            section: {
              state: 'unavailable',
              observed_at: null,
              last_success_at: null,
              reason_code: 'relation_unavailable',
            },
          })]}
          onCommand={vi.fn()}
          onRefresh={onRefresh}
          retrying={false}
        />
      </MemoryRouter>,
    )

    const item = screen.getByRole('listitem')
    const trigger = within(item).getByRole('button', { name: '服务—' })
    const retry = within(item).getByRole('button', { name: '重试 服务' })
    expect(trigger.contains(retry)).toBe(false)
    expect(retry.closest('.vps-overview-relations__link')).toBeNull()
    fireEvent.click(retry)
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })

  it('renders mismatched and unknown destinations as non-interactive information', () => {
    const unknown = {
      ...relation({ kind: 'subscriptions', label: '错误订阅', route: '/subscriptions' }),
      kind: 'future_relation',
    } as unknown as VPSOverviewRelation
    render(
      <MemoryRouter>
        <VPSOverviewRelations
          vpsId="vps_001"
          relations={[
            relation({ kind: 'subscriptions', label: '错误订阅', route: '/subscriptions' }),
            unknown,
          ]}
          onCommand={vi.fn()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.getAllByText('错误订阅')).toHaveLength(2)
    expect(screen.queryByRole('link', { name: /错误订阅/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /错误订阅/ })).not.toBeInTheDocument()
  })
})
