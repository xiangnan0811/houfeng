import { useEffect, useId, useRef, useState } from 'react'

import { Select } from '../../../components/atoms'
import { accessAdminErrorMessage, isAbortError, listMyGroups } from '../../../lib/accessApi'
import { useAuth } from '../../../lib/auth-context'
import type { AccessGroupSummary, RecordVisibility, RecordVisibilityKind, RecordVisibilityRole } from '../../../lib/types'

const ROLE_OPTIONS: readonly { value: RecordVisibilityRole; label: string; hint: string }[] = [
  {
    value: 'project_admin',
    label: '项目管理员',
    hint: '包括全部当前管理员，不能排除某一个管理员。',
  },
  {
    value: 'viewer',
    label: '只读成员',
    hint: '当前没有只读成员登录账号，勾选后仍会写入该角色。',
  },
]

type CatalogState = {
  status: 'loading' | 'ready' | 'error'
  groups: AccessGroupSummary[]
  error: string | null
}

const INITIAL_CATALOG: CatalogState = { status: 'loading', groups: [], error: null }

export function RecordVisibilityFields({
  visibility,
  onChange,
}: {
  visibility: RecordVisibility
  onChange: (visibility: RecordVisibility) => void
}) {
  const { user } = useAuth()
  const hintId = useId()
  const catalogGen = useRef(0)
  const [reloadKey, setReloadKey] = useState(0)
  const [catalog, setCatalog] = useState<CatalogState>(INITIAL_CATALOG)

  useEffect(() => {
    const generation = ++catalogGen.current
    const controller = new AbortController()
    listMyGroups(controller.signal)
      .then((groups) => {
        if (generation !== catalogGen.current) return
        setCatalog({ status: 'ready', groups, error: null })
      })
      .catch((error: unknown) => {
        if (isAbortError(error) || generation !== catalogGen.current) return
        setCatalog((current) => ({
          status: 'error',
          groups: current.groups,
          error: accessAdminErrorMessage(error) || '用户与权限服务暂不可用',
        }))
      })
    return () => {
      catalogGen.current += 1
      controller.abort()
    }
  }, [reloadKey])

  function changeKind(kind: RecordVisibilityKind) {
    if (kind === 'project') {
      onChange({ kind: 'project', allowed_roles: [], allowed_group_ids: [] })
      return
    }
    onChange({
      kind: 'restricted',
      allowed_roles: visibility.allowed_roles,
      allowed_group_ids: visibility.allowed_group_ids,
    })
  }

  function toggleRole(role: RecordVisibilityRole, selected: boolean) {
    const allowedRoles = selected
      ? [...new Set([...visibility.allowed_roles, role])]
      : visibility.allowed_roles.filter((item) => item !== role)
    onChange({
      kind: 'restricted',
      allowed_roles: allowedRoles,
      allowed_group_ids: visibility.allowed_group_ids,
    })
  }

  function toggleGroup(groupId: string, selected: boolean) {
    const allowedGroupIds = selected
      ? [...new Set([...visibility.allowed_group_ids, groupId])]
      : visibility.allowed_group_ids.filter((item) => item !== groupId)
    onChange({
      kind: 'restricted',
      allowed_roles: visibility.allowed_roles,
      allowed_group_ids: allowedGroupIds,
    })
  }

  function retryCatalog() {
    setCatalog((current) => ({ ...current, status: 'loading', error: null }))
    setReloadKey((value) => value + 1)
  }

  const roleMatch = (visibility.allowed_roles.includes('project_admin') && user?.role === 'admin')
    || (visibility.allowed_roles.includes('viewer') && user?.role === 'viewer')
  const mineIds = new Set(catalog.groups.map((group) => group.group_id))
  const groupMatch = visibility.allowed_group_ids.some((groupId) => mineIds.has(groupId))
  const unavailableIds = catalog.status === 'ready'
    ? visibility.allowed_group_ids.filter((groupId) => !mineIds.has(groupId))
    : []
  const pendingIds = catalog.status === 'ready'
    ? []
    : visibility.allowed_group_ids.filter((groupId) => !mineIds.has(groupId))
  let publishBarrier: string | null = null
  if (visibility.kind === 'restricted') {
    if (visibility.allowed_roles.length === 0 && visibility.allowed_group_ids.length === 0) {
      publishBarrier = '未选择角色或权限组，发布后无人可以查看。服务端发布时仍会最终判定。'
    } else if (!roleMatch && visibility.allowed_group_ids.length === 0) {
      publishBarrier = '当前账号不在所选角色或权限组中，发布后你将无法查看。服务端发布时仍会最终判定。'
    } else if (!roleMatch && !groupMatch && catalog.status === 'ready') {
      publishBarrier = '当前账号不在所选角色或权限组中，发布后你将无法查看。服务端发布时仍会最终判定。'
    } else if (!roleMatch && !groupMatch && catalog.status === 'loading') {
      publishBarrier = '正在确认权限组，已选组会保留。暂时无法确认你是否仍可查看。'
    } else if (!roleMatch && !groupMatch) {
      publishBarrier = '权限组目录暂不可用，已选组会保留。暂时无法确认你是否仍可查看。服务端发布时仍会最终判定。'
    }
  }

  return (
    <>
      <Select
        label="可见性"
        value={visibility.kind}
        aria-describedby={hintId}
        onChange={(event) => changeKind(event.target.value as RecordVisibilityKind)}
      >
        <option value="project">项目内</option>
        <option value="restricted">受限</option>
      </Select>
      {visibility.kind === 'restricted' ? (
        <div className="record-form-grid__wide record-visibility">
          <p className="record-visibility__note" id={hintId}>角色与权限组是或关系，满足任一即可查看。</p>
          <fieldset className="record-visibility__roles">
            <legend>角色</legend>
            {ROLE_OPTIONS.map((role) => {
              const inputId = `${hintId}-${role.value}`
              const descriptionId = `${inputId}-hint`
              return (
                <div className="record-visibility__choice" key={role.value}>
                  <label htmlFor={inputId}>
                    <input
                      id={inputId}
                      type="checkbox"
                      checked={visibility.allowed_roles.includes(role.value)}
                      aria-describedby={descriptionId}
                      onChange={(event) => toggleRole(role.value, event.target.checked)}
                    />
                    <span>{role.label}</span>
                  </label>
                  <p className="record-visibility__hint" id={descriptionId}>{role.hint}</p>
                </div>
              )
            })}
          </fieldset>
          <fieldset className="record-visibility__groups">
            <legend>我的权限组</legend>
            {catalog.status === 'loading' && catalog.groups.length === 0 ? <p className="record-visibility__hint">正在读取权限组…</p> : null}
            {catalog.status === 'error' ? (
              <p className="record-visibility__warning" role="alert">
                {catalog.error || '用户与权限服务暂不可用'}
                <button type="button" className="btn sm secondary" onClick={retryCatalog}>重试</button>
              </p>
            ) : null}
            {catalog.groups.map((group) => {
              const inputId = `${hintId}-group-${group.group_id}`
              return (
                <div className="record-visibility__choice" key={group.group_id}>
                  <label htmlFor={inputId}>
                    <input
                      id={inputId}
                      type="checkbox"
                      checked={visibility.allowed_group_ids.includes(group.group_id)}
                      onChange={(event) => toggleGroup(group.group_id, event.target.checked)}
                    />
                    <span>{group.display_name}</span>
                  </label>
                </div>
              )
            })}
            {catalog.status === 'ready' && catalog.groups.length === 0 ? <p className="record-visibility__hint">你还没有加入任何权限组。</p> : null}
          </fieldset>
          {pendingIds.length > 0 ? (
            <ul className="record-visibility__retained" aria-label="目录未确认的已选权限组">
              {pendingIds.map((groupId) => (
                <li key={groupId} className="record-visibility__missing">
                  <p>{catalog.status === 'error' ? '目录暂不可用，已保留' : '正在确认'} {groupId}</p>
                  <button type="button" className="btn sm ghost" onClick={() => toggleGroup(groupId, false)}>移除</button>
                </li>
              ))}
            </ul>
          ) : null}
          {unavailableIds.length > 0 ? (
            <ul className="record-visibility__retained" aria-label="不在本人目录中的已选权限组">
              {unavailableIds.map((groupId) => (
                <li key={groupId} className="record-visibility__missing">
                  <p>不可用 {groupId}</p>
                  <button type="button" className="btn sm ghost" onClick={() => toggleGroup(groupId, false)}>移除</button>
                </li>
              ))}
            </ul>
          ) : null}
          {publishBarrier ? <p className="record-visibility__warning" role="status">{publishBarrier}</p> : null}
        </div>
      ) : null}
    </>
  )
}
