import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { VPSDetailSectionNav } from './VPSDetailSectionNav'

describe('VPSDetailSectionNav', () => {
  it('closes on Escape and restores focus to the directory trigger', async () => {
    render(
      <MemoryRouter>
        <VPSDetailSectionNav />
      </MemoryRouter>,
    )

    const trigger = screen.getByRole('button', { name: '页面目录' })
    fireEvent.click(trigger)
    expect(screen.getByRole('link', { name: '订阅与续费' })).toBeVisible()

    await act(async () => {
      fireEvent.keyDown(document, { key: 'Escape' })
    })
    await waitFor(() => expect(screen.queryByRole('link', { name: '订阅与续费' })).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
  })

  it('closes on an outside pointer press and restores focus', async () => {
    render(
      <MemoryRouter>
        <VPSDetailSectionNav />
      </MemoryRouter>,
    )

    const trigger = screen.getByRole('button', { name: '页面目录' })
    fireEvent.click(trigger)
    expect(screen.getByRole('link', { name: '订阅与续费' })).toBeVisible()

    await act(async () => {
      fireEvent.mouseDown(document.body)
    })
    await waitFor(() => expect(screen.queryByRole('link', { name: '订阅与续费' })).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
  })
})
