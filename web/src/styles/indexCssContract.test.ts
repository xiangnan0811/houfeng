/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const indexCss = readFileSync('src/index.css', 'utf8')
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
