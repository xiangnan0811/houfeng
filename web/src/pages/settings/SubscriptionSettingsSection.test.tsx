import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SubscriptionSettingsSection } from './SubscriptionSettingsSection'

function json(body: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, text: async () => JSON.stringify(body) } as Response
}

const SETTINGS = {
  base_currency: 'CNY',
  exchange_rate_provider: 'frankfurter',
  fixer_configured: true,
  fixer_masked_summary: 'fx_****8a2c',
  default_reminder_offsets_days: [14, 7, 1],
  max_reminder_lead_days: 30,
  exchange_rate_stale_after_hours: 36,
}

const BUDGET = {
  budget_month: '2026-06-01', base_currency: 'CNY', monthly_limit: 100, warning_pct: 80, note: '',
  created_at: '2026-05-09T08:00:00Z', updated_at: '2026-05-09T08:00:00Z',
}

type Route = (url: string, method: string, body: unknown) => Response | Promise<Response> | undefined

function stubFetch(route: Route = () => undefined) {
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    const handled = route(url, method, body)
    if (handled) return Promise.resolve(handled)
    if (url === '/api/subscriptions/settings' && method === 'GET') return Promise.resolve(json(SETTINGS))
    if (url === '/api/subscription-monthly-budgets' && method === 'GET') return Promise.resolve(json([BUDGET]))
    if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') return Promise.resolve(json({ items: [] }))
    return Promise.resolve(json({ error: `unhandled ${method} ${url}` }, 404))
  })
  vi.stubGlobal('fetch', fetchMock)
  const calls = (method: string, url: string) => fetchMock.mock.calls.filter(([u, init]) => u === url && ((init as RequestInit | undefined)?.method ?? 'GET') === method)
  return { fetchMock, calls }
}

async function renderSection() {
  render(<SubscriptionSettingsSection />)
  await screen.findByRole('heading', { name: '成本基准与汇率' })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SubscriptionSettingsSection', () => {
  it('keeps the unsaved cost draft when a budget is added or rates are refreshed', async () => {
    const { calls } = stubFetch((url, method) => {
      if (url === '/api/subscription-monthly-budgets/2026-07' && method === 'PUT') return json({ ...BUDGET, budget_month: '2026-07-01' })
      if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
        return json({
          items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'missing', refresh_status: 'failed', attempt_count: 1 }],
        }, 202)
      }
      return undefined
    })
    await renderSection()
    fireEvent.change(screen.getByLabelText('最远提前天数'), { target: { value: '45' } })

    const addForm = screen.getByRole('form', { name: '新增月预算' })
    fireEvent.change(within(addForm).getByLabelText('预算月份'), { target: { value: '2026-07' } })
    fireEvent.change(within(addForm).getByLabelText('月预算 CNY'), { target: { value: '120' } })
    fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
    expect(await within(addForm).findByRole('status')).toHaveTextContent('预算已保存')
    // 只重新读取预算列表，成本设置不重新加载，草稿保持。
    await waitFor(() => expect(calls('GET', '/api/subscription-monthly-budgets')).toHaveLength(2))
    expect(calls('GET', '/api/subscriptions/settings')).toHaveLength(1)
    expect(screen.getByLabelText('最远提前天数')).toHaveValue(45)

    // 刷新有失败币种时用 alert 播报，且不重载设置。
    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('补取失败：USD')
    expect(calls('GET', '/api/subscriptions/settings')).toHaveLength(1)
    expect(screen.getByLabelText('最远提前天数')).toHaveValue(45)
  })

  it('never submits a Fixer key that is hidden after switching away from Fixer', async () => {
    const { calls } = stubFetch((url, method) => (url === '/api/subscriptions/settings' && method === 'PUT' ? json(SETTINGS) : undefined))
    await renderSection()
    fireEvent.change(screen.getByLabelText('汇率来源'), { target: { value: 'fixer' } })
    expect(screen.getByLabelText('Fixer key')).toHaveAccessibleDescription('已配置')
    fireEvent.change(screen.getByLabelText('Fixer key'), { target: { value: 'secret-key' } })
    fireEvent.change(screen.getByLabelText('汇率来源'), { target: { value: 'frankfurter' } })
    expect(screen.queryByLabelText('Fixer key')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '保存订阅配置' }))
    await waitFor(() => expect(calls('PUT', '/api/subscriptions/settings')).toHaveLength(1))
    const body = JSON.parse(String((calls('PUT', '/api/subscriptions/settings')[0]![1] as RequestInit).body))
    expect(body).not.toHaveProperty('fixer_api_key')
    expect(body.exchange_rate_provider).toBe('frankfurter')
  })

  it('edits a budget in place without changing its month and reports success on the list', async () => {
    const { calls } = stubFetch((url, method) => (url === '/api/subscription-monthly-budgets/2026-06' && method === 'PUT' ? json({ ...BUDGET, monthly_limit: 150 }) : undefined))
    await renderSection()
    fireEvent.click(screen.getByRole('button', { name: '编辑 2026-06 月预算' }))
    const editForm = screen.getByRole('form', { name: '编辑 2026-06 月预算' })
    // 月份是行身份，编辑时不可改。
    expect(within(editForm).queryByLabelText('预算月份')).not.toBeInTheDocument()
    fireEvent.change(within(editForm).getByLabelText('月预算 CNY'), { target: { value: '150' } })
    fireEvent.click(within(editForm).getByRole('button', { name: '保存预算' }))
    await waitFor(() => expect(calls('PUT', '/api/subscription-monthly-budgets/2026-06')).toHaveLength(1))
    expect(await screen.findByText('2026-06 月预算已保存')).toBeInTheDocument()
    expect(within(screen.getByRole('form', { name: '新增月预算' })).queryByRole('status')).not.toBeInTheDocument()
  })

  it.each(['success', 'failure'] as const)('keeps the newest budget list when a stale refresh returns late with %s', async (stale) => {
    const pending: Array<(response: Response) => void> = []
    let listGets = 0
    stubFetch((url, method) => {
      if (url === '/api/subscription-monthly-budgets' && method === 'GET') {
        listGets += 1
        if (listGets === 1) return json([BUDGET])
        // 写入后的列表刷新挂起，由用例决定返回顺序。
        return new Promise<Response>((resolve) => { pending.push(resolve) })
      }
      if (url.startsWith('/api/subscription-monthly-budgets/') && method === 'PUT') return json(BUDGET)
      return undefined
    })
    await renderSection()
    const addForm = screen.getByRole('form', { name: '新增月预算' })
    for (const month of ['2026-07', '2026-08']) {
      fireEvent.change(within(addForm).getByLabelText('预算月份'), { target: { value: month } })
      fireEvent.change(within(addForm).getByLabelText('月预算 CNY'), { target: { value: '120' } })
      fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
      await waitFor(() => expect(pending).toHaveLength(month === '2026-07' ? 1 : 2))
    }
    const list = (months: string[]) => months.map((month) => ({ ...BUDGET, budget_month: `${month}-01` }))
    const rows = () => within(screen.getByRole('list', { name: '月预算' })).getAllByRole('listitem')
    // 较新的刷新先返回三条；较旧的刷新随后返回（两条成功或失败）都不能把列表退回去。
    pending[1]!(json(list(['2026-08', '2026-07', '2026-06'])))
    await waitFor(() => expect(rows()).toHaveLength(3))
    pending[0]!(stale === 'success' ? json(list(['2026-07', '2026-06'])) : json({ error: '旧请求失败' }, 500))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(rows()).toHaveLength(3)
    expect(rows()[0]).toHaveTextContent('2026-08')
    expect(screen.queryByText('刷新月预算失败')).not.toBeInTheDocument()
    expect(screen.queryByText('旧请求失败')).not.toBeInTheDocument()
  })

  it('keeps the draft and error when adding a budget fails', async () => {
    const { calls } = stubFetch((url, method) => (url === '/api/subscription-monthly-budgets/2026-07' && method === 'PUT' ? json({ error: '预算服务暂不可用' }, 500) : undefined))
    await renderSection()
    const addForm = screen.getByRole('form', { name: '新增月预算' })
    fireEvent.change(within(addForm).getByLabelText('预算月份'), { target: { value: '2026-07' } })
    fireEvent.change(within(addForm).getByLabelText('月预算 CNY'), { target: { value: '120' } })
    fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
    expect(await within(addForm).findByRole('alert')).toBeInTheDocument()
    expect(within(addForm).getByLabelText('月预算 CNY')).toHaveValue(120)
    expect(calls('GET', '/api/subscription-monthly-budgets')).toHaveLength(1)
  })

  it('rechecks rate status after a settings save without posting a refresh or clearing the saved draft', async () => {
    let saved = false
    const { calls } = stubFetch((url, method) => {
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return json(saved ? {
          items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'missing', refresh_status: 'queued', attempt_count: 1 }],
        } : { items: [] })
      }
      if (url === '/api/subscriptions/settings' && method === 'PUT') {
        saved = true
        return json({ ...SETTINGS, max_reminder_lead_days: 45 })
      }
      return undefined
    })
    await renderSection()
    fireEvent.change(screen.getByLabelText('最远提前天数'), { target: { value: '45' } })
    const statusBefore = calls('GET', '/api/subscriptions/exchange-rates/status').length
    fireEvent.click(screen.getByRole('button', { name: '保存订阅配置' }))
    expect(await screen.findByText('订阅成本设置已保存')).toBeInTheDocument()
    expect(await screen.findByText('补取中 1 项')).toBeInTheDocument()
    expect(calls('GET', '/api/subscriptions/exchange-rates/status').length).toBeGreaterThan(statusBefore)
    expect(calls('POST', '/api/subscriptions/exchange-rates/refresh')).toHaveLength(0)
    expect(calls('GET', '/api/subscriptions/settings')).toHaveLength(1)
    expect(screen.getByLabelText('最远提前天数')).toHaveValue(45)
  })
})
