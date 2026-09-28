import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { VPSLifecycleWorkspace } from './VPSLifecycleWorkspace'
import * as api from '../../lib/api'

vi.mock('../../lib/api', () => ({
  listVPSFollowups: vi.fn(), listVPSAssociations: vi.fn(), listAssetServices: vi.fn(), listAssetDomains: vi.fn(), listTargets: vi.fn(), listVPSServices: vi.fn(),
  createVPSFollowup: vi.fn(), resolveVPSFollowup: vi.fn(), linkVPSAssociation: vi.fn(), endVPSAssociation: vi.fn(),
}))
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.listVPSFollowups).mockResolvedValue([])
  vi.mocked(api.listVPSAssociations).mockResolvedValue([])
  vi.mocked(api.listAssetServices).mockResolvedValue([])
  vi.mocked(api.listAssetDomains).mockResolvedValue([])
  vi.mocked(api.listTargets).mockResolvedValue([])
  vi.mocked(api.listVPSServices).mockResolvedValue([])
})
describe('VPS lifecycle workspace', () => {
  it('allows archived followups to resolve with a required reason', async () => {
    vi.mocked(api.listVPSFollowups).mockResolvedValue([{ followup_id: 'f1', vps_id: 'v1', kind: 'archived_online', dedupe_key: 's1', status: 'pending', summary: '归档后再次在线', details: {}, resolution_reason: '', resolved_by: '', created_at: '2026-09-01', updated_at: '2026-09-01' }])
    render(<VPSLifecycleWorkspace vpsId="v1" archived kind="followups" />)
    fireEvent.click(await screen.findByRole('button', { name: '处理' }))
    const resolve = screen.getByRole('button', { name: '解决' })
    expect(resolve).toBeDisabled()
    fireEvent.change(screen.getByLabelText('处理原因'), { target: { value: '已关闭旧 Agent' } })
    fireEvent.click(resolve)
    await waitFor(() => expect(api.resolveVPSFollowup).toHaveBeenCalledWith('v1', 'f1', 'resolved', '已关闭旧 Agent'))
  })
  it('records a migration followup with target and result kept separate from resolution reasons', async () => {
    render(<VPSLifecycleWorkspace vpsId="v1" archived kind="followups" />)
    fireEvent.click(await screen.findByText('新增迁移跟进'))
    fireEvent.change(screen.getByLabelText('新增跟进事项'), { target: { value: '迁移代理' } })
    fireEvent.change(screen.getByLabelText('迁移目标 VPS（名称或标识）'), { target: { value: 'Osaka Edge' } })
    fireEvent.change(screen.getByLabelText('迁移结果'), { target: { value: '流量已切换' } })
    fireEvent.click(screen.getByRole('button', { name: '记录迁移跟进' }))
    await waitFor(() => expect(api.createVPSFollowup).toHaveBeenCalledWith('v1', { kind: 'migration', summary: '迁移代理', details: { source_vps_id: 'v1', target_vps: 'Osaka Edge', result: '流量已切换' } }))
  })
  it('links an existing shared object without replacing its identity', async () => {
    vi.mocked(api.listAssetServices).mockResolvedValue([{ service_id: 'shared', name: '公共 API' } as never])
    render(<VPSLifecycleWorkspace vpsId="v2" kind="service" />)
    const select = await screen.findByRole('combobox', { name: '关联已有服务' })
    fireEvent.change(select, { target: { value: 'shared' } })
    fireEvent.change(screen.getByLabelText('此 VPS 上的承载地址'), { target: { value: '10.0.0.2' } })
    fireEvent.click(screen.getByRole('button', { name: '关联到此 VPS' }))
    await waitFor(() => expect(api.linkVPSAssociation).toHaveBeenCalledWith('v2', 'service', { object_id: 'shared', address: '10.0.0.2' }))
  })
  it('keeps archived association history read-only and displays the end snapshot', async () => {
    vi.mocked(api.listVPSAssociations).mockResolvedValue([{ association_id: 'a1', object_id: 's1', vps_id: 'v1', address: 'old-host', started_at: '2026-01-01', ended_at: '2026-09-01', end_reason: '已迁移', ended_by: 'operator', snapshot: { name: '归档时名称' } }])
    render(<VPSLifecycleWorkspace vpsId="v1" archived kind="service" />)
    expect(await screen.findByText('关联结束时的事实')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '结束此关联' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关联到此 VPS' })).not.toBeInTheDocument()
  })
  it('keeps association history readable when current identity metadata is unavailable', async () => {
    vi.mocked(api.listAssetServices).mockRejectedValue(new Error('catalog unavailable'))
    vi.mocked(api.listVPSAssociations).mockResolvedValue([{ association_id: 'a1', object_id: 'service-history', vps_id: 'v1', address: 'old-host', started_at: '2026-01-01', ended_at: '2026-09-01', end_reason: '已迁移', ended_by: 'operator', snapshot: { name: '归档时名称' } }])
    render(<VPSLifecycleWorkspace vpsId="v1" archived kind="service" />)
    expect(await screen.findByText('service-history')).toBeInTheDocument()
    expect(screen.getByText(/部分对象或探测目录暂不可用/)).toBeInTheDocument()
    expect(screen.getByText('关联结束时的事实')).toBeInTheDocument()
  })
  it('does not apply an old VPS read after switching subjects', async () => {
    let resolveOld!: (value: never[]) => void
    vi.mocked(api.listVPSFollowups).mockImplementation((id) => id === 'old' ? new Promise((resolve) => { resolveOld = resolve }) : Promise.resolve([]))
    const view = render(<VPSLifecycleWorkspace vpsId="old" kind="followups" />)
    view.rerender(<VPSLifecycleWorkspace vpsId="new" kind="followups" />)
    expect(await screen.findByText('暂无跟进事项。')).toBeInTheDocument()
    resolveOld([{ followup_id: 'f', summary: '旧 VPS 回应', status: 'pending' } as never])
    await waitFor(() => expect(screen.queryByText('旧 VPS 回应')).not.toBeInTheDocument())
  })
})
