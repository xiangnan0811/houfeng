import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import type { AssetServiceRecord } from '../lib/types'
import { DependencyStatusCorrection } from './DependencyStatusCorrection'

function renderCorrection(currentStatus: string, onCompleted = vi.fn()) {
  render(
    <DependencyStatusCorrection
      open
      kind="service"
      objectId="svc_001"
      displayName="Blog service"
      currentStatus={currentStatus}
      parentLifecycle="cancelled"
      onClose={vi.fn()}
      onCompleted={onCompleted}
    />,
  )
  return onCompleted
}

describe('DependencyStatusCorrection', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('requires an explicit accepted target status when correcting an unknown dependency', async () => {
    const update = vi.spyOn(api, 'updateAssetServiceStatus')
      .mockResolvedValue({} as AssetServiceRecord)
    const onCompleted = renderCorrection('unknown')

    const select = screen.getByLabelText('状态') as HTMLSelectElement
    expect(within(select).queryByRole('option', { name: '未确认' })).not.toBeInTheDocument()
    expect(select.value).toBe('')
    expect(screen.getByRole('button', { name: '确认纠正' })).toBeDisabled()

    fireEvent.change(select, { target: { value: 'paused' } })
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: 'provider confirmed the service is paused' } })
    fireEvent.click(screen.getByRole('button', { name: '确认纠正' }))

    await waitFor(() => expect(onCompleted).toHaveBeenCalledTimes(1))
    expect(update).toHaveBeenCalledWith('svc_001', { status: 'paused', reason: 'provider confirmed the service is paused' })
  })

  it('does not preselect a status the terminal parent cannot accept', () => {
    renderCorrection('active')

    const select = screen.getByLabelText('状态') as HTMLSelectElement
    expect(select.value).toBe('')
    expect(screen.getByRole('button', { name: '确认纠正' })).toBeDisabled()
    expect(within(select).getByRole('option', { name: /使用中/ })).toBeDisabled()
  })
})
