import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { VPSMaintenancePanel } from './VPSMaintenancePanel'
import { requestJSON } from '../../lib/apiRequest'

vi.mock('../../lib/apiRequest', () => ({ requestJSON: vi.fn() }))
const review = { vps_id: 'v1', active_action_id: '', preview_digest: 'maintenance-digest', resources: [{ kind: 'target', resource_id: 'shared', name: '公共入口', control: '启用', shared: true, shared_vps_ids: ['v2'], eligible: true }] }
beforeEach(() => { vi.resetAllMocks(); vi.mocked(requestJSON).mockResolvedValue(review) })
describe('VPS maintenance', () => {
  it('requires explicit selection for shared probes and sends the inspected digest', async () => {
    render(<VPSMaintenancePanel vpsId="v1" onChanged={vi.fn()} />)
    const shared = await screen.findByRole('checkbox', { name: '同时维护共享探测 公共入口' })
    expect(shared).not.toBeChecked()
    expect(screen.getByText(/涉及 VPS：v2/)).toBeInTheDocument()
    fireEvent.click(shared)
    fireEvent.change(screen.getByLabelText('维护原因'), { target: { value: '硬件检查' } })
    fireEvent.click(screen.getByRole('button', { name: '开始维护' }))
    await waitFor(() => expect(requestJSON).toHaveBeenCalledWith('/api/vps/v1/maintenance', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: '硬件检查', preview_digest: 'maintenance-digest', confirmed_shared_target_ids: ['shared'] }),
    }))
  })
  it('ends only the active maintenance action without resubmitting old resource controls', async () => {
    vi.mocked(requestJSON).mockResolvedValue({ ...review, active_action_id: 'action1' })
    render(<VPSMaintenancePanel vpsId="v1" onChanged={vi.fn()} />)
    await screen.findByRole('button', { name: '结束本次维护' })
    fireEvent.change(screen.getByLabelText('维护原因'), { target: { value: '检查完成' } })
    fireEvent.click(screen.getByRole('button', { name: '结束本次维护' }))
    await waitFor(() => expect(requestJSON).toHaveBeenCalledWith('/api/vps/v1/maintenance', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: '检查完成' }) }))
  })
})
