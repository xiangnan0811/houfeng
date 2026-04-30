export class AuthError extends Error {
  constructor() {
    super('unauthenticated')
    this.name = 'AuthError'
  }
}

let onUnauthorized: (() => void) | undefined

export function setUnauthorizedHandler(handler: (() => void) | undefined): void {
  onUnauthorized = handler
}

export async function fetcher<T = unknown>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: 'include', ...init })
  if (res.status === 401) {
    onUnauthorized?.()
    throw new AuthError()
  }
  if (!res.ok) {
    let detail = ''
    try {
      detail = await res.text()
    } catch {
      /* ignore */
    }
    throw new Error(`request failed (${res.status}): ${detail}`.trim())
  }
  if (res.status === 204) return undefined as unknown as T
  const ct = res.headers.get('Content-Type') ?? ''
  if (ct.includes('application/json')) return res.json() as Promise<T>
  return (await res.text()) as unknown as T
}
