import { readFileSync, readdirSync } from 'node:fs'
import { join, relative, resolve, sep } from 'node:path'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const WEB_ROOT = process.cwd()
const PRODUCTION_ROOT = resolve(WEB_ROOT, 'src')
const NON_SEMANTIC_CLICK_TAGS = new Set([
  'article',
  'div',
  'label',
  'li',
  'p',
  'section',
  'span',
  'td',
  'tr',
])
const APPROVED_REASONS = [
  'modal-backdrop',
  'event-propagation',
  'keyboard-complete-row',
  'primary-link-row-enhancement',
] as const
const APPROVED_REASON_SET = new Set<string>(APPROVED_REASONS)
const REASON_PATTERN = /a11y-allow-nonsemantic-click:\s*([a-z-]+)/

type AuditEntry = {
  path: string
  line: number
  tag: string
  reason: string | null
}

type AuditResult = {
  allowed: AuditEntry[]
  violations: AuditEntry[]
}

function previousJsxReason(node: ts.JsxElement | ts.JsxSelfClosingElement, sourceFile: ts.SourceFile) {
  const parent = node.parent
  if (!ts.isJsxElement(parent) && !ts.isJsxFragment(parent)) return null

  const index = parent.children.findIndex((child) => child === node)
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const sibling = parent.children[cursor]
    if (!sibling) continue
    if (ts.isJsxText(sibling) && sibling.getText(sourceFile).trim() === '') continue
    if (!ts.isJsxExpression(sibling) || sibling.expression) return null
    return sibling.getFullText(sourceFile).match(REASON_PATTERN)?.[1] ?? null
  }
  return null
}

function auditSource(path: string, source: string): AuditResult {
  const sourceFile = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const result: AuditResult = { allowed: [], violations: [] }

  function visit(node: ts.Node) {
    if (ts.isJsxElement(node) || ts.isJsxSelfClosingElement(node)) {
      const opening = ts.isJsxElement(node) ? node.openingElement : node
      const tag = opening.tagName.getText(sourceFile)
      const hasOnClick = opening.attributes.properties.some(
        (attribute) => ts.isJsxAttribute(attribute) && attribute.name.getText(sourceFile) === 'onClick',
      )
      if (NON_SEMANTIC_CLICK_TAGS.has(tag) && hasOnClick) {
        const reason = previousJsxReason(node, sourceFile)
        const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1
        const entry = { path, line, tag, reason }
        if (reason && APPROVED_REASON_SET.has(reason)) result.allowed.push(entry)
        else result.violations.push(entry)
      }
    }
    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return result
}

function productionTsxFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = join(root, entry.name)
    if (entry.isDirectory()) return productionTsxFiles(path)
    return path.endsWith('.tsx') && !path.endsWith('.test.tsx') ? [path] : []
  })
}

function repositoryAudit(): AuditResult {
  return productionTsxFiles(PRODUCTION_ROOT).reduce<AuditResult>(
    (result, path) => {
      const source = readFileSync(path, 'utf8')
      const portablePath = relative(WEB_ROOT, path).split(sep).join('/')
      const current = auditSource(portablePath, source)
      result.allowed.push(...current.allowed)
      result.violations.push(...current.violations)
      return result
    },
    { allowed: [], violations: [] },
  )
}

function formatEntry(entry: AuditEntry) {
  const reason = entry.reason ? ` reason=${entry.reason}` : ''
  return `${entry.path}:${entry.line} <${entry.tag}>${reason}`
}

describe('semantic interaction AST audit', () => {
  it('accepts native interactive elements without a marker', () => {
    const result = auditSource('Native.tsx', 'export const Native = () => <button onClick={() => {}}>保存</button>')

    expect(result).toEqual({ allowed: [], violations: [] })
  })

  it('rejects an unmarked non-semantic click', () => {
    const result = auditSource('Broken.tsx', 'export const Broken = () => <div onClick={() => {}}>保存</div>')

    expect(result.violations).toEqual([{ path: 'Broken.tsx', line: 1, tag: 'div', reason: null }])
  })

  it('rejects unknown and non-adjacent marker reasons', () => {
    const unknown = auditSource(
      'Unknown.tsx',
      'export const Unknown = () => <>{/* a11y-allow-nonsemantic-click: convenience */}<div onClick={() => {}} /></>',
    )
    const nonAdjacent = auditSource(
      'NonAdjacent.tsx',
      'export const NonAdjacent = () => <>{/* a11y-allow-nonsemantic-click: event-propagation */}<span /><div onClick={() => {}} /></>',
    )

    expect(unknown.violations[0]?.reason).toBe('convenience')
    expect(nonAdjacent.violations[0]?.reason).toBeNull()
  })

  it.each(APPROVED_REASONS)('accepts the finite %s reason', (reason) => {
    const result = auditSource(
      'Approved.tsx',
      `export const Approved = () => <>{/* a11y-allow-nonsemantic-click: ${reason} */}<div onClick={() => {}} /></>`,
    )

    expect(result.violations).toEqual([])
    expect(result.allowed[0]?.reason).toBe(reason)
  })

  it('has no unexplained production interactions and keeps the allowlist bounded', () => {
    const result = repositoryAudit()

    expect(result.violations.map(formatEntry)).toEqual([])
    expect(result.allowed.map(formatEntry)).toHaveLength(7)
  })
})
