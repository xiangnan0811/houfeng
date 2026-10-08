import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { COLLECT_POLL_INTERVAL_MS } from '../components/ip-quality/useIPQualityCollect'
import { VPSIPQualityPage } from './VPSIPQualityPage'

const ipQualitySummaryBody = {
  report_id: 'ipq_001',
  vps_id: 'vps_001',
  observed_at: '2026-06-08T12:00:00Z',
  ip_address: '192.0.2.1',
  ip_version: 4,
  status: 'success',
  risk_level: 'high',
  use_region_code: 'JP',
  use_region_name: 'Japan',
  asn: 'AS64500',
  organization: 'Example Transit',
  stale: false,
  ambiguous: false,
  assignment_mode: 'link',
  provider_count: 2,
  unlockable_count: 3,
}

const ipQualityReportBody = {
  summary: ipQualitySummaryBody,
  latest_report: {
    report_id: 'ipq_001',
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-06-08T12:00:00Z',
    received_at: '2026-06-08T12:00:05Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    sync_batch_id: 'sync_001',
    ip_address: '192.0.2.1',
    ip_version: 4,
    status: 'success',
    risk_level: 'high',
    is_backfilled: false,
    created_at: '2026-06-08T12:00:06Z',
  },
  provider_results: [
    { provider: 'ipinfo', status: 'success', source_type: 'default', usage_type: 'hosting', is_vpn: true, is_proxy: false },
  ],
  service_unlocks: [
    { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'JP' },
  ],
  history: [
    ipQualitySummaryBody,
    { ...ipQualitySummaryBody, report_id: 'ipq_000', observed_at: '2026-06-07T12:00:00Z', risk_level: 'medium' },
  ],
}

const emptyReportBody = { summary: null, latest_report: null, provider_results: [], service_unlocks: [], history: [] }
const availableStatus = { enabled: true, available: true, monitoring_instance_id: 'mi_001' }

function collectRequest(status: string, extra: Record<string, unknown> = {}) {
  return {
    request_id: 'ipqc_001',
    monitoring_instance_id: 'mi_001',
    status,
    requested_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 600_000).toISOString(),
    ...extra,
  }
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

type Route = { method?: string, path: string, responses: Array<{ body: unknown, status?: number }> }

// 按 method + path 分发的 fetch 替身；每条路由按顺序消费响应，最后一个响应重复使用。
function routeFetch(routes: Route[]) {
  const queues = routes.map((route) => ({ ...route, responses: [...route.responses] }))
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const method = init?.method ?? 'GET'
    const route = queues.find((item) => item.path === path && (item.method ?? 'GET') === method)
    if (!route) throw new Error(`unexpected request ${method} ${path}`)
    const next = route.responses.length > 1 ? route.responses.shift() : route.responses[0]
    if (!next) throw new Error(`no response configured for ${method} ${path}`)
    return mockJSONResponse(next.body, next.status)
  })
}

function renderPage(initialEntry = '/vps/vps_001/ip-quality') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/vps/:vpsId/ip-quality" element={<VPSIPQualityPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function callsTo(fetchMock: ReturnType<typeof routeFetch>, path: string, method = 'GET') {
  return fetchMock.mock.calls.filter(([input, init]) => String(input) === path && (init?.method ?? 'GET') === method)
}

describe('VPSIPQualityPage', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads the latest report and the collection status', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: ipQualityReportBody }] },
      { path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/ip-quality', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    await waitFor(() => expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality/collect')).toHaveLength(1))
    expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
    expect(screen.getByRole('link', { name: '返回 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent('192.0.2.1')
    expect(screen.getByRole('heading', { name: '服务解锁' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'IP 数据库判断' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '历史报告' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '采集诊断' }).closest('details')).not.toHaveAttribute('open')
    expect(screen.queryByRole('button', { name: /保存|编辑|删除/ })).not.toBeInTheDocument()
  })

  it('collects immediately from the empty state and shows the report when the agent answers', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: emptyReportBody }, { body: ipQualityReportBody }] },
      {
        path: '/api/vps/vps_001/ip-quality/collect',
        responses: [
          { body: availableStatus },
          { body: { ...availableStatus, request: collectRequest('dispatched', { dispatched_at: new Date().toISOString() }) } },
          { body: { ...availableStatus, request: collectRequest('completed', { report_status: 'success', completed_at: new Date().toISOString() }) } },
        ],
      },
      { method: 'POST', path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: { ...availableStatus, request: collectRequest('pending') }, status: 202 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '暂无 IP 质量报告' })).toBeInTheDocument())
    expect(screen.getAllByRole('button', { name: '立即采集' })).toHaveLength(1)
    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))

    await waitFor(() => expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled())
    expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality/collect', 'POST')).toHaveLength(1)
    expect(screen.getByRole('status')).toHaveTextContent('等待 agent 同步')

    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('agent 已接收，正在检测'))

    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent('192.0.2.1'))
    expect(screen.getByRole('status')).toHaveTextContent('采集完成，报告已更新')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
    expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality')).toHaveLength(2)

    // 请求结束后停止轮询。
    const pollsBefore = callsTo(fetchMock, '/api/vps/vps_001/ip-quality/collect').length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
    expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality/collect')).toHaveLength(pollsBefore)
  })

  it('keeps the previous report and says so when the manual collection fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: ipQualityReportBody }] },
      {
        path: '/api/vps/vps_001/ip-quality/collect',
        responses: [
          { body: { ...availableStatus, request: collectRequest('dispatched') } },
          { body: { ...availableStatus, request: collectRequest('completed', { report_status: 'failure', error_summary: 'lookup timeout' }) } },
        ],
      },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    // 页面打开时已有进行中的请求：直接接手并展示进度。
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在采集最新 IP 质量'))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('本次采集失败，仍展示上一份有效报告'))
    expect(screen.getByRole('status')).toHaveTextContent('检测请求超时，未能获取最新数据')
    const details = screen.getByRole('status').querySelector('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')
    expect(details).toHaveTextContent('lookup timeout')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent('192.0.2.1')
  })

  it('shows why collection is unavailable in the empty state and links to workbench with explicit vpsId', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: emptyReportBody }] },
      { path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: { enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' } }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('该 VPS 尚未接入监控 agent'))
    expect(within(screen.getByRole('status')).getByRole('link', { name: '前往监控工作台' })).toHaveAttribute('href', '/vps/vps_001?workbench=monitoring')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.queryByText('0.0.0.0')).not.toBeInTheDocument()
  })

  it('shows the server reason after a rejected collection request and links to actual MI with valid returnVPS', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: ipQualityReportBody }] },
      {
        path: '/api/vps/vps_001/ip-quality/collect',
        responses: [
          { body: availableStatus },
          { body: { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' } },
        ],
      },
      { method: 'POST', path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: { error: 'monitoring instance monitoring is paused', reason: 'monitoring_paused' }, status: 409 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('监控已暂停'))
    expect(within(screen.getByRole('status')).getByRole('link', { name: '前往监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /恢复/ })).not.toBeInTheDocument()
  })

  it('loads a historical report detail when report_id is present', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality/reports/ipq_000', responses: [{ body: { ...ipQualityReportBody, latest_report: { ...ipQualityReportBody.latest_report, report_id: 'ipq_000' } } }] },
      { path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?report_id=ipq_000')

    await waitFor(() => expect(screen.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality'))
    expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
    expect(within(screen.getByLabelText('采集诊断')).getByText('ipq_000')).toBeInTheDocument()
  })

  it('loads the selected history report after switching from latest', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: ipQualityReportBody }] },
      { path: '/api/vps/vps_001/ip-quality/reports/ipq_000', responses: [{ body: { ...ipQualityReportBody, summary: { ...ipQualitySummaryBody, report_id: 'ipq_000' } } }] },
      { path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '历史报告' })).toBeInTheDocument())
    const historyLink = screen.getAllByRole('link', { name: /^查看 .* 的报告$/ })
      .find((link) => link.getAttribute('href')?.includes('report_id=ipq_000'))
    expect(historyLink).toBeDefined()
    fireEvent.click(historyLink as HTMLAnchorElement)

    await waitFor(() => expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality/reports/ipq_000')).toHaveLength(1))
    await waitFor(() => expect(screen.getByRole('link', { name: '查看最新报告' })).toBeInTheDocument())
  })

  it('retries a failed report load without leaving the page', async () => {
    const fetchMock = routeFetch([
      { path: '/api/vps/vps_001/ip-quality', responses: [{ body: { error: 'temporary failure' }, status: 500 }, { body: ipQualityReportBody }] },
      { path: '/api/vps/vps_001/ip-quality/collect', responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量报告加载失败' })).toBeInTheDocument())
    expect(screen.getByText('temporary failure')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeInTheDocument())
    expect(callsTo(fetchMock, '/api/vps/vps_001/ip-quality')).toHaveLength(2)
  })
})
