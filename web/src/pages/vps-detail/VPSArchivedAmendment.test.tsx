import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import type { VPSAssetRecord } from '../../lib/types'
import { VPSArchivedAmendment } from './VPSArchivedAmendment'

vi.mock('../../lib/api', () => ({ updateVPSAsset: vi.fn() }))

const vps = {
  vps_id: 'v1',
  note: '已申请退款',
  auto_renew_check: 'disabled',
  auto_renew_checked_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
} as VPSAssetRecord

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.updateVPSAsset).mockResolvedValue(vps)
})

describe('VPSArchivedAmendment', () => {
  it('records a fresh verification when the provider result is unchanged', async () => {
    const onChanged = vi.fn()
    render(<MemoryRouter><VPSArchivedAmendment vps={vps} onChanged={onChanged} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: '确认仍为此结果' }))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const [, patch, options] = vi.mocked(api.updateVPSAsset).mock.calls[0] ?? []
    expect(patch).toMatchObject({ auto_renew_check: 'disabled', note: '已申请退款' })
    expect(Date.parse(String(patch?.auto_renew_checked_at))).toBeGreaterThan(Date.parse('2026-08-01T00:00:00Z'))
    expect(options).toEqual({ expectedUpdatedAt: '2026-09-01T00:00:00Z' })
  })

  it('does not offer an empty save while the result is still unchecked', () => {
    render(<MemoryRouter><VPSArchivedAmendment vps={{ ...vps, auto_renew_check: 'unchecked', auto_renew_checked_at: null }} onChanged={vi.fn()} /></MemoryRouter>)
    expect(screen.getByRole('button', { name: '确认仍为此结果' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('服务商自动续费核对'), { target: { value: 'enabled' } })
    expect(screen.getByRole('button', { name: '保存修订' })).toBeEnabled()
  })
})
