/// <reference types="node" />

import { readFileSync, readdirSync } from 'node:fs'
import { extname, join, relative } from 'node:path'
import postcss from 'postcss'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const productionExtensions = new Set(['.ts', '.tsx'])
// This repository-wide AST scan competes with the full coverage suite in CI.
const repositoryScanTimeoutMilliseconds = 15_000

function walkFiles(directory: string, accept: (path: string) => boolean): string[] {
  return readdirSync(directory, { withFileTypes: true })
    .sort((left, right) => left.name.localeCompare(right.name))
    .flatMap((entry) => {
      const path = join(directory, entry.name)
      return entry.isDirectory() ? walkFiles(path, accept) : accept(path) ? [path] : []
    })
}

function isProductionTypeScript(path: string) {
  return productionExtensions.has(extname(path)) &&
    !path.includes('/test/') &&
    !/\.(?:test|testFixtures)\.[^.]+$/.test(path)
}

function addSourceTokens(text: string, exact: Set<string>, prefixes: Set<string>) {
  for (const token of text.match(/[A-Za-z_][A-Za-z0-9_-]*/g) ?? []) {
    if (token.endsWith('-')) prefixes.add(token)
    else exact.add(token)
  }
}

function sourceClassInventory() {
  const exact = new Set<string>()
  const prefixes = new Set<string>()

  for (const path of walkFiles('src', isProductionTypeScript)) {
    const source = ts.createSourceFile(
      path,
      readFileSync(path, 'utf8'),
      ts.ScriptTarget.Latest,
      true,
      path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    )
    function visit(node: ts.Node) {
      if (
        ts.isStringLiteral(node) ||
        ts.isNoSubstitutionTemplateLiteral(node) ||
        ts.isTemplateHead(node) ||
        ts.isTemplateMiddle(node) ||
        ts.isTemplateTail(node)
      ) {
        addSourceTokens(node.text, exact, prefixes)
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
  }

  addSourceTokens(readFileSync('index.html', 'utf8'), exact, prefixes)
  return { exact, prefixes }
}

function splitSelectors(value: string) {
  const selectors: string[] = []
  let start = 0
  let depth = 0
  let quote = ''

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index]
    if (quote !== '') {
      if (character === quote && value[index - 1] !== '\\') quote = ''
      continue
    }
    if (character === '"' || character === "'") quote = character
    else if (character === '(' || character === '[') depth += 1
    else if (character === ')' || character === ']') depth = Math.max(0, depth - 1)
    else if (character === ',' && depth === 0) {
      selectors.push(value.slice(start, index).trim())
      start = index + 1
    }
  }

  selectors.push(value.slice(start).trim())
  return selectors.filter(Boolean)
}

describe('production CSS reachability', () => {
  it('requires every class selector branch to have a production source owner', {
    timeout: repositoryScanTimeoutMilliseconds,
  }, () => {
    const { exact, prefixes } = sourceClassInventory()
    const violations: string[] = []
    const isUsed = (className: string) =>
      exact.has(className) || [...prefixes].some((prefix) => className.startsWith(prefix))

    for (const path of walkFiles('src', (file) => file.endsWith('.css'))) {
      const root = postcss.parse(readFileSync(path, 'utf8'), { from: path })
      root.walkRules((rule) => {
        for (const selector of splitSelectors(rule.selector)) {
          const classes = [
            ...new Set([...selector.matchAll(/\.([_a-zA-Z][\w-]*)/g)].flatMap((match) => {
              const className = match[1]
              return className ? [className] : []
            })),
          ]
          const unowned = classes.filter((className) => !isUsed(className))
          if (unowned.length > 0) {
            violations.push(
              `${relative('.', path)}:${rule.source?.start?.line ?? 0} ${selector} -> ${unowned.join(', ')}`,
            )
          }
        }
      })
    }

    expect(violations, `unowned CSS selector branches (${violations.length})`).toEqual([])
  })
})
