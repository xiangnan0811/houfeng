import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const controllerSources = import.meta.glob('../pages/asset-decisions/hooks/useAssetDecision*.ts', {
  query: '?raw',
  import: 'default',
  eager: true,
})

const routePageSources = import.meta.glob([
  '../pages/AssetDecisionsPage*.tsx',
  '!../pages/AssetDecisionsPage*.test.tsx',
], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const EXPECTED_CONTROLLER_PATHS = [
  '../pages/asset-decisions/hooks/useAssetDecisionGroups.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRecords.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionRouteState.ts',
  '../pages/asset-decisions/hooks/useAssetDecisionTemplates.ts',
]

function auditRoutePage(path: string, source: string): string[] {
  const sourceFile = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const violations: string[] = []

  function addViolation(node: ts.Node, message: string) {
    const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1
    violations.push(`${path}:${line} ${message}`)
  }

  function visit(node: ts.Node) {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      if (node.moduleSpecifier.text.includes('/lib/api')) addViolation(node, 'imports lib/api')
    }
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
      if (node.expression.text === 'useEffect' || node.expression.text === 'useSearchParams') {
        addViolation(node, `calls ${node.expression.text}`)
      }
    }
    if (ts.isQualifiedName(node) && ts.isIdentifier(node.left) && node.left.text === 'React') {
      if (node.right.text === 'Dispatch' || node.right.text === 'SetStateAction') {
        addViolation(node, `uses React.${node.right.text}`)
      }
    }
    if (ts.isIdentifier(node) && node.text === 'SetStateAction') {
      addViolation(node, 'uses SetStateAction')
    }
    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return violations
}

describe('Asset Decisions architecture contract', () => {
  it('tracks every implemented controller entry point during migration', () => {
    expect(Object.keys(controllerSources).sort()).toEqual(EXPECTED_CONTROLLER_PATHS)
  })

  it('keeps one thin route page and forbids total-controller replacements', () => {
    expect(Object.keys(routePageSources).sort()).toEqual(['../pages/AssetDecisionsPage.tsx'])
    expect(
      Object.entries(routePageSources).flatMap(([path, source]) => auditRoutePage(path, source)),
    ).toEqual([])
  })
})
