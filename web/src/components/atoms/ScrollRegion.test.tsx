import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ScrollRegion } from './ScrollRegion'

type Observed = { callback: ResizeObserverCallback; disconnected: boolean; targets: Set<Element> }

function stubResizeObserver() {
  const observers: Observed[] = []
  vi.stubGlobal('ResizeObserver', class {
    private readonly entry: Observed
    constructor(callback: ResizeObserverCallback) {
      this.entry = { callback, disconnected: false, targets: new Set() }
      observers.push(this.entry)
    }
    observe(target: Element) { this.entry.targets.add(target) }
    unobserve(target: Element) { this.entry.targets.delete(target) }
    disconnect() {
      this.entry.disconnected = true
      this.entry.targets.clear()
    }
  })
  return {
    observers,
    notify() {
      for (const observer of observers) {
        if (!observer.disconnected) observer.callback([], {} as ResizeObserver)
      }
    },
  }
}

function setWidths(element: HTMLElement, scrollWidth: number, clientWidth: number) {
  Object.defineProperty(element, 'scrollWidth', { configurable: true, value: scrollWidth })
  Object.defineProperty(element, 'clientWidth', { configurable: true, value: clientWidth })
}

function Region({ empty = false }: { empty?: boolean }) {
  return (
    <>
      <h2 id="table-title">表格</h2>
      <ScrollRegion labelledBy="table-title" hintId="table-hint" hint="横向滚动查看完整列" hintClassName="hint">
        {empty ? <p>没有数据</p> : <table><tbody><tr><td>内容</td></tr></tbody></table>}
      </ScrollRegion>
    </>
  )
}

function renderRegion() {
  return render(<Region />)
}

describe('ScrollRegion', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('stays a named focusable region without a hint when content fits', () => {
    const resize = stubResizeObserver()
    renderRegion()
    const region = screen.getByRole('region', { name: '表格' })
    setWidths(region, 600, 600)
    act(() => resize.notify())
    expect(region).toHaveAttribute('tabindex', '0')
    expect(region).not.toHaveAttribute('aria-describedby')
    expect(screen.queryByText('横向滚动查看完整列')).not.toBeInTheDocument()
  })

  it('shows and describes the hint only while content overflows', () => {
    const resize = stubResizeObserver()
    renderRegion()
    const region = screen.getByRole('region', { name: '表格' })

    setWidths(region, 900, 600)
    act(() => resize.notify())
    expect(screen.getByText('横向滚动查看完整列')).toHaveAttribute('id', 'table-hint')
    expect(region).toHaveAttribute('aria-describedby', 'table-hint')
    expect(region).toHaveAccessibleDescription('横向滚动查看完整列')

    setWidths(region, 600, 600)
    act(() => resize.notify())
    expect(region).not.toHaveAttribute('aria-describedby')
    expect(screen.queryByText('横向滚动查看完整列')).not.toBeInTheDocument()
  })

  it.each([
    { scrollWidth: 600, overflowing: false },
    { scrollWidth: 601, overflowing: true },
    { scrollWidth: 602, overflowing: true },
  ])('treats scrollWidth $scrollWidth over clientWidth 600 as overflowing=$overflowing', ({ scrollWidth, overflowing }) => {
    const resize = stubResizeObserver()
    renderRegion()
    const region = screen.getByRole('region', { name: '表格' })
    setWidths(region, scrollWidth, 600)
    act(() => resize.notify())
    expect(region.hasAttribute('aria-describedby')).toBe(overflowing)
  })

  it('observes the region and re-observes replaced direct children', async () => {
    const resize = stubResizeObserver()
    const view = render(<Region />)
    const region = screen.getByRole('region', { name: '表格' })
    const [observer] = resize.observers
    const table = region.querySelector('table')!
    expect(observer!.targets).toEqual(new Set([region, table]))

    view.rerender(<Region empty />)
    const placeholder = region.querySelector('p')!
    // MutationObserver 异步回调：等待观察集合切换到新子节点。
    await vi.waitFor(() => expect(observer!.targets).toEqual(new Set([region, placeholder])))
    expect(observer!.targets.has(table)).toBe(false)
  })

  it('disconnects its observer on unmount', () => {
    const resize = stubResizeObserver()
    const mutationDisconnect = vi.spyOn(MutationObserver.prototype, 'disconnect')
    const view = render(<Region />)
    // 子节点替换已排队，随即卸载：两个 observer 都要解除。
    view.rerender(<Region empty />)
    view.unmount()
    expect(resize.observers.every((observer) => observer.disconnected)).toBe(true)
    expect(mutationDisconnect).toHaveBeenCalled()
    mutationDisconnect.mockRestore()
  })
})
