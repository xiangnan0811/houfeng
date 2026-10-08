import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { EvidenceSnapshotRead, SubjectActivityListResponse, VPSAssetRecord } from '../../../lib/types'
import { ComparisonObjectDialog } from './ComparisonObjectDialog'

const api = vi.hoisted(() => ({
  vps: vi.fn(),
  instances: vi.fn(),
  targets: vi.fn(),
  activity: vi.fn(),
  snapshot: vi.fn(),
}))

vi.mock('../../../lib/api', () => ({
  listVPSAssets: (...args: unknown[]) => api.vps(...args),
  listMonitoringInstances: (...args: unknown[]) => api.instances(...args),
  listTargets: (...args: unknown[]) => api.targets(...args),
}))

vi.mock('../../../lib/recordsApi', () => ({
  listSubjectActivity: (...args: unknown[]) => api.activity(...args),
  getEvidenceSnapshot: (...args: unknown[]) => api.snapshot(...args),
}))

const FROM = '2026-07-01T00:00:00Z'
const TO = '2026-07-02T00:00:00Z'

function activity(id = 'evs_a'): SubjectActivityListResponse {
  return {
    subject: {
      kind: 'vps',
      source_id: 'vps_a',
      identity: { display_name: '边缘甲' },
      status: 'live',
    },
    view: 'evidence',
    snapshot_cursor: 'snap-opaque',
    freshness: {
      state: 'ready',
      visible_observed_at: null,
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_a',
      event_kind: 'evidence_captured',
      event_at: '2026-07-01T12:00:00Z',
      recorded_at: '2026-07-01T12:00:01Z',
      source_kind: 'evidence_snapshot',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: '第三晚观测' },
      evidence_snapshot_id: id,
    }],
    source_statuses: [],
  }
}

function renderDialog(onAdd = vi.fn()) {
  return {
    onAdd,
    ...render(
      <MemoryRouter>
        <ComparisonObjectDialog
          open
          onClose={vi.fn()}
          from={FROM}
          to={TO}
          basketSnapshotIds={[]}
          selectionError="最多比较 6 项，超出的选择已拒绝。"
          onAdd={onAdd}
        />
      </MemoryRouter>,
    ),
  }
}

describe('ComparisonObjectDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.vps.mockResolvedValue([{ vps_id: 'vps_a', display_name: '边缘甲', provider_name: '甲云', region: '东京' } as VPSAssetRecord])
    api.instances.mockResolvedValue([])
    api.targets.mockResolvedValue([])
    api.activity.mockResolvedValue(activity())
    api.snapshot.mockResolvedValue({
      snapshot_id: 'evs_a',
      title: '第三晚观测',
      kind: 'monitoring.host',
      observed_at: '2026-07-01T12:30:00Z',
      quality: { status: 'partial' },
      subject: { type: 'vps', id: 'vps_a', display_name: '边缘甲' },
    } as EvidenceSnapshotRead)
  })

  it('shows readable evidence facts, keeps the technical id collapsed, and adds the snapshot id', async () => {
    const { onAdd } = renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    expect(await screen.findByText('第三晚观测')).toHaveClass('record-compare-items__title')
    expect(await screen.findByText(/部分覆盖/)).toBeInTheDocument()
    expect(screen.getByText(/主机监控/)).toBeInTheDocument()
    expect(screen.getByText('evs_a').closest('details')).toBeTruthy()
    expect(screen.getByRole('alert')).toHaveTextContent('最多比较 6 项')
    fireEvent.click(screen.getByRole('button', { name: '加入比较' }))
    await waitFor(() => expect(onAdd).toHaveBeenCalledWith({ snapshot_id: 'evs_a' }))
  })

  it('marks a failed snapshot read unreadable and does not add it', async () => {
    api.snapshot.mockRejectedValue(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    const { onAdd } = renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    expect(await screen.findByText(/不可读/)).toBeInTheDocument()
    expect(screen.queryByText(/数据完整/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试读取' }))
    await waitFor(() => expect(api.snapshot.mock.calls.length).toBeGreaterThan(1))
    expect(onAdd).not.toHaveBeenCalled()
  })

  it('links an empty evidence window to the subject workspace', async () => {
    api.activity.mockResolvedValue({ ...activity(), items: [] })
    renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    const link = await screen.findByRole('link', { name: '打开证据工作区' })
    expect(link).toHaveAttribute('href', '/vps/vps_a/evidence')
    expect(screen.getByText('当前窗口没有证据')).toBeInTheDocument()
    expect(screen.queryByText('当前主体的证据无法读取。')).not.toBeInTheDocument()
  })

  it('keeps returned rows and does not claim an empty window when the evidence source is stale', async () => {
    api.activity.mockResolvedValue({
      ...activity(),
      freshness: { state: 'ready', visible_observed_at: null, new_items_available: false, reason_code: '' },
      source_statuses: [
        { source_kind: 'evidence_snapshot', state: 'stale', reason_code: 'lease_expired' },
      ],
    })
    renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    expect(await screen.findByText('第三晚观测')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('证据快照：过期。当前列表可能不完整。')
    expect(screen.getByRole('status')).not.toHaveTextContent('lease_expired')
    expect(screen.queryByText('当前窗口没有证据')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '加入比较' })).toBeInTheDocument()
  })

  it('does not call an incomplete evidence source an empty window', async () => {
    api.activity.mockResolvedValueOnce({
      ...activity(),
      items: [],
      freshness: { state: 'ready', visible_observed_at: null, new_items_available: false, reason_code: '' },
      source_statuses: [
        { source_kind: 'evidence_snapshot', state: 'unavailable', reason_code: 'source_error' },
      ],
    })
    renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('证据快照：不可用。当前列表可能不完整。'))
    expect(screen.queryByText('当前窗口没有证据')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '打开证据工作区' })).not.toBeInTheDocument()
    api.activity.mockResolvedValueOnce(activity())
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('button', { name: '加入比较' })).toBeInTheDocument()
  })

  it('offers retry when the activity projection is unavailable', async () => {
    api.activity.mockRejectedValueOnce(new ApiError(503, 'down', { code: 'activity_projection_unavailable' }))
    renderDialog()
    fireEvent.click(await screen.findByRole('button', { name: '边缘甲' }))
    expect(await screen.findByRole('heading', { name: '活动投影暂不可用' })).toBeInTheDocument()
    api.activity.mockResolvedValueOnce(activity())
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('button', { name: '加入比较' })).toBeInTheDocument()
  })
})
