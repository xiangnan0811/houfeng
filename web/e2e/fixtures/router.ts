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
  phase: 'connecting' | 'open' | 'error' | 'closed'
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
      const pending = ((window as unknown as {
        __houfengE2EPendingRuntimeStreams?: Set<string>
      }).__houfengE2EPendingRuntimeStreams ??= new Set<string>())
      const allowed = new Set<string>(pending)
      const sockets: SocketRecord[] = []
      const unexpected: string[] = []

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
        if (!match?.[1] || parsed.search !== '' || parsed.hash !== '') return null
        const expectedURL = `${expectedProtocol}//${window.location.host}/api/monitoring-instances/${match[1]}/runtime-stream`
        if (url !== expectedURL) return null
        return match[1]
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
          unexpected() {
            return unexpected
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
        private readonly record: SocketRecord
        private readonly listeners = new Map<string, Array<(event: Event) => void>>()

        constructor(url: string | URL) {
          this.url = String(url)
          const monitoringInstanceId = runtimeStreamID(this.url) ?? ''
          this.record = {
            url: this.url,
            monitoringInstanceId,
            phase: 'connecting',
          }
          sockets.push(this.record)

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

  async assertRuntimeStreamConnected(monitoringInstanceId: string): Promise<void> {
    const pageURL = new URL(this.page.url())
    const sockets = await this.snapshotRuntimeStreamSockets()
    const matches = sockets.filter((socket) => socket.monitoringInstanceId === monitoringInstanceId)
    if (matches.length === 0) {
      throw new Error(`expected runtime stream socket for ${monitoringInstanceId}, got ${JSON.stringify(sockets)}`)
    }
    if (!matches.every((socket) => socket.phase === 'open')) {
      throw new Error(`expected runtime stream socket open for ${monitoringInstanceId}, got ${JSON.stringify(matches)}`)
    }
    for (const socket of matches) {
      const expectedProtocol = pageURL.protocol === 'https:' ? 'wss:' : 'ws:'
      const expectedURL = `${expectedProtocol}//${pageURL.host}/api/monitoring-instances/${monitoringInstanceId}/runtime-stream`
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
    if (fixture.expectNoBody) {
      const body = request.postData()
      if (body === null || body.length === 0) return true
      this.unexpectedRequests.push(`${key} body must be empty`)
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
    }
  }
}
