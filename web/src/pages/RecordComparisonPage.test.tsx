import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ComparisonEvaluateResponse } from '../lib/types'
import { RecordComparisonPage } from './RecordComparisonPage'
import {
  COMPARISON_URL_VERSION,
  encodeComparisonURLState,
  type ComparisonURLState,
} from './records/compare/comparisonQueryState'

vi.mock('../lib/auth-context', () => ({
  useAuth: () => ({
    user: { user_id: 'usr_1', username: 'admin', role: 'admin', display_name: '管理员' },
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }),
}))

const api = vi.hoisted(() => ({
  compare: vi.fn(),
  candidates: vi.fn(),
}))

vi.mock('../lib/recordsApi', () => ({
  resolveComparisonCandidates: (...args: unknown[]) => api.candidates(...args),
  evaluateFixedComparison: (...args: unknown[]) => api.compare(...args),
  createRecordDraft: vi.fn(),
  saveComparisonRecord: vi.fn(),
  createRecord: vi.fn(),
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

function renderCompare(state?: ComparisonURLState) {
  const search = state ? `?state=${encodeComparisonURLState(state)}` : ''
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

  it('keeps a recoverable shell for missing or damaged state', () => {
    renderCompare()
    expect(screen.getByRole('heading', { name: '横向比较工作台' })).toBeInTheDocument()
    expect(screen.getByText(/从选择篮开始/)).toBeInTheDocument()
    expect(screen.getByText(/至少选择 2 项才能比较/)).toBeInTheDocument()
    expect(api.compare).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '预览导出' })).not.toBeInTheDocument()
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
    expect(document.querySelectorAll('polyline')).toHaveLength(2)
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
    expect(screen.getByText(/有差值，匹配 2 桶，基准未匹配 0，候选项未匹配 1，差值 1 桶/)).toBeInTheDocument()
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
    expect(document.querySelectorAll('polyline')).toHaveLength(0)
    expect(screen.queryByRole('heading', { name: '趋势' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
  })
})
