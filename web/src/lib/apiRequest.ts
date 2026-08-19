export type ApiFieldError = {
  field: string
  message: string
}

export type ApiErrorDetails<TRecovery> = {
  code?: string | undefined
  field_errors?: ApiFieldError[]
  recovery?: TRecovery | undefined
}

export type ApiErrorDecoder = (
  errorBody: unknown,
  rawBody: string,
) => [message: string, details: ApiErrorDetails<unknown>]

export class ApiError<TRecovery = unknown> extends Error {
  declare status: number
  declare code: string | undefined
  declare field_errors: ApiFieldError[]
  declare recovery: TRecovery | undefined

  constructor(status: number, message: string, details?: ApiErrorDetails<TRecovery>) {
    super(message)
    Object.assign(this, { name: 'ApiError', status, field_errors: [] }, details)
  }
}

export type QueryValue = string | number | boolean | null | undefined

export function withQuery(
  path: string,
  filter?: Record<string, QueryValue | readonly QueryValue[]>,
): string {
  if (!filter) return path

  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filter)) {
    // An array repeats its key, which is how the search endpoints read an
    // OR-ed filter. Scalars keep the single-value form.
    for (const item of Array.isArray(value) ? value : [value]) {
      if (item == null || item === false) continue
      const normalized = String(item).trim()
      if (!normalized) continue
      query.append(key, normalized)
    }
  }

  return query.size ? `${path}?${query}` : path
}

export function jsonBodyInit(
  method: string,
  body: unknown,
  headers?: Record<string, string>,
): RequestInit {
  return {
    method,
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...headers,
    },
    body: JSON.stringify(body),
  }
}

let onUnauthorized: (() => void) | undefined

export function setUnauthorizedHandler(handler: (() => void) | undefined): void {
  onUnauthorized = handler
}

async function request(
  path: string,
  init?: RequestInit,
  decodeError?: ApiErrorDecoder,
): Promise<Response> {
  const response = await fetch(path, {
    headers: { Accept: 'application/json' },
    cache: 'no-store',
    credentials: 'include',
    ...init,
  })
  if (response.status === 401) {
    onUnauthorized?.()
    throw new ApiError(401, 'unauthenticated')
  }
  if (!response.ok) {
    const rawBody = await response.text()
    let message = `Request failed: ${response.status}`
    let details: ApiErrorDetails<unknown> | undefined
    if (rawBody.trim()) {
      try {
        const errorBody = JSON.parse(rawBody) as { error?: string; message?: string }
        if (decodeError) {
          ;[message, details] = decodeError(errorBody, rawBody)
        } else {
          message = errorBody.error ?? errorBody.message ?? rawBody
        }
      } catch {
        message = rawBody
      }
    }
    throw new ApiError(response.status, message, details)
  }
  return response
}

export async function requestJSON<T>(
  path: string,
  init?: RequestInit,
  decodeError?: ApiErrorDecoder,
): Promise<T> {
  const response = await request(path, init, decodeError)
  return JSON.parse(await response.text()) as T
}

export async function requestEmpty(
  path: string,
  init?: RequestInit,
  decodeError?: ApiErrorDecoder,
): Promise<void> {
  await request(path, init, decodeError)
}

export async function requestBlob(
  path: string,
  init?: RequestInit,
  decodeError?: ApiErrorDecoder,
): Promise<Blob> {
  const response = await request(path, init, decodeError)
  return response.blob()
}

export async function requestExternalEmpty(path: string, init: RequestInit): Promise<void> {
  const response = await fetch(path, {
    cache: 'no-store',
    credentials: 'omit',
    ...init,
  })
  if (!response.ok) {
    throw new ApiError(response.status, `Request failed: ${response.status}`)
  }
}

export function postJSON<T>(path: string): Promise<T> {
  return requestJSON<T>(path, { method: 'POST' })
}

export function postJSONBody<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, jsonBodyInit('POST', body))
}
