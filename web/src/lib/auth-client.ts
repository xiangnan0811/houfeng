import { ApiError, jsonBodyInit, postJSONBody, requestEmpty, requestJSON } from './apiRequest'

export interface RuntimeCapabilities {
  records: boolean
  comparison: boolean
  portability: boolean
}

/** Product-management permission. Independent of login role and runtime feature flags. */
export interface ManagementCapabilities {
  access: boolean
}

export interface User {
  user_id: string
  username: string
  role: string
  display_name: string
  runtime_capabilities: RuntimeCapabilities
  management_capabilities: ManagementCapabilities
}

const CLOSED_SESSION_CAPABILITIES: RuntimeCapabilities = {
  records: false,
  comparison: false,
  portability: false,
}

/** Ready sessions use the snapshot. Loading, anonymous, and failed reads stay closed and do not reuse a retained user. */
export function capabilityFlagsForSession(session: {
  loading: boolean
  status: 'loading' | 'anonymous' | 'ready' | 'error'
  user: User | null
}): RuntimeCapabilities {
  if (session.loading || session.status !== 'ready' || session.user == null) {
    return CLOSED_SESSION_CAPABILITIES
  }
  const capabilities = session.user.runtime_capabilities
  const records = capabilities.records === true
  return {
    records,
    comparison: records && capabilities.comparison === true,
    portability: records && capabilities.portability === true,
  }
}

/** True only for a ready snapshot whose persisted management flag is boolean true. */
export function managementAccessForSession(session: {
  loading: boolean
  status: 'loading' | 'anonymous' | 'ready' | 'error'
  user: User | null
}): boolean {
  if (session.loading || session.status !== 'ready' || session.user == null) return false
  return session.user.management_capabilities.access === true
}

const CAPABILITY_READ_ERROR = '能力读取失败'

export function authSnapshotError(): Error {
  const error = new Error(CAPABILITY_READ_ERROR)
  error.name = 'AuthSnapshotError'
  return error
}

export function isAuthSnapshotError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AuthSnapshotError'
}

// The login response only acknowledges the session; the complete identity comes from /me.
// A session whose identity cannot be read is revoked so a failed login never stays signed in.
export async function login(username: string, password: string): Promise<User> {
  await postJSONBody<unknown>('/api/auth/login', { username, password })
  let user: User | null
  try {
    user = await me()
  } catch (e) {
    await logout().catch(() => undefined)
    throw e
  }
  if (!user) {
    await logout().catch(() => undefined)
    throw new Error('登录成功但无法读取当前用户')
  }
  return user
}

export async function logout(): Promise<void> {
  await requestEmpty('/api/auth/logout', { method: 'POST' })
}

export async function me(): Promise<User | null> {
  try {
    const result = await requestJSON<unknown>('/api/auth/me')
    const user = parseUser(result)
    if (!user) throw authSnapshotError()
    return user
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

function parseUser(value: unknown): User | null {
  if (typeof value !== 'object' || value === null) return null
  const raw = value as Record<string, unknown>
  if (typeof raw.user_id !== 'string' || typeof raw.username !== 'string') return null
  const capabilities = parseRuntimeCapabilities(raw.runtime_capabilities)
  const management = parseManagementCapabilities(raw.management_capabilities)
  if (!capabilities || !management) return null
  return {
    user_id: raw.user_id,
    username: raw.username,
    role: typeof raw.role === 'string' ? raw.role : '',
    display_name: typeof raw.display_name === 'string' ? raw.display_name : '',
    runtime_capabilities: capabilities,
    management_capabilities: management,
  }
}

function parseManagementCapabilities(value: unknown): ManagementCapabilities | null {
  if (typeof value !== 'object' || value === null) return null
  const raw = value as Record<string, unknown>
  if (typeof raw.access !== 'boolean') return null
  return { access: raw.access }
}

function parseRuntimeCapabilities(value: unknown): RuntimeCapabilities | null {
  if (typeof value !== 'object' || value === null) return null
  const raw = value as Record<string, unknown>
  if (typeof raw.records !== 'boolean' || typeof raw.comparison !== 'boolean' || typeof raw.portability !== 'boolean') {
    return null
  }
  if (!raw.records) return { records: false, comparison: false, portability: false }
  return { records: true, comparison: raw.comparison, portability: raw.portability }
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await requestEmpty(
    '/api/auth/password',
    jsonBodyInit('PUT', { old_password: oldPassword, new_password: newPassword }),
  )
}
