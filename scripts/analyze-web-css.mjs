#!/usr/bin/env node

import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const requireFromWeb = createRequire(resolve(scriptDirectory, '..', 'web', 'package.json'))
const postcss = requireFromWeb('postcss')

const OWNER_NAMES = [
  'app-shell',
  'dashboard',
  'assets',
  'vps',
  'observability',
  'settings-subscriptions',
  'shared-atoms-page',
]

const REQUIRED_LIMITS = {
  sourceFilesMax: 'sourceFiles',
  sourceBytesMax: 'sourceBytes',
  rulesMax: 'rules',
  declarationsMax: 'declarations',
  repeatedSelectorTextsMax: 'repeatedSelectorTexts',
  literalColorDeclarationsMax: 'literalColorDeclarations',
  importantDeclarationsMax: 'importantDeclarations',
  productionCssBytesMax: 'productionCssBytes',
  productionCssGzipBytesMax: 'productionCssGzipBytes',
}

const LITERAL_COLOR_PATTERN = /(?:#[\da-f]{3,8}\b|\b(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch|color)\s*\()/i

function normalizePath(path) {
  return path.split('\\').join('/')
}

function normalizeWhitespace(value) {
  return value.replace(/\s+/g, ' ').trim()
}

function listCssFiles(directory) {
  const entries = readdirSync(directory, { withFileTypes: true })
    .sort((left, right) => left.name.localeCompare(right.name))
  const files = []

  for (const entry of entries) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...listCssFiles(path))
    } else if (entry.isFile() && entry.name.endsWith('.css')) {
      files.push(path)
    }
  }

  return files
}

function contextFor(node) {
  const contexts = []
  let parent = node.parent
  while (parent) {
    if (parent.type === 'atrule') {
      const params = normalizeWhitespace(parent.params)
      contexts.unshift(`@${parent.name}${params === '' ? '' : ` ${params}`}`)
    }
    parent = parent.parent
  }
  return contexts.length === 0 ? 'root' : contexts.join(' > ')
}

function parseArguments(argv) {
  const webRoot = resolve(scriptDirectory, '..', 'web')
  const options = {
    webRoot,
    ownersPath: resolve(webRoot, 'css-owners.json'),
    budgetPath: resolve(webRoot, 'css-budget.json'),
    distPath: resolve(webRoot, 'dist'),
    format: 'json',
  }

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    const value = argv[index + 1]
    if (argument === '--web-root' && value) {
      options.webRoot = resolve(value)
      index += 1
    } else if (argument === '--owners' && value) {
      options.ownersPath = resolve(value)
      index += 1
    } else if (argument === '--budget' && value) {
      options.budgetPath = resolve(value)
      index += 1
    } else if (argument === '--dist' && value) {
      options.distPath = resolve(value)
      index += 1
    } else if (argument === '--format' && (value === 'json' || value === 'text')) {
      options.format = value
      index += 1
    } else {
      throw new Error(`unknown or incomplete argument: ${argument}`)
    }
  }

  return options
}

function readJson(path, label) {
  try {
    return JSON.parse(readFileSync(path, 'utf8'))
  } catch (error) {
    throw new Error(`${label} is not valid JSON at ${path}: ${error.message}`)
  }
}

function buildOwnership(sourcePaths, webRoot, ownersPath) {
  const config = readJson(ownersPath, 'CSS owner map')
  if (config.version !== 1 || typeof config.owners !== 'object' || config.owners === null) {
    throw new Error('CSS owner map must contain version=1 and an owners object')
  }

  const configuredNames = Object.keys(config.owners).sort()
  const expectedNames = [...OWNER_NAMES].sort()
  if (JSON.stringify(configuredNames) !== JSON.stringify(expectedNames)) {
    throw new Error(`CSS owner map must define exactly: ${OWNER_NAMES.join(', ')}`)
  }

  const sourceFiles = new Set(sourcePaths.map((path) => normalizePath(relative(webRoot, path))))
  const ownersByFile = new Map()
  const filesByOwner = new Map(OWNER_NAMES.map((owner) => [owner, []]))

  for (const owner of OWNER_NAMES) {
    const files = config.owners[owner]
    if (!Array.isArray(files) || files.some((file) => typeof file !== 'string')) {
      throw new Error(`CSS owner ${owner} must contain an array of source paths`)
    }

    for (const rawPath of files) {
      const path = normalizePath(rawPath)
      if (!sourceFiles.has(path)) {
        throw new Error(`CSS owner ${owner} references unknown source stylesheet ${path}`)
      }
      const priorOwner = ownersByFile.get(path)
      if (priorOwner) {
        throw new Error(`${path} must have exactly one owner; found ${priorOwner} and ${owner}`)
      }
      ownersByFile.set(path, owner)
      filesByOwner.get(owner).push(path)
    }
  }

  const missing = [...sourceFiles].filter((path) => !ownersByFile.has(path)).sort()
  if (missing.length > 0) {
    throw new Error(`${missing.join(', ')} must have exactly one owner`)
  }

  for (const files of filesByOwner.values()) {
    files.sort()
  }

  return { ownersByFile, filesByOwner }
}

function analyzeSource(webRoot, ownersPath) {
  const sourceRoot = resolve(webRoot, 'src')
  const sourcePaths = listCssFiles(sourceRoot)
  const { ownersByFile, filesByOwner } = buildOwnership(sourcePaths, webRoot, ownersPath)
  const rulesByOwner = new Map(OWNER_NAMES.map((owner) => [owner, 0]))
  const selectorOccurrences = new Map()
  const selectorContexts = []
  const literalColors = []
  const importantDeclarations = []
  let bytes = 0
  let rules = 0
  let declarations = 0

  for (const absolutePath of sourcePaths) {
    const file = normalizePath(relative(webRoot, absolutePath))
    const owner = ownersByFile.get(file)
    const contents = readFileSync(absolutePath, 'utf8')
    const root = postcss.parse(contents, { from: absolutePath })
    bytes += Buffer.byteLength(contents)

    root.walkRules((rule) => {
      const selector = normalizeWhitespace(rule.selector)
      const context = contextFor(rule)
      const line = rule.source?.start?.line ?? 0
      rules += 1
      rulesByOwner.set(owner, rulesByOwner.get(owner) + 1)
      selectorContexts.push({ selector, context, owner, file, line })

      const existing = selectorOccurrences.get(selector) ?? {
        selector,
        occurrences: 0,
        contexts: [],
      }
      existing.occurrences += 1
      if (!existing.contexts.includes(context)) existing.contexts.push(context)
      selectorOccurrences.set(selector, existing)
    })

    root.walkDecls((declaration) => {
      declarations += 1
      const record = {
        property: declaration.prop,
        value: declaration.value,
        context: contextFor(declaration),
        owner,
        file,
        line: declaration.source?.start?.line ?? 0,
      }
      if (LITERAL_COLOR_PATTERN.test(declaration.value)) literalColors.push(record)
      if (declaration.important) importantDeclarations.push(record)
    })
  }

  selectorContexts.sort((left, right) =>
    left.selector.localeCompare(right.selector)
      || left.context.localeCompare(right.context)
      || left.file.localeCompare(right.file)
      || left.line - right.line,
  )
  literalColors.sort((left, right) =>
    left.file.localeCompare(right.file) || left.line - right.line || left.property.localeCompare(right.property),
  )
  importantDeclarations.sort((left, right) =>
    left.file.localeCompare(right.file) || left.line - right.line || left.property.localeCompare(right.property),
  )

  const repeatedSelectors = [...selectorOccurrences.values()]
    .filter((entry) => entry.occurrences > 1)
    .sort((left, right) => left.selector.localeCompare(right.selector))

  return {
    source: {
      files: sourcePaths.length,
      bytes,
      rules,
      declarations,
      repeatedSelectorTexts: repeatedSelectors.length,
      literalColorDeclarations: literalColors.length,
      importantDeclarations: importantDeclarations.length,
    },
    owners: OWNER_NAMES.map((owner) => ({
      owner,
      files: filesByOwner.get(owner),
      rules: rulesByOwner.get(owner),
    })),
    selectorContexts,
    repeatedSelectors,
    literalColors,
    importantDeclarations,
  }
}

function analyzeProduction(distPath) {
  const productionPaths = listCssFiles(distPath)
  let rawBytes = 0
  let gzipBytes = 0

  for (const path of productionPaths) {
    const contents = readFileSync(path)
    rawBytes += contents.byteLength
    gzipBytes += gzipSync(contents, { level: 9 }).byteLength
  }

  return { files: productionPaths.length, rawBytes, gzipBytes }
}

function checkBudget(source, production, budgetPath) {
  const config = readJson(budgetPath, 'CSS budget')
  if (config.version !== 1 || typeof config.limits !== 'object' || config.limits === null) {
    throw new Error('CSS budget must contain version=1 and a limits object')
  }

  const actuals = {
    sourceFiles: source.files,
    sourceBytes: source.bytes,
    rules: source.rules,
    declarations: source.declarations,
    repeatedSelectorTexts: source.repeatedSelectorTexts,
    literalColorDeclarations: source.literalColorDeclarations,
    importantDeclarations: source.importantDeclarations,
    productionCssBytes: production.rawBytes,
    productionCssGzipBytes: production.gzipBytes,
  }
  const violations = []

  for (const [limitName, metricName] of Object.entries(REQUIRED_LIMITS)) {
    const maximum = config.limits[limitName]
    if (!Number.isInteger(maximum) || maximum < 0) {
      throw new Error(`CSS budget limit ${limitName} must be a non-negative integer`)
    }
    const actual = actuals[metricName]
    if (actual > maximum) violations.push({ metric: metricName, actual, max: maximum })
  }

  return { status: violations.length === 0 ? 'pass' : 'fail', violations }
}

function formatText(report) {
  const lines = [
    `CSS source: ${report.source.files} files, ${report.source.bytes} bytes, ${report.source.rules} rules, ${report.source.declarations} declarations`,
    `CSS debt: ${report.source.repeatedSelectorTexts} repeated selectors, ${report.source.literalColorDeclarations} literal-color declarations, ${report.source.importantDeclarations} !important declarations`,
    `CSS production: ${report.production.files} files, ${report.production.rawBytes} raw bytes, ${report.production.gzipBytes} gzip bytes`,
    `CSS budget: ${report.budget.status}`,
  ]
  return `${lines.join('\n')}\n`
}

function main() {
  try {
    const options = parseArguments(process.argv.slice(2))
    const sourceReport = analyzeSource(options.webRoot, options.ownersPath)
    const production = analyzeProduction(options.distPath)
    const budget = checkBudget(sourceReport.source, production, options.budgetPath)
    const report = { ...sourceReport, production, budget }

    process.stdout.write(
      options.format === 'json' ? `${JSON.stringify(report, null, 2)}\n` : formatText(report),
    )

    if (budget.status === 'fail') {
      for (const violation of budget.violations) {
        process.stderr.write(
          `CSS budget exceeded: ${violation.metric} actual=${violation.actual} max=${violation.max}\n`,
        )
      }
      process.exitCode = 1
    }
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`)
    process.exitCode = 1
  }
}

main()
