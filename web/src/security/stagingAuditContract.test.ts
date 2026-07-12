import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

import { shouldIgnoreResourceConsoleError } from '../../e2e/staging/audit'

const RESOURCE_ERROR_401 =
  'Failed to load resource: the server responded with a status of 401 ()'

function auditedRouteNavigationPositions() {
  const path = resolve(process.cwd(), 'e2e/staging/staging-smoke.spec.ts')
  const source = readFileSync(path, 'utf8')
  const sourceFile = ts.createSourceFile(
    path,
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  )
  let routeFunction: ts.FunctionDeclaration | undefined

  sourceFile.forEachChild((node) => {
    if (ts.isFunctionDeclaration(node) && node.name?.text === 'gotoAuditedRoute') {
      routeFunction = node
    }
  })
  if (!routeFunction?.body) throw new Error('gotoAuditedRoute function was not found')

  let gotoPosition: number | undefined
  const fontWaitPositions: number[] = []
  function visit(node: ts.Node) {
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
      const receiver = node.expression.expression.getText(sourceFile)
      const method = node.expression.name.text
      if (receiver === 'page' && method === 'goto') gotoPosition ??= node.getStart(sourceFile)
      if (
        receiver === 'page'
        && method === 'evaluate'
        && node.getText(sourceFile).includes('document.fonts.ready')
      ) {
        fontWaitPositions.push(node.getStart(sourceFile))
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(routeFunction.body)
  if (gotoPosition == null) throw new Error('gotoAuditedRoute does not navigate with page.goto')
  return { gotoPosition, fontWaitPositions }
}

describe('staging audit contracts', () => {
  it('settles the current document fonts before starting audited route navigation', () => {
    const { gotoPosition, fontWaitPositions } = auditedRouteNavigationPositions()

    expect(fontWaitPositions.some((position) => position < gotoPosition)).toBe(true)
  })

  it('ignores a locationless Chromium console error for an explicitly allowed status', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/auth/me 401']),
      RESOURCE_ERROR_401,
      '',
    )).toBe(true)
  })

  it('keeps exact path matching when Chromium provides a console location', () => {
    const allowed = new Set(['/api/auth/me 401'])

    expect(shouldIgnoreResourceConsoleError(
      allowed,
      RESOURCE_ERROR_401,
      'https://staging.example/api/auth/me',
    )).toBe(true)
    expect(shouldIgnoreResourceConsoleError(
      allowed,
      RESOURCE_ERROR_401,
      'https://staging.example/api/private-data',
    )).toBe(false)
  })

  it('does not ignore a locationless status that was not explicitly allowed', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/vps 503']),
      RESOURCE_ERROR_401,
      '',
    )).toBe(false)
  })

  it('does not turn arbitrary console errors into allowed resource failures', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/auth/me 401']),
      'Failed to load resource: net::ERR_CONNECTION_RESET',
      '',
    )).toBe(false)
  })
})
