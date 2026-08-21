import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { Page, Response } from '@playwright/test'

import { canonicalApiPath, type ApiMethod } from '../fixtures/contracts'

export const EXPECTED_CSP = readFileSync(
  resolve(import.meta.dirname, '../../..', 'internal/center/http/csp-policy.txt'),
  'utf8',
).trim()

type BrowserSideDiagnostic = {
  kind: 'csp' | 'unhandledrejection'
  message: string
}

declare global {
  interface Window {
    __houfengE2EDiagnostics?: BrowserSideDiagnostic[]
  }
}

export class BrowserDiagnostics {
  private readonly diagnostics: string[] = []
  private readonly allowedHttpErrors = new Set<string>()
  private readonly allowedConsoleResourceErrors = new Set<string>()
  private readonly allowedRequestFailures = new Set<string>()

  async install(page: Page): Promise<void> {
    page.on('console', (message) => {
      if (message.type() !== 'error') return
      const statusMatch = message.text().match(
        /^Failed to load resource: the server responded with a status of (\d+) \([^)]+\)$/,
      )
      const location = message.location().url
      if (statusMatch && location) {
        const status = Number(statusMatch[1])
        const key = this.consoleResourceErrorKey(canonicalApiPath(location), status)
        if (this.allowedConsoleResourceErrors.has(key)) return
      }
      this.diagnostics.push(
        `console.error: ${message.text()}${location ? ` (${canonicalApiPath(location)})` : ''}`,
      )
    })
    page.on('pageerror', (error) => {
      this.diagnostics.push(`pageerror: ${error.name}: ${error.message}`)
    })
    page.on('requestfailed', (request) => {
      const failure = request.failure()?.errorText ?? 'unknown'
      const key = this.requestFailureKey(
        request.method() as ApiMethod,
        canonicalApiPath(request.url()),
        failure,
      )
      if (this.allowedRequestFailures.has(key)) return
      this.diagnostics.push(
        `requestfailed: ${request.method()} ${canonicalApiPath(request.url())} ${failure}`,
      )
    })
    page.on('response', (response) => {
      if (response.status() < 400) return
      const request = response.request()
      const key = this.httpErrorKey(
        request.method() as ApiMethod,
        canonicalApiPath(request.url()),
        response.status(),
      )
      if (!this.allowedHttpErrors.has(key)) {
        this.diagnostics.push(`http: ${key}`)
      }
    })
    await page.addInitScript(() => {
      window.__houfengE2EDiagnostics = []
      window.addEventListener('securitypolicyviolation', (event) => {
        window.__houfengE2EDiagnostics?.push({
          kind: 'csp',
          message: `${event.effectiveDirective} ${event.blockedURI || 'inline'} ${event.disposition}`,
        })
      })
      window.addEventListener('unhandledrejection', (event) => {
        const reason = event.reason
        const message = reason instanceof Error
          ? `${reason.name}: ${reason.message}`
          : String(reason)
        window.__houfengE2EDiagnostics?.push({ kind: 'unhandledrejection', message })
      })
    })
  }

  allowHttpError(method: ApiMethod, path: string, status: number): void {
    const canonicalPath = canonicalApiPath(path)
    this.allowedHttpErrors.add(this.httpErrorKey(method, canonicalPath, status))
    this.allowedConsoleResourceErrors.add(
      this.consoleResourceErrorKey(canonicalPath, status),
    )
  }

  allowRequestFailure(method: ApiMethod, path: string, errorText = 'net::ERR_ABORTED'): void {
    this.allowedRequestFailures.add(
      this.requestFailureKey(method, canonicalApiPath(path), errorText),
    )
  }

  async assertClean(page: Page): Promise<void> {
    const browserDiagnostics = await page.evaluate(
      () => window.__houfengE2EDiagnostics ?? [],
    )
    const failures = [
      ...this.diagnostics,
      ...browserDiagnostics.map((entry) => `${entry.kind}: ${entry.message}`),
    ]
    if (failures.length > 0) {
      throw new Error(`browser diagnostics were not clean:\n${failures.join('\n')}`)
    }
  }

  private httpErrorKey(method: ApiMethod, path: string, status: number): string {
    return `${method} ${path} ${status}`
  }

  private requestFailureKey(method: ApiMethod, path: string, errorText: string): string {
    return `${method} ${path} ${errorText}`
  }

  private consoleResourceErrorKey(path: string, status: number): string {
    return `${path} ${status}`
  }
}

export function expectMainDocumentCsp(response: Response | null): void {
  if (!response) throw new Error('navigation did not return a main document response')
  const actual = response.headers()['content-security-policy']
  if (actual !== EXPECTED_CSP) {
    throw new Error(`main document CSP mismatch: expected ${EXPECTED_CSP}, received ${actual ?? 'missing'}`)
  }
}
