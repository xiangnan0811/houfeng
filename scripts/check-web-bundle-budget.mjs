#!/usr/bin/env node

import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { dirname, isAbsolute, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(scriptDirectory, '..')
const METRIC_NAMES = [
  'entryJsGzipBytes',
  'entryCssGzipBytes',
  'maxAsyncJsGzipBytes',
  'fontWoff2RawBytes',
]

function parseArguments(argv) {
  const options = {
    distPath: resolve(repositoryRoot, 'web', 'dist'),
    budgetPath: resolve(repositoryRoot, 'web', 'bundle-budget.json'),
    format: 'text',
    writeBaseline: false,
  }

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    const value = argv[index + 1]
    if (argument === '--dist' && value) {
      options.distPath = resolve(value)
      index += 1
    } else if (argument === '--budget' && value) {
      options.budgetPath = resolve(value)
      index += 1
    } else if (argument === '--format' && (value === 'json' || value === 'text')) {
      options.format = value
      index += 1
    } else if (argument === '--write-baseline') {
      options.writeBaseline = true
    } else {
      throw new Error(`unknown or incomplete argument: ${argument}`)
    }
  }

  return options
}

function normalizePath(path) {
  return path.split('\\').join('/')
}

function listFiles(directory, predicate) {
  const entries = readdirSync(directory, { withFileTypes: true })
    .sort((left, right) => left.name.localeCompare(right.name))
  const files = []

  for (const entry of entries) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...listFiles(path, predicate))
    } else if (entry.isFile() && predicate(entry.name)) {
      files.push(path)
    }
  }

  return files
}

function parseAttributes(tag) {
  const attributes = new Map()
  const attributePattern = /([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'))?/g
  for (const match of tag.matchAll(attributePattern)) {
    const name = match[1]?.toLowerCase()
    if (!name || name === 'script' || name === 'link') continue
    attributes.set(name, match[2] ?? match[3] ?? '')
  }
  return attributes
}

function requireExactlyOne(values, label) {
  if (values.length !== 1) {
    throw new Error(`expected exactly one ${label}; found ${values.length}`)
  }
  const value = values[0]
  if (!value) throw new Error(`expected exactly one ${label}; found 0`)
  return value
}

function entryReferences(html) {
  const moduleEntries = [...html.matchAll(/<script\b[^>]*>/gi)]
    .map((match) => parseAttributes(match[0]))
    .filter((attributes) => attributes.get('type')?.toLowerCase() === 'module')
    .map((attributes) => attributes.get('src'))
    .filter((value) => typeof value === 'string' && value !== '')

  const stylesheets = [...html.matchAll(/<link\b[^>]*>/gi)]
    .map((match) => parseAttributes(match[0]))
    .filter((attributes) => (
      attributes.get('rel')?.toLowerCase().split(/\s+/).includes('stylesheet')
    ))
    .map((attributes) => attributes.get('href'))
    .filter((value) => typeof value === 'string' && value !== '')

  return {
    entryJsReference: requireExactlyOne(moduleEntries, 'module entry'),
    entryCssReference: requireExactlyOne(stylesheets, 'entry stylesheet'),
  }
}

function resolveDistReference(distPath, reference, extension, label) {
  const url = new URL(reference, 'http://bundle-budget.invalid/')
  let decodedPath
  try {
    decodedPath = decodeURIComponent(url.pathname)
  } catch {
    throw new Error(`${label} contains an invalid encoded path: ${reference}`)
  }
  if (!decodedPath.endsWith(extension)) {
    throw new Error(`${label} must reference a ${extension} file: ${reference}`)
  }

  const absolutePath = resolve(distPath, decodedPath.replace(/^\/+/, ''))
  const relativePath = relative(distPath, absolutePath)
  if (relativePath.startsWith('..') || isAbsolute(relativePath)) {
    throw new Error(`${label} escapes the production dist: ${reference}`)
  }
  return absolutePath
}

function gzipBytes(path) {
  return gzipSync(readFileSync(path), { level: 9 }).byteLength
}

function analyzeBundle(distPath) {
  const html = readFileSync(resolve(distPath, 'index.html'), 'utf8')
  const { entryJsReference, entryCssReference } = entryReferences(html)
  const entryJsPath = resolveDistReference(distPath, entryJsReference, '.js', 'module entry')
  const entryCssPath = resolveDistReference(distPath, entryCssReference, '.css', 'entry stylesheet')
  const javascriptPaths = listFiles(distPath, (name) => name.endsWith('.js'))
  const asyncJavascriptPaths = javascriptPaths.filter((path) => path !== entryJsPath)
  const fontPaths = listFiles(distPath, (name) => name.toLowerCase().endsWith('.woff2'))

  if (asyncJavascriptPaths.length === 0) {
    throw new Error('expected at least one async JavaScript chunk; found 0')
  }
  if (fontPaths.length === 0) {
    throw new Error('expected at least one WOFF2 font; found 0')
  }

  const asyncGzipSizes = asyncJavascriptPaths.map((path) => ({
    path,
    gzipBytes: gzipBytes(path),
  }))
  const largestAsync = [...asyncGzipSizes]
    .sort((left, right) => right.gzipBytes - left.gzipBytes || left.path.localeCompare(right.path))[0]
  if (!largestAsync) throw new Error('expected at least one async JavaScript chunk; found 0')

  const metrics = {
    entryJsGzipBytes: gzipBytes(entryJsPath),
    entryCssGzipBytes: gzipBytes(entryCssPath),
    maxAsyncJsGzipBytes: largestAsync.gzipBytes,
    fontWoff2RawBytes: fontPaths.reduce((total, path) => total + readFileSync(path).byteLength, 0),
  }

  return {
    metrics,
    files: {
      entryJs: normalizePath(relative(distPath, entryJsPath)),
      entryCss: normalizePath(relative(distPath, entryCssPath)),
      largestAsyncJs: normalizePath(relative(distPath, largestAsync.path)),
      asyncJsCount: asyncJavascriptPaths.length,
      fontWoff2Count: fontPaths.length,
    },
  }
}

function readBudget(path) {
  let budget
  try {
    budget = JSON.parse(readFileSync(path, 'utf8'))
  } catch (error) {
    throw new Error(`bundle budget is not valid JSON at ${path}: ${error instanceof Error ? error.message : String(error)}`)
  }

  const keys = budget && typeof budget === 'object' && !Array.isArray(budget)
    ? Object.keys(budget).sort()
    : []
  const expectedKeys = [...METRIC_NAMES].sort()
  const hasExactKeys = JSON.stringify(keys) === JSON.stringify(expectedKeys)
  const hasPositiveIntegers = hasExactKeys && METRIC_NAMES.every(
    (name) => Number.isInteger(budget[name]) && budget[name] > 0,
  )
  if (!hasPositiveIntegers) {
    throw new Error('bundle budget must define exactly four positive integer metrics')
  }
  return budget
}

function checkBudget(metrics, budget) {
  return METRIC_NAMES.flatMap((metric) => (
    metrics[metric] > budget[metric]
      ? [{ metric, actual: metrics[metric], limit: budget[metric] }]
      : []
  ))
}

function formatText(report) {
  const lines = METRIC_NAMES.map((metric) => {
    const limit = report.budget?.[metric]
    return limit === undefined
      ? `${metric}: ${report.metrics[metric]} bytes (baseline written)`
      : `${metric}: actual ${report.metrics[metric]} <= limit ${limit}`
  })
  lines.push(
    `entry files: ${report.files.entryJs}, ${report.files.entryCss}`,
    `largest async: ${report.files.largestAsyncJs} (${report.files.asyncJsCount} chunks)`,
    `fonts: ${report.files.fontWoff2Count} WOFF2 files`,
  )
  return `${lines.join('\n')}\n`
}

function main() {
  try {
    const options = parseArguments(process.argv.slice(2))
    const analysis = analyzeBundle(options.distPath)
    let budget
    let violations = []

    if (options.writeBaseline) {
      writeFileSync(options.budgetPath, `${JSON.stringify(analysis.metrics, null, 2)}\n`)
    } else {
      budget = readBudget(options.budgetPath)
      violations = checkBudget(analysis.metrics, budget)
    }

    const report = {
      ...analysis,
      ...(budget ? { budget } : {}),
      status: violations.length === 0 ? 'pass' : 'fail',
    }
    process.stdout.write(
      options.format === 'json' ? `${JSON.stringify(report, null, 2)}\n` : formatText(report),
    )

    for (const violation of violations) {
      process.stderr.write(
        `${violation.metric}: actual ${violation.actual} > limit ${violation.limit}\n`,
      )
    }
    if (violations.length > 0) process.exitCode = 1
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`)
    process.exitCode = 1
  }
}

main()
