import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { extname, join, resolve } from 'node:path'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const WEB_ROOT = process.cwd()
const REPOSITORY_ROOT = resolve(WEB_ROOT, '..')
const CSP_POLICY = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'"
const EXPECTED_PUBLIC_RESOURCES = [
  'theme-bootstrap.js',
  'select-caret-houfeng-dark.svg',
  'select-caret-houfeng-light.svg',
  'select-caret-classic-dark.svg',
  'fonts/ibm-plex-sans-400.woff2',
  'fonts/ibm-plex-sans-500.woff2',
  'fonts/ibm-plex-sans-600.woff2',
  'fonts/ibm-plex-sans-700.woff2',
  'fonts/ibm-plex-mono-400.woff2',
  'fonts/ibm-plex-mono-500.woff2',
  'fonts/ibm-plex-mono-600.woff2',
  'fonts/OFL.txt',
] as const

function walkFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = join(root, entry.name)
    return entry.isDirectory() ? walkFiles(path) : [path]
  })
}

function relativeToWeb(path: string): string {
  return path.slice(WEB_ROOT.length + 1)
}

function findMatches(paths: string[], pattern: RegExp): string[] {
  return paths.flatMap((path) => {
    const source = readFileSync(path, 'utf8')
    return source.split('\n').flatMap((line, index) => (
      pattern.test(line) ? [`${relativeToWeb(path)}:${index + 1}`] : []
    ))
  })
}

function publicSource(path: string): string {
  const absolutePath = resolve(WEB_ROOT, 'public', path)
  return existsSync(absolutePath) ? readFileSync(absolutePath, 'utf8') : ''
}

describe('strict CSP source contract', () => {
  it('defines one exact same-origin policy file', () => {
    const policyPath = resolve(REPOSITORY_ROOT, 'internal/center/http/csp-policy.txt')

    expect(existsSync(policyPath)).toBe(true)
    if (!existsSync(policyPath)) return
    expect(readFileSync(policyPath, 'utf8').trim()).toBe(CSP_POLICY)
  })

  it('keeps production HTML free of remote fonts and inline scripts', () => {
    const html = readFileSync(resolve(WEB_ROOT, 'index.html'), 'utf8')
    const inlineScripts = html.match(/<script(?![^>]*\bsrc=)[^>]*>[\s\S]*?<\/script>/gi) ?? []

    expect(html).not.toMatch(/fonts\.(?:googleapis|gstatic)\.com/)
    expect(inlineScripts).toEqual([])
    expect(html).toContain('<script src="/theme-bootstrap.js"></script>')
  })

  it('keeps production CSS free of remote and data resources', () => {
    const sourceFiles = walkFiles(resolve(WEB_ROOT, 'src'))
    const cssFiles = sourceFiles.filter((path) => extname(path) === '.css')

    expect(findMatches(cssFiles, /(?:url\([^)]*data:|https?:\/\/fonts\.)/i)).toEqual([])
  })

  it('keeps production TSX free of style attributes', () => {
    const sourceFiles = walkFiles(resolve(WEB_ROOT, 'src'))
    const productionTSX = sourceFiles.filter(
      (path) => extname(path) === '.tsx' && !path.endsWith('.test.tsx'),
    )

    expect(findMatches(productionTSX, /\bstyle\s*=/)).toEqual([])
  })

  it('tracks every self-hosted font, caret, bootstrap, and font license asset', () => {
    const missing = EXPECTED_PUBLIC_RESOURCES.filter(
      (path) => !existsSync(resolve(WEB_ROOT, 'public', path)),
    )

    expect(missing).toEqual([])
  })

  it('wires every approved font weight and theme caret through same-origin URLs', () => {
    const tokens = readFileSync(resolve(WEB_ROOT, 'src/styles/tokens.css'), 'utf8')

    for (const weight of [400, 500, 600, 700]) {
      expect(tokens).toContain(`/fonts/ibm-plex-sans-${weight}.woff2`)
    }
    for (const weight of [400, 500, 600]) {
      expect(tokens).toContain(`/fonts/ibm-plex-mono-${weight}.woff2`)
    }
    expect(tokens).toContain("url('/select-caret-houfeng-dark.svg')")
    expect(tokens).toContain("url('/select-caret-houfeng-light.svg')")
    expect(tokens).toContain("url('/select-caret-classic-dark.svg')")
  })

  it('routes custom select arrows through the shared same-origin caret token', () => {
    const subscriptions = readFileSync(
      resolve(WEB_ROOT, 'src/styles/partials/legacy-subscriptions.css'),
      'utf8',
    )

    expect(subscriptions).toMatch(
      /\.subscription-panel-select select\{[^}]*background-image:var\(--select-caret\)/,
    )
    expect(subscriptions).not.toMatch(
      /\.subscription-panel-select select\{[^}]*linear-gradient\(/,
    )
  })
})

describe('theme bootstrap allowlist', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    localStorage.clear()
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false }))
  })

  it('rejects unknown persisted values instead of creating arbitrary classes', () => {
    localStorage.setItem('houfeng.theme.preset', 'attacker')
    localStorage.setItem('houfeng.theme.mode', 'surprise')

    window.eval(publicSource('theme-bootstrap.js'))

    expect(document.documentElement.className).toBe('theme-houfeng-dark')
  })

  it('keeps classic light aligned with the runtime houfeng-light fallback', () => {
    localStorage.setItem('houfeng.theme.preset', 'classic')
    localStorage.setItem('houfeng.theme.mode', 'light')

    window.eval(publicSource('theme-bootstrap.js'))

    expect(document.documentElement.className).toBe('theme-houfeng-light')
  })
})
