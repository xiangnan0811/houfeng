import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { FactEditFormState } from './types'
import { VPSFactsEditForm } from './VPSFactsEditForm'

function draftFixture(overrides: Partial<FactEditFormState> = {}): FactEditFormState {
  return {
    displayName: '东京边缘',
    providerID: '',
    providerName: 'Example',
    productName: 'VPS',
    orderRef: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: 'TK1',
    ipv4: '192.0.2.1',
    ipv6: '',
    sshHost: '192.0.2.1',
    sshPort: '22',
    sshUser: 'root',
    osName: 'Debian',
    virtualization: 'KVM',
    usageStatus: 'in_use',
    importance: 'high',
    labels: '',
    note: '',
    ...overrides,
  }
}

function renderForm(
  draft: FactEditFormState,
  onDraftChange = vi.fn(),
) {
  const view = render(
    <MemoryRouter>
      <VPSFactsEditForm
        key="2026-08-20T00:00:00Z"
        draft={draft}
        providers={[]}
        providersLoading={false}
        providersError={null}
        submitting={false}
        error={null}
        notice={null}
        onCancel={vi.fn()}
        onDraftChange={onDraftChange}
        onSubmit={vi.fn()}
      />
    </MemoryRouter>,
  )
  return { ...view, onDraftChange }
}

describe('VPSFactsEditForm', () => {
  it('rehydrates SSH / IPv6 / country after a merged draft replace so editing IPv4 keeps the independent SSH host', () => {
    const initial = draftFixture()
    const { rerender, onDraftChange } = renderForm(initial)
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('192.0.2.1')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toBeDisabled()
    expect(screen.queryByRole('textbox', { name: 'IPv6 地址' })).not.toBeInTheDocument()

    const merged = draftFixture({
      sshHost: 'ssh.example.test',
      ipv6: '2001:db8::1',
      country: 'US',
    })
    rerender(
      <MemoryRouter>
        <VPSFactsEditForm
          key="2026-08-21T00:00:00Z"
          draft={merged}
          providers={[]}
          providersLoading={false}
          providersError={null}
          submitting={false}
          error={null}
          notice={null}
          onCancel={vi.fn()}
          onDraftChange={onDraftChange}
          onSubmit={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toBeEnabled()
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toHaveValue('2001:db8::1')
    expect(screen.getByRole('combobox', { name: '国家 / 地区' })).toHaveValue('US')

    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4 / 主入口' }), {
      target: { value: '198.51.100.9' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      ipv4: '198.51.100.9',
      sshHost: 'ssh.example.test',
    }))
  })
})
