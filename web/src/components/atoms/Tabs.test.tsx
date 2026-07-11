import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TabPanel, Tabs } from './Tabs'
import { tabId, tabPanelId } from './tabIds'

const items = [
  { value: 'a', label: '概览' },
  { value: 'b', label: '指标趋势' },
  { value: 'c', label: '活跃异常', count: 2 },
] as const

function renderTabs(value: 'a' | 'b' | 'c' | 'missing' = 'a', onChange = vi.fn()) {
  render(
    <Tabs
      label="监控视图"
      idBase="monitoring-view"
      items={items}
      value={value}
      onChange={onChange}
      variant="pill"
    />,
  )
  return onChange
}

describe('Tabs', () => {
  it('names the tablist and connects every tab to a deterministic panel id', () => {
    renderTabs('b')

    expect(screen.getByRole('tablist', { name: '监控视图' })).toHaveClass('tabs--pill')
    const tabs = screen.getAllByRole('tab')
    expect(tabs).toHaveLength(3)
    expect(tabs[0]).toHaveAttribute('id', tabId('monitoring-view', 'a'))
    expect(tabs[0]).toHaveAttribute('aria-controls', tabPanelId('monitoring-view', 'a'))
    expect(tabs[0]).toHaveAttribute('aria-selected', 'false')
    expect(tabs[0]).toHaveAttribute('tabindex', '-1')
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true')
    expect(tabs[1]).toHaveAttribute('tabindex', '0')
    expect(tabs[2]).toHaveAttribute('tabindex', '-1')
  })

  it('passes the generic value to onChange when clicked', () => {
    const onChange = renderTabs()

    fireEvent.click(screen.getByRole('tab', { name: '指标趋势' }))
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('moves focus and activates tabs with ArrowRight and ArrowLeft including wraparound', () => {
    const onChange = renderTabs('a')
    const overview = screen.getByRole('tab', { name: '概览' })
    const metrics = screen.getByRole('tab', { name: '指标趋势' })
    const anomalies = screen.getByRole('tab', { name: /活跃异常/ })

    overview.focus()
    fireEvent.keyDown(overview, { key: 'ArrowRight' })
    expect(metrics).toHaveFocus()
    expect(onChange).toHaveBeenLastCalledWith('b')

    fireEvent.keyDown(metrics, { key: 'ArrowLeft' })
    expect(overview).toHaveFocus()
    expect(onChange).toHaveBeenLastCalledWith('a')

    fireEvent.keyDown(overview, { key: 'ArrowLeft' })
    expect(anomalies).toHaveFocus()
    expect(onChange).toHaveBeenLastCalledWith('c')
  })

  it('moves focus and activates the boundaries with Home and End', () => {
    const onChange = renderTabs('b')
    const metrics = screen.getByRole('tab', { name: '指标趋势' })
    const overview = screen.getByRole('tab', { name: '概览' })
    const anomalies = screen.getByRole('tab', { name: /活跃异常/ })

    metrics.focus()
    fireEvent.keyDown(metrics, { key: 'End' })
    expect(anomalies).toHaveFocus()
    expect(onChange).toHaveBeenLastCalledWith('c')

    fireEvent.keyDown(anomalies, { key: 'Home' })
    expect(overview).toHaveFocus()
    expect(onChange).toHaveBeenLastCalledWith('a')
  })

  it('scrolls the keyboard focus target into the nearest tablist viewport', () => {
    const onChange = vi.fn()
    const requestFrame = vi.fn()
    vi.stubGlobal('requestAnimationFrame', requestFrame)
    const { rerender } = render(
      <Tabs
        label="监控视图"
        idBase="monitoring-view"
        items={items}
        value="a"
        onChange={onChange}
        variant="pill"
      />,
    )
    const overview = screen.getByRole('tab', { name: '概览' })
    const anomalies = screen.getByRole('tab', { name: /活跃异常/ })
    const scrollIntoView = vi.fn()
    anomalies.scrollIntoView = scrollIntoView

    fireEvent.keyDown(overview, { key: 'End' })

    expect(anomalies).toHaveFocus()
    expect(onChange).toHaveBeenCalledWith('c')
    expect(scrollIntoView).not.toHaveBeenCalled()
    rerender(
      <Tabs
        label="监控视图"
        idBase="monitoring-view"
        items={items}
        value="a"
        onChange={onChange}
        variant="pill"
      />,
    )
    expect(scrollIntoView).not.toHaveBeenCalled()
    rerender(
      <Tabs
        label="监控视图"
        idBase="monitoring-view"
        items={items}
        value="c"
        onChange={onChange}
        variant="pill"
      />,
    )
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' })
    expect(requestFrame).not.toHaveBeenCalled()
  })

  it('uses the first item as the only tab stop when the controlled value is absent', () => {
    const onChange = renderTabs('missing')
    const tabs = screen.getAllByRole('tab')

    expect(tabs.map((tab) => tab.tabIndex)).toEqual([0, -1, -1])
    expect(screen.queryByRole('tab', { selected: true })).not.toBeInTheDocument()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('renders an empty named tablist without crashing', () => {
    render(<Tabs label="空视图" idBase="empty-view" items={[]} value="missing" onChange={() => {}} />)

    expect(screen.getByRole('tablist', { name: '空视图' })).toBeEmptyDOMElement()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
  })

  it('keeps the count badge in the tab accessible name', () => {
    renderTabs()

    expect(screen.getByRole('tab', { name: /活跃异常.*2/ })).toBeInTheDocument()
    expect(screen.getByText('2')).toHaveClass('badge--count')
  })
})

describe('TabPanel', () => {
  it('connects the active panel back to its owning tab', () => {
    render(
      <TabPanel idBase="monitoring-view" value="b" className="custom-panel">
        指标内容
      </TabPanel>,
    )

    const panel = screen.getByRole('tabpanel')
    expect(panel).toHaveAttribute('id', tabPanelId('monitoring-view', 'b'))
    expect(panel).toHaveAttribute('aria-labelledby', tabId('monitoring-view', 'b'))
    expect(panel).toHaveAttribute('tabindex', '0')
    expect(panel).toHaveClass('custom-panel')
    expect(panel).toHaveTextContent('指标内容')
  })
})
