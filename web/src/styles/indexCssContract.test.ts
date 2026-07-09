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

function ruleBody(css: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  return match?.[1] ?? ''
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
})
