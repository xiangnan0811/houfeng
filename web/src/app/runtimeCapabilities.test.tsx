import { act, fireEvent, render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AuthProvider } from '../lib/auth-context'
import { dashboardOverviewFixture } from '../pages/dashboard/dashboardTestFixtures'
import { recordDetailFixture } from '../pages/records/testFixtures'
import { appRoutes } from './router'

const RECORDS_FETCH = /\/api\/(?:records\/search|subjects\/|record-notifications|record-export|record-imports|evidence\/comparison)/

type MeMode = 'user' | 'anonymous' | 'error' | 'hang'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function userBody(capabilities: { records: boolean; comparison: boolean; portability: boolean }) {
  return {
    user_id: 'u1',
    username: 'admin',
    role: 'admin',
    display_name: '管理员',
    runtime_capabilities: capabilities,
    management_capabilities: { access: false },
  }
}

function installFetch(me: { mode: MeMode; body?: ReturnType<typeof userBody> }) {
  const urls: string[] = []
  let releaseHang: ((response: Response) => void) | undefined
  const hang = new Promise<Response>((resolve) => {
    releaseHang = resolve
  })
  let meCalls = 0
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    urls.push(url)
    const path = url.split('?')[0] ?? url
    if (path === '/api/auth/me') {
      meCalls += 1
      if (me.mode === 'hang' && meCalls === 1) return hang
      if (me.mode === 'error' && meCalls === 1) return Promise.resolve(jsonResponse({ error: 'unavailable' }, 503))
      if (me.mode === 'anonymous') return Promise.resolve(jsonResponse({ error: 'unauthenticated' }, 401))
      return Promise.resolve(jsonResponse(me.body ?? userBody({ records: true, comparison: true, portability: true })))
    }
    if (path === '/api/dashboard') return Promise.resolve(jsonResponse(dashboardOverviewFixture()))
    if (path === '/api/vps') return Promise.resolve(jsonResponse([]))
    if (path === '/api/subscriptions/overview') return Promise.resolve(jsonResponse({}))
    if (path === '/api/records/search') {
      return Promise.resolve(jsonResponse({ items: [recordDetailFixture()], generation: 1 }))
    }
    if (path === '/api/record-notifications/unread-count') {
      return Promise.resolve(jsonResponse({ unread_count: 0 }))
    }
    if (path === '/api/command-audits') return Promise.resolve(jsonResponse({ items: [] }))
    if (path.startsWith('/api/asset-decisions') || path.startsWith('/api/subscriptions')) {
      return Promise.resolve(jsonResponse({ error: 'missing' }, 404))
    }
    return Promise.resolve(jsonResponse({ error: 'missing' }, 404))
  }))
  return {
    urls,
    releaseHang: (response: Response) => releaseHang?.(response),
  }
}

function renderRoute(path: string) {
  const router = createMemoryRouter(appRoutes, { initialEntries: [path] })
  return render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  )
}

describe('runtime capability route gates', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('does not fetch records surfaces when records are off', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: false, comparison: true, portability: true }),
    })
    renderRoute('/records')

    expect(await screen.findByRole('heading', { name: '记录平台未启用' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '运维记录' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '命令审计' })).toBeInTheDocument()
    expect(fetchLog.urls.some((url) => RECORDS_FETCH.test(url))).toBe(false)
  })

  it('does not fetch subject activity when records are off', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: false, comparison: false, portability: false }),
    })
    renderRoute('/vps/vps_001/activity')

    expect(await screen.findByRole('heading', { name: '记录平台未启用' })).toBeInTheDocument()
    expect(fetchLog.urls.some((url) => url.includes('/api/subjects/'))).toBe(false)
  })

  it('keeps search and hides comparison and portability producers together', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: true, comparison: false, portability: false }),
    })
    renderRoute('/records')

    expect(await screen.findByRole('heading', { name: '运维记录' })).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: 'Database outage' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '草稿' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '新建记录' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '横向比较' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '导入' })).not.toBeInTheDocument()
    expect(screen.queryByText('导出选中记录')).not.toBeInTheDocument()
    expect(fetchLog.urls.some((url) => url.includes('/api/records/search'))).toBe(true)
    expect(fetchLog.urls.some((url) => /\/api\/(?:record-export|record-imports|evidence\/comparison)/.test(url))).toBe(false)
  })

  it('blocks the compare page without calling comparison APIs', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: true, comparison: false, portability: true }),
    })
    renderRoute('/records/compare')

    expect(await screen.findByRole('heading', { name: '比较功能关闭' })).toBeInTheDocument()
    expect(fetchLog.urls.some((url) => url.includes('/api/evidence/comparison'))).toBe(false)
  })

  it('still mounts asset decisions when records are off', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: false, comparison: false, portability: false }),
    })
    renderRoute('/asset-decisions')

    expect(await screen.findByRole('heading', { name: '资产组合决策' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '记录平台未启用' })).not.toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '决策组不可用' })).toBeInTheDocument()
    expect(fetchLog.urls.some((url) => url.includes('/api/records/search'))).toBe(false)
  })

  it('still mounts monitoring compare when records are off', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: false, comparison: false, portability: false }),
    })
    renderRoute('/monitoring/compare')

    expect(await screen.findByRole('heading', { name: '需要选择 2 个监控实例' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '记录平台未启用' })).not.toBeInTheDocument()
    expect(fetchLog.urls.some((url) => RECORDS_FETCH.test(url))).toBe(false)
  })

  it('still mounts command audit when records are off', async () => {
    const fetchLog = installFetch({
      mode: 'user',
      body: userBody({ records: false, comparison: false, portability: false }),
    })
    renderRoute('/command-audit')

    expect(await screen.findByRole('heading', { name: '命令审计' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '记录平台未启用' })).not.toBeInTheDocument()
    expect(fetchLog.urls.some((url) => url.includes('/api/command-audits'))).toBe(true)
    expect(fetchLog.urls.some((url) => url.includes('/api/records/search'))).toBe(false)
  })

  it('keeps a failed capability read retryable instead of signing out', async () => {
    installFetch({ mode: 'error' })
    renderRoute('/')

    expect(await screen.findByRole('heading', { name: '能力读取错误' })).toBeInTheDocument()
    expect(screen.queryByLabelText('用户名')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('link', { name: '工作台' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '能力读取错误' })).not.toBeInTheDocument()
  })

  it('treats an actual 401 as signed out', async () => {
    installFetch({ mode: 'anonymous' })
    renderRoute('/records')

    expect(await screen.findByLabelText('用户名')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '能力读取错误' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '记录平台未启用' })).not.toBeInTheDocument()
  })

  it('does not treat a pending capability read as disabled or signed out', async () => {
    const fetchLog = installFetch({ mode: 'hang' })
    renderRoute('/records')

    expect(screen.queryByRole('heading', { name: '记录平台未启用' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '能力读取错误' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('用户名')).not.toBeInTheDocument()

    await act(async () => {
      fetchLog.releaseHang(jsonResponse({ error: 'unauthenticated' }, 401))
    })
    expect(await screen.findByLabelText('用户名')).toBeInTheDocument()
  })
})
