import { useState } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  submitting = false,
  onSubmit = vi.fn(),
) {
  const view = render(
    <MemoryRouter>
      <VPSFactsEditForm
        key="2026-08-20T00:00:00Z"
        formId="vps-facts-form"
        draft={draft}
        providers={[]}
        providersLoading={false}
        providersError={null}
        submitting={submitting}
        onDraftChange={onDraftChange}
        onSubmit={onSubmit}
      />
    </MemoryRouter>,
  )
  return { ...view, onDraftChange, onSubmit }
}

describe('VPSFactsEditForm', () => {
  it('lets optional product fields save while 可选设置 starts collapsed', () => {
    const { onDraftChange } = renderForm(draftFixture())
    const optional = screen.getByText('可选设置').closest('details')
    expect(optional).toBeInstanceOf(HTMLDetailsElement)
    expect((optional as HTMLDetailsElement).open).toBe(false)
    fireEvent.change(within(optional as HTMLElement).getByLabelText('产品名'), {
      target: { value: 'cx32' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ productName: 'cx32' }))
  })

  it('writes the usage enum and low/normal/high importance into the draft', () => {
    const { onDraftChange } = renderForm(draftFixture())
    fireEvent.change(screen.getByRole('combobox', { name: '使用状态' }), {
      target: { value: 'standby' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ usageStatus: 'standby' }))
    fireEvent.change(screen.getByRole('combobox', { name: '重要性' }), {
      target: { value: 'low' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ importance: 'low' }))
  })

  it('hides IPv6 and SSH boxes until enabled and does not erase values when hiding', () => {
    const initial = draftFixture({ ipv6: '2001:db8::1', sshHost: 'ssh.example.test' })
    const { onDraftChange } = renderForm(initial)
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toHaveValue('2001:db8::1')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')

    fireEvent.click(screen.getByRole('checkbox', { name: '启用 IPv6' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.queryByRole('textbox', { name: 'IPv6 地址' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    expect(onDraftChange).not.toHaveBeenCalled()

    fireEvent.change(screen.getByRole('textbox', { name: 'VPS 名称' }), {
      target: { value: '仍保留地址' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      displayName: '仍保留地址',
      ipv6: '2001:db8::1',
      sshHost: 'ssh.example.test',
    }))
  })

  it('keeps a SSH host typed after enable when the box is hidden and shown again', () => {
    function Harness() {
      const [draft, setDraft] = useState(draftFixture())
      return (
        <MemoryRouter>
          <VPSFactsEditForm
            formId="vps-facts-form"
            draft={draft}
            providers={[]}
            providersLoading={false}
            providersError={null}
            submitting={false}
            onDraftChange={setDraft}
            onSubmit={vi.fn()}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'SSH Host' }), {
      target: { value: 'ssh.example.test' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
  })

  it('keeps an IPv6 address typed after enable when the box is hidden and shown again', () => {
    function Harness() {
      const [draft, setDraft] = useState(draftFixture())
      return (
        <MemoryRouter>
          <VPSFactsEditForm
            formId="vps-facts-form"
            draft={draft}
            providers={[]}
            providersLoading={false}
            providersError={null}
            submitting={false}
            onDraftChange={setDraft}
            onSubmit={vi.fn()}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    fireEvent.click(screen.getByRole('checkbox', { name: '启用 IPv6' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'IPv6 地址' }), {
      target: { value: '2001:db8::1' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: '启用 IPv6' }))
    expect(screen.queryByRole('textbox', { name: 'IPv6 地址' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: '启用 IPv6' }))
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toHaveValue('2001:db8::1')
  })

  it('enables custom SSH when the host differs or the port is not 22', () => {
    const { unmount } = renderForm(draftFixture({ sshPort: '2222' }))
    expect(screen.getByRole('checkbox', { name: '单独填写 SSH' })).toBeChecked()
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('192.0.2.1')
    expect(screen.getByRole('spinbutton', { name: 'SSH 端口' })).toHaveValue(2222)
    unmount()

    renderForm(draftFixture())
    expect(screen.getByRole('checkbox', { name: '单独填写 SSH' })).not.toBeChecked()
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'IPv6 地址' })).not.toBeInTheDocument()
  })

  it('derives SSH host from IPv4 only while custom SSH is off', () => {
    const { onDraftChange, unmount } = renderForm(draftFixture())
    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
      target: { value: '198.51.100.9' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      ipv4: '198.51.100.9',
      sshHost: '198.51.100.9',
    }))
    unmount()

    const next = renderForm(draftFixture({ sshHost: 'ssh.example.test' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
      target: { value: '198.51.100.9' },
    })
    expect(next.onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      ipv4: '198.51.100.9',
      sshHost: 'ssh.example.test',
    }))
  })

  it('keeps a hidden custom SSH host when IPv4 changes and shows it again on re-enable', () => {
    function Harness() {
      const [draft, setDraft] = useState(draftFixture({ sshHost: 'ssh.example.test' }))
      return (
        <MemoryRouter>
          <VPSFactsEditForm
            formId="vps-facts-form"
            draft={draft}
            providers={[]}
            providersLoading={false}
            providersError={null}
            submitting={false}
            onDraftChange={setDraft}
            onSubmit={vi.fn()}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
      target: { value: '198.51.100.9' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.getByRole('textbox', { name: 'IPv4' })).toHaveValue('198.51.100.9')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
  })

  it('keeps a hidden custom SSH port when IPv4 changes and shows host and port again on re-enable', () => {
    function Harness() {
      const [draft, setDraft] = useState(draftFixture({ sshPort: '2222' }))
      return (
        <MemoryRouter>
          <VPSFactsEditForm
            formId="vps-facts-form"
            draft={draft}
            providers={[]}
            providersLoading={false}
            providersError={null}
            submitting={false}
            onDraftChange={setDraft}
            onSubmit={vi.fn()}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    expect(screen.getByRole('checkbox', { name: '单独填写 SSH' })).toBeChecked()
    expect(screen.getByRole('spinbutton', { name: 'SSH 端口' })).toHaveValue(2222)
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
      target: { value: '198.51.100.9' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(screen.getByRole('textbox', { name: 'IPv4' })).toHaveValue('198.51.100.9')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('192.0.2.1')
    expect(screen.getByRole('spinbutton', { name: 'SSH 端口' })).toHaveValue(2222)
  })

  it('fills an empty SSH host from IPv4 when custom SSH is turned on', () => {
    const { onDraftChange } = renderForm(draftFixture({ sshHost: '' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      sshHost: '192.0.2.1',
      sshPort: '22',
    }))
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toBeInTheDocument()
  })

  it('rehydrates SSH / IPv6 / country after a merged draft replace so editing IPv4 keeps the independent SSH host', () => {
    const initial = draftFixture()
    const { rerender, onDraftChange } = renderForm(initial)
    expect(screen.queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
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
          formId="vps-facts-form"
          draft={merged}
          providers={[]}
          providersLoading={false}
          providersError={null}
          submitting={false}
          onDraftChange={onDraftChange}
          onSubmit={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toHaveValue('2001:db8::1')
    expect(screen.getByRole('combobox', { name: '国家 / 地区' })).toHaveValue('美国')

    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
      target: { value: '198.51.100.9' },
    })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({
      ipv4: '198.51.100.9',
      sshHost: 'ssh.example.test',
    }))
  })

  it('searches country by Chinese, English, or code and allows custom input', () => {
    const { onDraftChange } = renderForm(draftFixture())
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' })
    fireEvent.focus(combo)
    const list = screen.getByRole('listbox')
    expect(within(list).getByText('亚洲')).toBeInTheDocument()

    fireEvent.change(combo, { target: { value: 'JP' } })
    expect(within(screen.getByRole('listbox')).getByRole('option', { name: /日本/ })).toBeInTheDocument()
    fireEvent.change(combo, { target: { value: 'Germany' } })
    expect(within(screen.getByRole('listbox')).getByRole('option', { name: /德国/ })).toBeInTheDocument()
    fireEvent.change(combo, { target: { value: '德国' } })
    fireEvent.mouseDown(within(screen.getByRole('listbox')).getByRole('option', { name: /德国/ }))
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: 'DE' }))
    expect(combo).toHaveValue('德国')

    fireEvent.click(combo)
    fireEvent.change(combo, { target: { value: '欧洲区' } })
    fireEvent.mouseDown(within(screen.getByRole('listbox')).getByRole('option', { name: /使用「欧洲区」/ }))
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: '欧洲区' }))
    expect(combo).toHaveValue('欧洲区')
  })

  it('publishes exact ISO or custom country to the parent on each input without selecting', () => {
    const { onDraftChange } = renderForm(draftFixture())
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' })
    fireEvent.change(combo, { target: { value: 'Andorra' } })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: 'AD' }))
    fireEvent.change(combo, { target: { value: '北境观测' } })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: '北境观测' }))
  })

  it('publishes the IME final country value', () => {
    const { onDraftChange } = renderForm(draftFixture())
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' })
    fireEvent.compositionStart(combo)
    fireEvent.change(combo, { target: { value: '德国' } })
    expect(onDraftChange).not.toHaveBeenCalled()
    fireEvent.compositionEnd(combo)
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: 'DE' }))
  })

  it('reopens the grouped browse list after a search and expands the remaining ISO codes', async () => {
    renderForm(draftFixture())
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' })
    fireEvent.focus(combo)
    fireEvent.change(combo, { target: { value: 'US' } })
    expect(within(screen.getByRole('listbox')).queryByText('亚洲')).not.toBeInTheDocument()
    fireEvent.mouseDown(within(screen.getByRole('listbox')).getByRole('option', { name: /美国/ }))
    expect(combo).toHaveValue('美国')

    fireEvent.click(combo)
    expect(within(screen.getByRole('listbox')).getByText('亚洲')).toBeInTheDocument()
    const list = screen.getByRole('listbox')
    expect(within(list).queryByRole('button')).not.toBeInTheDocument()
    fireEvent.mouseDown(within(list).getByRole('option', { name: /展开其他 \d+ 个国家／地区/ }))
    await waitFor(() => expect(within(screen.getByRole('listbox')).getByRole('option', { name: /安道尔/ })).toBeInTheDocument())
  })

  it('selects committed country text on first focus', () => {
    renderForm(draftFixture())
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' }) as HTMLInputElement
    fireEvent.focus(combo, { relatedTarget: document.body })
    expect(combo.selectionStart).toBe(0)
    expect(combo.selectionEnd).toBe('日本'.length)
  })

  it('reverts an in-progress country search on Escape and restores the parent country', () => {
    const onDraftChange = vi.fn()
    const onSubmit = vi.fn((event: { preventDefault: () => void }) => event.preventDefault())
    function Harness() {
      const [draft, setDraft] = useState(draftFixture())
      return (
        <MemoryRouter>
          <VPSFactsEditForm
            formId="vps-facts-form"
            draft={draft}
            providers={[]}
            providersLoading={false}
            providersError={null}
            submitting={false}
            onDraftChange={(next) => {
              onDraftChange(next)
              setDraft(next)
            }}
            onSubmit={onSubmit}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    const combo = screen.getByRole('combobox', { name: '国家 / 地区' })
    fireEvent.focus(combo)
    fireEvent.change(combo, { target: { value: 'xx' } })
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: 'xx' }))
    fireEvent.keyDown(combo, { key: 'Escape' })
    expect(combo).toHaveValue('日本')
    expect(combo).toHaveAttribute('aria-expanded', 'false')
    expect(onDraftChange).toHaveBeenLastCalledWith(expect.objectContaining({ country: 'JP' }))
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('disables fields while submitting', () => {
    renderForm(draftFixture({ ipv6: '2001:db8::1', sshHost: 'ssh.example.test' }), vi.fn(), true)
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toBeDisabled()
    expect(screen.getByRole('combobox', { name: '国家 / 地区' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: 'IPv4' })).toBeDisabled()
    expect(screen.getByRole('combobox', { name: '使用状态' })).toBeDisabled()
    expect(screen.getByRole('checkbox', { name: '启用 IPv6' })).toBeDisabled()
    expect(screen.getByRole('checkbox', { name: '单独填写 SSH' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: '备注' })).toBeDisabled()
  })
})
