import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// 分类图表色（订阅成本环形图等）不能与状态色撞色，否则扇区会被读成“正常/提醒/告警”。
const THEMES = [
  ':root, .theme-houfeng-dark',
  '.theme-houfeng-light',
  '.theme-precision-dark',
  '.theme-precision-light',
  '.theme-observatory-dark',
]
const STATE_TOKENS = [
  '--color-state-normal',
  '--color-state-notice',
  '--color-state-alert',
  '--color-state-critical',
  '--color-state-maintenance',
  '--color-state-offline',
]

function themeTokens(css: string, selector: string): Map<string, string> {
  const start = css.indexOf(`\n${selector}{`)
  expect(start, `theme block ${selector}`).toBeGreaterThanOrEqual(0)
  const body = css.slice(css.indexOf('{', start) + 1, css.indexOf('\n}', start))
  return new Map([...body.matchAll(/(--[a-z0-9-]+):([^;]+);/g)].map((match) => [match[1]!, match[2]!.trim()]))
}

function resolve(tokens: Map<string, string>, value: string, depth = 0): string {
  const alias = /^var\((--[a-z0-9-]+)\)$/.exec(value)
  if (!alias || depth > 5) return value.toUpperCase()
  return resolve(tokens, tokens.get(alias[1]!) ?? value, depth + 1)
}

describe('categorical chart palette', () => {
  const css = readFileSync('src/styles/tokens.css', 'utf8')

  it.each(THEMES)('defines six distinct colors that never reuse a state color in %s', (selector) => {
    const tokens = themeTokens(css, selector)
    const chart = [1, 2, 3, 4, 5, 6].map((index) => {
      const value = tokens.get(`--chart-${index}`)
      expect(value, `--chart-${index}`).toBeDefined()
      return resolve(tokens, value!)
    })
    expect(new Set(chart).size).toBe(6)
    const states = STATE_TOKENS.map((name) => resolve(tokens, tokens.get(name) ?? ''))
    for (const color of chart) expect(states).not.toContain(color)
  })
})
