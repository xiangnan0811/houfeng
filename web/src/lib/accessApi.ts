import allowlistedApiError from './apiError'
import {
  ApiError,
  jsonBodyInit,
  requestEmpty as transportRequestEmpty,
  requestJSON as transportRequestJSON,
  type ApiFieldError,
} from './apiRequest'
import type {
  AccessGroupSummary,
  AccessMemberSummary,
  AccessUserSummary,
  CreateAccessGroupInput,
  CreateAccessUserInput,
  RenameAccessGroupInput,
  ResetAccessUserPasswordInput,
} from './types'

const ACCESS_ERROR_COPY: Record<string, string> = {
  invalid_request: '请求无效',
  management_forbidden: '无权管理用户与权限',
  resource_not_found: '找不到该用户或权限组',
  username_taken: '用户名已存在',
  group_name_taken: '权限组名称已存在',
  supervisor_protected: '主管理员受保护，请使用自助修改密码',
  user_disabled: '该账号已停用',
  management_unavailable: '用户与权限服务暂不可用',
  unauthenticated: '未登录',
}

const STATUS_COPY: Record<number, string> = {
  400: '请求无效',
  401: '未登录',
  403: '无权管理用户与权限',
  404: '找不到该用户或权限组',
  409: '操作无法完成',
  503: '用户与权限服务暂不可用',
}

const FIELD_COPY: Record<string, string> = {
  username: '用户名无效',
  password: '密码无效',
  display_name: '显示名无效',
}

const ALLOWED_MESSAGES = new Set([
  ...Object.values(ACCESS_ERROR_COPY),
  ...Object.values(STATUS_COPY),
  ...Object.values(FIELD_COPY),
  '填写内容无效',
])

function accessAdminError(
  errorBody: unknown,
  rawBody: string,
): [message: string, details: { code?: string; field_errors?: ApiFieldError[] }] {
  const [parsedMessage, parsedDetails] = allowlistedApiError(errorBody, rawBody)
  const code = parsedDetails.code
  const fieldErrors = (parsedDetails.field_errors ?? []).map((item) => ({
    field: item.field,
    message: FIELD_COPY[item.field] ?? '填写内容无效',
  }))
  const message = (code && ACCESS_ERROR_COPY[code])
    || (fieldErrors.length > 0 ? fieldErrors.map((item) => item.message).join(' ') : '')
    || (ALLOWED_MESSAGES.has(parsedMessage) ? parsedMessage : '用户与权限服务暂不可用')
  return [message, {
    ...(code ? { code } : {}),
    ...(fieldErrors.length > 0 ? { field_errors: fieldErrors } : {}),
  }]
}

function translateAccessError(error: unknown): unknown {
  if (!(error instanceof ApiError)) return error
  if (ALLOWED_MESSAGES.has(error.message)) return error
  const message = (error.code && ACCESS_ERROR_COPY[error.code])
    || STATUS_COPY[error.status]
    || '用户与权限服务暂不可用'
  return new ApiError(error.status, message, {
    code: error.code ?? (error.status === 401 ? 'unauthenticated' : undefined),
    field_errors: error.field_errors.map((item) => ({
      field: item.field,
      message: FIELD_COPY[item.field] ?? '填写内容无效',
    })),
  })
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError'
}

export function accessAdminErrorMessage(error: unknown): string {
  if (isAbortError(error)) return ''
  if (error instanceof ApiError && ALLOWED_MESSAGES.has(error.message)) return error.message
  if (error instanceof ApiError) {
    return (error.code && ACCESS_ERROR_COPY[error.code])
      || STATUS_COPY[error.status]
      || '用户与权限服务暂不可用'
  }
  return '用户与权限服务暂不可用'
}

function malformed(): ApiError {
  return new ApiError(503, '用户与权限服务暂不可用', { code: 'management_unavailable' })
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  try {
    return await transportRequestJSON<T>(path, init, accessAdminError)
  } catch (error) {
    throw translateAccessError(error)
  }
}

async function requestEmpty(path: string, init?: RequestInit): Promise<void> {
  try {
    await transportRequestEmpty(path, init, accessAdminError)
  } catch (error) {
    throw translateAccessError(error)
  }
}

function withSignal(init: RequestInit, signal?: AbortSignal): RequestInit {
  return signal ? { ...init, signal } : init
}

function encodedPath(path: string): string {
  return encodeURIComponent(path)
}

function readRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw malformed()
  return value as Record<string, unknown>
}

function readString(value: unknown): string {
  if (typeof value !== 'string' || value.trim() === '') throw malformed()
  return value
}

function readItems(value: unknown): unknown[] {
  const record = readRecord(value)
  if (!Array.isArray(record.items)) throw malformed()
  return record.items
}

export function parseAccessUserSummary(value: unknown): AccessUserSummary {
  const record = readRecord(value)
  if (typeof record.is_supervisor !== 'boolean') throw malformed()
  if (record.disabled_at !== null && typeof record.disabled_at !== 'string') throw malformed()
  if (typeof record.created_at !== 'string' || record.created_at.trim() === '') throw malformed()
  if (typeof record.role !== 'string') throw malformed()
  if (typeof record.display_name !== 'string') throw malformed()
  return {
    user_id: readString(record.user_id),
    username: readString(record.username),
    display_name: record.display_name,
    role: record.role,
    is_supervisor: record.is_supervisor,
    disabled_at: record.disabled_at,
    created_at: record.created_at,
  }
}

export function parseAccessGroupSummary(value: unknown): AccessGroupSummary {
  const record = readRecord(value)
  if (typeof record.display_name !== 'string' || record.display_name.trim() === '') throw malformed()
  return {
    group_id: readString(record.group_id),
    display_name: record.display_name,
  }
}

export function parseAccessMemberSummary(value: unknown): AccessMemberSummary {
  const record = readRecord(value)
  if (record.missing === true) {
    const keys = Object.keys(record)
    if (keys.length !== 2 || !Object.hasOwn(record, 'user_id') || !Object.hasOwn(record, 'missing')) {
      throw malformed()
    }
    return { user_id: readString(record.user_id), missing: true }
  }
  if (Object.hasOwn(record, 'missing') && record.missing !== false) throw malformed()
  return parseAccessUserSummary(value)
}

function parseUsers(value: unknown): AccessUserSummary[] {
  return readItems(value).map((item) => parseAccessUserSummary(item))
}

function parseGroups(value: unknown): AccessGroupSummary[] {
  return readItems(value).map((item) => parseAccessGroupSummary(item))
}

function parseMembers(value: unknown): AccessMemberSummary[] {
  return readItems(value).map((item) => parseAccessMemberSummary(item))
}

export function listUsers(signal?: AbortSignal): Promise<AccessUserSummary[]> {
  return requestJSON<unknown>('/api/admin/users', withSignal({ method: 'GET' }, signal)).then(parseUsers)
}

export function createUser(input: CreateAccessUserInput, signal?: AbortSignal): Promise<AccessUserSummary> {
  const body: CreateAccessUserInput = {
    username: input.username,
    password: input.password,
    display_name: input.display_name,
  }
  return requestJSON<unknown>(
    '/api/admin/users',
    withSignal(jsonBodyInit('POST', body), signal),
  ).then(parseAccessUserSummary)
}

export function disableUser(userId: string, signal?: AbortSignal): Promise<AccessUserSummary> {
  return requestJSON<unknown>(
    `/api/admin/users/${encodedPath(userId)}/disable`,
    withSignal(jsonBodyInit('POST', {}), signal),
  ).then(parseAccessUserSummary)
}

export function enableUser(userId: string, signal?: AbortSignal): Promise<AccessUserSummary> {
  return requestJSON<unknown>(
    `/api/admin/users/${encodedPath(userId)}/enable`,
    withSignal(jsonBodyInit('POST', {}), signal),
  ).then(parseAccessUserSummary)
}

export function resetUserPassword(userId: string, password: string, signal?: AbortSignal): Promise<void> {
  const body: ResetAccessUserPasswordInput = { password }
  return requestEmpty(
    `/api/admin/users/${encodedPath(userId)}/reset-password`,
    withSignal(jsonBodyInit('POST', body), signal),
  )
}

export function listGroups(signal?: AbortSignal): Promise<AccessGroupSummary[]> {
  return requestJSON<unknown>('/api/admin/record-access-groups', withSignal({ method: 'GET' }, signal)).then(parseGroups)
}

export function createGroup(displayName: string, signal?: AbortSignal): Promise<AccessGroupSummary> {
  const body: CreateAccessGroupInput = { display_name: displayName }
  return requestJSON<unknown>(
    '/api/admin/record-access-groups',
    withSignal(jsonBodyInit('POST', body), signal),
  ).then(parseAccessGroupSummary)
}

export function renameGroup(groupId: string, displayName: string, signal?: AbortSignal): Promise<AccessGroupSummary> {
  const body: RenameAccessGroupInput = { display_name: displayName }
  return requestJSON<unknown>(
    `/api/admin/record-access-groups/${encodedPath(groupId)}`,
    withSignal(jsonBodyInit('PATCH', body), signal),
  ).then(parseAccessGroupSummary)
}

export function listMembers(groupId: string, signal?: AbortSignal): Promise<AccessMemberSummary[]> {
  return requestJSON<unknown>(
    `/api/admin/record-access-groups/${encodedPath(groupId)}/members`,
    withSignal({ method: 'GET' }, signal),
  ).then(parseMembers)
}

export function addMember(groupId: string, userId: string, signal?: AbortSignal): Promise<void> {
  return requestEmpty(
    `/api/admin/record-access-groups/${encodedPath(groupId)}/members/${encodedPath(userId)}`,
    withSignal(jsonBodyInit('PUT', {}), signal),
  )
}

export function removeMember(groupId: string, userId: string, signal?: AbortSignal): Promise<void> {
  return requestEmpty(
    `/api/admin/record-access-groups/${encodedPath(groupId)}/members/${encodedPath(userId)}`,
    withSignal({ method: 'DELETE' }, signal),
  )
}

export function listMyGroups(signal?: AbortSignal): Promise<AccessGroupSummary[]> {
  return requestJSON<unknown>('/api/record-access-groups/mine', withSignal({ method: 'GET' }, signal)).then(parseGroups)
}
