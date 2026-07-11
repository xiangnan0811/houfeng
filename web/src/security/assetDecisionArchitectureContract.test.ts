import { posix } from 'node:path'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const rawRouteSources = import.meta.glob('../pages/AssetDecisionsPage*.tsx', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const rawDomainSources = import.meta.glob('../pages/asset-decisions/**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const ROUTE_PAGE_PATH = 'src/pages/AssetDecisionsPage.tsx'
const DOMAIN_ROOT = 'src/pages/asset-decisions/'
const ROUTE_STATE_PATH = DOMAIN_ROOT + 'hooks/useAssetDecisionRouteState.ts'
const UTILS_PATH = DOMAIN_ROOT + 'utils.ts'
const API_MODULE_KEY = 'src/lib/api'
const MAX_ROUTE_PAGE_LINES = 400
const MAX_CONTROLLER_LINES = 600
const MAX_PRODUCTION_LINES = 800
const MAX_CONTROLLER_EFFECTS = 3

const CONTROLLER_CONFIG = [
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionGroups.ts',
    owner: 'groups',
    api: ['listAssetDecisionGroups', 'getAssetDecisionGroup'],
  },
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionManualGroups.ts',
    owner: 'manual-groups',
    api: [
      'listAssetDecisionManualGroups',
      'getAssetDecisionManualGroup',
      'listVPSAssets',
      'createAssetDecisionManualGroup',
      'createManualGroupFromScenarioTemplate',
      'patchAssetDecisionManualGroup',
      'addAssetDecisionManualGroupMember',
      'deleteAssetDecisionManualGroupMember',
    ],
  },
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionPortfolio.ts',
    owner: 'portfolio',
    api: ['getAssetDecisionOverview'],
  },
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionRecords.ts',
    owner: 'records',
    api: [
      'listAssetDecisionRecords',
      'getAssetDecisionRecord',
      'createAssetDecisionRecord',
      'patchAssetDecisionRecord',
    ],
  },
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionRenewalQueue.ts',
    owner: 'renewal-queue',
    api: ['listSubscriptions', 'listVPSAssets', 'updateVPSAsset'],
  },
  {
    path: ROUTE_STATE_PATH,
    owner: 'route-state',
    api: [],
  },
  {
    path: DOMAIN_ROOT + 'hooks/useAssetDecisionTemplates.ts',
    owner: 'templates',
    api: [
      'listAssetDecisionScenarioTemplates',
      'getAssetDecisionScenarioTemplate',
      'createAssetDecisionScenarioTemplate',
      'patchAssetDecisionScenarioTemplate',
    ],
  },
] as const

const EXPECTED_CONTROLLER_PATHS = CONTROLLER_CONFIG.map((entry) => entry.path).sort()
const CONTROLLER_PATH_SET = new Set<string>(EXPECTED_CONTROLLER_PATHS)
const CONTROLLER_MODULE_SET = new Set<string>(EXPECTED_CONTROLLER_PATHS.map(stripSourceExtension))
const CONTROLLER_OWNER_BY_PATH = new Map<string, string>(
  CONTROLLER_CONFIG.map((entry) => [entry.path, entry.owner]),
)
const API_SYMBOL_ALLOWLIST = new Map<string, ReadonlySet<string>>(
  CONTROLLER_CONFIG.map((entry) => [entry.path, new Set<string>(entry.api)]),
)
API_SYMBOL_ALLOWLIST.set(UTILS_PATH, new Set(['ApiError']))

const ROUTER_HOOKS = new Set(['useNavigate', 'useSearchParams'])
const REACT_SETTER_TYPES = new Set(['Dispatch', 'SetStateAction'])
const PRESENTATION_DIRECTORIES = [
  DOMAIN_ROOT + 'components/',
  DOMAIN_ROOT + 'modals/',
  DOMAIN_ROOT + 'modal-content/',
]
const PRESENTATION_ROOT_MODULES = new Set([
  DOMAIN_ROOT + 'AssetDecisionSecondaryNav',
  DOMAIN_ROOT + 'renderHelpers',
  DOMAIN_ROOT + 'tableColumns',
])

type SourceMap = Readonly<Record<string, string>>

type ArchitectureViolation = Readonly<{
  path: string
  line: number
  rule: string
  detail: string
}>

type ImportBinding = Readonly<{
  imported: string
  local: string
  node: ts.Node
}>

type StaticModuleEdge = Readonly<{
  moduleSpecifier: string
  node: ts.Node
  bindings: readonly ImportBinding[]
}>

function stripSourceExtension(path: string): string {
  for (const extension of ['.tsx', '.ts', '.jsx', '.js']) {
    if (path.endsWith(extension)) return path.slice(0, -extension.length)
  }
  return path
}

function sourceLineCount(source: string): number {
  if (source.length === 0) return 0
  let count = 1
  for (const character of source) {
    if (character === '\n') count += 1
  }
  return source.endsWith('\n') ? count - 1 : count
}

function isTestSupportPath(path: string): boolean {
  const basename = posix.basename(path)
  return basename.endsWith('.test.ts') ||
    basename.endsWith('.test.tsx') ||
    basename === 'testFixtures.ts'
}

function webRelativePath(globPath: string): string {
  if (globPath.startsWith('../')) return posix.normalize('src/' + globPath.slice(3))
  return posix.normalize(globPath)
}

function repositorySources(): SourceMap {
  return Object.fromEntries(
    Object.entries({ ...rawRouteSources, ...rawDomainSources })
      .map(([path, source]) => [webRelativePath(path), source] as const)
      .filter(([path]) => !isTestSupportPath(path)),
  )
}

function isControllerPath(path: string): boolean {
  return CONTROLLER_PATH_SET.has(path)
}

function controllerPathForModule(moduleKey: string): string | null {
  if (!CONTROLLER_MODULE_SET.has(moduleKey)) return null
  return EXPECTED_CONTROLLER_PATHS.find((path) => stripSourceExtension(path) === moduleKey) ?? null
}

function controllerOwner(path: string): string {
  return CONTROLLER_OWNER_BY_PATH.get(path) ?? 'unapproved'
}

function isPotentialControllerEntry(path: string): boolean {
  const basename = posix.basename(path)
  return posix.dirname(path) === DOMAIN_ROOT.slice(0, -1) + '/hooks' &&
    basename.startsWith('useAssetDecision') &&
    (basename.endsWith('.ts') || basename.endsWith('.tsx'))
}

function isPotentialRouteEntry(path: string): boolean {
  const basename = posix.basename(path)
  return posix.dirname(path) === 'src/pages' &&
    basename.startsWith('AssetDecisionsPage') &&
    basename.endsWith('.tsx')
}

function isPresentationPath(path: string, sourceIndex: ReadonlyMap<string, string>): boolean {
  const moduleKey = stripSourceExtension(path)
  const indexedPath = sourceIndex.get(moduleKey)
  if (indexedPath?.startsWith(DOMAIN_ROOT) && indexedPath.endsWith('.tsx')) return true
  if (PRESENTATION_ROOT_MODULES.has(moduleKey)) return true
  return PRESENTATION_DIRECTORIES.some((directory) => path.startsWith(directory))
}

function moduleTargetKey(importerPath: string, moduleSpecifier: string): string {
  if (!moduleSpecifier.startsWith('.')) return stripSourceExtension(moduleSpecifier)
  return stripSourceExtension(
    posix.normalize(posix.join(posix.dirname(importerPath), moduleSpecifier)),
  )
}

function collectStaticModuleEdges(sourceFile: ts.SourceFile): StaticModuleEdge[] {
  const edges: StaticModuleEdge[] = []

  for (const statement of sourceFile.statements) {
    if (ts.isImportDeclaration(statement) && ts.isStringLiteralLike(statement.moduleSpecifier)) {
      const bindings: ImportBinding[] = []
      const clause = statement.importClause
      if (clause?.name) {
        bindings.push({ imported: 'default', local: clause.name.text, node: clause.name })
      }
      if (clause?.namedBindings && ts.isNamespaceImport(clause.namedBindings)) {
        bindings.push({
          imported: '*',
          local: clause.namedBindings.name.text,
          node: clause.namedBindings,
        })
      }
      if (clause?.namedBindings && ts.isNamedImports(clause.namedBindings)) {
        for (const element of clause.namedBindings.elements) {
          bindings.push({
            imported: element.propertyName?.text ?? element.name.text,
            local: element.name.text,
            node: element,
          })
        }
      }
      if (bindings.length === 0) {
        bindings.push({
          imported: '(side-effect)',
          local: '',
          node: statement.moduleSpecifier,
        })
      }
      edges.push({
        moduleSpecifier: statement.moduleSpecifier.text,
        node: statement,
        bindings,
      })
      continue
    }

    if (ts.isExportDeclaration(statement) && statement.moduleSpecifier &&
      ts.isStringLiteralLike(statement.moduleSpecifier)) {
      const bindings: ImportBinding[] = []
      if (statement.exportClause && ts.isNamedExports(statement.exportClause)) {
        for (const element of statement.exportClause.elements) {
          bindings.push({
            imported: element.propertyName?.text ?? element.name.text,
            local: element.name.text,
            node: element,
          })
        }
      } else {
        bindings.push({ imported: '*', local: '*', node: statement })
      }
      edges.push({
        moduleSpecifier: statement.moduleSpecifier.text,
        node: statement,
        bindings,
      })
    }
  }

  return edges
}

function formatViolation(violation: ArchitectureViolation): string {
  return violation.path + ':' + violation.line +
    ' [' + violation.rule + '] ' + violation.detail
}

function auditSources(sources: SourceMap): ArchitectureViolation[] {
  const normalizedSources = new Map<string, string>(
    Object.entries(sources).map(([path, source]) => [posix.normalize(path), source]),
  )
  const sourceIndex = new Map<string, string>()
  for (const path of normalizedSources.keys()) {
    sourceIndex.set(stripSourceExtension(path), path)
  }
  const violations: ArchitectureViolation[] = []

  function addViolation(
    path: string,
    sourceFile: ts.SourceFile | null,
    node: ts.Node | null,
    rule: string,
    detail: string,
    fallbackLine = 1,
  ) {
    const line = sourceFile && node
      ? sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1
      : fallbackLine
    violations.push({ path, line, rule, detail })
  }

  const actualControllers = [...normalizedSources.keys()]
    .filter(isPotentialControllerEntry)
    .sort()
  for (const expectedPath of EXPECTED_CONTROLLER_PATHS) {
    if (!actualControllers.includes(expectedPath)) {
      addViolation(
        expectedPath,
        null,
        null,
        'controller-entry',
        'symbol=' + posix.basename(expectedPath, '.ts') + ' missing',
      )
    }
  }
  for (const actualPath of actualControllers) {
    if (!CONTROLLER_PATH_SET.has(actualPath)) {
      addViolation(
        actualPath,
        null,
        null,
        'controller-entry',
        'symbol=' + stripSourceExtension(posix.basename(actualPath)) + ' unexpected',
      )
    }
  }

  const actualRouteEntries = [...normalizedSources.keys()]
    .filter(isPotentialRouteEntry)
    .sort()
  if (!actualRouteEntries.includes(ROUTE_PAGE_PATH)) {
    addViolation(
      ROUTE_PAGE_PATH,
      null,
      null,
      'route-entry',
      'symbol=AssetDecisionsPage missing',
    )
  }
  for (const routePath of actualRouteEntries) {
    if (routePath !== ROUTE_PAGE_PATH) {
      addViolation(
        routePath,
        null,
        null,
        'route-entry',
        'symbol=' + stripSourceExtension(posix.basename(routePath)) + ' unexpected',
      )
    }
  }

  for (const [path, source] of normalizedSources) {
    const basename = posix.basename(path)
    if (basename.endsWith('PageContent.tsx')) {
      addViolation(
        path,
        null,
        null,
        'page-content',
        'symbol=' + stripSourceExtension(basename) + ' forbidden',
      )
    }

    const lines = sourceLineCount(source)
    if (lines > MAX_PRODUCTION_LINES) {
      addViolation(
        path,
        null,
        null,
        'line-budget',
        'budget=production-lines actual=' + lines + ' limit=' + MAX_PRODUCTION_LINES,
        MAX_PRODUCTION_LINES + 1,
      )
    }
    if (path === ROUTE_PAGE_PATH && lines > MAX_ROUTE_PAGE_LINES) {
      addViolation(
        path,
        null,
        null,
        'line-budget',
        'budget=route-page-lines actual=' + lines + ' limit=' + MAX_ROUTE_PAGE_LINES,
        MAX_ROUTE_PAGE_LINES + 1,
      )
    }
    if (isControllerPath(path) && lines > MAX_CONTROLLER_LINES) {
      addViolation(
        path,
        null,
        null,
        'line-budget',
        'budget=controller-lines actual=' + lines + ' limit=' + MAX_CONTROLLER_LINES,
        MAX_CONTROLLER_LINES + 1,
      )
    }

    const sourceFile = ts.createSourceFile(
      path,
      source,
      ts.ScriptTarget.Latest,
      true,
      path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    )
    const edges = collectStaticModuleEdges(sourceFile)
    const effectLocals = new Set<string>()
    const reactObjects = new Set<string>()
    const reactSetterLocals = new Map<string, string>()
    const routerLocals = new Map<string, string>()

    for (const edge of edges) {
      const targetKey = moduleTargetKey(path, edge.moduleSpecifier)
      const targetPath = sourceIndex.get(targetKey) ?? targetKey
      const targetController = controllerPathForModule(targetKey)
      const sourceIsController = isControllerPath(path)
      const sourceIsPresentation = isPresentationPath(path, sourceIndex)
      const targetIsPresentation = isPresentationPath(targetPath, sourceIndex)
      const targetsAPI = targetKey === API_MODULE_KEY

      if (edge.moduleSpecifier === 'react') {
        for (const binding of edge.bindings) {
          if (binding.imported === 'useEffect') effectLocals.add(binding.local)
          if (binding.imported === '*' || binding.imported === 'default') {
            reactObjects.add(binding.local)
          }
          if (REACT_SETTER_TYPES.has(binding.imported)) {
            reactSetterLocals.set(binding.local, binding.imported)
          }
        }
      }

      if (edge.moduleSpecifier === 'react-router-dom') {
        for (const binding of edge.bindings) {
          if (!ROUTER_HOOKS.has(binding.imported)) continue
          routerLocals.set(binding.local, binding.imported)
          if (path !== ROUTE_STATE_PATH) {
            addViolation(
              path,
              sourceFile,
              binding.node,
              'router-owner',
              'symbol=' + binding.imported + ' import owner=' +
                (path === ROUTE_PAGE_PATH ? 'route-page' : controllerOwner(path)),
            )
          }
        }
      }

      if (targetsAPI) {
        const allowedSymbols = API_SYMBOL_ALLOWLIST.get(path)
        for (const binding of edge.bindings) {
          if (!allowedSymbols?.has(binding.imported)) {
            const owner = path === ROUTE_PAGE_PATH
              ? 'route-page'
              : sourceIsPresentation
                ? 'presentation'
                : path === UTILS_PATH
                  ? 'utils'
                  : controllerOwner(path)
            addViolation(
              path,
              sourceFile,
              binding.node,
              'api-owner',
              'symbol=' + binding.imported + ' owner=' + owner,
            )
          }
        }
        if (sourceIsPresentation) {
          addViolation(
            path,
            sourceFile,
            edge.node,
            'forbidden-edge',
            'edge=presentation->api target=' + API_MODULE_KEY,
          )
        }
        if (path === ROUTE_PAGE_PATH) {
          addViolation(
            path,
            sourceFile,
            edge.node,
            'forbidden-edge',
            'edge=route-page->api target=' + API_MODULE_KEY,
          )
        }
      }

      if (sourceIsController && targetController) {
        addViolation(
          path,
          sourceFile,
          edge.node,
          'forbidden-edge',
          'edge=controller->controller target=' + targetController,
        )
      }
      if (sourceIsController && targetIsPresentation) {
        addViolation(
          path,
          sourceFile,
          edge.node,
          'forbidden-edge',
          'edge=controller->presentation target=' + targetPath,
        )
      }
      if (sourceIsController && targetPath === ROUTE_PAGE_PATH) {
        addViolation(
          path,
          sourceFile,
          edge.node,
          'forbidden-edge',
          'edge=controller->route-page target=' + ROUTE_PAGE_PATH,
        )
      }
      if (sourceIsPresentation && targetController) {
        addViolation(
          path,
          sourceFile,
          edge.node,
          'forbidden-edge',
          'edge=presentation->controller target=' + targetController,
        )
      }
    }

    const effectCalls: ts.CallExpression[] = []

    function visit(node: ts.Node) {
      if (ts.isCallExpression(node)) {
        let effectCall = false
        let routerSymbol: string | null = null

        if (ts.isIdentifier(node.expression)) {
          effectCall = effectLocals.has(node.expression.text)
          routerSymbol = routerLocals.get(node.expression.text) ?? null
        } else if (
          ts.isPropertyAccessExpression(node.expression) &&
          ts.isIdentifier(node.expression.expression) &&
          reactObjects.has(node.expression.expression.text)
        ) {
          effectCall = node.expression.name.text === 'useEffect'
        }

        if (effectCall) {
          effectCalls.push(node)
          if (path === ROUTE_PAGE_PATH) {
            addViolation(
              path,
              sourceFile,
              node,
              'route-page',
              'symbol=useEffect forbidden',
            )
          }
        }

        if (routerSymbol && path !== ROUTE_STATE_PATH) {
          addViolation(
            path,
            sourceFile,
            node,
            'router-owner',
            'symbol=' + routerSymbol + ' call owner=' +
              (path === ROUTE_PAGE_PATH ? 'route-page' : controllerOwner(path)),
          )
          if (path === ROUTE_PAGE_PATH && routerSymbol === 'useSearchParams') {
            addViolation(
              path,
              sourceFile,
              node,
              'route-page',
              'symbol=useSearchParams forbidden',
            )
          }
        }
      }

      if (path === ROUTE_PAGE_PATH && ts.isTypeReferenceNode(node)) {
        if (ts.isIdentifier(node.typeName)) {
          const importedType = reactSetterLocals.get(node.typeName.text)
          if (importedType) {
            addViolation(
              path,
              sourceFile,
              node.typeName,
              'route-page',
              'symbol=' + importedType + ' forbidden',
            )
          }
        } else if (
          ts.isQualifiedName(node.typeName) &&
          ts.isIdentifier(node.typeName.left) &&
          reactObjects.has(node.typeName.left.text) &&
          REACT_SETTER_TYPES.has(node.typeName.right.text)
        ) {
          addViolation(
            path,
            sourceFile,
            node.typeName,
            'route-page',
            'symbol=React.' + node.typeName.right.text + ' forbidden',
          )
        }
      }

      ts.forEachChild(node, visit)
    }

    visit(sourceFile)

    if (isControllerPath(path) && effectCalls.length > MAX_CONTROLLER_EFFECTS) {
      const firstOverBudgetEffect = effectCalls[MAX_CONTROLLER_EFFECTS]
      if (!firstOverBudgetEffect) continue
      addViolation(
        path,
        sourceFile,
        firstOverBudgetEffect,
        'effect-budget',
        'budget=useEffect actual=' + effectCalls.length + ' limit=' + MAX_CONTROLLER_EFFECTS,
      )
    }
  }

  return violations.sort((left, right) => (
    left.path.localeCompare(right.path) ||
    left.line - right.line ||
    left.rule.localeCompare(right.rule) ||
    left.detail.localeCompare(right.detail)
  ))
}

function validSyntheticSources(): Record<string, string> {
  const sources: Record<string, string> = {
    [ROUTE_PAGE_PATH]: 'export function AssetDecisionsPage() { return null }\n',
    [DOMAIN_ROOT + 'components/AssetDecisionPageView.tsx']:
      'export function AssetDecisionPageView() { return <main /> }\n',
    [UTILS_PATH]: [
      "import { ApiError } from '../../lib/api'",
      'export function describe(error: unknown) { return error instanceof ApiError }',
    ].join('\n'),
  }

  for (const path of EXPECTED_CONTROLLER_PATHS) {
    const hookName = posix.basename(path, '.ts')
    sources[path] =
      'export function ' + hookName + '() { return { state: {}, commands: {} } }\n'
  }
  sources[DOMAIN_ROOT + 'hooks/useAssetDecisionGroups.ts'] = [
    "import { getAssetDecisionGroup } from '../../../lib/api'",
    'export function useAssetDecisionGroups() {',
    '  void getAssetDecisionGroup',
    '  return { state: {}, commands: {} }',
    '}',
  ].join('\n')
  sources[ROUTE_STATE_PATH] = [
    "import { useNavigate as navigateHook, useSearchParams as paramsHook } from 'react-router-dom'",
    'export function useAssetDecisionRouteState() {',
    '  const navigate = navigateHook()',
    '  const params = paramsHook()',
    '  return { state: { navigate, params }, commands: {} }',
    '}',
  ].join('\n')
  return sources
}

function padSource(source: string, targetLines: number): string {
  const lines = source.split('\n')
  if (lines.length > targetLines) throw new Error('source already exceeds target line count')
  while (lines.length < targetLines) lines.push('// filler')
  return lines.join('\n')
}

describe('Asset Decisions architecture AST audit', () => {
  it('accepts the approved controller, API, router, and presentation graph', () => {
    expect(auditSources(validSyntheticSources()).map(formatViolation)).toEqual([])
  })

  it('reports a forbidden API symbol with its controller owner and source line', () => {
    const sources = validSyntheticSources()
    sources[DOMAIN_ROOT + 'hooks/useAssetDecisionGroups.ts'] =
      "import { updateVPSAsset } from '../../../lib/api'\n"

    expect(auditSources(sources).map(formatViolation)).toEqual([
      'src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts:1 [api-owner] symbol=updateVPSAsset owner=groups',
    ])
  })

  it('rejects controller and presentation imports in both forbidden directions', () => {
    const sources = validSyntheticSources()
    sources[DOMAIN_ROOT + 'hooks/useAssetDecisionGroups.ts'] = [
      "import { useAssetDecisionRecords } from './useAssetDecisionRecords'",
      "import { BrokenView } from '../components/BrokenView'",
      'export function useAssetDecisionGroups() {',
      '  return { state: { useAssetDecisionRecords, BrokenView }, commands: {} }',
      '}',
    ].join('\n')
    sources[DOMAIN_ROOT + 'components/BrokenView.tsx'] = [
      "import { useAssetDecisionGroups } from '../hooks/useAssetDecisionGroups'",
      "import { updateVPSAsset } from '../../../lib/api'",
      'export function BrokenView() {',
      '  return <div>{String(useAssetDecisionGroups)}{String(updateVPSAsset)}</div>',
      '}',
    ].join('\n')

    const failures = auditSources(sources).map(formatViolation)
    expect(failures).toEqual(expect.arrayContaining([
      'src/pages/asset-decisions/components/BrokenView.tsx:1 [forbidden-edge] edge=presentation->controller target=src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts',
      'src/pages/asset-decisions/components/BrokenView.tsx:2 [api-owner] symbol=updateVPSAsset owner=presentation',
      'src/pages/asset-decisions/components/BrokenView.tsx:2 [forbidden-edge] edge=presentation->api target=src/lib/api',
      'src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts:1 [forbidden-edge] edge=controller->controller target=src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts',
      'src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts:2 [forbidden-edge] edge=controller->presentation target=src/pages/asset-decisions/components/BrokenView.tsx',
    ]))
  })

  it('keeps router hooks owned by the route-state controller even when aliased', () => {
    const sources = validSyntheticSources()
    sources[DOMAIN_ROOT + 'hooks/useAssetDecisionRecords.ts'] = [
      "import { useNavigate as routeNavigate } from 'react-router-dom'",
      'export function useAssetDecisionRecords() {',
      '  const navigate = routeNavigate()',
      '  return { state: { navigate }, commands: {} }',
      '}',
    ].join('\n')

    expect(auditSources(sources).map(formatViolation)).toEqual(expect.arrayContaining([
      'src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts:1 [router-owner] symbol=useNavigate import owner=records',
      'src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts:3 [router-owner] symbol=useNavigate call owner=records',
    ]))
  })

  it('keeps effects, router state, and raw React setters out of the route page', () => {
    const sources = validSyntheticSources()
    sources[ROUTE_PAGE_PATH] = [
      "import React, { useEffect, type SetStateAction } from 'react'",
      "import { useSearchParams } from 'react-router-dom'",
      'type Update = SetStateAction<string>',
      'type Setter = React.Dispatch<Update>',
      'export function AssetDecisionsPage() {',
      '  useEffect(() => {}, [])',
      '  useSearchParams()',
      '  return null',
      '}',
    ].join('\n')

    const failures = auditSources(sources).map(formatViolation)
    expect(failures).toEqual(expect.arrayContaining([
      'src/pages/AssetDecisionsPage.tsx:2 [router-owner] symbol=useSearchParams import owner=route-page',
      'src/pages/AssetDecisionsPage.tsx:3 [route-page] symbol=SetStateAction forbidden',
      'src/pages/AssetDecisionsPage.tsx:4 [route-page] symbol=React.Dispatch forbidden',
      'src/pages/AssetDecisionsPage.tsx:6 [route-page] symbol=useEffect forbidden',
      'src/pages/AssetDecisionsPage.tsx:7 [route-page] symbol=useSearchParams forbidden',
      'src/pages/AssetDecisionsPage.tsx:7 [router-owner] symbol=useSearchParams call owner=route-page',
    ]))
  })

  it('rejects missing or unexpected controller entries and PageContent replacements', () => {
    const sources = validSyntheticSources()
    delete sources[DOMAIN_ROOT + 'hooks/useAssetDecisionPortfolio.ts']
    sources[DOMAIN_ROOT + 'hooks/useAssetDecisionEverything.ts'] =
      'export function useAssetDecisionEverything() { return null }\n'
    sources[DOMAIN_ROOT + 'components/ReplacementPageContent.tsx'] =
      'export function ReplacementPageContent() { return <main /> }\n'

    const failures = auditSources(sources).map(formatViolation)
    expect(failures).toEqual(expect.arrayContaining([
      'src/pages/asset-decisions/components/ReplacementPageContent.tsx:1 [page-content] symbol=ReplacementPageContent forbidden',
      'src/pages/asset-decisions/hooks/useAssetDecisionEverything.ts:1 [controller-entry] symbol=useAssetDecisionEverything unexpected',
      'src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts:1 [controller-entry] symbol=useAssetDecisionPortfolio missing',
    ]))
  })

  it('reports route, controller, production, and effect budgets at the violating line', () => {
    const sources = validSyntheticSources()
    sources[ROUTE_PAGE_PATH] = padSource(
      'export function AssetDecisionsPage() { return null }',
      MAX_ROUTE_PAGE_LINES + 1,
    )
    sources[DOMAIN_ROOT + 'hooks/useAssetDecisionManualGroups.ts'] = padSource([
      "import { useEffect } from 'react'",
      'export function useAssetDecisionManualGroups() {',
      '  useEffect(() => {}, [])',
      '  useEffect(() => {}, [])',
      '  useEffect(() => {}, [])',
      '  useEffect(() => {}, [])',
      '  return { state: {}, commands: {} }',
      '}',
    ].join('\n'), MAX_CONTROLLER_LINES + 1)
    sources[DOMAIN_ROOT + 'oversizedPureModel.ts'] = padSource(
      'export const oversizedPureModel = true',
      MAX_PRODUCTION_LINES + 1,
    )

    const failures = auditSources(sources).map(formatViolation)
    expect(failures).toEqual(expect.arrayContaining([
      'src/pages/AssetDecisionsPage.tsx:401 [line-budget] budget=route-page-lines actual=401 limit=400',
      'src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts:6 [effect-budget] budget=useEffect actual=4 limit=3',
      'src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts:601 [line-budget] budget=controller-lines actual=601 limit=600',
      'src/pages/asset-decisions/oversizedPureModel.ts:801 [line-budget] budget=production-lines actual=801 limit=800',
    ]))
  })

  it('keeps the repository production graph within every ownership and budget contract', () => {
    expect(auditSources(repositorySources()).map(formatViolation)).toEqual([])
  })
})
