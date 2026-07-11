import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { afterEach, describe, expect, it } from 'vitest'

const REPOSITORY_ROOT = resolve(process.cwd(), '..')
const CHECKER_PATH = resolve(REPOSITORY_ROOT, 'scripts/check-web-bundle-budget.mjs')
const temporaryRoots: string[] = []

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
})
