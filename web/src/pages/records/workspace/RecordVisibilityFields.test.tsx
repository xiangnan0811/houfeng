import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { User } from '../../../lib/auth-client'
import * as authContext from '../../../lib/auth-context'
import type { RecordVisibility } from '../../../lib/types'
import { RecordVisibilityFields } from './RecordVisibilityFields'

function session(role: string): User {
  return {
    user_id: 'u1',
    username: 'admin',
    role,
    display_name: '管理员',
    runtime_capabilities: { records: true, comparison: true, portability: true },
    management_capabilities: { access: false },
  }
}

function mockSession(role = 'admin') {
  vi.spyOn(authContext, 'useAuth').mockReturnValue({
    user: session(role),
    loading: false,
    status: 'ready',
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    retry: vi.fn(),
  })
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function Harness({
  initial,
  onChange,
}: {
  initial: RecordVisibility
  onChange: (visibility: RecordVisibility) => void
}) {
  const [visibility, setVisibility] = useState(initial)
  return (
    <>
      <div data-testid="draft">{JSON.stringify(visibility)}</div>
      <RecordVisibilityFields
        visibility={visibility}
        onChange={(next) => {
          onChange(next)
          setVisibility(next)
        }}
      />
    </>
  )
}

const restricted: RecordVisibility = {
  kind: 'restricted',
  allowed_roles: ['project_admin', 'viewer'],
  allowed_group_ids: ['rag_a', 'rag_kept'],
}

describe('RecordVisibilityFields', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('clears residual grants when switching to project and does not preselect roles for a new restriction', async () => {
    mockSession()
    const onChange = vi.fn()
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({
      items: [
        { group_id: 'rag_a', display_name: '值班组' },
        { group_id: 'rag_b', display_name: '网络组' },
      ],
    }))))

    const { rerender } = render(<Harness initial={restricted} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('可见性'), { target: { value: 'project' } })

    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenLastCalledWith({ kind: 'project', allowed_roles: [], allowed_group_ids: [] })
    expect(screen.queryByRole('checkbox', { name: '项目管理员' })).not.toBeInTheDocument()

    rerender(<Harness initial={{ kind: 'project', allowed_roles: [], allowed_group_ids: [] }} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('可见性'), { target: { value: 'restricted' } })
    expect(onChange).toHaveBeenLastCalledWith({ kind: 'restricted', allowed_roles: [], allowed_group_ids: [] })
    expect(screen.getByRole('checkbox', { name: '项目管理员' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: '只读成员' })).not.toBeChecked()
    expect(screen.getByTestId('draft')).toHaveTextContent('"allowed_roles":[]')
    expect(screen.getByTestId('draft')).toHaveTextContent('"allowed_group_ids":[]')
  })

  it('selects only the named group that was checked', async () => {
    mockSession()
    const onChange = vi.fn()
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      expect(url).toBe('/api/record-access-groups/mine')
      return Promise.resolve(jsonResponse({
        items: [
          { group_id: 'rag_a', display_name: '值班组' },
          { group_id: 'rag_b', display_name: '网络组' },
        ],
      }))
    }))

    render(<Harness initial={{ kind: 'restricted', allowed_roles: [], allowed_group_ids: [] }} onChange={onChange} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: '网络组' }))

    expect(onChange).toHaveBeenLastCalledWith({
      kind: 'restricted',
      allowed_roles: [],
      allowed_group_ids: ['rag_b'],
    })
    expect(screen.getByRole('checkbox', { name: '值班组' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: '网络组' })).toBeChecked()
    expect(vi.mocked(fetch).mock.calls.every(([url]) => url === '/api/record-access-groups/mine')).toBe(true)
  })

  it('retains selected groups when the catalog fails and only marks them unavailable after a successful catalog', async () => {
    mockSession()
    const onChange = vi.fn()
    let calls = 0
    vi.stubGlobal('fetch', vi.fn(() => {
      calls += 1
      if (calls === 1) {
        return Promise.resolve(jsonResponse({
          error: 'select secret from record_access_groups',
          code: 'management_unavailable',
        }, 503))
      }
      return Promise.resolve(jsonResponse({ items: [] }))
    }))

    render(<Harness initial={{ kind: 'restricted', allowed_roles: [], allowed_group_ids: ['rag_kept'] }} onChange={onChange} />)

    const retained = await screen.findByRole('list', { name: '目录未确认的已选权限组' })
    expect(retained).toHaveTextContent('rag_kept')
    expect(screen.queryByRole('list', { name: '不在本人目录中的已选权限组' })).not.toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('select secret')
    expect(screen.getByTestId('draft')).toHaveTextContent('rag_kept')
    expect(onChange).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    const unavailable = await screen.findByRole('list', { name: '不在本人目录中的已选权限组' })
    expect(unavailable).toHaveTextContent('rag_kept')
    expect(screen.queryByRole('list', { name: '目录未确认的已选权限组' })).not.toBeInTheDocument()
    expect(screen.getByTestId('draft')).toHaveTextContent('rag_kept')
    expect(onChange).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '移除' }))
    expect(onChange).toHaveBeenCalledWith({ kind: 'restricted', allowed_roles: [], allowed_group_ids: [] })
    await waitFor(() => expect(screen.getByTestId('draft')).not.toHaveTextContent('rag_kept'))
  })

  it('explains when the current account cannot publish and clears that when the admin role matches', async () => {
    mockSession('admin')
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({ items: [] }))))
    const onChange = vi.fn()
    render(<Harness initial={{ kind: 'restricted', allowed_roles: [], allowed_group_ids: [] }} onChange={onChange} />)

    expect(await screen.findByRole('status')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: '只读成员' }))
    expect(screen.getByRole('checkbox', { name: '只读成员' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: '项目管理员' })).not.toBeChecked()
    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByTestId('draft')).toHaveTextContent('"allowed_roles":["viewer"]')
    fireEvent.click(screen.getByRole('checkbox', { name: '项目管理员' }))
    expect(screen.getByRole('checkbox', { name: '项目管理员' })).toBeChecked()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByTestId('draft')).toHaveTextContent('"allowed_roles":["viewer","project_admin"]')
  })
})
