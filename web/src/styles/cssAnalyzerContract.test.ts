/// <reference types="node" />

import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { afterEach, describe, expect, it } from 'vitest'

const analyzerPath = resolve('..', 'scripts', 'analyze-web-css.mjs')
const temporaryRoots: string[] = []

type FixtureOptions = {
  budget?: Record<string, number>
  owners?: Record<string, string[]>
}

function writeFixtureFile(root: string, relativePath: string, contents: string) {
  const path = resolve(root, relativePath)
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(path, contents)
}

function createFixture(options: FixtureOptions = {}) {
  const root = mkdtempSync(resolve(tmpdir(), 'houfeng-css-analyzer-'))
  temporaryRoots.push(root)

  writeFixtureFile(
    root,
    'web/src/index.css',
    "@import './styles/shared.css';\n@import './styles/assets.css';\n",
  )
  writeFixtureFile(
    root,
    'web/src/styles/shared.css',
    [
      '.shared { color: #fff; display: block !important; }',
      '@media (max-width: 40rem) { .shared { color: var(--text-primary); } }',
      '',
    ].join('\n'),
  )
  writeFixtureFile(root, 'web/src/styles/assets.css', '.asset { color: rgb(1 2 3); }\n')
  writeFixtureFile(
    root,
    'web/src/pages/LoginPage.css',
    '.login { color: var(--text-primary); display: block; }\n',
  )
  writeFixtureFile(root, 'web/dist/assets/index-fixture.css', '.built{color:#fff}\n')
  writeFixtureFile(root, 'web/dist/assets/LoginPage-fixture.css', '.login{color:#000}\n')

  const owners = options.owners ?? {
    'app-shell': [],
    dashboard: [],
    assets: ['src/styles/assets.css'],
    vps: [],
    observability: [],
    'settings-subscriptions': [],
    'shared-atoms-page': [
      'src/index.css',
      'src/styles/shared.css',
      'src/pages/LoginPage.css',
    ],
  }
  writeFixtureFile(root, 'web/css-owners.json', `${JSON.stringify({ version: 1, owners }, null, 2)}\n`)

  const budget = options.budget ?? {
    sourceFilesMax: 4,
    sourceBytesMax: 1_000,
    rulesMax: 5,
    declarationsMax: 6,
    repeatedSelectorTextsMax: 1,
    literalColorDeclarationsMax: 2,
    importantDeclarationsMax: 1,
    productionCssBytesMax: 1_000,
    productionCssGzipBytesMax: 1_000,
  }
  writeFixtureFile(root, 'web/css-budget.json', `${JSON.stringify({ version: 1, limits: budget }, null, 2)}\n`)

  return root
}

function runAnalyzer(root: string) {
  return spawnSync(
    process.execPath,
    [
      analyzerPath,
      '--web-root',
      resolve(root, 'web'),
      '--owners',
      resolve(root, 'web/css-owners.json'),
      '--budget',
      resolve(root, 'web/css-budget.json'),
      '--dist',
      resolve(root, 'web/dist'),
      '--format',
      'json',
    ],
    { encoding: 'utf8' },
  )
}

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) {
    rmSync(root, { recursive: true, force: true })
  }
})

describe('CSS analyzer CLI contract', () => {
  it('allows only the split login bundle to repeat a selector in the same context', () => {
    const emptyDist = mkdtempSync(resolve(tmpdir(), 'houfeng-css-source-contract-'))
    temporaryRoots.push(emptyDist)
    const result = spawnSync(
      process.execPath,
      [analyzerPath, '--dist', emptyDist, '--format', 'json'],
      { encoding: 'utf8' },
    )

    expect(result.stderr).toBe('')
    expect(result.status).toBe(0)

    const report = JSON.parse(result.stdout) as {
      selectorContexts: Array<{
        selector: string
        context: string
        file: string
        line: number
      }>
    }
    const groups = new Map<string, typeof report.selectorContexts>()
    for (const entry of report.selectorContexts) {
      const key = `${entry.context}\u0000${entry.selector}`
      groups.set(key, [...(groups.get(key) ?? []), entry])
    }
    const duplicates = [...groups.values()]
      .filter((entries) => entries.length > 1)
      .filter((entries) => {
        const [first] = entries
        return !(
          first.context === 'root' &&
          first.selector === '.login-page' &&
          entries.map((entry) => entry.file).sort().join(',') ===
            'src/pages/LoginPage.css,src/styles/partials/page.css'
        )
      })
      .map((entries) => entries.map(({ selector, context, file, line }) => ({
        selector,
        context,
        file,
        line,
      })))

    expect(duplicates).toEqual([])
  })

  it('reports deterministic AST, owner, context, and production inventories', () => {
    const root = createFixture()
    const result = runAnalyzer(root)

    expect(result.stderr).toBe('')
    expect(result.status).toBe(0)

    const report = JSON.parse(result.stdout) as {
      source: {
        files: number
        bytes: number
        rules: number
        declarations: number
        repeatedSelectorTexts: number
        literalColorDeclarations: number
        importantDeclarations: number
      }
      owners: Array<{ owner: string; files: string[]; rules: number }>
      repeatedSelectors: Array<{
        selector: string
        occurrences: number
        contexts: string[]
      }>
      production: { files: number; rawBytes: number; gzipBytes: number }
      budget: { status: string; violations: unknown[] }
    }

    const sourceFiles = [
      'web/src/index.css',
      'web/src/styles/shared.css',
      'web/src/styles/assets.css',
      'web/src/pages/LoginPage.css',
    ]
    const expectedBytes = sourceFiles.reduce(
      (total, path) => total + Buffer.byteLength(readFileSync(resolve(root, path))),
      0,
    )
    const productionFiles = [
      'web/dist/assets/index-fixture.css',
      'web/dist/assets/LoginPage-fixture.css',
    ]

    expect(report.source).toEqual({
      files: 4,
      bytes: expectedBytes,
      rules: 4,
      declarations: 6,
      repeatedSelectorTexts: 1,
      literalColorDeclarations: 2,
      importantDeclarations: 1,
    })
    expect(report.owners).toContainEqual({
      owner: 'assets',
      files: ['src/styles/assets.css'],
      rules: 1,
    })
    expect(report.repeatedSelectors).toContainEqual({
      selector: '.shared',
      occurrences: 2,
      contexts: ['root', '@media (max-width: 40rem)'],
    })
    expect(report.production.files).toBe(2)
    expect(report.production.rawBytes).toBe(
      productionFiles.reduce(
        (total, path) => total + Buffer.byteLength(readFileSync(resolve(root, path))),
        0,
      ),
    )
    expect(report.production.gzipBytes).toBeGreaterThan(0)
    expect(report.budget).toEqual({ status: 'pass', violations: [] })
  })

  it('fails closed when a production stylesheet has no unique owner', () => {
    const root = createFixture({
      owners: {
        'app-shell': [],
        dashboard: [],
        assets: [],
        vps: [],
        observability: [],
        'settings-subscriptions': [],
        'shared-atoms-page': [
          'src/index.css',
          'src/styles/shared.css',
          'src/pages/LoginPage.css',
        ],
      },
    })
    const result = runAnalyzer(root)

    expect(result.status).toBe(1)
    expect(result.stderr).toContain('src/styles/assets.css')
    expect(result.stderr).toContain('exactly one owner')
  })

  it('fails with metric evidence when a checked budget is exceeded', () => {
    const root = createFixture({
      budget: {
        sourceFilesMax: 4,
        sourceBytesMax: 1,
        rulesMax: 5,
        declarationsMax: 6,
        repeatedSelectorTextsMax: 1,
        literalColorDeclarationsMax: 2,
        importantDeclarationsMax: 1,
        productionCssBytesMax: 1_000,
        productionCssGzipBytesMax: 1_000,
      },
    })
    const result = runAnalyzer(root)

    expect(result.status).toBe(1)
    expect(result.stderr).toContain('sourceBytes')
    expect(result.stderr).toContain('actual=')
    expect(result.stderr).toContain('max=1')
  })
})
