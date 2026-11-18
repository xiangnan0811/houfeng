import { posix } from 'node:path'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const rawSources = import.meta.glob('../**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const RECORDS_API_PATH = 'src/lib/recordsApi.ts'
const RECORDS_API_MODULE = 'src/lib/recordsApi'
const EAGER_ROOTS = [
  'src/main.tsx',
  'src/app/router.tsx',
  'src/app/layout/AppShell.tsx',
  'src/app/layout/TopBar.tsx',
  'src/app/layout/Sidebar.tsx',
  'src/lib/api.ts',
] as const

type SourceMap = Readonly<Record<string, string>>

type ModuleEdge = Readonly<{
  specifier: string
  runtime: boolean
  line: number
}>

function sourcePath(globPath: string): string {
  return posix.normalize(globPath.startsWith('../') ? `src/${globPath.slice(3)}` : globPath)
}

function isProductionSource(path: string): boolean {
  const basename = posix.basename(path)
  return !basename.includes('.test.') && basename !== 'testFixtures.ts'
}

function repositorySources(): Record<string, string> {
  return Object.fromEntries(Object.entries(rawSources)
    .map(([path, source]) => [sourcePath(path), source] as const)
    .filter(([path]) => isProductionSource(path)))
}

function stripSourceExtension(path: string): string {
  for (const extension of ['.tsx', '.ts', '.jsx', '.js']) {
    if (path.endsWith(extension)) return path.slice(0, -extension.length)
  }
  return path
}

function sourceFile(path: string, source: string): ts.SourceFile {
  return ts.createSourceFile(
    path,
    source,
    ts.ScriptTarget.Latest,
    true,
    path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  )
}

function importDeclarationIsRuntime(statement: ts.ImportDeclaration): boolean {
  const clause = statement.importClause
  if (!clause) return true
  if (clause.isTypeOnly) return false
  if (clause.name || !clause.namedBindings || ts.isNamespaceImport(clause.namedBindings)) return true
  return clause.namedBindings.elements.some((element) => !element.isTypeOnly)
}

function moduleEdges(path: string, source: string): ModuleEdge[] {
  const parsed = sourceFile(path, source)
  const edges: ModuleEdge[] = []
  for (const statement of parsed.statements) {
    if (ts.isImportDeclaration(statement) && ts.isStringLiteralLike(statement.moduleSpecifier)) {
      edges.push({
        specifier: statement.moduleSpecifier.text,
        runtime: importDeclarationIsRuntime(statement),
        line: parsed.getLineAndCharacterOfPosition(statement.getStart(parsed)).line + 1,
      })
    }
    if (ts.isExportDeclaration(statement) && statement.moduleSpecifier &&
      ts.isStringLiteralLike(statement.moduleSpecifier)) {
      edges.push({
        specifier: statement.moduleSpecifier.text,
        runtime: !statement.isTypeOnly,
        line: parsed.getLineAndCharacterOfPosition(statement.getStart(parsed)).line + 1,
      })
    }
  }
  return edges
}

function resolveSourceModule(
  importer: string,
  specifier: string,
  sourceIndex: ReadonlyMap<string, string>,
): string | null {
  if (!specifier.startsWith('.')) return null
  const moduleKey = stripSourceExtension(posix.normalize(posix.join(posix.dirname(importer), specifier)))
  return sourceIndex.get(moduleKey) ?? sourceIndex.get(`${moduleKey}/index`) ?? null
}

function facadeViolations(source: string): string[] {
  const parsed = sourceFile(RECORDS_API_PATH, source)
  const violations: string[] = []

  function visit(node: ts.Node) {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'fetch') {
      const line = parsed.getLineAndCharacterOfPosition(node.getStart(parsed)).line + 1
      violations.push(`${RECORDS_API_PATH}:${line} raw-fetch forbidden`)
    }
    if (ts.isJsxElement(node) || ts.isJsxSelfClosingElement(node) || ts.isJsxFragment(node)) {
      const line = parsed.getLineAndCharacterOfPosition(node.getStart(parsed)).line + 1
      violations.push(`${RECORDS_API_PATH}:${line} JSX forbidden`)
    }
    ts.forEachChild(node, visit)
  }
  visit(parsed)

  return violations.sort()
}

function eagerImportViolations(sources: SourceMap): string[] {
  const normalized = new Map<string, string>(Object.entries(sources)
    .map(([path, source]) => [posix.normalize(path), source]))
  const sourceIndex = new Map<string, string>()
  for (const path of normalized.keys()) sourceIndex.set(stripSourceExtension(path), path)
  const edgeCache = new Map<string, readonly ModuleEdge[]>()
  const runtimeTargetCache = new Map<string, readonly string[]>()

  function edgesOf(path: string): readonly ModuleEdge[] {
    const cached = edgeCache.get(path)
    if (cached) return cached
    const source = normalized.get(path)
    const edges = source === undefined ? [] : moduleEdges(path, source)
    edgeCache.set(path, edges)
    return edges
  }

  function runtimeTargetsOf(path: string): readonly string[] {
    const cached = runtimeTargetCache.get(path)
    if (cached) return cached
    const targets = edgesOf(path)
      .filter((edge) => edge.runtime)
      .map((edge) => resolveSourceModule(path, edge.specifier, sourceIndex))
      .filter((target): target is string => target !== null)
    runtimeTargetCache.set(path, targets)
    return targets
  }

  const violations: string[] = []
  for (const root of EAGER_ROOTS) {
    const rootEdges = edgesOf(root)
    for (const edge of rootEdges) {
      if (resolveSourceModule(root, edge.specifier, sourceIndex) === RECORDS_API_PATH) {
        violations.push(`${root}:${edge.line} direct recordsApi import forbidden`)
      }
    }

    const pending: Array<{ path: string; chain: string[] }> = [{ path: root, chain: [root] }]
    const visited = new Set<string>()
    while (pending.length > 0) {
      const current = pending.shift()
      if (!current || visited.has(current.path)) continue
      visited.add(current.path)
      for (const target of runtimeTargetsOf(current.path)) {
        const chain = [...current.chain, target]
        if (stripSourceExtension(target) === RECORDS_API_MODULE) {
          violations.push(`${root}:1 eager chain=${chain.join(' -> ')}`)
          continue
        }
        pending.push({ path: target, chain })
      }
    }
  }
  return [...new Set(violations)].sort()
}

describe('Records transport architecture contract', () => {
  it('keeps raw fetch and JSX out of the transport façade', () => {
    const sources = repositorySources()
    expect(facadeViolations(sources[RECORDS_API_PATH] ?? '')).toEqual([])
  })

  it('keeps recordsApi out of the eager application and compatibility façade graph', () => {
    expect(eagerImportViolations(repositorySources())).toEqual([])
  })

  it('detects a transitive eager import and a direct compatibility re-export', () => {
    const sources = {
      'src/main.tsx': "import './app/eager'\n",
      'src/app/router.tsx': 'export const router = {}\n',
      'src/app/eager.ts': "import '../lib/recordsApi'\n",
      'src/app/layout/AppShell.tsx': 'export function AppShell() { return null }\n',
      'src/app/layout/TopBar.tsx': 'export function TopBar() { return null }\n',
      'src/app/layout/Sidebar.tsx': 'export function Sidebar() { return null }\n',
      'src/lib/api.ts': "export * from './recordsApi'\n",
      [RECORDS_API_PATH]: 'export function listRecords() { return null }\n',
    }

    expect(eagerImportViolations(sources)).toEqual([
      'src/lib/api.ts:1 direct recordsApi import forbidden',
      'src/lib/api.ts:1 eager chain=src/lib/api.ts -> src/lib/recordsApi.ts',
      'src/main.tsx:1 eager chain=src/main.tsx -> src/app/eager.ts -> src/lib/recordsApi.ts',
    ])
  })

  it('ignores unreachable and type-only paths while tolerating cycles and external imports', () => {
    const sources = {
      'src/main.tsx': "import React from 'react'\nimport './app/cycleA'\n",
      'src/app/cycleA.ts': "import './cycleB'\nimport type { Shape } from './typed'\n",
      'src/app/cycleB.ts': "import './cycleA'\nexport type { Shape } from './typed'\n",
      'src/app/typed.ts': "import '../lib/recordsApi'\nexport type Shape = object\n",
      'src/app/orphan.ts': "import '../lib/recordsApi'\n",
      'src/app/router.tsx': "import type { listRecords } from '../lib/recordsApi'\n",
      'src/app/layout/AppShell.tsx': 'export function AppShell() { return null }\n',
      'src/app/layout/TopBar.tsx': 'export function TopBar() { return null }\n',
      'src/app/layout/Sidebar.tsx': 'export function Sidebar() { return null }\n',
      'src/lib/api.ts': "import './missing'\n",
      [RECORDS_API_PATH]: 'export function listRecords() { return null }\n',
    }

    expect(eagerImportViolations(sources)).toEqual([
      'src/app/router.tsx:1 direct recordsApi import forbidden',
    ])
  })

})
