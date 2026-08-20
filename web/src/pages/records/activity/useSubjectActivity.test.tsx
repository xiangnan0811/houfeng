import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type { SubjectActivityListResponse } from '../../../lib/types'
import { useSubjectActivity } from './useSubjectActivity'

function page(overrides: Partial<SubjectActivityListResponse> = {}): SubjectActivityListResponse {
  return {
    subject: {
      kind: 'vps',
      source_id: 'vps_001',
      identity: { display_name: 'Edge' },
      live_route: '/vps/vps_001',
      status: 'live',
    },
    view: 'activity',
    snapshot_cursor: 'snap-opaque',
    freshness: {
      state: 'ready',
      visible_observed_at: '2026-08-01T00:00:00Z',
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_1',
      event_kind: 'record_created',
      event_at: '2026-08-01T00:00:00Z',
      recorded_at: '2026-08-01T00:00:01Z',
      source_kind: 'record_domain',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: '新建记录' },
    }],
    source_statuses: [],
    ...overrides,
  }
}

describe('useSubjectActivity', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the first page and keeps opaque cursors without decoding', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(page({
      next_cursor: 'next-opaque',
      freshness: {
        state: 'ready',
        visible_observed_at: null,
        new_items_available: true,
        reason_code: '',
      },
    }))

    const { result } = renderHook(() => useSubjectActivity({
      kind: 'vps',
      sourceId: 'vps_001',
      view: 'activity',
      filters: {},
    }))

    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    expect(list).toHaveBeenCalledWith('vps', 'vps_001', {})
    expect(result.current.state.snapshotCursor).toBe('snap-opaque')
    expect(result.current.state.nextCursor).toBe('next-opaque')
    expect(result.current.state.freshness?.new_items_available).toBe(true)
  })

  it('ignores a stale response when a newer request has started', async () => {
    let resolveFirst!: (value: SubjectActivityListResponse) => void
    const first = new Promise<SubjectActivityListResponse>((resolve) => {
      resolveFirst = resolve
    })
    const list = vi.spyOn(recordsApi, 'listSubjectActivity')
      .mockImplementationOnce(() => first)
      .mockResolvedValueOnce(page({
        items: [{
          activity_id: 'act_2',
          event_kind: 'command_executed',
          event_at: '2026-08-02T00:00:00Z',
          recorded_at: '2026-08-02T00:00:01Z',
          source_kind: 'command_audit',
          backfilled: false,
          subjects: [],
          presentation: { version: 1, title: '命令' },
        }],
      }))

    const { result, rerender } = renderHook(
      ({ sourceId }) => useSubjectActivity({
        kind: 'vps',
        sourceId,
        view: 'activity',
        filters: {},
      }),
      { initialProps: { sourceId: 'vps_001' } },
    )

    rerender({ sourceId: 'vps_002' })
    resolveFirst(page({ items: [{
      activity_id: 'act_stale',
      event_kind: 'record_created',
      event_at: '2026-08-01T00:00:00Z',
      recorded_at: '2026-08-01T00:00:01Z',
      source_kind: 'record_domain',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: '陈旧' },
    }] }))

    await waitFor(() => expect(result.current.state.items[0]?.activity_id).toBe('act_2'))
    expect(result.current.state.items.some((item) => item.activity_id === 'act_stale')).toBe(false)
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('appends with the opaque next cursor and recovers cursor errors', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity')
      .mockResolvedValueOnce(page({ next_cursor: 'page-2' }))
      .mockRejectedValueOnce(new ApiError(400, 'cursor invalid', { code: 'cursor_invalid' }))

    const { result } = renderHook(() => useSubjectActivity({
      kind: 'vps',
      sourceId: 'vps_001',
      view: 'records',
      filters: { source: ['record_domain'] },
    }))

    await waitFor(() => expect(result.current.state.nextCursor).toBe('page-2'))
    expect(list).toHaveBeenCalledWith('vps', 'vps_001', {
      view: 'records',
      source: ['record_domain'],
    })

    result.current.commands.append()
    await waitFor(() => expect(result.current.state.status).toBe('error'))
    expect(list).toHaveBeenLastCalledWith('vps', 'vps_001', {
      view: 'records',
      source: ['record_domain'],
      cursor: 'page-2',
    })
    expect(result.current.state.errorCode).toBe('cursor_invalid')
    // Append failure keeps already-visible items.
    expect(result.current.state.items).toHaveLength(1)
  })

  it('maps projection unavailable to a dedicated status', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockRejectedValue(
      new ApiError(503, 'unavailable', { code: 'activity_projection_unavailable' }),
    )

    const { result } = renderHook(() => useSubjectActivity({
      kind: 'target',
      sourceId: 'tg_001',
      view: 'evidence',
      filters: {},
    }))

    await waitFor(() => expect(result.current.state.status).toBe('unavailable'))
    expect(result.current.state.errorMessage).toContain('投影')
  })
})
