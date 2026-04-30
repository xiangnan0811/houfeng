import { fetcher, AuthError } from './fetcher'

export interface User {
  user_id: string
  username: string
  role: string
  display_name: string
}

export async function login(username: string, password: string): Promise<User> {
  return fetcher<User>('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

export async function logout(): Promise<void> {
  await fetcher<void>('/api/auth/logout', { method: 'POST' })
}

export async function me(): Promise<User | null> {
  try {
    const result = await fetcher<unknown>('/api/auth/me')
    if (!isUser(result)) return null
    return result
  } catch (e) {
    if (e instanceof AuthError) return null
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
  await fetcher<void>('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
  })
}
