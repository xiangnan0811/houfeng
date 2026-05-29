import { mkdir, writeFile } from 'node:fs/promises'
import { dirname, join, relative, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import net from 'node:net'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..')
const OUT_DIR = join(ROOT, '.trellis/tasks/05-22-frontend-workbench-visual-correction/research/screenshots')
const OUT_JSON = join(ROOT, '.trellis/tasks/05-22-frontend-workbench-visual-correction/research/visual-audit-raw-node.json')
const BASE_URL = process.env.HOUFENG_VISUAL_BASE_URL || 'http://127.0.0.1:5178'
const CHROME = process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const DEBUG_PORT = Number(process.env.HOUFENG_CHROME_DEBUG_PORT || 9223)
const VIEWPORTS = [
  { width: 1440, height: 1000 },
  { width: 390, height: 900 },
]
const ROUTES = ['/', '/vps', '/nodes', '/targets', '/events', '/providers', '/subscriptions', '/asset-decisions']

function sleep(ms) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, ms))
}

function waitForPort(port, host = '127.0.0.1', timeoutMs = 10000) {
  const started = Date.now()
  return new Promise((resolvePromise, reject) => {
    function tryConnect() {
      const socket = net.connect(port, host)
      socket.once('connect', () => {
        socket.end()
        resolvePromise()
      })
      socket.once('error', () => {
        socket.destroy()
        if (Date.now() - started > timeoutMs) {
          reject(new Error(`Timed out waiting for ${host}:${port}`))
        } else {
          setTimeout(tryConnect, 120)
        }
      })
    }
    tryConnect()
  })
}

async function jsonFetch(url, options) {
  const response = await fetch(url, options)
  if (!response.ok) throw new Error(`${url} returned ${response.status}: ${await response.text()}`)
  return response.json()
}

function websocketRequest(ws, method, params = {}) {
  const id = websocketRequest.nextId++
  ws.send(JSON.stringify({ id, method, params }))
  return new Promise((resolvePromise, reject) => {
    const timeout = setTimeout(() => {
      websocketRequest.pending.delete(id)
      reject(new Error(`CDP ${method} timed out`))
    }, 20000)
    websocketRequest.pending.set(id, { resolve: resolvePromise, reject, timeout })
  })
}
websocketRequest.nextId = 1
websocketRequest.pending = new Map()

async function connectTab(wsUrl) {
  const ws = new WebSocket(wsUrl)
  ws.addEventListener('message', (event) => {
    const message = JSON.parse(event.data)
    if (!message.id) return
    const pending = websocketRequest.pending.get(message.id)
    if (!pending) return
    websocketRequest.pending.delete(message.id)
    clearTimeout(pending.timeout)
    if (message.error) {
      pending.reject(new Error(message.error.message || JSON.stringify(message.error)))
    } else {
      pending.resolve(message.result)
    }
  })
  await new Promise((resolvePromise, reject) => {
    ws.addEventListener('open', resolvePromise, { once: true })
    ws.addEventListener('error', reject, { once: true })
  })
  return ws
}

function routeName(route) {
  return route === '/' ? 'dashboard' : route.replace(/^\//, '').replace(/[/?=&]/g, '-')
}

function pageUrl(route) {
  return `${BASE_URL.replace(/\/$/, '')}${route}`
}

async function waitForRouteReady(ws) {
  for (let i = 0; i < 80; i += 1) {
    const result = await websocketRequest(ws, 'Runtime.evaluate', {
      expression: `(() => {
        const text = (document.body && document.body.innerText || '').trim()
        const main = document.querySelector('main')
        return Boolean(text.length > 80 && main && !text.includes('正在加载'))
      })()`,
      returnByValue: true,
    })
    if (result.result.value) return
    await sleep(150)
  }
}

async function evaluate(ws, expression) {
  const result = await websocketRequest(ws, 'Runtime.evaluate', {
    expression,
    returnByValue: true,
    awaitPromise: true,
  })
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails))
  return result.result.value
}

async function captureRoute(browserVersion, route, viewport) {
  const target = await jsonFetch(`http://127.0.0.1:${DEBUG_PORT}/json/new?${encodeURIComponent(pageUrl('/'))}`, { method: 'PUT' })
  const ws = await connectTab(target.webSocketDebuggerUrl)
  try {
    await websocketRequest(ws, 'Page.enable')
    await websocketRequest(ws, 'Runtime.enable')
    await websocketRequest(ws, 'Emulation.setDeviceMetricsOverride', {
      width: viewport.width,
      height: viewport.height,
      deviceScaleFactor: 1,
      mobile: false,
    })
    await websocketRequest(ws, 'Page.navigate', { url: pageUrl(route) })
    await waitForRouteReady(ws)
    await sleep(500)

    const screenshot = await websocketRequest(ws, 'Page.captureScreenshot', { format: 'png', fromSurface: true })
    const shotPath = join(OUT_DIR, `${routeName(route)}-${viewport.width}x${viewport.height}.png`)
    await writeFile(shotPath, Buffer.from(screenshot.data, 'base64'))

    const data = await evaluate(ws, `(() => {
      const rect = (el) => {
        if (!el) return null
        const r = el.getBoundingClientRect()
        return { x: Math.round(r.x), y: Math.round(r.y), width: Math.round(r.width), height: Math.round(r.height) }
      }
      const text = (el) => el ? (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 220) : null
      const style = (el) => {
        if (!el) return null
        const s = window.getComputedStyle(el)
        return {
          display: s.display,
          gap: s.gap,
          gridTemplateColumns: s.gridTemplateColumns,
          alignItems: s.alignItems,
          justifyContent: s.justifyContent,
          overflowX: s.overflowX,
        }
      }
      const selectors = [
        '.app-shell__main', '.top-bar', '.global-alert', '.compact-header', '.compact-header__actions', '.compact-header__metrics',
        '.workbench-layout', '.workbench-main', '.workbench-aside', '.workbench-context', '.table-workbench', '.table-workbench__header',
        '.table-workbench__toolbar', '.table-workbench__tabs', '.table-workbench__chips', '.table-workbench__priority', '.table-workbench__body',
        '.table-workbench__content', '.table-workbench__aside', '.filter-bar', '.data-table', '.dashboard-command-grid',
        '.asset-decision-flow-context', '.events-flow-context', '.providers-flow-context', '.subscriptions-evidence-workbench__context',
        '.observability-support', '.vps-evidence-card'
      ]
      const sections = selectors.map((selector) => ({
        selector,
        count: document.querySelectorAll(selector).length,
        rects: Array.from(document.querySelectorAll(selector)).slice(0, 6).map((el) => ({ rect: rect(el), style: style(el), text: text(el) })),
      }))
      const buttons = Array.from(document.querySelectorAll('button, a.btn, .tab, .chip, .filter-chip')).map((el) => ({
        text: text(el),
        className: el.className ? String(el.className) : '',
        rect: rect(el),
        tag: el.tagName.toLowerCase(),
        visible: !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length),
      })).filter((item) => item.visible && item.rect && item.rect.y < window.innerHeight)
      const firstHeadings = Array.from(document.querySelectorAll('h1,h2,h3')).slice(0, 18).map((el) => ({ level: el.tagName, text: text(el), rect: rect(el) }))
      return {
        browser: ${JSON.stringify(browserVersion.Browser || '')},
        currentUrl: window.location.href,
        bodyTextLength: (document.body.innerText || '').trim().length,
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
        scrollHeight: document.documentElement.scrollHeight,
        firstHeadings,
        sections,
        buttons,
      }
    })()`)

    return {
      route,
      viewport: `${viewport.width}x${viewport.height}`,
      screenshot: relative(ROOT, shotPath),
      data,
    }
  } finally {
    ws.close()
    await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/close/${target.id}`).catch(() => {})
  }
}

async function main() {
  await mkdir(OUT_DIR, { recursive: true })
  const userDataDir = join(ROOT, 'web/.tmp/chrome-capture-profile')
  const chrome = spawn(CHROME, [
    `--remote-debugging-port=${DEBUG_PORT}`,
    `--user-data-dir=${userDataDir}`,
    '--headless=new',
    '--disable-gpu',
    '--hide-scrollbars',
    '--no-first-run',
    '--no-default-browser-check',
    'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] })

  let chromeError = ''
  chrome.stderr.on('data', (chunk) => { chromeError += chunk.toString() })

  try {
    await waitForPort(DEBUG_PORT)
    const browserVersion = await jsonFetch(`http://127.0.0.1:${DEBUG_PORT}/json/version`)
    const results = []
    for (const viewport of VIEWPORTS) {
      for (const route of ROUTES) {
        results.push(await captureRoute(browserVersion, route, viewport))
      }
    }
    await writeFile(OUT_JSON, JSON.stringify(results, null, 2))
    console.log(`wrote ${relative(ROOT, OUT_JSON)}`)
  } finally {
    chrome.kill('SIGTERM')
    if (chromeError.trim()) console.error(chromeError.trim().split('\n').slice(-5).join('\n'))
  }
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
