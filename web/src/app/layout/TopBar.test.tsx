import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, type InitialEntry } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '../../lib/theme-context'
import { invalidateRecordNotificationUnreadCount } from '../../lib/recordInboxUnreadApi'
import { TopBar } from './TopBar'

const sync = { state: 'clear' as const, label: '系统摘要无异常' }
const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

function renderTopBar(entry: InitialEntry = '/') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <ThemeProvider>
        <TopBar sync={sync} user={user} />
      </ThemeProvider>
    </MemoryRouter>,
  )
}

describe('TopBar theme menu', () => {
  it('titles the comparison workbench instead of a record detail', async () => {
    renderTopBar('/records/compare')
    expect(await screen.findByText('横向比较')).toBeInTheDocument()
    expect(screen.queryByText('运维记录详情')).not.toBeInTheDocument()
  })

  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ unread_count: 0 }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('links to the private record inbox and renders its bounded unread count', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ unread_count: 12 }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    renderTopBar()

    const inbox = await screen.findByRole('link', { name: '记录通知，12 条未读' })
    expect(inbox).toHaveAttribute('href', '/record-inbox')
    await waitFor(() => expect(inbox).toHaveTextContent('12'))
  })

  it('shows unread availability failures explicitly instead of a false zero', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new Error('unavailable'))
    renderTopBar()

    expect(await screen.findByRole('link', { name: '记录通知，未读数暂不可用' })).toHaveAttribute('href', '/record-inbox')
    expect(screen.queryByRole('link', { name: '记录通知' })).not.toBeInTheDocument()
  })

  it('refreshes the narrow unread seam on focus and inbox invalidation', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(new Response(JSON.stringify({ unread_count: 1 }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ unread_count: 2 }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ unread_count: 3 }), { status: 200 }))
    renderTopBar()
    expect(await screen.findByRole('link', { name: '记录通知，1 条未读' })).toBeInTheDocument()

    fireEvent.focus(window)
    expect(await screen.findByRole('link', { name: '记录通知，2 条未读' })).toBeInTheDocument()
    invalidateRecordNotificationUnreadCount()
    expect(await screen.findByRole('link', { name: '记录通知，3 条未读' })).toBeInTheDocument()
  })

  it('exposes menu button state and four radio menu items', () => {
    renderTopBar()
    const trigger = screen.getByRole('button', { name: '切换主题' })

    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveAttribute('aria-controls')
    fireEvent.click(trigger)

    const menu = screen.getByRole('menu', { name: '主题选项' })
    expect(menu).toHaveAttribute('id', trigger.getAttribute('aria-controls'))
    const options = screen.getAllByRole('menuitemradio')
    expect(options).toHaveLength(4)
    expect(options.filter((option) => option.getAttribute('aria-checked') === 'true')).toHaveLength(1)
    const firstOption = options[0]
    if (!firstOption) throw new Error('theme menu must expose a first option')
    expect(firstOption).toHaveFocus()
  })

  it('moves focus with Arrow, Home, and End without changing the selected theme', () => {
    renderTopBar()
    fireEvent.click(screen.getByRole('button', { name: '切换主题' }))
    const options = screen.getAllByRole('menuitemradio')
    const initiallyChecked = options.find((option) => option.getAttribute('aria-checked') === 'true')
    const [firstOption, secondOption, , lastOption] = options
    if (!firstOption || !secondOption || !lastOption) {
      throw new Error('theme menu must expose four options')
    }

    fireEvent.keyDown(firstOption, { key: 'ArrowDown' })
    expect(secondOption).toHaveFocus()
    fireEvent.keyDown(secondOption, { key: 'End' })
    expect(lastOption).toHaveFocus()
    fireEvent.keyDown(lastOption, { key: 'Home' })
    expect(firstOption).toHaveFocus()
    fireEvent.keyDown(firstOption, { key: 'ArrowUp' })
    expect(lastOption).toHaveFocus()
    expect(initiallyChecked).toHaveAttribute('aria-checked', 'true')
  })

  it('activates a keyboard choice once, closes, and restores trigger focus', () => {
    renderTopBar()
    const trigger = screen.getByRole('button', { name: '切换主题' })
    fireEvent.click(trigger)
    const classic = screen.getByRole('menuitemradio', { name: /克制工程/ })

    fireEvent.keyDown(classic, { key: 'Enter' })

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    fireEvent.click(trigger)
    expect(screen.getByRole('menuitemradio', { name: /克制工程/ })).toHaveAttribute('aria-checked', 'true')
  })

  it('closes on Escape, Tab, and outside pointer press', () => {
    renderTopBar()
    const trigger = screen.getByRole('button', { name: '切换主题' })

    fireEvent.click(trigger)
    const escapeOption = screen.getAllByRole('menuitemradio')[0]
    if (!escapeOption) throw new Error('theme menu must expose an option')
    fireEvent.keyDown(escapeOption, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()

    fireEvent.click(trigger)
    const tabOption = screen.getAllByRole('menuitemradio')[0]
    if (!tabOption) throw new Error('theme menu must expose an option')
    fireEvent.keyDown(tabOption, { key: 'Tab' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    fireEvent.click(trigger)
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })
})

describe('TopBar VPS breadcrumb', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ unread_count: 0 }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('keeps the inventory title without a return link', () => {
    renderTopBar('/vps')
    expect(screen.getByText('VPS 资产')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '返回 VPS 列表' })).not.toBeInTheDocument()
  })

  it('renders one inventory return link with the current route label', () => {
    renderTopBar({
      pathname: '/vps/vps_001',
      state: { vpsInventoryHref: '/vps?workspace=workbench&view=unlinked&q=Tokyo&selected=vps_001' },
    })

    const returnLink = screen.getByRole('link', { name: '返回 VPS 列表' })
    expect(returnLink).toHaveTextContent('VPS 资产')
    expect(returnLink).toHaveAttribute('href', '/vps?workspace=workbench&view=unlinked&q=Tokyo&selected=vps_001')
    expect(screen.getByText('VPS 详情')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: '返回 VPS 列表' })).toHaveLength(1)
  })

  it('keeps sibling route labels next to the same inventory return link', () => {
    renderTopBar({
      pathname: '/vps/vps_001/ip-quality',
      state: { vpsInventoryHref: '/vps?workspace=ledger' },
    })
    expect(screen.getByRole('link', { name: '返回 VPS 列表' })).toHaveAttribute('href', '/vps?workspace=ledger')
    expect(screen.getByText('IP 质量')).toBeInTheDocument()
    expect(screen.queryByText('VPS 详情')).not.toBeInTheDocument()
  })

  it.each(['https://example.invalid/vps', '/settings', '/vps/vps_001', '/vps#frag'])(
    'falls back to /vps when location state is not an inventory href: %s',
    (vpsInventoryHref) => {
      renderTopBar({ pathname: '/vps/vps_001', state: { vpsInventoryHref } })
      expect(screen.getByRole('link', { name: '返回 VPS 列表' })).toHaveAttribute('href', '/vps')
    },
  )
})

describe('TopBar monitoring breadcrumb', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ unread_count: 0 }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('keeps the list title without a return link', () => {
    renderTopBar('/monitoring')
    expect(screen.getByText('监控')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '返回监控实例列表' })).not.toBeInTheDocument()
  })

  it('returns to the validated list href from detail and compare', () => {
    renderTopBar({
      pathname: '/monitoring/mi_001',
      state: { monitoringListHref: '/monitoring?view=abnormal&q=Tokyo', vpsInventoryHref: '/vps?q=keep' },
    })
    expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute(
      'href',
      '/monitoring?view=abnormal&q=Tokyo',
    )
    expect(screen.getByText('监控实例详情')).toBeInTheDocument()
  })

  it.each(['/monitoring/mi_001', '/settings', '/monitoring#frag'])(
    'falls back to /monitoring when location state is not a list href: %s',
    (monitoringListHref) => {
      renderTopBar({ pathname: '/monitoring/mi_001', state: { monitoringListHref } })
      expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute('href', '/monitoring')
    },
  )

})
