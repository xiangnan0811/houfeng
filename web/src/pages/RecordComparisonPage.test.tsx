import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../lib/apiRequest'
import type { ComparisonEvaluateResponse } from '../lib/types'
import { RecordComparisonPage } from './RecordComparisonPage'
import {
  COMPARISON_URL_VERSION,
  encodeComparisonURLState,
  type ComparisonURLState,
} from './records/compare/comparisonQueryState'

vi.mock('../lib/auth-context', () => {
  const auth = {
    user: {
      user_id: 'usr_1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: { records: true, comparison: true, portability: true },
      management_capabilities: { access: false },
    },
    loading: false,
    status: 'ready' as const,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    retry: vi.fn(),
  }
  return {
    useAuth: () => auth,
  }
})

const api = vi.hoisted(() => ({
  compare: vi.fn(),
  candidates: vi.fn(),
  snapshot: vi.fn(),
  revision: vi.fn(),
  activity: vi.fn(),
  vps: vi.fn(),
  instances: vi.fn(),
  targets: vi.fn(),
  createRecordDraft: vi.fn(),
  saveComparisonRecord: vi.fn(),
}))

vi.mock('../lib/recordsApi', () => ({
  resolveComparisonCandidates: (...args: unknown[]) => api.candidates(...args),
  evaluateFixedComparison: (...args: unknown[]) => api.compare(...args),
  getEvidenceSnapshot: (...args: unknown[]) => api.snapshot(...args),
  getRecordRevision: (...args: unknown[]) => api.revision(...args),
  listSubjectActivity: (...args: unknown[]) => api.activity(...args),
  createRecordDraft: (...args: unknown[]) => api.createRecordDraft(...args),
  saveComparisonRecord: (...args: unknown[]) => api.saveComparisonRecord(...args),
  createRecord: vi.fn(),
}))

vi.mock('../lib/api', () => ({
  listVPSAssets: (...args: unknown[]) => api.vps(...args),
  listMonitoringInstances: (...args: unknown[]) => api.instances(...args),
  listTargets: (...args: unknown[]) => api.targets(...args),
}))

const WINDOW = {
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
}

const response: ComparisonEvaluateResponse = {
  digest: 'aa'.repeat(32),
  items: [
    {
      snapshot_id: 'evs_a',
      canonical_hash: '11'.repeat(32),
      kind: 'monitoring.probe',
      schema_version: 2,
      revision_context: 'not_applicable',
    },
    {
      snapshot_id: 'evs_b',
      canonical_hash: '22'.repeat(32),
      kind: 'monitoring.probe',
      schema_version: 2,
      revision_context: 'not_applicable',
    },
  ],
  review: [{ item_index: 0, kind: 'monitoring.probe', schema_version: 2, reason: 'coverage_partial' }],
  available_kinds: [{ kind: 'monitoring.probe', schema_version: 2 }],
  pairwise: [],
  series: [{
    item_index: 0,
    metric_id: 'latency_ms',
    unit: 'ms',
    segments: [
      [{ start: '2026-07-01T00:00:00Z', end: '2026-07-01T00:05:00Z', value: 1 }],
      [{ start: '2026-07-01T00:20:00Z', end: '2026-07-01T00:25:00Z', value: 2 }],
    ],
  }],
  save_eligibility: { eligible: true, blockers: [] },
  comparison_intent: {
    token: 'cmp1.valid.payload.mac',
    key_id: 'k',
    issued_at: '2026-08-20T10:00:00Z',
    expires_at: '2026-08-20T10:15:00Z',
  },
}

function encodeRaw(value: unknown): string {
  return btoa(JSON.stringify(value)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function renderCompare(state?: ComparisonURLState) {
  const search = state ? `?state=${encodeComparisonURLState(state)}` : ''
  return renderSearch(search)
}

function renderSearch(search: string) {
  return render(
    <MemoryRouter initialEntries={[`/records/compare${search}`]}>
      <RecordComparisonPage />
    </MemoryRouter>,
  )
}

describe('RecordComparisonPage', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('treats a missing link as an empty basket with an add action', () => {
    renderCompare()
    expect(screen.getByRole('heading', { name: '横向比较', level: 1 })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '比较篮是空的' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '添加对象' })).toBeInTheDocument()
    expect(screen.getByText(/至少选择 2 项才能比较/)).toBeInTheDocument()
    expect(screen.queryByText(/比较链接已损坏/)).not.toBeInTheDocument()
    expect(screen.queryByText(/不支持的比较链接版本/)).not.toBeInTheDocument()
    expect(api.compare).not.toHaveBeenCalled()
    expect(api.vps).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '预览导出' })).not.toBeInTheDocument()
  })

  it('distinguishes a damaged link from an unsupported version', () => {
    const { unmount } = renderSearch('?state=%25%25%25')
    expect(screen.getByRole('heading', { name: '比较链接已损坏' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '不支持的比较链接版本' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '比较篮是空的' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '添加对象' })).toBeInTheDocument()
    expect(api.compare).not.toHaveBeenCalled()
    unmount()

    renderSearch(`?state=${encodeRaw({
      version: 'comparison-url/v0',
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }],
      baseline: 0,
      alignment: 'actual_coverage',
      requested_from: WINDOW.requested_from,
      requested_to: WINDOW.requested_to,
    })}`)
    expect(screen.getByRole('heading', { name: '不支持的比较链接版本' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '比较链接已损坏' })).not.toBeInTheDocument()
    expect(api.compare).not.toHaveBeenCalled()
  })

  it('does not compare a single seed item', () => {
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }],
      baseline: 0,
      alignment: 'actual_coverage',
      ...WINDOW,
    })
    expect(screen.getByText(/当前 1 项/)).toBeInTheDocument()
    expect(api.compare).not.toHaveBeenCalled()
  })

  it('places comparability review before the first chart and draws one polyline per gap', async () => {
    api.compare.mockResolvedValue(response)
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline: 0,
      alignment: 'actual_coverage',
      kind: 'monitoring.probe/v2',
      metric: 'latency_ms',
      tolerance_seconds: 60,
      ...WINDOW,
    })
    expect(await screen.findByRole('heading', { name: '可比性审查' })).toBeInTheDocument()
    const review = screen.getByRole('heading', { name: '可比性审查' })
    const trend = screen.getByRole('heading', { name: '趋势' })
    expect(review.compareDocumentPosition(trend) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(document.querySelectorAll('polyline[data-segment]')).toHaveLength(2)
    expect(screen.getByRole('button', { name: '另存为记录' })).toBeInTheDocument()
  })

  it('shows a cancel action while a compare is in flight', async () => {
    let resolveCompare: ((value: ComparisonEvaluateResponse) => void) | undefined
    api.compare.mockReturnValue(new Promise<ComparisonEvaluateResponse>((resolve) => {
      resolveCompare = resolve
    }))
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline: 0,
      alignment: 'actual_coverage',
      kind: 'monitoring.probe/v2',
      metric: 'latency_ms',
      ...WINDOW,
    })
    expect(await screen.findByRole('heading', { name: '正在加载比较' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消比较' })).toBeInTheDocument()
    resolveCompare?.(response)
    expect(await screen.findByRole('heading', { name: '可比性审查' })).toBeInTheDocument()
  })

  it('summarizes host deltas in system differences instead of only compatibility', async () => {
    api.compare.mockResolvedValue({
      ...response,
      pairwise: [{
        item_index: 1,
        kind: 'monitoring.probe',
        schema_version: 2,
        compatible: true,
        reason: 'coverage_partial',
        values: {
          matched: 2,
          unmatched_baseline: 0,
          unmatched_item: 1,
          equal: false,
          deltas: [{ delta: 8 }, { delta: 0 }],
        },
      }],
    })
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline: 0,
      alignment: 'actual_coverage',
      kind: 'monitoring.probe/v2',
      metric: 'latency_ms',
      ...WINDOW,
    })
    expect(await screen.findByRole('heading', { name: '系统差异' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '入口探测' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'monitoring.probe/v2' })).not.toBeInTheDocument()
    expect(screen.getByText(/入口探测：有差值，匹配 2 桶，基准未匹配 0，候选项未匹配 1，差值 1 桶/)).toBeInTheDocument()
    expect(screen.getByText(/入口探测：有差值/).textContent).not.toContain('monitoring.probe/v2')
  })

  it('does not invent series for non-host kinds and hides save when unreadable', async () => {
    api.compare.mockResolvedValue({
      ...response,
      items: response.items.map((item) => ({ ...item, kind: 'command.audit', schema_version: 1 })),
      available_kinds: [{ kind: 'command.audit', schema_version: 1 }],
      series: [],
      save_eligibility: { eligible: false, blockers: ['snapshot_unreadable'] },
      comparison_intent: undefined,
    })
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline: 0,
      alignment: 'actual_coverage',
      kind: 'command.audit/v1',
      ...WINDOW,
    })
    expect(await screen.findByRole('heading', { name: '系统差异' })).toBeInTheDocument()
    expect(document.querySelectorAll('polyline[data-segment]')).toHaveLength(0)
    expect(screen.queryByRole('heading', { name: '趋势' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
  })

  it('removes the visible comparison when saving is denied for an invalid intent', async () => {
    api.compare.mockResolvedValue(response)
    api.createRecordDraft.mockResolvedValue({
      draft_id: 'rdf_compare',
      etag: 'rdt1_compare',
    })
    api.saveComparisonRecord.mockRejectedValue(
      new ApiError(422, 'comparison intent is invalid', { code: 'comparison_intent_invalid' }),
    )
    renderCompare({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline: 0,
      alignment: 'actual_coverage',
      kind: 'monitoring.probe/v2',
      metric: 'latency_ms',
      tolerance_seconds: 60,
      ...WINDOW,
    })
    expect(await screen.findByRole('heading', { name: '比较结果' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '趋势' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '可比性审查' })).toBeInTheDocument()
    expect(screen.getByText('1 项需注意')).toBeInTheDocument()
    expect(document.querySelectorAll('polyline[data-segment]')).toHaveLength(2)
    fireEvent.click(screen.getByRole('button', { name: '另存为记录' }))

    expect(await screen.findByRole('heading', { name: '比较不可用' })).toBeInTheDocument()
    expect(screen.getByText('comparison intent is invalid')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '比较结果' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '趋势' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '可比性审查' })).not.toBeInTheDocument()
    expect(screen.queryByText('1 项需注意')).not.toBeInTheDocument()
    expect(screen.queryByText(response.digest)).not.toBeInTheDocument()
    expect(document.querySelectorAll('polyline[data-segment]')).toHaveLength(0)
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
    expect(screen.getByText('缺少有效的比较意图，请重新比较后再保存。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移出第 1 项' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移出第 2 项' })).toBeInTheDocument()
    expect(screen.getByText('2 项')).toBeInTheDocument()
    expect(api.saveComparisonRecord).toHaveBeenCalledTimes(1)
  })
})
