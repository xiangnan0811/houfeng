import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AccessGroupSummary, AccessMemberSummary, AccessUserSummary } from '../../lib/types'
import { AccessManagementSection } from './AccessManagementSection'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function account(overrides: Partial<AccessUserSummary> = {}): AccessUserSummary {
  return {
    user_id: 'usr_ops',
    username: 'ops',
    display_name: '值班',
    role: 'admin',
    is_supervisor: false,
    disabled_at: null,
    created_at: '2026-01-02T00:00:00Z',
    ...overrides,
  }
}

const supervisor = account({
  user_id: 'usr_root',
  username: 'root',
  display_name: '主管理员',
  is_supervisor: true,
})
const operator = account()
const disabledAccount = account({
  user_id: 'usr_old',
  username: 'old-ops',
  display_name: '已停用账号',
  disabled_at: '2026-02-02T00:00:00Z',
})
const joinedAccount = account({
  user_id: 'usr_joined',
  username: 'joined-ops',
  display_name: '已加入',
})
const eligibleAccount = account({
  user_id: 'usr_new',
  username: 'new-ops',
  display_name: '新账号',
})
const dutyGroup: AccessGroupSummary = { group_id: 'rag_duty', display_name: '值班组' }
const networkGroup: AccessGroupSummary = { group_id: 'rag_net', display_name: '网络组' }
const createPassword = 'c4e8b1a09f6d2e73'
const resetPasswordValue = 'e7c1a4b90d2f6a85'

type Directory = {
  users?: AccessUserSummary[]
  groups?: AccessGroupSummary[]
  members?: Record<string, AccessMemberSummary[] | 'error' | 'hang'>
  usersError?: boolean
}

function installDirectory(directory: Directory = {}) {
  const users = directory.users ?? [supervisor, operator]
  const groups = directory.groups ?? []
  const members = directory.members ?? {}
  const pending = new Map<string, Array<(response: Response) => void>>()
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url.endsWith('/members') && method === 'GET') {
      const groupId = url.split('/').at(-2) ?? ''
      const listed = members[groupId]
      if (listed === 'hang') {
        return new Promise<Response>((resolve) => {
          const queue = pending.get(url) ?? []
          queue.push(resolve)
          pending.set(url, queue)
        })
      }
      if (listed === 'error') {
        return Promise.resolve(jsonResponse({ error: 'select password from users where secret = true' }, 503))
      }
      return Promise.resolve(jsonResponse({ items: listed ?? [] }))
    }
    if (url === '/api/admin/users' && method === 'GET') {
      if (directory.usersError) {
        return Promise.resolve(jsonResponse({ error: 'select password from users where secret = true' }, 503))
      }
      return Promise.resolve(jsonResponse({ items: users }))
    }
    if (url === '/api/admin/record-access-groups' && method === 'GET') {
      return Promise.resolve(jsonResponse({ items: groups }))
    }
    return Promise.resolve(jsonResponse({ error: `unexpected ${method} ${url}` }, 500))
  })
  vi.stubGlobal('fetch', fetchMock)
  return {
    fetchMock,
    release(url: string, body: unknown) {
      const queue = pending.get(url)
      const resolve = queue?.shift()
      if (!resolve) throw new Error(`no pending response for ${url}`)
      resolve(jsonResponse(body))
    },
  }
}

async function openGroup(name: string) {
  fireEvent.click(await screen.findByRole('button', { name }))
}

describe('AccessManagementSection', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('keeps only the latest group members when an older response resolves after switching', async () => {
    const alpha: AccessMemberSummary = account({
      user_id: 'usr_alpha',
      username: 'alpha-member',
      display_name: '甲组账号',
    })
    const beta: AccessMemberSummary = account({
      user_id: 'usr_beta',
      username: 'beta-member',
      display_name: '乙组账号',
    })
    const directory = installDirectory({
      users: [supervisor],
      groups: [dutyGroup, networkGroup],
      members: { rag_duty: 'hang', rag_net: 'hang' },
    })

    render(<AccessManagementSection />)
    await openGroup('值班组')
    await openGroup('网络组')
    directory.release('/api/admin/record-access-groups/rag_net/members', { items: [beta] })

    const members = await screen.findByRole('region', { name: '成员' })
    expect(await within(members).findByText('beta-member')).toBeInTheDocument()
    directory.release('/api/admin/record-access-groups/rag_duty/members', { items: [alpha] })
    await waitFor(() => expect(within(members).queryByText('alpha-member')).not.toBeInTheDocument())
    expect(within(members).getByText('beta-member')).toBeInTheDocument()
  })

  it('keeps the create draft and hides the database error when the username is taken', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/admin/users' && method === 'GET') return Promise.resolve(jsonResponse({ items: [operator] }))
      if (url === '/api/admin/record-access-groups' && method === 'GET') return Promise.resolve(jsonResponse({ items: [] }))
      if (url === '/api/admin/users' && method === 'POST') {
        return Promise.resolve(jsonResponse({
          error: 'duplicate key value violates unique constraint users_username_key',
          code: 'username_taken',
        }, 409))
      }
      return Promise.resolve(jsonResponse({ error: `unexpected ${method} ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<AccessManagementSection />)

    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'ops' } })
    fireEvent.change(screen.getByLabelText('显示名'), { target: { value: '值班员' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: createPassword } })
    fireEvent.click(screen.getByRole('button', { name: '创建账号' }))

    const alert = await screen.findByRole('alert')
    expect(alert.textContent ?? '').not.toMatch(/duplicate key|users_username_key/i)
    expect(screen.getByLabelText('用户名')).toHaveValue('ops')
    expect(screen.getByLabelText('显示名')).toHaveValue('值班员')
    expect(screen.getByLabelText('密码')).toHaveValue(createPassword)
    expect(document.body.textContent).not.toContain(createPassword)
    expect(document.body.textContent).not.toContain('duplicate key')
  })

  it('clears the password after creation and does not render a password echoed by the server', async () => {
    let created: AccessUserSummary | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/admin/users' && method === 'GET') {
        return Promise.resolve(jsonResponse({ items: created ? [operator, created] : [operator] }))
      }
      if (url === '/api/admin/record-access-groups' && method === 'GET') return Promise.resolve(jsonResponse({ items: [] }))
      if (url === '/api/admin/users' && method === 'POST') {
        const body = JSON.parse(String(init?.body)) as { username: string; password: string; display_name: string }
        created = account({ user_id: 'usr_created', username: body.username, display_name: body.display_name })
        return Promise.resolve(jsonResponse({ ...created, password: body.password }, 201))
      }
      return Promise.resolve(jsonResponse({ error: `unexpected ${method} ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<AccessManagementSection />)

    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: ' created-ops ' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: createPassword } })
    fireEvent.click(screen.getByRole('button', { name: '创建账号' }))

    await waitFor(() => expect(screen.getByLabelText('密码')).toHaveValue(''))
    expect(screen.getByLabelText('用户名')).toHaveValue('')
    const post = fetchMock.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'POST')
    expect(JSON.parse(String(post?.[1] && (post[1] as RequestInit).body))).toEqual({
      username: 'created-ops',
      password: createPassword,
      display_name: 'created-ops',
    })
    expect(document.body.textContent).not.toContain(createPassword)
    const createdRow = await screen.findByRole('row', { name: /created-ops/ })
    expect(within(createdRow).getByText('created-ops', { selector: 'span' })).toBeInTheDocument()
  })

  it('clears a reset password when the dialog closes', async () => {
    installDirectory()
    render(<AccessManagementSection />)

    const row = await screen.findByRole('row', { name: /ops/ })
    fireEvent.click(within(row).getByRole('button', { name: '重置密码' }))
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: resetPasswordValue } })
    fireEvent.change(screen.getByLabelText('再输入一次'), { target: { value: resetPasswordValue } })
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    expect(screen.queryByLabelText('新密码')).not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(resetPasswordValue)
    fireEvent.click(within(row).getByRole('button', { name: '重置密码' }))
    expect(screen.getByLabelText('新密码')).toHaveValue('')
    expect(screen.getByLabelText('再输入一次')).toHaveValue('')
  })

  it('protects the supervisor and opens the self-service password dialog', async () => {
    installDirectory()
    render(<AccessManagementSection />)

    const row = await screen.findByRole('row', { name: /root/ })
    expect(within(row).queryByRole('button', { name: '停用' })).not.toBeInTheDocument()
    expect(within(row).queryByRole('button', { name: '重置密码' })).not.toBeInTheDocument()
    fireEvent.click(within(row).getByRole('button', { name: '修改自己的密码' }))
    expect(await screen.findByRole('dialog', { name: '修改密码' })).toBeInTheDocument()
    expect(screen.getByLabelText('当前密码')).toBeInTheDocument()
  })

  it('blocks a second disable and the close button while the request is pending', async () => {
    let release: (response: Response) => void = () => {}
    const disablePosts: unknown[] = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/admin/users' && method === 'GET') return Promise.resolve(jsonResponse({ items: [operator] }))
      if (url === '/api/admin/record-access-groups' && method === 'GET') return Promise.resolve(jsonResponse({ items: [] }))
      if (url === `/api/admin/users/${operator.user_id}/disable` && method === 'POST') {
        disablePosts.push(JSON.parse(String(init?.body)))
        return new Promise<Response>((resolve) => {
          release = resolve
        })
      }
      return Promise.resolve(jsonResponse({ error: `unexpected ${method} ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<AccessManagementSection />)

    fireEvent.click(await screen.findByRole('button', { name: '停用' }))
    expect(screen.getByRole('button', { name: '确认停用' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认停用' }))
    const pending = await screen.findByRole('button', { name: '正在提交…' })
    expect(pending).toBeDisabled()
    fireEvent.click(pending)
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.getByRole('alertdialog', { name: '停用账号' })).toBeInTheDocument()
    expect(disablePosts).toEqual([{}])

    release(jsonResponse({ ...operator, disabled_at: '2026-03-03T00:00:00Z' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: '停用账号' })).not.toBeInTheDocument())
    expect(disablePosts).toEqual([{}])
  })

  it('offers only enabled accounts that are not already in the selected group', async () => {
    installDirectory({
      users: [eligibleAccount, disabledAccount, joinedAccount],
      groups: [dutyGroup],
      members: { rag_duty: [joinedAccount] },
    })
    render(<AccessManagementSection />)
    await openGroup('值班组')

    const select = await screen.findByLabelText('添加成员')
    await waitFor(() => expect(select).toBeEnabled())
    const names = within(select).getAllByRole('option').map((option) => option.textContent)
    expect(names).toContain('新账号（new-ops）')
    expect(names.join(' ')).not.toContain('old-ops')
    expect(names.join(' ')).not.toContain('joined-ops')
  })

  it('confirms removal with the group name and username, and names a dangling account', async () => {
    const missing: AccessMemberSummary = { user_id: 'usr_missing', missing: true }
    installDirectory({
      users: [joinedAccount],
      groups: [dutyGroup],
      members: { rag_duty: [joinedAccount, missing] },
    })
    render(<AccessManagementSection />)
    await openGroup('值班组')

    const members = await screen.findByRole('region', { name: '成员' })
    const joinedRow = await within(members).findByRole('row', { name: /joined-ops/ })
    const missingRow = within(members).getByRole('row', { name: /usr_missing/ })
    fireEvent.click(within(joinedRow).getByRole('button', { name: '移出' }))
    const joinedDialog = screen.getByRole('alertdialog', { name: '移出成员' })
    expect(joinedDialog).toHaveTextContent('joined-ops')
    expect(joinedDialog).toHaveTextContent('值班组')
    expect(joinedDialog).not.toHaveTextContent('usr_missing')
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    fireEvent.click(within(missingRow).getByRole('button', { name: '移出' }))
    const missingDialog = screen.getByRole('alertdialog', { name: '移出成员' })
    expect(missingDialog).toHaveTextContent('usr_missing')
    expect(missingDialog).toHaveTextContent('值班组')
    expect(missingDialog).not.toHaveTextContent('joined-ops')
  })

  it('keeps a failed account list from replacing a successful group list', async () => {
    installDirectory({ usersError: true, groups: [dutyGroup] })
    render(<AccessManagementSection />)

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(screen.getByRole('button', { name: '值班组' })).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('select password')
  })
})
