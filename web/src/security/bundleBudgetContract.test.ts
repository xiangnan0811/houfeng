import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, relative, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { build, type Plugin } from 'vite'
import { afterEach, describe, expect, it } from 'vitest'

const REPOSITORY_ROOT = resolve(process.cwd(), '..')
const WEB_ROOT = resolve(REPOSITORY_ROOT, 'web')
const CHECKER_PATH = resolve(REPOSITORY_ROOT, 'scripts/check-web-bundle-budget.mjs')
const RECORDS_LAZY_MODULES = [
  { name: 'apiError', path: resolve(WEB_ROOT, 'src/lib/apiError.ts') },
  { name: 'recordsApi', path: resolve(WEB_ROOT, 'src/lib/recordsApi.ts') },
] as const
const RECORDS_API_PATH = RECORDS_LAZY_MODULES[1].path
const temporaryRoots: string[] = []

type RecordsModulePlacement = {
  module: typeof RECORDS_LAZY_MODULES[number]['name']
  fileName: string
  isEntry: boolean
  isDynamicEntry: boolean
}

type FixtureOptions = {
  html?: string
  includeAsync?: boolean
  includeFonts?: boolean
  budget?: Record<string, number>
}

function createFixture(options: FixtureOptions = {}) {
  const root = mkdtempSync(join(tmpdir(), 'houfeng-bundle-budget-'))
  temporaryRoots.push(root)
  const dist = join(root, 'dist')
  const assets = join(dist, 'assets')
  const fonts = join(dist, 'fonts')
  mkdirSync(assets, { recursive: true })
  mkdirSync(fonts, { recursive: true })
  writeFileSync(
    join(dist, 'index.html'),
    options.html ?? '<script type="module" src="/assets/entry-abc.js"></script><link rel="stylesheet" href="/assets/entry-abc.css">',
  )
  writeFileSync(join(assets, 'entry-abc.js'), 'console.log("entry")')
  writeFileSync(join(assets, 'entry-abc.css'), '.root{display:block}')
  if (options.includeAsync ?? true) writeFileSync(join(assets, 'route-def.js'), 'export const route = true')
  if (options.includeFonts ?? true) writeFileSync(join(fonts, 'font.woff2'), 'font-data')
  const budgetPath = join(root, 'bundle-budget.json')
  writeFileSync(budgetPath, JSON.stringify(options.budget ?? {
    entryJsGzipBytes: 10_000,
    entryCssGzipBytes: 10_000,
    maxAsyncJsGzipBytes: 10_000,
    fontWoff2RawBytes: 10_000,
  }))
  return { dist, budgetPath }
}

function runChecker(dist: string, budgetPath: string, ...extraArgs: string[]) {
  return spawnSync(process.execPath, [
    CHECKER_PATH,
    '--dist', dist,
    '--budget', budgetPath,
    ...extraArgs,
  ], { encoding: 'utf8' })
}

function recordsModuleCapture(placements: RecordsModulePlacement[]): Plugin {
  return {
    name: 'records-module-capture',
    generateBundle(_options, bundle) {
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        const modulePaths = new Set(Object.keys(output.modules).map((moduleId) => moduleId.split('?')[0]))
        for (const recordsModule of RECORDS_LAZY_MODULES) {
          if (!modulePaths.has(recordsModule.path)) continue
          placements.push({
            module: recordsModule.name,
            fileName: output.fileName,
            isEntry: output.isEntry,
            isDynamicEntry: output.isDynamicEntry,
          })
        }
      }
    },
  }
}

function temporaryBuildRoot(prefix: string): string {
  const root = mkdtempSync(join(tmpdir(), prefix))
  temporaryRoots.push(root)
  return root
}

async function buildCurrentApplication(placements: RecordsModulePlacement[]) {
  const outputRoot = temporaryBuildRoot('houfeng-records-production-build-')
  await build({
    root: WEB_ROOT,
    configFile: resolve(WEB_ROOT, 'vite.config.ts'),
    mode: 'production',
    logLevel: 'silent',
    plugins: [recordsModuleCapture(placements)],
    build: {
      outDir: join(outputRoot, 'dist'),
      emptyOutDir: true,
    },
  })
}

async function buildSyntheticLazyConsumer(placements: RecordsModulePlacement[]) {
  const root = temporaryBuildRoot('houfeng-records-lazy-build-')
  const sourceRoot = join(root, 'src')
  mkdirSync(sourceRoot, { recursive: true })
  writeFileSync(
    join(root, 'index.html'),
    '<main>entry</main><script type="module" src="/src/main.ts"></script>',
  )
  const relativeRecordsApi = relative(sourceRoot, RECORDS_API_PATH)
  const recordsApiSpecifier = relativeRecordsApi.startsWith('.')
    ? relativeRecordsApi
    : `./${relativeRecordsApi}`
  writeFileSync(join(sourceRoot, 'main.ts'), [
    "document.body.addEventListener('click', () => {",
    `  void import(${JSON.stringify(recordsApiSpecifier)}).then(({ listRecords }) => listRecords())`,
    '})',
  ].join('\n'))

  await build({
    root,
    configFile: false,
    mode: 'production',
    logLevel: 'silent',
    plugins: [recordsModuleCapture(placements)],
    build: {
      outDir: join(root, 'dist'),
      emptyOutDir: true,
    },
  })
}

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('bundle budget checker', () => {
  it('measures a hashed production fixture deterministically', () => {
    const fixture = createFixture()

    const result = runChecker(fixture.dist, fixture.budgetPath, '--format', 'json')

    expect(result.status, result.stderr).toBe(0)
    const report = JSON.parse(result.stdout) as { metrics: Record<string, number> }
    expect(report.metrics).toEqual({
      entryJsGzipBytes: expect.any(Number),
      entryCssGzipBytes: expect.any(Number),
      maxAsyncJsGzipBytes: expect.any(Number),
      fontWoff2RawBytes: 9,
    })
  })

  it('fails closed when the module entry is missing or ambiguous', () => {
    const missing = createFixture({ html: '<link rel="stylesheet" href="/assets/entry-abc.css">' })
    const ambiguous = createFixture({
      html: '<script type="module" src="/assets/entry-abc.js"></script><script type="module" src="/assets/route-def.js"></script><link rel="stylesheet" href="/assets/entry-abc.css">',
    })

    const missingResult = runChecker(missing.dist, missing.budgetPath)
    const ambiguousResult = runChecker(ambiguous.dist, ambiguous.budgetPath)

    expect(missingResult.status).toBe(1)
    expect(missingResult.stderr).toContain('expected exactly one module entry')
    expect(ambiguousResult.status).toBe(1)
    expect(ambiguousResult.stderr).toContain('expected exactly one module entry')
  })

  it('fails closed when the entry stylesheet is missing or ambiguous', () => {
    const missing = createFixture({ html: '<script type="module" src="/assets/entry-abc.js"></script>' })
    const ambiguous = createFixture({
      html: '<script type="module" src="/assets/entry-abc.js"></script><link rel="stylesheet" href="/assets/entry-abc.css"><link rel="stylesheet" href="/assets/second.css">',
    })

    const missingResult = runChecker(missing.dist, missing.budgetPath)
    const ambiguousResult = runChecker(ambiguous.dist, ambiguous.budgetPath)

    expect(missingResult.status).toBe(1)
    expect(missingResult.stderr).toContain('expected exactly one entry stylesheet')
    expect(ambiguousResult.status).toBe(1)
    expect(ambiguousResult.stderr).toContain('expected exactly one entry stylesheet')
  })

  it('fails closed for zero fonts or zero async chunks', () => {
    const noFonts = createFixture({ includeFonts: false })
    const noAsync = createFixture({ includeAsync: false })

    const noFontsResult = runChecker(noFonts.dist, noFonts.budgetPath)
    const noAsyncResult = runChecker(noAsync.dist, noAsync.budgetPath)

    expect(noFontsResult.status).toBe(1)
    expect(noFontsResult.stderr).toContain('expected at least one WOFF2 font')
    expect(noAsyncResult.status).toBe(1)
    expect(noAsyncResult.stderr).toContain('expected at least one async JavaScript chunk')
  })

  it('reports metric, actual and limit without mutating the budget', () => {
    const fixture = createFixture()
    const measurement = runChecker(fixture.dist, fixture.budgetPath, '--format', 'json')
    expect(measurement.status, measurement.stderr).toBe(0)
    const report = JSON.parse(measurement.stdout) as { metrics: Record<string, number> }
    const entryJsGzipBytes = report.metrics.entryJsGzipBytes
    if (entryJsGzipBytes === undefined || entryJsGzipBytes <= 1) {
      throw new Error('fixture entry JS gzip measurement must be greater than one byte')
    }
    writeFileSync(fixture.budgetPath, JSON.stringify({
      ...report.metrics,
      entryJsGzipBytes: entryJsGzipBytes - 1,
    }))
    const before = readFileSync(fixture.budgetPath, 'utf8')

    const result = runChecker(fixture.dist, fixture.budgetPath)

    expect(result.status).toBe(1)
    expect(result.stderr).toContain(
      `entryJsGzipBytes: actual ${entryJsGzipBytes} > limit ${entryJsGzipBytes - 1}`,
    )
    expect(readFileSync(fixture.budgetPath, 'utf8')).toBe(before)
  })

  it('rejects incomplete or non-positive budget values', () => {
    const fixture = createFixture({ budget: {
      entryJsGzipBytes: 10_000,
      entryCssGzipBytes: 0,
      maxAsyncJsGzipBytes: 10_000,
    } })

    const result = runChecker(fixture.dist, fixture.budgetPath)

    expect(result.status).toBe(1)
    expect(result.stderr).toContain('bundle budget must define exactly four positive integer metrics')
  })

  it('writes a reviewed baseline only in explicit baseline mode', () => {
    const fixture = createFixture({ budget: {
      entryJsGzipBytes: 1,
      entryCssGzipBytes: 1,
      maxAsyncJsGzipBytes: 1,
      fontWoff2RawBytes: 1,
    } })

    const result = runChecker(
      fixture.dist,
      fixture.budgetPath,
      '--write-baseline',
      '--format',
      'json',
    )

    expect(result.status, result.stderr).toBe(0)
    const report = JSON.parse(result.stdout) as { metrics: Record<string, number> }
    expect(JSON.parse(readFileSync(fixture.budgetPath, 'utf8'))).toEqual(report.metrics)
  })

  it('keeps the Records transport out of the production entry after lazy record routes consume it', async () => {
    const placements: RecordsModulePlacement[] = []

    await buildCurrentApplication(placements)

    const recordsApi = placements.filter((placement) => placement.module === 'recordsApi')
    expect(recordsApi.length).toBeGreaterThan(0)
    expect(recordsApi.every((placement) => !placement.isEntry)).toBe(true)
    expect(placements.filter((placement) => placement.module === 'apiError').every((placement) => (
      !placement.isEntry
    ))).toBe(true)
  })

  it('places a synthetic Records consumer only in its lazy chunk', async () => {
    const placements: RecordsModulePlacement[] = []

    await buildSyntheticLazyConsumer(placements)

    expect(placements.map((placement) => placement.module).sort()).toEqual([
      'apiError',
      'recordsApi',
    ])
    expect(new Set(placements.map((placement) => placement.fileName))).toHaveLength(1)
    expect(placements.every((placement) => (
      !placement.isEntry && placement.isDynamicEntry
    ))).toBe(true)
  })
})
