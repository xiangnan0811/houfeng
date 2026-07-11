import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import type { ConsoleMessage, Page, Request, Response } from '@playwright/test'

import { EXPECTED_CSP } from '../support/diagnostics'

type AuditNetworkEntry = {
  method: string
  path: string
  status: number
  resourceType: string
  durationMs: number | null
}

type BrowserSideDiagnostic = {
  kind: 'csp' | 'unhandledrejection'
  message: string
}

type AuditLane = 'real-environment' | 'deployed-frontend-injection'

type AuditStep = {
  lane: AuditLane
  name: string
  outcome: 'passed' | 'failed'
}

type AuditDocumentEntry = {
  path: string
  status: number
  headers: Record<string, string>
}

type AuditManifest = {
  schemaVersion: 1
  run: {
    id: string
    url: string
    commit: string
    expectedVersion: string
    observedVersion: string
    browserVersion: string
  }
  conclusion: 'passed' | 'failed'
  failureCategory: string | null
  evidence: {
    routes: string[]
    viewports: string[]
    screenshots: string[]
    documents: AuditDocumentEntry[]
    network: AuditNetworkEntry[]
    steps: AuditStep[]
    counters: ReturnType<StagingAudit['counters']>
  }
}

declare global {
  interface Window {
    __houfengStagingDiagnostics?: BrowserSideDiagnostic[]
  }
}

const HEADER_ALLOWLIST = [
  'cache-control',
  'content-security-policy',
  'content-type',
  'permissions-policy',
  'referrer-policy',
  'strict-transport-security',
  'x-content-type-options',
  'x-frame-options',
] as const

function sanitizePath(rawURL: string): string {
  const url = new URL(rawURL, 'http://staging.invalid')
  const keys = [...new Set(url.searchParams.keys())].sort()
  return `${url.pathname}${keys.length > 0 ? `?${keys.map((key) => `${key}=<redacted>`).join('&')}` : ''}`
}

function sanitizeMessage(message: string): string {
  return message
    .replace(/https?:\/\/[^\s"')]+/g, (value) => sanitizePath(value))
    .replace(/(username|password|token|authorization|cookie)=?[^\s,;]*/gi, '$1=<redacted>')
    .slice(0, 400)
}

function resourceConsoleStatus(message: ConsoleMessage): number | null {
  const match = message.text().match(
    /^Failed to load resource: the server responded with a status of (\d+) \([^)]+\)$/,
  )
  return match ? Number(match[1]) : null
}

function requestDuration(request: Request): number | null {
  const timing = request.timing()
  return timing.responseEnd >= 0 ? Math.round(timing.responseEnd) : null
}

export class StagingAudit {
  private readonly consoleErrors: string[] = []
  private readonly pageErrors: string[] = []
  private readonly requestFailures: string[] = []
  private readonly httpErrors: string[] = []
  private readonly expectedHttpErrors: string[] = []
  private readonly allowedHttpErrors = new Set<string>()
  private readonly network: AuditNetworkEntry[] = []
  private readonly routes = new Set<string>()
  private readonly viewports = new Set<string>()
  private readonly screenshots: string[] = []
  private readonly documents: AuditDocumentEntry[] = []
  private readonly steps: AuditStep[] = []
  private readonly browserDiagnostics: BrowserSideDiagnostic[] = []

  async install(page: Page): Promise<void> {
    page.on('console', (message) => {
      if (message.type() !== 'error') return
      const status = resourceConsoleStatus(message)
      const location = message.location().url
      if (status != null && location) {
        const key = this.httpErrorKey(sanitizePath(location), status)
        if (this.allowedHttpErrors.has(key)) return
      }
      this.consoleErrors.push(sanitizeMessage(message.text()))
    })
    page.on('pageerror', (error) => {
      this.pageErrors.push(`${error.name}: ${sanitizeMessage(error.message)}`)
    })
    page.on('requestfailed', (request) => {
      this.requestFailures.push(
        `${request.method()} ${sanitizePath(request.url())} ${sanitizeMessage(request.failure()?.errorText ?? 'unknown')}`,
      )
    })
    page.on('response', (response) => this.recordResponse(response))
    await page.addInitScript(() => {
      window.__houfengStagingDiagnostics = []
      window.addEventListener('securitypolicyviolation', (event) => {
        window.__houfengStagingDiagnostics?.push({
          kind: 'csp',
          message: `${event.effectiveDirective} ${event.disposition}`,
        })
      })
      window.addEventListener('unhandledrejection', (event) => {
        const reason = event.reason
        window.__houfengStagingDiagnostics?.push({
          kind: 'unhandledrejection',
          message: reason instanceof Error ? reason.name : typeof reason,
        })
      })
    })
  }

  allowHttpError(path: string, status: number): void {
    this.allowedHttpErrors.add(this.httpErrorKey(sanitizePath(path), status))
  }

  recordRoute(path: string, width: number, height: number): void {
    this.routes.add(path)
    this.viewports.add(`${width}x${height}`)
  }

  recordScreenshot(filename: string): void {
    this.screenshots.push(filename)
  }

  recordStep(lane: AuditLane, name: string, outcome: 'passed' | 'failed' = 'passed'): void {
    this.steps.push({ lane, name, outcome })
  }

  captureMainDocumentHeaders(response: Response): void {
    const responseHeaders = response.headers()
    const headers: Record<string, string> = {}
    for (const name of HEADER_ALLOWLIST) {
      const value = responseHeaders[name]
      if (value) headers[name] = value
    }
    if (headers['content-security-policy'] !== EXPECTED_CSP) {
      throw new Error('staging main document CSP does not match repository policy')
    }
    this.documents.push({
      path: sanitizePath(response.url()),
      status: response.status(),
      headers,
    })
  }

  counters() {
    return {
      consoleErrors: this.consoleErrors.length,
      pageErrors: this.pageErrors.length,
      requestFailures: this.requestFailures.length,
      unexpectedHttpErrors: this.httpErrors.length,
      expectedHttpErrors: this.expectedHttpErrors.length,
      cspViolations: this.browserDiagnostics.filter((entry) => entry.kind === 'csp').length,
      unhandledRejections: this.browserDiagnostics.filter(
        (entry) => entry.kind === 'unhandledrejection',
      ).length,
    }
  }

  async assertClean(page: Page): Promise<void> {
    const browserDiagnostics = await page.evaluate(
      () => {
        const diagnostics = window.__houfengStagingDiagnostics ?? []
        window.__houfengStagingDiagnostics = []
        return diagnostics
      },
    )
    this.browserDiagnostics.push(...browserDiagnostics)
    const failures = [
      ...this.consoleErrors.map((message) => `console: ${message}`),
      ...this.pageErrors.map((message) => `page: ${message}`),
      ...this.requestFailures.map((message) => `request: ${message}`),
      ...this.httpErrors.map((message) => `http: ${message}`),
      ...this.browserDiagnostics.map((entry) => `${entry.kind}: ${entry.message}`),
    ]
    if (failures.length > 0) {
      throw new Error(`staging diagnostics were not clean:\n${failures.join('\n')}`)
    }
  }

  async write(options: {
    directory: string
    expectedVersion: string
    observedVersion: string
    browserVersion: string
    conclusion: 'passed' | 'failed'
    failureCategory: string | null
  }): Promise<void> {
    await mkdir(options.directory, { recursive: true })
    const repository = process.env.GITHUB_REPOSITORY ?? ''
    const server = process.env.GITHUB_SERVER_URL ?? 'https://github.com'
    const runID = process.env.GITHUB_RUN_ID ?? 'local'
    const runURL = repository && runID !== 'local'
      ? `${server}/${repository}/actions/runs/${runID}`
      : 'local'
    const manifest: AuditManifest = {
      schemaVersion: 1,
      run: {
        id: runID,
        url: runURL,
        commit: process.env.GITHUB_SHA ?? 'local',
        expectedVersion: options.expectedVersion,
        observedVersion: options.observedVersion,
        browserVersion: options.browserVersion,
      },
      conclusion: options.conclusion,
      failureCategory: options.failureCategory,
      evidence: {
        routes: [...this.routes],
        viewports: [...this.viewports],
        screenshots: [...this.screenshots],
        documents: this.documents,
        network: this.network,
        steps: this.steps,
        counters: this.counters(),
      },
    }
    await writeFile(
      join(options.directory, 'manifest.json'),
      `${JSON.stringify(manifest, null, 2)}\n`,
      'utf8',
    )
    await writeFile(
      join(options.directory, 'summary.md'),
      [
        '# Frontend staging audit',
        '',
        `- Conclusion: ${manifest.conclusion}`,
        `- Run: ${manifest.run.url}`,
        `- Commit: ${manifest.run.commit}`,
        `- Version: expected ${manifest.run.expectedVersion}; observed ${manifest.run.observedVersion}`,
        `- Browser: ${manifest.run.browserVersion}`,
        `- Routes: ${manifest.evidence.routes.join(', ')}`,
        `- Viewports: ${manifest.evidence.viewports.join(', ')}`,
        `- Real-environment steps: ${manifest.evidence.steps.filter((step) => step.lane === 'real-environment' && step.outcome === 'passed').length}`,
        `- Deployed-frontend injection steps: ${manifest.evidence.steps.filter((step) => step.lane === 'deployed-frontend-injection' && step.outcome === 'passed').length}`,
        `- Counters: ${JSON.stringify(manifest.evidence.counters)}`,
        '',
        'Real-environment observations and deployed-frontend injections are recorded by the test steps; injected responses do not prove backend or production data health.',
        '',
      ].join('\n'),
      'utf8',
    )
  }

  private recordResponse(response: Response): void {
    const request = response.request()
    const url = new URL(response.url())
    const resourceType = request.resourceType()
    if (resourceType !== 'document' && !url.pathname.startsWith('/api/')) return
    const path = sanitizePath(response.url())
    const entry = {
      method: request.method(),
      path,
      status: response.status(),
      resourceType,
      durationMs: requestDuration(request),
    } satisfies AuditNetworkEntry
    this.network.push(entry)
    if (response.status() < 400) return
    const key = this.httpErrorKey(path, response.status())
    if (this.allowedHttpErrors.has(key)) this.expectedHttpErrors.push(key)
    else this.httpErrors.push(key)
  }

  private httpErrorKey(path: string, status: number): string {
    return `${path} ${status}`
  }
}
