import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import { ThemeProvider } from '../../lib/theme-context'
import { TopBar } from './TopBar'

const sync = { state: 'clear' as const, label: '摘要无异常' }
const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

function renderTopBar() {
  return render(
    <MemoryRouter>
      <ThemeProvider>
        <TopBar sync={sync} user={user} />
      </ThemeProvider>
    </MemoryRouter>,
  )
}

describe('TopBar theme menu', () => {
  afterEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
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
    expect(options[0]).toHaveFocus()
  })

  it('moves focus with Arrow, Home, and End without changing the selected theme', () => {
    renderTopBar()
    fireEvent.click(screen.getByRole('button', { name: '切换主题' }))
    const options = screen.getAllByRole('menuitemradio')
    const initiallyChecked = options.find((option) => option.getAttribute('aria-checked') === 'true')

    fireEvent.keyDown(options[0], { key: 'ArrowDown' })
    expect(options[1]).toHaveFocus()
    fireEvent.keyDown(options[1], { key: 'End' })
    expect(options[3]).toHaveFocus()
    fireEvent.keyDown(options[3], { key: 'Home' })
    expect(options[0]).toHaveFocus()
    fireEvent.keyDown(options[0], { key: 'ArrowUp' })
    expect(options[3]).toHaveFocus()
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
    fireEvent.keyDown(screen.getAllByRole('menuitemradio')[0], { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()

    fireEvent.click(trigger)
    fireEvent.keyDown(screen.getAllByRole('menuitemradio')[0], { key: 'Tab' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    fireEvent.click(trigger)
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })
})
