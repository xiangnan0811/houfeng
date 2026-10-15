import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { IPQualityCollectButton } from './IPQualityCollectControls'
import type { IPQualityCollectController } from './useIPQualityCollect'

function controller(patch: Partial<IPQualityCollectController> = {}): IPQualityCollectController {
  return {
    status: { enabled: true, available: true, monitoring_instance_id: 'mi_001' },
    watchedRequestId: null,
    submitting: false,
    error: null,
    active: false,
    start: vi.fn(),
    ...patch,
  }
}

const items = () => screen.getAllByRole('menuitem')

describe('IPQualityCollectButton', () => {
  it('opens three scope actions directly below the trigger and starts the chosen scope', () => {
    const collect = controller()
    render(<IPQualityCollectButton collect={collect} />)
    const trigger = screen.getByRole('button', { name: '立即采集' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    fireEvent.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('menu', { name: '立即采集' })).toBeInTheDocument()
    expect(items().map((item) => item.textContent)).toEqual(['采集 IPv4', '采集 IPv6', '采集 IPv4 / IPv6'])
    expect(document.activeElement).toBe(items()[0])

    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv6' }))
    expect(collect.start).toHaveBeenCalledWith([6])
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(trigger)
  })

  it('supports arrow, Home/End, Escape and Tab keyboard handling', () => {
    const collect = controller()
    render(<IPQualityCollectButton collect={collect} />)
    const trigger = screen.getByRole('button', { name: '立即采集' })

    // 方向上键从最后一项打开。
    fireEvent.keyDown(trigger, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(items()[2])
    fireEvent.keyDown(items()[2]!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items()[0])
    fireEvent.keyDown(items()[0]!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(items()[2])
    fireEvent.keyDown(items()[2]!, { key: 'Home' })
    expect(document.activeElement).toBe(items()[0])
    fireEvent.keyDown(items()[0]!, { key: 'End' })
    expect(document.activeElement).toBe(items()[2])

    fireEvent.keyDown(items()[2]!, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(trigger)

    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items()[0])
    fireEvent.keyDown(items()[0]!, { key: 'Tab' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(collect.start).not.toHaveBeenCalled()
  })

  it('closes on an outside click without starting a collection', () => {
    const collect = controller()
    render(<><IPQualityCollectButton collect={collect} /><button type="button">外部</button></>)
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('button', { name: '外部' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(collect.start).not.toHaveBeenCalled()
  })

  it('cannot open while a collection is submitting or running', () => {
    render(<IPQualityCollectButton collect={controller({ active: true })} />)
    const trigger = screen.getByRole('button', { name: /采集中/ })
    expect(trigger).toBeDisabled()
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })
})
