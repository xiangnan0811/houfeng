/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const indexPath = 'src/index.css'

/**
 * index.css is now a manifest of `@import './styles/partials/*.css'` statements.
 * Vite inlines those partials in import order, so the contract must run against
 * the final, concatenated stylesheet (identical to what the browser receives).
 * We resolve the @import chain here so the same assertions hold post-split.
 */
function resolveImported(filePath: string, seen = new Set<string>()): string {
  const full = resolve(filePath)
  if (seen.has(full)) return ''
  seen.add(full)
  const raw = readFileSync(full, 'utf8')
  const dir = dirname(full)
  return raw
    .split('\n')
    .map((line) => {
      const m = line.match(/^\s*@import\s+['"]([^'"]+)['"]\s*;?/)
      if (m) return resolveImported(resolve(dir, m[1]), seen)
      return line
    })
    .join('\n')
}

const indexCss = resolveImported(indexPath)
const loginPageCss = readFileSync('src/pages/LoginPage.css', 'utf8')

function ruleBodies(css: string, selector: string): string[] {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return [...css.matchAll(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`, 'g'))]
    .map((match) => match[1] ?? '')
}

function ruleBody(css: string, selector: string): string {
  return ruleBodies(css, selector)[0] ?? ''
}

function compact(css: string): string {
  return css.replace(/\s+/g, '')
}

describe('index.css modernization contracts', () => {
  it('anchors section title accent markers to the title itself', () => {
    expect(ruleBody(indexCss, '.section-title')).toContain('position:relative')
  })

  it('only shows watchtower h1 eyebrow pseudo elements for explicit variants', () => {
    expect(ruleBody(indexCss, '.watchtower-header__title-block h1::before')).toContain('display:none')
    expect(ruleBody(indexCss, '.watchtower-header[aria-label="VPS 身份与操作"] .watchtower-header__title-block h1::before')).toContain('display:block')
    expect(ruleBody(indexCss, '.provider-directory .watchtower-header__title-block h1::before')).toContain('display:block')
  })

  it('stacks the login card and footer vertically', () => {
    expect(compact(ruleBody(indexCss, '.login-page'))).toContain('flex-direction:column')
    expect(compact(ruleBody(loginPageCss, '.login-page'))).toContain('flex-direction:column')
  })

  it('keeps responsive tabs and asset commands readable within their owner', () => {
    for (const variant of ['underline', 'pill']) {
      const tabs = compact(ruleBody(indexCss, `.tabs--${variant}`))
      const tab = compact(ruleBody(indexCss, `.tabs--${variant} .tab`))

      expect(tabs).toContain('max-width:100%')
      expect(tabs).toContain('overflow-x:auto')
      expect(tab).toContain('flex:00auto')
      expect(tab).toContain('white-space:nowrap')
    }

    const title = compact(ruleBody(indexCss, '.asset-decision-support-strip__title'))
    expect(title).toContain('white-space:normal')
    expect(title).toContain('overflow:visible')
    expect(title).toContain('text-overflow:clip')
    expect(title).not.toContain('text-overflow:ellipsis')

    const gridRules = ruleBodies(indexCss, '.asset-decision-support-strip')
      .map(compact)
      .filter((body) => body.includes('grid-template-columns:'))
    expect(gridRules).toHaveLength(1)
  })

  it('isolates provider table overflow and keeps entry labels visible', () => {
    const table = compact(ruleBody(indexCss, '.provider-directory-table'))
    const scrollRegion = compact(ruleBody(indexCss, '.provider-directory-table-scroll'))
    const focusRing = compact(ruleBody(indexCss, '.provider-directory-table-scroll:focus-visible'))
    const entryLinks = compact(ruleBody(indexCss, '.provider-directory-entry-links'))
    const entryLink = compact(ruleBody(indexCss, '.provider-directory-entry-link'))

    expect(table).toContain('min-width:1000px')
    expect(scrollRegion).toContain('max-width:100%')
    expect(scrollRegion).toContain('overflow-x:auto')
    expect(scrollRegion).toContain('scrollbar-gutter:stable')
    expect(focusRing).toContain('outline:var(--border-w-strong)solidvar(--accent)')
    expect(entryLinks).toContain('flex-wrap:wrap')
    expect(entryLinks).toContain('overflow:visible')
    expect(entryLink).toContain('max-width:none')
    expect(entryLink).toContain('overflow:visible')
    expect(entryLink).toContain('text-overflow:clip')
    expect(entryLink).not.toContain('text-overflow:ellipsis')
  })
})
