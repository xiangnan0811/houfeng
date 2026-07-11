export type ApiMethod = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
export type ApiRouteKey = `${ApiMethod} ${string}`

export type ApiFixtureResponse = {
  status: number
  body: unknown
  delayMs?: number
  waitFor?: Promise<void>
  headers?: Readonly<Record<string, string>>
  expectedBodyKeys?: readonly string[]
}

export type ApiFixtureProfile = Readonly<Record<ApiRouteKey, ApiFixtureResponse>>

export function canonicalApiPath(rawUrl: string): string {
  const url = new URL(rawUrl, 'http://fixture.invalid')
  const entries = [...url.searchParams.entries()]
    .sort(([leftKey, leftValue], [rightKey, rightValue]) => (
      leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue)
    ))
  const query = new URLSearchParams(entries).toString()
  return `${url.pathname}${query ? `?${query}` : ''}`
}

export function apiRouteKey(method: ApiMethod, path: string): ApiRouteKey {
  return `${method} ${canonicalApiPath(path)}`
}
