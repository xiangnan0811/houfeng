import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type {
  EvidenceSnapshotRead,
  MonitoringInstanceRecord,
  SubjectActivityListResponse,
  TargetRecord,
  VPSAssetRecord,
} from '../../../lib/types'
import { targetObservationFixture } from '../../../lib/targetObservationFixture'
import { useComparisonObjectPicker } from './useComparisonObjectPicker'

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

function comparisonTarget(targetId: string, name: string, host: string): TargetRecord {
  const updatedAt = '2026-07-01T00:00:00Z'
  return {
    target_id: targetId,
    lifecycle_status: 'active',
    name,
    target_type: 'service',
    host,
    execution_monitoring_instance_labels: [],
    run_status: '启用',
    group: '',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: 1,
    observation_freshness: targetObservationFixture({
      target_id: targetId,
      run_status: '启用',
      lifecycle_status: 'active',
      evaluated_at: updatedAt,
      enabled_probe_count: 1,
    }),
    matching_executor_count: 1,
    current_primary_issue_summary: '',
    created_at: updatedAt,
    updated_at: updatedAt,
  }
}

function vps(id: string, name: string): VPSAssetRecord {
  return { vps_id: id, display_name: name, provider_name: '甲云', region: '东京' } as VPSAssetRecord
}

function activity(ids: string[], nextCursor?: string): SubjectActivityListResponse {
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
    items: ids.map((id) => ({
      activity_id: `act_${id}`,
      event_kind: 'evidence_captured' as const,
      event_at: '2026-07-01T12:00:00Z',
      recorded_at: '2026-07-01T12:00:01Z',
      source_kind: 'evidence_snapshot' as const,
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: `证据 ${id}` },
      evidence_snapshot_id: id,
    })),
    source_statuses: [],
    ...(nextCursor ? { next_cursor: nextCursor } : {}),
  }
}

function snapshot(id: string, status: 'complete' | 'partial' = 'complete'): EvidenceSnapshotRead {
  return {
    snapshot_id: id,
    title: `快照 ${id}`,
    kind: 'monitoring.host',
    observed_at: '2026-07-01T12:30:00Z',
    quality: { status },
    subject: { type: 'vps', id: 'vps_a', display_name: '边缘甲' },
  } as EvidenceSnapshotRead
}

function deferred<T>() {
  let resolve: (value: T) => void = () => {}
  let reject: (error: unknown) => void = () => {}
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function abortError(): Error {
  const error = new Error('The operation was aborted')
  error.name = 'AbortError'
  return error
}

function renderPicker(open = true) {
  return renderHook(
    (props: { open: boolean; from: string; to: string }) => useComparisonObjectPicker({
      ...props,
      onAdd: vi.fn(),
    }),
    { initialProps: { open, from: FROM, to: TO } },
  )
}

describe('useComparisonObjectPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.vps.mockResolvedValue([vps('vps_a', '边缘甲'), vps('vps_b', '边缘乙')])
    api.instances.mockResolvedValue([{ monitoring_instance_id: 'mi_a', display_name: '监控甲' } as MonitoringInstanceRecord])
    api.targets.mockResolvedValue([comparisonTarget('tg_a', '入口甲', 'a.example')])
    api.activity.mockResolvedValue(activity(['evs_a']))
    api.snapshot.mockImplementation(async (id: string) => snapshot(id))
  })

  it('does not read assets until the dialog is open', () => {
    renderPicker(false)
    expect(api.vps).not.toHaveBeenCalled()
    expect(api.activity).not.toHaveBeenCalled()
  })

  it('loads the authorized asset list, then evidence with the comparison window', async () => {
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjects.map((subject) => subject.label)).toEqual(['边缘甲', '边缘乙']))
    expect(api.vps).toHaveBeenCalledWith()
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_a']))
    expect(api.activity).toHaveBeenCalledWith('vps', 'vps_a', {
      view: 'evidence',
      source: ['evidence_snapshot'],
      versions: 'history',
      from: FROM,
      to: TO,
    }, expect.any(AbortSignal))
    await waitFor(() => expect(result.current.state.rows[0]?.qualityLabel).toBe('数据完整'))
    expect(result.current.state.rows[0]?.title).toBe('快照 evs_a')
  })

  it('keeps two snapshots from the same subject', async () => {
    api.activity.mockResolvedValue(activity(['evs_a', 'evs_b']))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_a', 'evs_b']))
  })

  it('resets the opaque cursor when the window changes and ignores a stale page', async () => {
    const first = deferred<SubjectActivityListResponse>()
    const more = deferred<SubjectActivityListResponse>()
    const nextWindow = deferred<SubjectActivityListResponse>()
    api.activity.mockImplementation((_kind: string, _id: string, filter?: { cursor?: string; to?: string }) => {
      if (filter?.cursor) return more.promise
      if (filter?.to === '2026-07-03T00:00:00Z') return nextWindow.promise
      return first.promise
    })
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await act(async () => {
      first.resolve(activity(['evs_a'], 'opaque-cursor'))
    })
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_a']))
    act(() => result.current.commands.loadMore())
    act(() => result.current.commands.setWindow(FROM, '2026-07-03T00:00:00Z'))
    await act(async () => {
      more.resolve(activity(['evs_stale']))
      nextWindow.resolve(activity(['evs_next']))
    })
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_next']))
    expect(api.activity).toHaveBeenCalledWith(
      'vps',
      'vps_a',
      expect.objectContaining({ cursor: 'opaque-cursor' }),
      expect.any(AbortSignal),
    )
    expect(api.activity).toHaveBeenLastCalledWith('vps', 'vps_a', {
      view: 'evidence',
      source: ['evidence_snapshot'],
      versions: 'history',
      from: FROM,
      to: '2026-07-03T00:00:00Z',
    }, expect.any(AbortSignal))
  })

  it('drops an in-flight subject page after the selection changes', async () => {
    const first = deferred<SubjectActivityListResponse>()
    api.activity.mockImplementation((_kind: string, id: string) => (
      id === 'vps_a' ? first.promise : Promise.resolve(activity(['evs_b']))
    ))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjects).toHaveLength(2))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[1]!))
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_b']))
    await act(async () => {
      first.resolve(activity(['evs_a']))
    })
    expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_b'])
  })

  it('shows projection failure with retry, and permission failure as unreadable rather than empty', async () => {
    api.activity
      .mockRejectedValueOnce(new ApiError(503, 'down', { code: 'activity_projection_unavailable' }))
      .mockResolvedValueOnce(activity(['evs_a']))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.evidenceStatus).toBe('unavailable'))
    expect(result.current.state.evidenceMessage).toBe('活动投影暂不可用。')
    expect(result.current.state.rows).toEqual([])
    act(() => result.current.commands.retryEvidence())
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_a']))

    api.activity.mockRejectedValueOnce(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    act(() => result.current.commands.retryEvidence())
    await waitFor(() => expect(result.current.state.evidenceStatus).toBe('unreadable'))
    expect(result.current.state.evidenceMessage).toBe('当前主体的证据无法读取。')
    expect(result.current.state.evidenceMessage).not.toContain('没有证据')
    expect(result.current.state.rows).toEqual([])
  })

  it('does not treat an empty page as authoritative when the evidence source is stale or unavailable', async () => {
    api.activity.mockResolvedValueOnce({
      ...activity([]),
      freshness: { state: 'ready', visible_observed_at: null, new_items_available: false, reason_code: '' },
      source_statuses: [
        { source_kind: 'evidence_snapshot', state: 'stale', reason_code: 'checkpoint_missing' },
        { source_kind: 'command_audit', state: 'ready', reason_code: '' },
      ],
    })
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.evidenceNotice).toBe('证据快照：过期。当前列表可能不完整。'))
    expect(result.current.state.evidenceStatus).not.toBe('empty')
    expect(result.current.state.evidenceMessage).toBeNull()
    expect(result.current.state.evidenceNotice).not.toContain('checkpoint_missing')
    expect(result.current.state.rows).toEqual([])

    api.activity.mockResolvedValueOnce({
      ...activity(['evs_a']),
      source_statuses: [
        { source_kind: 'evidence_snapshot', state: 'unavailable', reason_code: 'source_error' },
      ],
    })
    act(() => result.current.commands.retryEvidence())
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_a']))
    expect(result.current.state.evidenceStatus).toBe('ready')
    expect(result.current.state.evidenceNotice).toBe('证据快照：不可用。当前列表可能不完整。')
    expect(result.current.state.evidenceNotice).not.toContain('source_error')
    expect(result.current.state.evidenceMessage).toBeNull()
  })

  it('still reports an empty window when only a non-evidence source is incomplete', async () => {
    api.activity.mockResolvedValueOnce({
      ...activity([]),
      freshness: { state: 'ready', visible_observed_at: null, new_items_available: true, reason_code: 'hidden_scope' },
      source_statuses: [
        { source_kind: 'command_audit', state: 'unavailable', reason_code: 'source_error' },
        { source_kind: 'evidence_snapshot', state: 'ready', reason_code: '' },
      ],
    })
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.evidenceStatus).toBe('empty'))
    expect(result.current.state.evidenceMessage).toBe('当前窗口没有证据。')
    expect(result.current.state.evidenceNotice).toBeNull()
  })

  it('uses fixed copy for an unknown asset or evidence failure', async () => {
    api.vps.mockRejectedValueOnce(new Error('postgres.internal:5432 connection refused'))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('error'))
    expect(result.current.state.subjectsMessage).toBe('无法读取资产，请重试')
    expect(result.current.state.subjectsMessage).not.toContain('postgres')

    api.vps.mockResolvedValueOnce([vps('vps_a', '边缘甲')])
    act(() => result.current.commands.retrySubjects())
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    api.activity.mockRejectedValueOnce(new Error('ECONNRESET upstream'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.evidenceStatus).toBe('error'))
    expect(result.current.state.evidenceMessage).toBe('无法读取证据，请重试')
    expect(result.current.state.evidenceMessage).not.toContain('ECONNRESET')
  })

  it('does not present a permission failure of the asset list as an empty catalog', async () => {
    api.vps.mockRejectedValueOnce(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('unreadable'))
    expect(result.current.state.subjectsMessage).toBe('这些资产无法读取。')
    expect(result.current.state.subjects).toEqual([])

    api.vps.mockResolvedValueOnce([])
    act(() => result.current.commands.retrySubjects())
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('empty'))
    expect(result.current.state.subjectsMessage).toBe('暂无 VPS。')
  })

  it('marks a failed snapshot read unreadable and does not add it', async () => {
    const onAdd = vi.fn()
    api.snapshot.mockRejectedValue(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    const { result } = renderHook(() => useComparisonObjectPicker({
      open: true,
      from: FROM,
      to: TO,
      onAdd,
    }))
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.rows[0]?.unreadable).toBe(true))
    expect(result.current.state.rows[0]?.qualityLabel).toBeNull()
    await act(async () => {
      await result.current.commands.addSnapshot('evs_a')
    })
    expect(onAdd).not.toHaveBeenCalled()
    expect(result.current.state.rows[0]?.unreadable).toBe(true)
  })

  it('adds only after the snapshot read succeeds', async () => {
    const onAdd = vi.fn()
    const { result } = renderHook(() => useComparisonObjectPicker({
      open: true,
      from: FROM,
      to: TO,
      onAdd,
    }))
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(result.current.state.rows[0]?.pending).toBe(false))
    await act(async () => {
      await result.current.commands.addSnapshot('evs_a')
    })
    expect(onAdd).toHaveBeenCalledWith({ snapshot_id: 'evs_a' })
  })

  it('switches the authorized catalog with the subject kind', async () => {
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.setSubjectKind('monitoring_instance'))
    await waitFor(() => expect(result.current.state.subjects.map((subject) => subject.id)).toEqual(['mi_a']))
    expect(api.instances).toHaveBeenCalledWith()
    act(() => result.current.commands.setSubjectKind('target'))
    await waitFor(() => expect(result.current.state.subjects.map((subject) => subject.label)).toEqual(['入口甲']))
    expect(api.targets).toHaveBeenCalledWith()
  })

  it('drops a stale window page and does not surface its abort as a failure', async () => {
    const first = deferred<SubjectActivityListResponse>()
    const nextWindow = deferred<SubjectActivityListResponse>()
    api.activity.mockImplementation((_kind: string, _id: string, filter?: { to?: string }) => (
      filter?.to === '2026-07-03T00:00:00Z' ? nextWindow.promise : first.promise
    ))
    const { result } = renderPicker()
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(api.activity).toHaveBeenCalled())
    const signal = api.activity.mock.calls[0]?.[3] as AbortSignal
    act(() => {
      result.current.commands.setWindow(FROM, '2026-07-03T00:00:00Z')
      expect(signal.aborted).toBe(true)
    })
    await act(async () => {
      first.reject(abortError())
    })
    expect(result.current.state.rows.map((row) => row.snapshotId)).not.toContain('evs_stale')
    expect(result.current.state.evidenceMessage).toBeNull()
    expect(result.current.state.evidenceStatus).not.toBe('error')
    await act(async () => {
      nextWindow.resolve(activity(['evs_next']))
    })
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_next']))
  })

  it('drops a stale subject page and a snapshot add that started before the change', async () => {
    const first = deferred<SubjectActivityListResponse>()
    const second = deferred<SubjectActivityListResponse>()
    const snap = deferred<EvidenceSnapshotRead>()
    let addSignal: AbortSignal | undefined
    api.activity.mockImplementation((_kind: string, id: string) => (
      id === 'vps_a' ? first.promise : second.promise
    ))
    api.snapshot.mockImplementation((_id: string, signal?: AbortSignal) => {
      addSignal = signal
      return snap.promise
    })
    const onAdd = vi.fn()
    const { result } = renderHook(() => useComparisonObjectPicker({
      open: true,
      from: FROM,
      to: TO,
      onAdd,
    }))
    await waitFor(() => expect(result.current.state.subjects).toHaveLength(2))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(api.activity).toHaveBeenCalled())
    const evidenceSignal = api.activity.mock.calls[0]?.[3] as AbortSignal
    let pendingAdd = Promise.resolve()
    act(() => {
      pendingAdd = result.current.commands.addSnapshot('evs_a')
    })
    act(() => {
      result.current.commands.selectSubject(result.current.state.subjects[1]!)
      expect(evidenceSignal.aborted).toBe(true)
      expect(addSignal?.aborted).toBe(true)
    })
    await act(async () => {
      first.resolve(activity(['evs_stale']))
      snap.resolve(snapshot('evs_a'))
      await pendingAdd
    })
    expect(onAdd).not.toHaveBeenCalled()
    expect(result.current.state.rows.map((row) => row.snapshotId)).not.toContain('evs_stale')
    await act(async () => {
      second.resolve(activity(['evs_b']))
    })
    await waitFor(() => expect(result.current.state.rows.map((row) => row.snapshotId)).toEqual(['evs_b']))
  })

  it('does not publish a pending evidence page or snapshot add after close or unmount', async () => {
    const page = deferred<SubjectActivityListResponse>()
    const snap = deferred<EvidenceSnapshotRead>()
    let addSignal: AbortSignal | undefined
    api.activity.mockImplementation(() => page.promise)
    api.snapshot.mockImplementation((_id: string, signal?: AbortSignal) => {
      addSignal = signal
      return snap.promise
    })
    const onAdd = vi.fn()
    const { result, rerender, unmount } = renderHook(
      (props: { open: boolean }) => useComparisonObjectPicker({
        open: props.open,
        from: FROM,
        to: TO,
        onAdd,
      }),
      { initialProps: { open: true } },
    )
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(api.activity).toHaveBeenCalled())
    const evidenceSignal = api.activity.mock.calls.at(-1)?.[3] as AbortSignal
    let pendingAdd = Promise.resolve()
    act(() => {
      pendingAdd = result.current.commands.addSnapshot('evs_a')
    })
    rerender({ open: false })
    expect(evidenceSignal.aborted).toBe(true)
    expect(addSignal?.aborted).toBe(true)
    await act(async () => {
      page.resolve(activity(['evs_stale']))
      snap.resolve(snapshot('evs_a'))
      await pendingAdd
    })
    expect(onAdd).not.toHaveBeenCalled()
    expect(result.current.state.rows).toEqual([])
    expect(result.current.state.evidenceStatus).toBe('idle')
    expect(result.current.state.evidenceMessage).toBeNull()

    const laterPage = deferred<SubjectActivityListResponse>()
    const laterSnap = deferred<EvidenceSnapshotRead>()
    api.activity.mockImplementation(() => laterPage.promise)
    api.snapshot.mockImplementation(() => laterSnap.promise)
    rerender({ open: true })
    await waitFor(() => expect(result.current.state.subjectsStatus).toBe('ready'))
    act(() => result.current.commands.selectSubject(result.current.state.subjects[0]!))
    await waitFor(() => expect(api.activity.mock.calls.length).toBeGreaterThan(1))
    let laterAdd = Promise.resolve()
    act(() => {
      laterAdd = result.current.commands.addSnapshot('evs_later')
    })
    unmount()
    await act(async () => {
      laterPage.resolve(activity(['evs_unmounted']))
      laterSnap.resolve(snapshot('evs_later'))
      await laterAdd
    })
    expect(onAdd).not.toHaveBeenCalled()
  })
})
