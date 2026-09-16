import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterSearchSelect } from './FilterSearchSelect'

const OPTIONS = [
  { value: 'vps_001', label: 'Tokyo Edge', hint: 'Hetzner · Tokyo', keywords: 'vps_001 10.0.0.1' },
  { value: 'vps_002', label: 'Osaka Backup', hint: 'Hetzner · Osaka', keywords: 'vps_002 10.0.0.2' },
]

const emptyRect = {
  x: 0,
  y: 0,
  width: 0,
  height: 0,
  top: 0,
  right: 0,
  bottom: 0,
  left: 0,
  toJSON() {
    return this
  },
}

describe('FilterSearchSelect', () => {
  it('names the trigger with the label and selected value', () => {
    const { rerender } = render(
      <FilterSearchSelect label="VPS" value={null} options={OPTIONS} onChange={() => {}} />,
    )
    expect(screen.getByRole('button', { name: 'VPS 全部' })).toBeInTheDocument()
    rerender(
      <FilterSearchSelect label="VPS" value="vps_001" options={OPTIONS} onChange={() => {}} />,
    )
    expect(screen.getByRole('button', { name: 'VPS Tokyo Edge' })).toBeInTheDocument()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('opens a searchable list and commits a single selection', () => {
    const onChange = vi.fn()
    render(
      <FilterSearchSelect label="VPS" value={null} options={OPTIONS} onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    expect(screen.getByRole('listbox', { name: 'VPS' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('option', { name: /Osaka Backup/ }))
    expect(onChange).toHaveBeenCalledWith('vps_002')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('clears the value when 全部 is chosen', () => {
    const onChange = vi.fn()
    render(
      <FilterSearchSelect label="VPS" value="vps_001" options={OPTIONS} onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS Tokyo Edge' }))
    fireEvent.click(screen.getByRole('option', { name: '全部' }))
    expect(onChange).toHaveBeenCalledWith(null)
  })

  it('keeps 全部 available when the catalog is empty so a stale id can be cleared', () => {
    const onChange = vi.fn()
    render(
      <FilterSearchSelect label="VPS" value="vps_gone" options={[]} onChange={onChange} />,
    )
    expect(screen.getByRole('button', { name: 'VPS vps_gone' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'VPS vps_gone' }))
    expect(screen.getByRole('option', { name: '全部' })).toBeInTheDocument()
    expect(screen.getByText('暂无可选项')).toBeInTheDocument()
    const search = screen.getByRole('combobox', { name: '搜索VPS' })
    expect(search).toHaveAttribute('aria-activedescendant', expect.stringMatching(/opt-all$/))
    fireEvent.click(screen.getByRole('option', { name: '全部' }))
    expect(onChange).toHaveBeenCalledWith(null)
  })

  it('filters a long catalog by name and keywords', () => {
    const options = Array.from({ length: 120 }, (_, index) => ({
      value: `vps_${String(index).padStart(3, '0')}`,
      label: index === 87 ? 'Unique Osaka Node' : `Host ${index}`,
      keywords: index === 87 ? '10.8.7.1 JP' : `10.0.0.${index}`,
    }))
    render(
      <FilterSearchSelect label="VPS" value={null} options={options} onChange={() => {}} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    fireEvent.change(screen.getByRole('combobox', { name: '搜索VPS' }), { target: { value: 'osaka' } })
    expect(screen.getByRole('option', { name: /Unique Osaka Node/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /Host 1$/ })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: '搜索VPS' }), { target: { value: '10.8.7.1' } })
    expect(screen.getByRole('option', { name: /Unique Osaka Node/ })).toBeInTheDocument()
  })

  it('scrolls the End-highlighted option into the list viewport', async () => {
    const options = Array.from({ length: 40 }, (_, index) => ({
      value: `vps_${index}`,
      label: `Standby-Node ${index}`,
    }))
    render(
      <FilterSearchSelect label="VPS" value={null} options={options} onChange={() => {}} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    const list = screen.getByRole('listbox', { name: 'VPS' })
    const search = screen.getByRole('combobox', { name: '搜索VPS' })
    let scrollTop = 0
    Object.defineProperty(list, 'scrollTop', {
      configurable: true,
      get: () => scrollTop,
      set: (next: number) => {
        scrollTop = next
      },
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if (this === list || this.getAttribute('role') === 'listbox') {
        return { ...emptyRect, width: 200, height: 218, bottom: 218 }
      }
      if (this.getAttribute('role') === 'option' && this.classList.contains('is-active')) {
        return { ...emptyRect, width: 200, height: 32, top: 955 - scrollTop, bottom: 987 - scrollTop, y: 955 - scrollTop }
      }
      return { ...emptyRect, width: 200, height: 32 }
    })
    fireEvent.keyDown(search, { key: 'End' })
    await waitFor(() => {
      const active = document.getElementById(search.getAttribute('aria-activedescendant')!)!
      const rect = active.getBoundingClientRect()
      expect(rect.top).toBeGreaterThanOrEqual(list.getBoundingClientRect().top)
      expect(rect.bottom).toBeLessThanOrEqual(list.getBoundingClientRect().bottom)
    })
  })

  it('moves Tab from the open search field to the next control', () => {
    render(
      <div>
        <FilterSearchSelect label="VPS" value={null} options={OPTIONS} onChange={() => {}} />
        <button type="button">续费窗口</button>
      </div>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    const search = screen.getByRole('combobox', { name: '搜索VPS' })
    search.focus()
    fireEvent.keyDown(search, { key: 'Tab' })
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '续费窗口' })).toHaveFocus()
  })

  it('commits the highlighted option with Enter and ignores IME Enter', () => {
    const onChange = vi.fn()
    render(
      <FilterSearchSelect label="VPS" value={null} options={OPTIONS} onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    const search = screen.getByRole('combobox', { name: '搜索VPS' })
    fireEvent.keyDown(search, { key: 'Enter', isComposing: true, keyCode: 229 })
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.keyDown(search, { key: 'ArrowDown' })
    fireEvent.keyDown(search, { key: 'Enter' })
    expect(onChange).toHaveBeenCalledWith('vps_001')
  })

  it('closes on Escape and restores trigger focus without changing the value', () => {
    const onChange = vi.fn()
    render(
      <FilterSearchSelect label="VPS" value="vps_001" options={OPTIONS} onChange={onChange} />,
    )
    const trigger = screen.getByRole('button', { name: 'VPS Tokyo Edge' })
    fireEvent.click(trigger)
    fireEvent.keyDown(screen.getByRole('combobox', { name: '搜索VPS' }), { key: 'Escape' })
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('closes on outside pointer down', () => {
    render(
      <div>
        <FilterSearchSelect label="VPS" value={null} options={OPTIONS} onChange={() => {}} />
        <button type="button">outside</button>
      </div>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'VPS 全部' }))
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('button', { name: 'outside' }))
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })
})
