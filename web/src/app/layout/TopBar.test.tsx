import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '../../lib/theme-context'
import { invalidateRecordNotificationUnreadCount } from '../../lib/recordInboxUnreadApi'
import { TopBar } from './TopBar'

const sync = { state: 'clear' as const, label: '摘要无异常' }
const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

function renderTopBar(path = '/') {
  return render(
    <MemoryRouter initialEntries={[path]}>
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
