import type { Page, Request, Route } from '@playwright/test'

import { BrowserDiagnostics } from '../support/diagnostics'
import {
  apiRouteKey,
  canonicalApiPath,
  type ApiFixtureProfile,
  type ApiFixtureResponse,
  type ApiMethod,
  type ApiRouteKey,
} from './contracts'

function sortedKeys(value: unknown): string[] | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  return Object.keys(value).sort()
}

export class ApiFixtureController {
  private profile: ApiFixtureProfile = {}
  private readonly requests: ApiRouteKey[] = []
  private readonly unexpectedRequests: string[] = []
  private readonly page: Page
  private readonly diagnostics: BrowserDiagnostics

  constructor(
    page: Page,
    diagnostics: BrowserDiagnostics,
  ) {
    this.page = page
    this.diagnostics = diagnostics
  }

  async install(): Promise<void> {
    await this.page.route('**/api/**', async (route) => this.handle(route))
  }

  useProfile(profile: ApiFixtureProfile): void {
    this.profile = profile
  }

  requestCount(method: ApiMethod, path: string): number {
    const key = apiRouteKey(method, path)
    return this.requests.filter((requestKey) => requestKey === key).length
  }

  assertNoUnexpectedRequests(): void {
    if (this.unexpectedRequests.length > 0) {
      throw new Error(
        `unexpected API requests:\n${this.unexpectedRequests.join('\n')}`,
      )
    }
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const method = request.method() as ApiMethod
    const path = canonicalApiPath(request.url())
    const key = apiRouteKey(method, path)
    this.requests.push(key)
    const fixture = this.profile[key]
    if (!fixture) {
      this.unexpectedRequests.push(key)
      await route.fulfill({
        status: 501,
        contentType: 'application/json',
        body: JSON.stringify({ error: `unexpected fixture request: ${key}` }),
      })
      return
    }

    if (method !== 'GET' && !this.requestBodyMatches(request, fixture, key)) {
      await route.fulfill({
        status: 422,
        contentType: 'application/json',
        body: JSON.stringify({ error: `fixture request body mismatch: ${key}` }),
      })
      return
    }
    if (fixture.status >= 400) {
      this.diagnostics.allowHttpError(method, path, fixture.status)
    }
    if (fixture.delayMs !== undefined) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, fixture.delayMs))
    }
    if (fixture.waitFor) await fixture.waitFor
    await route.fulfill({
      status: fixture.status,
      headers: {
        'Content-Type': 'application/json',
        ...fixture.headers,
      },
      body: fixture.status === 204 ? '' : JSON.stringify(fixture.body),
    })
  }

  private requestBodyMatches(
    request: Request,
    fixture: ApiFixtureResponse,
    key: string,
  ): boolean {
    if (!fixture.expectedBodyKeys) {
      this.unexpectedRequests.push(`${key} must declare expectedBodyKeys`)
      return false
    }
    let body: unknown
    try {
      body = request.postDataJSON()
    } catch {
      this.unexpectedRequests.push(`${key} body must be valid JSON`)
      return false
    }
    const actualKeys = sortedKeys(body)
    const expectedKeys = [...fixture.expectedBodyKeys].sort()
    if (!actualKeys || JSON.stringify(actualKeys) !== JSON.stringify(expectedKeys)) {
      this.unexpectedRequests.push(
        `${key} body keys expected ${expectedKeys.join(',')} received ${actualKeys?.join(',') ?? 'non-object'}`,
      )
      return false
    }
    return true
  }
}
