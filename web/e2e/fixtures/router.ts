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

type RuntimeStreamSocketRecord = {
  url: string
  monitoringInstanceId: string
  includeSamples: boolean
  phase: 'connecting' | 'open' | 'error' | 'closed'
}

type RecordCleanupSocketRecord = {
  url: string
  browserId: string
  phase: RuntimeStreamSocketRecord['phase']
}

type RecordCleanupReconcileBody = {
  browser_id: string
  record_ids: string[]
  allocation_references?: Array<{ record_id: string; allocation_id: string }>
}

const RECORD_CLEANUP_RECONCILE_KEY = apiRouteKey('POST', '/api/records/client-cleanup/reconcile')
const BROWSER_ID_PATTERN = /^browser_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0
}

// Mirrors the reconcile request the record cleanup consumer sends on startup.
function parseRecordCleanupReconcileBody(body: unknown): RecordCleanupReconcileBody | null {
  const keys = sortedKeys(body)
  if (!keys) return null
  const value = body as Record<string, unknown>
  const allowedKeys = ['allocation_references', 'browser_id', 'record_ids']
  if (!keys.includes('browser_id') || !keys.includes('record_ids') || keys.some((key) => !allowedKeys.includes(key))) return null
  if (typeof value.browser_id !== 'string' || !BROWSER_ID_PATTERN.test(value.browser_id)) return null
  if (!Array.isArray(value.record_ids) || !value.record_ids.every(isNonEmptyString)) return null
  let references: RecordCleanupReconcileBody['allocation_references']
  if ('allocation_references' in value) {
    const raw = value.allocation_references
    if (!Array.isArray(raw) || raw.length === 0) return null
    references = []
    for (const reference of raw) {
      if (JSON.stringify(sortedKeys(reference)) !== JSON.stringify(['allocation_id', 'record_id'])) return null
      const { record_id: recordId, allocation_id: allocationId } = reference as Record<string, unknown>
      if (!isNonEmptyString(recordId) || !isNonEmptyString(allocationId)) return null
      references.push({ record_id: recordId, allocation_id: allocationId })
    }
  }
  const ids = [...value.record_ids, ...(references ?? []).map((reference) => reference.record_id)]
  if (new Set(ids).size !== ids.length) return null
  return { browser_id: value.browser_id, record_ids: value.record_ids, ...(references ? { allocation_references: references } : {}) }
}

function recordCleanupReconcileResponse(body: RecordCleanupReconcileBody) {
  return {
    allowed: body.record_ids,
    invalidate: [],
    pending: (body.allocation_references ?? []).map((reference) => reference.record_id),
  }
}

export class ApiFixtureController {
  private profile: ApiFixtureProfile = {}
  private readonly requests: ApiRouteKey[] = []
  private readonly unexpectedRequests: string[] = []
  private readonly allowedRuntimeStreams = new Set<string>()
  private readonly acknowledgedUnexpectedRuntimeStreams: string[] = []
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
    await this.page.addInitScript(() => {
      type SocketRecord = RuntimeStreamSocketRecord
      type CleanupSocketRecord = RecordCleanupSocketRecord
      const pending = ((window as unknown as {
        __houfengE2EPendingRuntimeStreams?: Set<string>
      }).__houfengE2EPendingRuntimeStreams ??= new Set<string>())
      const allowed = new Set<string>(pending)
      const sockets: SocketRecord[] = []
      const socketInstances: Array<FixtureWebSocket & { record: SocketRecord }> = []
      const unexpected: string[] = []
      const cleanupSockets: CleanupSocketRecord[] = []

      function runtimeStreamID(url: string): string | null {
        let parsed: URL
        try {
          parsed = new URL(url, window.location.href)
        } catch {
          return null
        }
        if (parsed.protocol !== 'ws:' && parsed.protocol !== 'wss:') return null
        const expectedProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        if (parsed.protocol !== expectedProtocol) return null
        if (parsed.hostname !== window.location.hostname || parsed.port !== window.location.port) return null
        const match = parsed.pathname.match(/^\/api\/monitoring-instances\/([^/]+)\/runtime-stream$/)
        if (!match?.[1] || !['', '?include_samples=false'].includes(parsed.search) || parsed.hash !== '') return null
        const expectedURL = `${expectedProtocol}//${window.location.host}/api/monitoring-instances/${match[1]}/runtime-stream${parsed.search}`
        if (url !== expectedURL) return null
        return match[1]
      }

      // Exact same-origin record client-cleanup channel opened by the consumer:
      // ?browser_id=browser_<uuid>[&cursor=<value>], nothing else.
      function recordCleanupBrowserID(url: string): string | null {
        let parsed: URL
        try {
          parsed = new URL(url, window.location.href)
        } catch {
          return null
        }
        const expectedProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        if (parsed.protocol !== expectedProtocol) return null
        if (parsed.hostname !== window.location.hostname || parsed.port !== window.location.port) return null
        if (parsed.pathname !== '/api/records/client-cleanup' || parsed.hash !== '') return null
        const entries = [...parsed.searchParams.entries()]
        const [first, second, ...rest] = entries
        if (!first || first[0] !== 'browser_id' || rest.length > 0) return null
        if (!/^browser_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(first[1])) return null
        if (second && (second[0] !== 'cursor' || second[1] === '')) return null
        const query = new URLSearchParams(entries).toString()
        if (url !== `${expectedProtocol}//${window.location.host}/api/records/client-cleanup?${query}`) return null
        return first[1]
      }

      function isAllowed(id: string): boolean {
        return allowed.has(id) || pending.has(id)
      }

      Object.defineProperty(window, '__houfengE2EWebSockets', {
        configurable: true,
        value: {
          allow(id: string) {
            allowed.add(id)
            pending.add(id)
          },
          sockets() {
            return sockets
          },
          deliver(id: string, message: unknown) {
            for (const socket of socketInstances) {
              if (socket.readyState === FixtureWebSocket.OPEN && socket.record.monitoringInstanceId === id) {
                socket.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(message) }))
              }
            }
          },
          disconnect(id: string) {
            for (const socket of socketInstances) {
              if (socket.readyState === FixtureWebSocket.OPEN && socket.record.monitoringInstanceId === id) socket.close()
            }
          },
          unexpected() {
            return unexpected
          },
          cleanupSockets() {
            return cleanupSockets
          },
        },
      })

      class FixtureWebSocket {
        static readonly CONNECTING = 0
        static readonly OPEN = 1
        static readonly CLOSING = 2
        static readonly CLOSED = 3

        url: string
        readyState = FixtureWebSocket.CONNECTING
        protocol = ''
        extensions = ''
        binaryType: BinaryType = 'blob'
        bufferedAmount = 0
        onopen: ((event: Event) => void) | null = null
        onmessage: ((event: MessageEvent) => void) | null = null
        onerror: ((event: Event) => void) | null = null
        onclose: ((event: CloseEvent) => void) | null = null
        readonly record: SocketRecord | CleanupSocketRecord
        private readonly listeners = new Map<string, Array<(event: Event) => void>>()

        constructor(url: string | URL) {
          this.url = String(url)
          const browserId = recordCleanupBrowserID(this.url)
          if (browserId) {
            // Healthy idle cleanup channel: opens and never sends frames.
            this.record = { url: this.url, browserId, phase: 'connecting' }
            cleanupSockets.push(this.record)
            this.open()
            return
          }
          const monitoringInstanceId = runtimeStreamID(this.url) ?? ''
          const record: SocketRecord = {
            url: this.url,
            monitoringInstanceId,
            includeSamples: !this.url.endsWith('?include_samples=false'),
            phase: 'connecting',
          }
          this.record = record
          sockets.push(record)
          socketInstances.push(this as FixtureWebSocket & { record: SocketRecord })

          if (!monitoringInstanceId || !isAllowed(monitoringInstanceId)) {
            unexpected.push(this.url)
            queueMicrotask(() => {
              this.readyState = FixtureWebSocket.CLOSED
              this.record.phase = 'error'
              this.dispatch('error', new Event('error'))
              this.dispatch('close', new CloseEvent('close'))
            })
            return
          }

          this.open()
        }

        private open(): void {
          queueMicrotask(() => {
            if (this.readyState === FixtureWebSocket.CLOSED) return
            this.readyState = FixtureWebSocket.OPEN
            this.record.phase = 'open'
            this.dispatch('open', new Event('open'))
          })
        }

        send(): void {}

        close(): void {
          this.readyState = FixtureWebSocket.CLOSED
          this.record.phase = 'closed'
          this.dispatch('close', new CloseEvent('close'))
        }

        addEventListener(type: string, listener: (event: Event) => void): void {
          const current = this.listeners.get(type) ?? []
          current.push(listener)
          this.listeners.set(type, current)
        }

        removeEventListener(type: string, listener: (event: Event) => void): void {
          const current = this.listeners.get(type) ?? []
          this.listeners.set(type, current.filter((item) => item !== listener))
        }

        dispatchEvent(event: Event): boolean {
          this.dispatch(event.type, event)
          return true
        }

        private dispatch(type: string, event: Event): void {
          if (type === 'open') this.onopen?.(event)
          if (type === 'error') this.onerror?.(event)
          if (type === 'close') this.onclose?.(event as CloseEvent)
          if (type === 'message') this.onmessage?.(event as MessageEvent)
          for (const listener of this.listeners.get(type) ?? []) listener(event)
        }
      }

      window.WebSocket = FixtureWebSocket as unknown as typeof WebSocket
    })
    await this.page.route('**/api/**', async (route) => this.handle(route))
  }

  async allowRuntimeStream(monitoringInstanceId: string): Promise<void> {
    this.allowedRuntimeStreams.add(monitoringInstanceId)
    await this.page.addInitScript((id) => {
      const pending = ((window as unknown as {
        __houfengE2EPendingRuntimeStreams?: Set<string>
      }).__houfengE2EPendingRuntimeStreams ??= new Set<string>())
      pending.add(id)
      window.__houfengE2EWebSockets?.allow(id)
    }, monitoringInstanceId)
    try {
      await this.page.evaluate((id) => {
        const pending = ((window as unknown as {
          __houfengE2EPendingRuntimeStreams?: Set<string>
        }).__houfengE2EPendingRuntimeStreams ??= new Set<string>())
        pending.add(id)
        window.__houfengE2EWebSockets?.allow(id)
      }, monitoringInstanceId)
    } catch {
      // Document may not exist yet; addInitScript covers the next navigation.
    }
  }

  async snapshotRuntimeStreamSockets(): Promise<RuntimeStreamSocketRecord[]> {
    return this.page.evaluate(() => window.__houfengE2EWebSockets?.sockets() ?? [])
  }

  async assertRuntimeStreamConnected(monitoringInstanceId: string, includeSamples = true): Promise<void> {
    const pageURL = new URL(this.page.url())
    const sockets = await this.snapshotRuntimeStreamSockets()
    const matches = sockets.filter((socket) => socket.monitoringInstanceId === monitoringInstanceId && socket.phase !== 'closed')
    if (matches.length === 0) {
      throw new Error(`expected runtime stream socket for ${monitoringInstanceId}, got ${JSON.stringify(sockets)}`)
    }
    if (!matches.every((socket) => socket.phase === 'open')) {
      throw new Error(`expected runtime stream socket open for ${monitoringInstanceId}, got ${JSON.stringify(matches)}`)
    }
    for (const socket of matches) {
      const expectedProtocol = pageURL.protocol === 'https:' ? 'wss:' : 'ws:'
      const expectedURL = `${expectedProtocol}//${pageURL.host}/api/monitoring-instances/${monitoringInstanceId}/runtime-stream${includeSamples ? '' : '?include_samples=false'}`
      if (socket.url !== expectedURL) {
        throw new Error(`runtime stream socket ${socket.url} is not the exact raw owner URL`)
      }
      let parsed: URL
      try {
        parsed = new URL(socket.url, pageURL)
      } catch {
        throw new Error(`runtime stream socket url is not a URL: ${socket.url}`)
      }
      if (parsed.hostname !== pageURL.hostname || parsed.port !== pageURL.port) {
        throw new Error(`runtime stream socket ${socket.url} is not page-owned`)
      }
      if (parsed.pathname !== `/api/monitoring-instances/${monitoringInstanceId}/runtime-stream`) {
        throw new Error(`runtime stream socket ${socket.url} path is not the exact owner`)
      }
    }
  }

  async sendRuntimeStreamMessage(monitoringInstanceId: string, message: unknown): Promise<void> {
    await this.page.evaluate(({ id, message }) => window.__houfengE2EWebSockets?.deliver(id, message), { id: monitoringInstanceId, message })
  }

  async disconnectRuntimeStream(monitoringInstanceId: string): Promise<void> {
    await this.page.evaluate((id) => window.__houfengE2EWebSockets?.disconnect(id), monitoringInstanceId)
  }

  async assertRuntimeStreamNotConnected(monitoringInstanceId: string): Promise<void> {
    const sockets = await this.snapshotRuntimeStreamSockets()
    const matches = sockets.filter((socket) => socket.monitoringInstanceId === monitoringInstanceId)
    if (matches.length > 0) {
      throw new Error(`expected no runtime stream socket for ${monitoringInstanceId}, got ${JSON.stringify(matches)}`)
    }
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

  acknowledgeUnexpectedRuntimeStream(urlPart: string): void {
    this.acknowledgedUnexpectedRuntimeStreams.push(urlPart)
  }

  async assertRuntimeStreamsClean(): Promise<void> {
    const [unexpected, sockets] = await this.page.evaluate(() => [
      window.__houfengE2EWebSockets?.unexpected() ?? [],
      window.__houfengE2EWebSockets?.sockets() ?? [],
    ] as const)
    const leftover = unexpected.filter((url) => (
      !this.acknowledgedUnexpectedRuntimeStreams.some((part) => url.includes(part))
    ))
    if (leftover.length > 0) {
      throw new Error(`unexpected runtime stream sockets:\n${leftover.join('\n')}`)
    }
    const allowed = this.allowedRuntimeStreams
    for (const socket of sockets) {
      const acknowledged = this.acknowledgedUnexpectedRuntimeStreams.some((part) => socket.url.includes(part))
      if (acknowledged) {
        if (socket.phase !== 'error' && socket.phase !== 'closed') {
          throw new Error(`rejected runtime stream socket ${socket.url} ended in ${socket.phase}`)
        }
        continue
      }
      if (!allowed.has(socket.monitoringInstanceId)) {
        throw new Error(`runtime stream socket ${socket.url} is not allowlisted`)
      }
      if (socket.phase !== 'open' && socket.phase !== 'closed') {
        throw new Error(`runtime stream socket ${socket.url} ended in ${socket.phase}`)
      }
    }
  }

  async snapshotRecordCleanupSockets(): Promise<RecordCleanupSocketRecord[]> {
    return this.page.evaluate(() => window.__houfengE2EWebSockets?.cleanupSockets() ?? [])
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const method = request.method() as ApiMethod
    const path = canonicalApiPath(request.url())
    const key = apiRouteKey(method, path)
    this.requests.push(key)
    const fixture = this.profile[key]
    if (!fixture && key === RECORD_CLEANUP_RECONCILE_KEY) {
      await this.fulfillRecordCleanupReconcile(route, request, key)
      return
    }
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
      body: fixture.status === 204 ? '' : fixture.rawBody ?? JSON.stringify(fixture.body),
    })
  }

  // Default idle, healthy record client-cleanup channel: every requested id is
  // allowed (or pending for allocation references) and nothing is invalidated.
  private async fulfillRecordCleanupReconcile(route: Route, request: Request, key: string): Promise<void> {
    let body: RecordCleanupReconcileBody | null = null
    try {
      body = parseRecordCleanupReconcileBody(request.postDataJSON())
    } catch {
      body = null
    }
    if (!body) {
      this.unexpectedRequests.push(`${key} body must be {browser_id, record_ids, allocation_references?}`)
      await route.fulfill({
        status: 422,
        contentType: 'application/json',
        body: JSON.stringify({ error: `fixture request body mismatch: ${key}` }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(recordCleanupReconcileResponse(body)),
    })
  }

  private requestBodyMatches(
    request: Request,
    fixture: ApiFixtureResponse,
    key: string,
  ): boolean {
    if (fixture.expectNoBody) {
      const body = request.postData()
      if (body === null || body.length === 0) return true
      this.unexpectedRequests.push(`${key} body must be empty`)
      return false
    }
    if (fixture.expectRawBodyBytes !== undefined) {
      const size = request.postDataBuffer()?.length ?? 0
      if (size === fixture.expectRawBodyBytes) return true
      this.unexpectedRequests.push(`${key} raw body expected ${fixture.expectRawBodyBytes} bytes received ${size}`)
      return false
    }
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

declare global {
  interface Window {
    __houfengE2EWebSockets?: {
      allow: (monitoringInstanceId: string) => void
      sockets: () => RuntimeStreamSocketRecord[]
      unexpected: () => string[]
      deliver: (monitoringInstanceId: string, message: unknown) => void
      disconnect: (monitoringInstanceId: string) => void
      cleanupSockets: () => RecordCleanupSocketRecord[]
    }
  }
}
