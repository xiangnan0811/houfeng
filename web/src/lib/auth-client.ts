import { ApiError, jsonBodyInit, postJSONBody, requestEmpty, requestJSON } from './apiRequest'

export interface User {
  user_id: string
  username: string
  role: string
  display_name: string
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
    if (!isUser(result)) return null
    return result
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

function isUser(v: unknown): v is User {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as User).user_id === 'string' &&
    typeof (v as User).username === 'string'
  )
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await requestEmpty(
    '/api/auth/password',
    jsonBodyInit('PUT', { old_password: oldPassword, new_password: newPassword }),
  )
}
