import { type FormEvent, useEffect, useId, useRef, useState } from 'react'

import { ChangePasswordModal } from '../../app/layout/ChangePasswordModal'
import { Badge, DataTable, Input, Modal, ScrollRegion, Select } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import {
  accessAdminErrorMessage,
  addMember,
  createGroup,
  createUser,
  disableUser,
  enableUser,
  isAbortError,
  listGroups,
  listMembers,
  listUsers,
  removeMember,
  renameGroup,
  resetUserPassword,
} from '../../lib/accessApi'
import { formatDateTime } from '../../lib/format'
import type { AccessGroupSummary, AccessMemberSummary, AccessUserSummary } from '../../lib/types'
import './AccessManagementSection.css'

type ResourcePhase = 'loading' | 'ready' | 'error'

type UserListState = {
  phase: ResourcePhase
  items: AccessUserSummary[]
  error: string | null
}

type GroupListState = {
  phase: ResourcePhase
  items: AccessGroupSummary[]
  error: string | null
}

type MemberListState = {
  groupId: string | null
  phase: 'idle' | ResourcePhase
  items: AccessMemberSummary[]
  error: string | null
}

type AccountDialog =
  | { kind: 'disable' | 'enable' | 'reset'; user: AccessUserSummary }
  | { kind: 'remove-member'; member: AccessMemberSummary }
  | null

const INITIAL_USERS: UserListState = { phase: 'loading', items: [], error: null }
const INITIAL_GROUPS: GroupListState = { phase: 'loading', items: [], error: null }
const INITIAL_MEMBERS: MemberListState = { groupId: null, phase: 'idle', items: [], error: null }

function isMissingMember(member: AccessMemberSummary): member is { user_id: string; missing: true } {
  return 'missing' in member && member.missing === true
}

function compareText(left: string, right: string): number {
  return left.localeCompare(right, 'zh')
}

function byUser(left: AccessUserSummary, right: AccessUserSummary): number {
  return compareText(left.username, right.username) || compareText(left.user_id, right.user_id)
}

function byGroup(left: AccessGroupSummary, right: AccessGroupSummary): number {
  return compareText(left.display_name, right.display_name) || compareText(left.group_id, right.group_id)
}

function byMember(left: AccessMemberSummary, right: AccessMemberSummary): number {
  if (isMissingMember(left) || isMissingMember(right)) {
    if (isMissingMember(left) && isMissingMember(right)) return compareText(left.user_id, right.user_id)
    return isMissingMember(left) ? 1 : -1
  }
  return byUser(left, right)
}

function upsertUser(items: AccessUserSummary[], user: AccessUserSummary): AccessUserSummary[] {
  const exists = items.some((item) => item.user_id === user.user_id)
  const next = exists ? items.map((item) => (item.user_id === user.user_id ? user : item)) : [...items, user]
  return next.sort(byUser)
}

function upsertGroup(items: AccessGroupSummary[], group: AccessGroupSummary): AccessGroupSummary[] {
  const exists = items.some((item) => item.group_id === group.group_id)
  const next = exists ? items.map((item) => (item.group_id === group.group_id ? group : item)) : [...items, group]
  return next.sort(byGroup)
}

export function AccessManagementSection() {
  const usersHeadingId = useId()
  const groupsHeadingId = useId()
  const membersHeadingId = useId()
  const usersHintId = useId()
  const membersHintId = useId()
  const writeLock = useRef(false)
  const usersGen = useRef(0)
  const groupsGen = useRef(0)
  const membersGen = useRef(0)
  const selectedGroupRef = useRef<string | null>(null)

  const [users, setUsers] = useState<UserListState>(INITIAL_USERS)
  const [groups, setGroups] = useState<GroupListState>(INITIAL_GROUPS)
  const [members, setMembers] = useState<MemberListState>(INITIAL_MEMBERS)
  const [usersReload, setUsersReload] = useState(0)
  const [groupsReload, setGroupsReload] = useState(0)
  const [membersReload, setMembersReload] = useState(0)
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null)
  const [writing, setWriting] = useState(false)
  const [dialog, setDialog] = useState<AccountDialog>(null)
  const [dialogError, setDialogError] = useState<string | null>(null)
  const [selfPasswordOpen, setSelfPasswordOpen] = useState(false)

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)

  const [groupName, setGroupName] = useState('')
  const [groupError, setGroupError] = useState<string | null>(null)
  const [renameDrafts, setRenameDrafts] = useState<Record<string, string>>({})
  const [renameError, setRenameError] = useState<string | null>(null)

  const [memberToAdd, setMemberToAdd] = useState('')
  const [memberError, setMemberError] = useState<string | null>(null)
  const [resetPassword, setResetPassword] = useState('')
  const [resetPasswordAgain, setResetPasswordAgain] = useState('')

  function reloadUsers() {
    setUsers((current) => ({
      ...current,
      phase: current.items.length === 0 ? 'loading' : current.phase,
      error: null,
    }))
    setUsersReload((value) => value + 1)
  }

  function reloadGroups() {
    setGroups((current) => ({
      ...current,
      phase: current.items.length === 0 ? 'loading' : current.phase,
      error: null,
    }))
    setGroupsReload((value) => value + 1)
  }

  function reloadMembers(groupId: string) {
    setMembers((current) => ({
      groupId,
      phase: 'loading',
      items: current.groupId === groupId ? current.items : [],
      error: null,
    }))
    setMembersReload((value) => value + 1)
  }

  useEffect(() => {
    const generation = ++usersGen.current
    const controller = new AbortController()
    listUsers(controller.signal)
      .then((items) => {
        if (generation !== usersGen.current) return
        setUsers({ phase: 'ready', items: items.slice().sort(byUser), error: null })
      })
      .catch((error: unknown) => {
        if (isAbortError(error) || generation !== usersGen.current) return
        setUsers((current) => ({
          phase: current.items.length === 0 ? 'error' : 'ready',
          items: current.items,
          error: accessAdminErrorMessage(error),
        }))
      })
    return () => {
      usersGen.current += 1
      controller.abort()
    }
  }, [usersReload])

  useEffect(() => {
    const generation = ++groupsGen.current
    const controller = new AbortController()
    listGroups(controller.signal)
      .then((items) => {
        if (generation !== groupsGen.current) return
        setGroups({ phase: 'ready', items: items.slice().sort(byGroup), error: null })
      })
      .catch((error: unknown) => {
        if (isAbortError(error) || generation !== groupsGen.current) return
        setGroups((current) => ({
          phase: current.items.length === 0 ? 'error' : 'ready',
          items: current.items,
          error: accessAdminErrorMessage(error),
        }))
      })
    return () => {
      groupsGen.current += 1
      controller.abort()
    }
  }, [groupsReload])

  useEffect(() => {
    if (!selectedGroupId) return
    const groupId = selectedGroupId
    const generation = ++membersGen.current
    const controller = new AbortController()
    listMembers(groupId, controller.signal)
      .then((items) => {
        if (generation !== membersGen.current) return
        setMembers({ groupId, phase: 'ready', items: items.slice().sort(byMember), error: null })
      })
      .catch((error: unknown) => {
        if (isAbortError(error) || generation !== membersGen.current) return
        setMembers((current) => ({
          groupId,
          phase: current.groupId === groupId && current.items.length > 0 ? 'ready' : 'error',
          items: current.groupId === groupId ? current.items : [],
          error: accessAdminErrorMessage(error),
        }))
      })
    return () => {
      membersGen.current += 1
      controller.abort()
    }
  }, [membersReload, selectedGroupId])

  function beginWrite(): boolean {
    if (writeLock.current) return false
    writeLock.current = true
    setWriting(true)
    return true
  }

  function endWrite() {
    writeLock.current = false
    setWriting(false)
  }

  function clearResetSecret() {
    setResetPassword('')
    setResetPasswordAgain('')
  }

  function requestDismiss() {
    if (writeLock.current) return
    setDialog(null)
    setDialogError(null)
    clearResetSecret()
  }

  function selectGroup(groupId: string) {
    if (writeLock.current || groupId === selectedGroupId) return
    setMemberError(null)
    setRenameError(null)
    setMemberToAdd('')
    selectedGroupRef.current = groupId
    setSelectedGroupId(groupId)
    setMembers({ groupId, phase: 'loading', items: [], error: null })
  }

  async function submitUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextUsername = username.trim()
    const nextDisplay = displayName.trim()
    if (nextUsername.length < 1 || nextUsername.length > 64) {
      setCreateError('用户名无效')
      return
    }
    if (password.length < 8 || password.length > 256) {
      setCreateError('密码无效')
      return
    }
    if (nextDisplay.length > 100) {
      setCreateError('显示名无效')
      return
    }
    if (!beginWrite()) return
    const submitted = { username, password, displayName }
    setCreateError(null)
    try {
      const created = await createUser({
        username: nextUsername,
        password,
        display_name: nextDisplay || nextUsername,
      })
      setUsers((current) => ({ ...current, phase: 'ready', items: upsertUser(current.items, created), error: null }))
      reloadUsers()
      setUsername((current) => (current === submitted.username ? '' : current))
      setPassword((current) => (current === submitted.password ? '' : current))
      setDisplayName((current) => (current === submitted.displayName ? '' : current))
    } catch (error) {
      if (!isAbortError(error)) setCreateError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  async function submitGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextName = groupName.trim()
    if (nextName.length < 1 || nextName.length > 100) {
      setGroupError('显示名无效')
      return
    }
    if (!beginWrite()) return
    const submitted = groupName
    setGroupError(null)
    try {
      const created = await createGroup(nextName)
      setGroups((current) => ({ ...current, phase: 'ready', items: upsertGroup(current.items, created), error: null }))
      reloadGroups()
      setGroupName((current) => (current === submitted ? '' : current))
    } catch (error) {
      if (!isAbortError(error)) setGroupError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  async function submitRename(event: FormEvent<HTMLFormElement>, group: AccessGroupSummary) {
    event.preventDefault()
    const draft = renameDrafts[group.group_id] ?? group.display_name
    const nextName = draft.trim()
    if (nextName.length < 1 || nextName.length > 100) {
      setRenameError('显示名无效')
      return
    }
    if (nextName === group.display_name) return
    if (!beginWrite()) return
    setRenameError(null)
    try {
      const renamed = await renameGroup(group.group_id, nextName)
      setGroups((current) => ({ ...current, items: upsertGroup(current.items, renamed) }))
      reloadGroups()
      setRenameDrafts((current) => {
        const next = { ...current }
        delete next[group.group_id]
        return next
      })
    } catch (error) {
      if (!isAbortError(error)) setRenameError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  async function confirmAccount() {
    if (!dialog || dialog.kind === 'remove-member') return
    if (dialog.kind === 'reset') {
      if (resetPassword.length < 8 || resetPassword.length > 256) {
        setDialogError('密码无效')
        return
      }
      if (resetPassword !== resetPasswordAgain) {
        setDialogError('两次输入不一致')
        return
      }
    }
    if (!beginWrite()) return
    setDialogError(null)
    try {
      if (dialog.kind === 'disable') {
        const updated = await disableUser(dialog.user.user_id)
        setUsers((current) => ({ ...current, items: upsertUser(current.items, updated) }))
      } else if (dialog.kind === 'enable') {
        const updated = await enableUser(dialog.user.user_id)
        setUsers((current) => ({ ...current, items: upsertUser(current.items, updated) }))
      } else {
        await resetUserPassword(dialog.user.user_id, resetPassword)
      }
      reloadUsers()
      clearResetSecret()
      setDialog(null)
    } catch (error) {
      if (!isAbortError(error)) setDialogError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  async function confirmRemove(group: AccessGroupSummary) {
    if (!dialog || dialog.kind !== 'remove-member') return
    if (!beginWrite()) return
    setDialogError(null)
    const userId = dialog.member.user_id
    try {
      await removeMember(group.group_id, userId)
      if (selectedGroupRef.current === group.group_id) {
        setMembers((current) => current.groupId === group.group_id
          ? { ...current, items: current.items.filter((item) => item.user_id !== userId) }
          : current)
        reloadMembers(group.group_id)
      }
      setDialog(null)
    } catch (error) {
      if (!isAbortError(error)) setDialogError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  async function submitAddMember(group: AccessGroupSummary) {
    if (!memberToAdd) return
    if (!beginWrite()) return
    const userId = memberToAdd
    setMemberError(null)
    try {
      await addMember(group.group_id, userId)
      const account = users.items.find((item) => item.user_id === userId)
      if (account && selectedGroupRef.current === group.group_id) {
        setMembers((current) => {
          if (current.groupId !== group.group_id) return current
          if (current.items.some((item) => item.user_id === userId)) return current
          return { ...current, phase: 'ready', items: [...current.items, account].sort(byMember) }
        })
      }
      if (selectedGroupRef.current === group.group_id) reloadMembers(group.group_id)
      setMemberToAdd('')
    } catch (error) {
      if (!isAbortError(error)) setMemberError(accessAdminErrorMessage(error))
    } finally {
      endWrite()
    }
  }

  const selectedGroup = groups.items.find((group) => group.group_id === selectedGroupId) ?? null
  const visibleMembers = members.groupId === selectedGroupId ? members : { ...INITIAL_MEMBERS, groupId: selectedGroupId, phase: selectedGroupId ? 'loading' as const : 'idle' as const }
  const joinedIds = new Set(visibleMembers.items.map((item) => item.user_id))
  const candidates = users.items.filter((user) => user.disabled_at == null && !joinedIds.has(user.user_id)).sort(byUser)

  return (
    <div className="access-management">
      <div className="access-management__layout">
        <div className="access-management__column">
          <section className="settings-section" aria-labelledby={usersHeadingId}>
            <h2 className="ss-title" id={usersHeadingId}>账号</h2>
            <p className="ss-desc">创建普通管理员，或停用、启用、重置密码。不能删除账号，也不能改用户名。</p>
            <form className="access-form" onSubmit={(event) => void submitUser(event)}>
              <div className="access-form__grid">
                <Input label="用户名" autoComplete="off" value={username} disabled={writing} onChange={(event) => setUsername(event.target.value)} />
                <Input label="显示名" autoComplete="off" value={displayName} disabled={writing} hint="留空则与用户名相同" onChange={(event) => setDisplayName(event.target.value)} />
                <div className="access-form__wide">
                  <Input label="密码" type="password" autoComplete="new-password" value={password} disabled={writing} hint="至少 8 个字符" onChange={(event) => setPassword(event.target.value)} />
                </div>
              </div>
              {createError ? <p className="access-inline-error" role="alert">{createError}</p> : null}
              <div>
                <button className="btn md primary" type="submit" disabled={writing}>创建账号</button>
              </div>
            </form>
            <UserDirectory
              state={users}
              headingId={usersHeadingId}
              hintId={usersHintId}
              writing={writing}
              onRetry={reloadUsers}
              onDisable={(user) => { setDialogError(null); setDialog({ kind: 'disable', user }) }}
              onEnable={(user) => { setDialogError(null); setDialog({ kind: 'enable', user }) }}
              onReset={(user) => { setDialogError(null); clearResetSecret(); setDialog({ kind: 'reset', user }) }}
              onSelfPassword={() => setSelfPasswordOpen(true)}
            />
          </section>
        </div>

        <div className="access-management__column">
          <section className="settings-section" aria-labelledby={groupsHeadingId}>
            <h2 className="ss-title" id={groupsHeadingId}>权限组</h2>
            <p className="ss-desc">组可以新建和改名。历史修订仍引用组 ID，因此不能删除组。</p>
            <form className="access-form" onSubmit={(event) => void submitGroup(event)}>
              <Input label="新权限组名称" autoComplete="off" value={groupName} disabled={writing} onChange={(event) => setGroupName(event.target.value)} />
              {groupError ? <p className="access-inline-error" role="alert">{groupError}</p> : null}
              <div>
                <button className="btn md primary" type="submit" disabled={writing}>创建权限组</button>
              </div>
            </form>
            <GroupDirectory
              state={groups}
              writing={writing}
              selectedGroupId={selectedGroupId}
              onRetry={reloadGroups}
              onSelect={selectGroup}
            />
          </section>

          <section className="settings-section" aria-labelledby={membersHeadingId}>
            <h2 className="ss-title" id={membersHeadingId}>成员</h2>
            {selectedGroup ? (
              <>
                <p className="access-note">当前组：{selectedGroup.display_name}</p>
                <form className="access-form" onSubmit={(event) => void submitRename(event, selectedGroup)}>
                  <Input
                    label="组名称"
                    autoComplete="off"
                    value={renameDrafts[selectedGroup.group_id] ?? selectedGroup.display_name}
                    disabled={writing}
                    onChange={(event) => setRenameDrafts((current) => ({ ...current, [selectedGroup.group_id]: event.target.value }))}
                  />
                  {renameError ? <p className="access-inline-error" role="alert">{renameError}</p> : null}
                  <div>
                    <button className="btn sm secondary" type="submit" disabled={writing}>重命名</button>
                  </div>
                </form>
                <div className="access-member-add">
                  <Select
                    label="添加成员"
                    value={memberToAdd}
                    disabled={writing || visibleMembers.phase === 'loading' || candidates.length === 0}
                    onChange={(event) => setMemberToAdd(event.target.value)}
                  >
                    <option value="">{candidates.length === 0 ? '没有可添加的账号' : '选择要添加的账号'}</option>
                    {candidates.map((user) => (
                      <option key={user.user_id} value={user.user_id}>{user.display_name || user.username}（{user.username}）</option>
                    ))}
                  </Select>
                  <button className="btn sm primary" type="button" disabled={writing || visibleMembers.phase === 'loading' || !memberToAdd} onClick={() => void submitAddMember(selectedGroup)}>添加</button>
                </div>
                {memberError ? <p className="access-inline-error" role="alert">{memberError}</p> : null}
                <MemberDirectory
                  state={visibleMembers}
                  headingId={membersHeadingId}
                  hintId={membersHintId}
                  writing={writing}
                  onRetry={() => reloadMembers(selectedGroup.group_id)}
                  onRemove={(member) => { setDialogError(null); setDialog({ kind: 'remove-member', member }) }}
                />
              </>
            ) : (
              <PageState kind="empty" title="先选择一个权限组" description="成员按组单独读取，切换组后不会沿用上一组的名单。" surface="empty" compact />
            )}
          </section>
        </div>
      </div>

      <Modal
        open={dialog?.kind === 'disable' || dialog?.kind === 'enable'}
        onClose={requestDismiss}
        persistent={writing}
        dialogRole="alertdialog"
        title={dialog?.kind === 'enable' ? '启用账号' : '停用账号'}
        footer={dialog && dialog.kind !== 'reset' && dialog.kind !== 'remove-member' ? (
          <>
            <button type="button" className="btn md ghost" onClick={requestDismiss} disabled={writing}>取消</button>
            <button type="button" className="btn md primary" onClick={() => void confirmAccount()} disabled={writing}>
              {writing ? '正在提交…' : dialog.kind === 'enable' ? '确认启用' : '确认停用'}
            </button>
          </>
        ) : null}
      >
        {dialog && dialog.kind !== 'reset' && dialog.kind !== 'remove-member' ? (
          <>
            <p>
              {dialog.kind === 'disable'
                ? `确认停用账号“${dialog.user.username}”？该账号将无法登录，现有会话会立即失效。`
                : `确认重新启用账号“${dialog.user.username}”？需要使用密码重新登录，原会话不会恢复。`}
            </p>
            {dialogError ? <p className="access-inline-error" role="alert">{dialogError}</p> : null}
          </>
        ) : null}
      </Modal>

      <Modal
        open={dialog?.kind === 'reset'}
        onClose={requestDismiss}
        persistent={writing}
        title="重置密码"
        footer={dialog?.kind === 'reset' ? (
          <>
            <button type="button" className="btn md ghost" onClick={requestDismiss} disabled={writing}>取消</button>
            <button type="button" className="btn md primary" onClick={() => void confirmAccount()} disabled={writing}>
              {writing ? '正在提交…' : '确认重置'}
            </button>
          </>
        ) : null}
      >
        {dialog?.kind === 'reset' ? (
          <form className="access-form" onSubmit={(event) => { event.preventDefault(); void confirmAccount() }}>
            <p className="access-note">为“{dialog.user.username}”设置新密码。该账号需要用新密码重新登录，这不会自动启用已停用的账号。</p>
            <Input label="新密码" type="password" autoComplete="new-password" value={resetPassword} disabled={writing} onChange={(event) => setResetPassword(event.target.value)} />
            <Input label="再输入一次" type="password" autoComplete="new-password" value={resetPasswordAgain} disabled={writing} onChange={(event) => setResetPasswordAgain(event.target.value)} />
            {dialogError ? <p className="access-inline-error" role="alert">{dialogError}</p> : null}
          </form>
        ) : null}
      </Modal>

      <Modal
        open={dialog?.kind === 'remove-member'}
        onClose={requestDismiss}
        persistent={writing}
        dialogRole="alertdialog"
        title="移出成员"
        footer={dialog?.kind === 'remove-member' && selectedGroup ? (
          <>
            <button type="button" className="btn md ghost" onClick={requestDismiss} disabled={writing}>取消</button>
            <button type="button" className="btn md primary" onClick={() => void confirmRemove(selectedGroup)} disabled={writing}>
              {writing ? '正在提交…' : '确认移出'}
            </button>
          </>
        ) : null}
      >
        {dialog?.kind === 'remove-member' && selectedGroup ? (
          <>
            <p>
              {isMissingMember(dialog.member)
                ? `确认将账号不可用（${dialog.member.user_id}）移出权限组“${selectedGroup.display_name}”？`
                : `确认将“${dialog.member.username}”移出权限组“${selectedGroup.display_name}”？`}
            </p>
            {dialogError ? <p className="access-inline-error" role="alert">{dialogError}</p> : null}
          </>
        ) : null}
      </Modal>

      {selfPasswordOpen ? <ChangePasswordModal onClose={() => setSelfPasswordOpen(false)} /> : null}
    </div>
  )
}

function UserDirectory({
  state,
  headingId,
  hintId,
  writing,
  onRetry,
  onDisable,
  onEnable,
  onReset,
  onSelfPassword,
}: {
  state: UserListState
  headingId: string
  hintId: string
  writing: boolean
  onRetry: () => void
  onDisable: (user: AccessUserSummary) => void
  onEnable: (user: AccessUserSummary) => void
  onReset: (user: AccessUserSummary) => void
  onSelfPassword: () => void
}) {
  if (state.phase === 'loading' && state.items.length === 0) {
    return <PageState kind="loading" title="正在读取账号" surface="empty" compact />
  }
  if (state.phase === 'error' && state.items.length === 0) {
    return (
      <PageState
        kind="error"
        title="账号列表不可用"
        description={state.error ?? '用户与权限服务暂不可用'}
        action={<button type="button" className="btn sm secondary" onClick={onRetry}>重试</button>}
        surface="empty"
        compact
      />
    )
  }
  if (state.items.length === 0) {
    return <PageState kind="empty" title="还没有其他账号" description="创建一个普通管理员后会出现在这里。" surface="empty" compact />
  }
  return (
    <>
      {state.error ? (
        <p className="access-inline-error" role="alert">
          {state.error} <button type="button" className="btn sm ghost" onClick={onRetry}>重试</button>
        </p>
      ) : null}
      <ScrollRegion labelledBy={headingId} hintId={hintId} hint="横向滚动查看完整列" hintClassName="access-note" className="access-table-scroll">
        <DataTable
          density="compact"
          rows={state.items}
          rowKey={(user) => user.user_id}
          columns={[
            {
              key: 'account',
              label: '账号',
              render: (user) => (
                <span className="access-account">
                  <strong>{user.display_name || user.username}</strong>
                  <span>{user.username}</span>
                </span>
              ),
            },
            {
              key: 'role',
              label: '身份',
              render: (user) => (
                <span className="access-row-actions">
                  <span>{user.role === 'admin' ? '管理员' : user.role}</span>
                  {user.is_supervisor ? <Badge tone="notice">主管理员</Badge> : null}
                </span>
              ),
            },
            {
              key: 'status',
              label: '状态',
              render: (user) => user.disabled_at
                ? <Badge tone="alert">已停用 {formatDateTime(user.disabled_at)}</Badge>
                : <Badge tone="normal">启用</Badge>,
            },
            {
              key: 'actions',
              label: '操作',
              render: (user) => user.is_supervisor ? (
                <span className="access-protected">
                  <span className="access-note">主管理员不可停用</span>
                  <button type="button" className="btn sm secondary" onClick={onSelfPassword} disabled={writing}>修改自己的密码</button>
                </span>
              ) : (
                <span className="access-row-actions">
                  {user.disabled_at ? (
                    <button type="button" className="btn sm secondary" onClick={() => onEnable(user)} disabled={writing}>启用</button>
                  ) : (
                    <button type="button" className="btn sm secondary" onClick={() => onDisable(user)} disabled={writing}>停用</button>
                  )}
                  <button type="button" className="btn sm secondary" onClick={() => onReset(user)} disabled={writing}>重置密码</button>
                </span>
              ),
            },
          ]}
        />
      </ScrollRegion>
    </>
  )
}

function GroupDirectory({
  state,
  writing,
  selectedGroupId,
  onRetry,
  onSelect,
}: {
  state: GroupListState
  writing: boolean
  selectedGroupId: string | null
  onRetry: () => void
  onSelect: (groupId: string) => void
}) {
  if (state.phase === 'loading' && state.items.length === 0) {
    return <PageState kind="loading" title="正在读取权限组" surface="empty" compact />
  }
  if (state.phase === 'error' && state.items.length === 0) {
    return (
      <PageState
        kind="error"
        title="权限组列表不可用"
        description={state.error ?? '用户与权限服务暂不可用'}
        action={<button type="button" className="btn sm secondary" onClick={onRetry}>重试</button>}
        surface="empty"
        compact
      />
    )
  }
  if (state.items.length === 0) {
    return <PageState kind="empty" title="还没有权限组" description="新建一个组后，才能把账号加进去。" surface="empty" compact />
  }
  return (
    <>
      {state.error ? (
        <p className="access-inline-error" role="alert">
          {state.error} <button type="button" className="btn sm ghost" onClick={onRetry}>重试</button>
        </p>
      ) : null}
      <ul className="access-group-list">
        {state.items.map((group) => (
          <li key={group.group_id}>
            <button
              type="button"
              className="access-group-button"
              aria-pressed={group.group_id === selectedGroupId}
              aria-label={group.display_name}
              disabled={writing}
              onClick={() => onSelect(group.group_id)}
            >
              <strong>{group.display_name}</strong>
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}

function MemberDirectory({
  state,
  headingId,
  hintId,
  writing,
  onRetry,
  onRemove,
}: {
  state: MemberListState
  headingId: string
  hintId: string
  writing: boolean
  onRetry: () => void
  onRemove: (member: AccessMemberSummary) => void
}) {
  if (state.phase === 'loading' && state.items.length === 0) {
    return <PageState kind="loading" title="正在读取成员" surface="empty" compact />
  }
  if (state.phase === 'error' && state.items.length === 0) {
    return (
      <PageState
        kind="error"
        title="成员列表不可用"
        description={state.error ?? '用户与权限服务暂不可用'}
        action={<button type="button" className="btn sm secondary" onClick={onRetry}>重试</button>}
        surface="empty"
        compact
      />
    )
  }
  if (state.phase === 'idle' || state.items.length === 0) {
    return <PageState kind="empty" title="这个组还没有成员" description="从上面的账号里添加。停用账号不会出现在可选名单中。" surface="empty" compact />
  }
  return (
    <>
      {state.error ? (
        <p className="access-inline-error" role="alert">
          {state.error} <button type="button" className="btn sm ghost" onClick={onRetry}>重试</button>
        </p>
      ) : null}
      <ScrollRegion labelledBy={headingId} hintId={hintId} hint="横向滚动查看完整列" hintClassName="access-note" className="access-table-scroll">
        <DataTable
          density="compact"
          rows={state.items}
          rowKey={(member) => member.user_id}
          columns={[
            {
              key: 'member',
              label: '成员',
              render: (member) => isMissingMember(member) ? (
                <span className="access-account">
                  <strong>账号不可用</strong>
                  <span>{member.user_id}</span>
                </span>
              ) : (
                <span className="access-account">
                  <strong>{member.display_name || member.username}</strong>
                  <span>{member.username}</span>
                </span>
              ),
            },
            {
              key: 'action',
              label: '操作',
              render: (member) => (
                <button type="button" className="btn sm secondary" disabled={writing} onClick={() => onRemove(member)}>
                  移出
                </button>
              ),
            },
          ]}
        />
      </ScrollRegion>
    </>
  )
}
