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
    return await fetcher<User>('/api/auth/me')
  } catch (e) {
    if (e instanceof AuthError) return null
    throw e
  }
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await fetcher<void>('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
  })
}
