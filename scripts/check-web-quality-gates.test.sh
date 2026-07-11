#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

REPO_ROOT="$repo_root" node <<'EOF'
const fs = require('node:fs')
const path = require('node:path')

const root = process.env.REPO_ROOT
const failures = []
const read = (relativePath) => {
  const absolutePath = path.join(root, relativePath)
  if (!fs.existsSync(absolutePath)) {
    failures.push(`missing ${relativePath}`)
    return ''
  }
  return fs.readFileSync(absolutePath, 'utf8')
}
const readJSON = (relativePath) => {
  const source = read(relativePath)
  if (!source) return null
  try {
    return JSON.parse(source)
  } catch (error) {
    failures.push(`${relativePath} is not valid JSON: ${error.message}`)
    return null
  }
}
const requireMatch = (source, pattern, message) => {
  if (!pattern.test(source)) failures.push(message)
}
const leadingSpaces = (line) => line.match(/^ */)[0].length
const yamlBlock = (source, key) => {
  const lines = source.split(/\r?\n/)
  const heading = lines.findIndex((line) => line.trim() === `${key}:`)
  if (heading < 0) return ''
  const indent = leadingSpaces(lines[heading])
  const block = []
  for (let index = heading + 1; index < lines.length; index += 1) {
    const line = lines[index]
    if (line.trim() !== '' && !line.trimStart().startsWith('#') && leadingSpaces(line) <= indent) break
    block.push(line)
  }
  return block.join('\n')
}
const directYamlKeys = (source, indent) => source
  .split(/\r?\n/)
  .filter((line) => leadingSpaces(line) === indent && /^[^#\s][^:]*:/.test(line.trim()))
  .map((line) => line.trim().match(/^([^:]+):/)?.[1] ?? '')
const makeTarget = (source, target) => {
  const lines = source.split(/\r?\n/)
  const heading = lines.findIndex((line) => line.startsWith(`${target}:`))
  if (heading < 0) return ''
  const block = []
  for (let index = heading + 1; index < lines.length; index += 1) {
    const line = lines[index]
    if (line && !/^\s/.test(line) && /^[^#][^:]*:/.test(line)) break
    block.push(line)
  }
  return block.join('\n')
}

const packageJSON = readJSON('web/package.json')
if (packageJSON) {
  for (const dependency of ['@vitest/coverage-v8', '@playwright/test', '@axe-core/playwright']) {
    if (!packageJSON.devDependencies?.[dependency]) failures.push(`missing direct devDependency ${dependency}`)
  }
  for (const script of ['test:coverage', 'test:e2e', 'test:e2e:staging', 'bundle:check']) {
    if (!packageJSON.scripts?.[script]) failures.push(`missing package script ${script}`)
  }
}

const vitestConfig = read('web/vitest.config.ts')
requireMatch(vitestConfig, /configDefaults/, 'Vitest must preserve its default exclude inventory')
requireMatch(vitestConfig, /exclude:\s*\[[^\]]*e2e\/\*\*/s, 'Vitest must exclude Playwright e2e specs from unit/coverage collection')

for (const relativePath of [
  'web/coverage-budget.json',
  'web/bundle-budget.json',
  'web/playwright.config.ts',
  'web/playwright.staging.config.ts',
  'scripts/check-web-bundle-budget.mjs',
  'web/e2e/support/diagnostics.ts',
  'web/e2e/staging/audit.ts',
  'web/e2e/staging/staging-smoke.spec.ts',
  '.github/workflows/frontend-staging-smoke.yml',
]) {
  read(relativePath)
}

const makefile = read('Makefile')
const toolchainTarget = makeTarget(makefile, 'test-web-toolchain')
const verifyWebTarget = makeTarget(makefile, 'verify-web')
requireMatch(toolchainTarget, /scripts\/check-web-quality-gates\.test\.sh/, 'Make must invoke the web quality source contract')
requireMatch(verifyWebTarget, /run test:coverage\b/, 'make verify-web must run test:coverage')
requireMatch(verifyWebTarget, /run bundle:check\b/, 'make verify-web must run bundle:check')
requireMatch(verifyWebTarget, /run css:analyze\b/, 'make verify-web must run the Task 9 CSS analyzer')

const ci = read('.github/workflows/ci.yml')
const browserJob = yamlBlock(ci, 'web-browser')
if (!browserJob) failures.push('CI must define an independent web-browser job')
requireMatch(browserJob, /node-version-file:\s*\.node-version/, 'the browser job must read .node-version')
requireMatch(browserJob, /env -u NODE_ENV npm --prefix web ci --include=dev/, 'the browser job must install dev dependencies reproducibly')
requireMatch(browserJob, /run:\s*npm --prefix web run test:e2e/, 'the browser job must run test:e2e')
requireMatch(browserJob, /playwright install[^\n]*chromium/, 'the browser job must install lockfile-compatible Chromium')

const diagnostics = read('web/e2e/support/diagnostics.ts')
requireMatch(
  diagnostics,
  /internal[\\/]center[\\/]http[\\/]csp-policy\.txt/,
  'browser diagnostics must read the repository CSP policy source',
)
if (/default-src\s+'self'/.test(diagnostics)) {
  failures.push('browser diagnostics must not duplicate the CSP policy literal')
}

const stagingConfig = read('web/playwright.staging.config.ts')
requireMatch(stagingConfig, /trace:\s*'off'/, 'staging Playwright must disable traces')
requireMatch(stagingConfig, /screenshot:\s*'off'/, 'staging Playwright must use only explicitly masked screenshots')
requireMatch(stagingConfig, /video:\s*'off'/, 'staging Playwright must disable video capture')
requireMatch(stagingConfig, /preserveOutput:\s*'never'/, 'staging Playwright must discard its unsanitized internal output')
if (/outputDir:[^\n]*staging-audit/.test(stagingConfig)) {
  failures.push('staging Playwright internal output must stay outside the uploaded audit directory')
}

const stagingSmoke = read('web/e2e/staging/staging-smoke.spec.ts')
for (const route of ['/', '/vps', '/asset-decisions', '/monitoring', '/targets', '/events', '/providers', '/subscriptions', '/settings']) {
  if (!stagingSmoke.includes(`path: '${route}'`)) failures.push(`staging smoke must declare core route ${route}`)
}
requireMatch(stagingSmoke, /\/api\/healthz/, 'staging smoke must verify the deployed health version')
requireMatch(stagingSmoke, /HOUFENG_EXPECTED_VERSION/, 'staging smoke must require an expected version')
requireMatch(stagingSmoke, /restoreRawLayerDays/, 'staging smoke must own an explicit settings restore path')
requireMatch(stagingSmoke, /\bfinally\s*\{/, 'staging smoke must restore settings from a finally block')
requireMatch(stagingSmoke, /recordScreenshot/, 'staging smoke must inventory audit screenshots')
requireMatch(stagingSmoke, /\.write\(/, 'staging smoke must write a sanitized audit manifest')
if (/waitForTimeout\s*\(/.test(stagingSmoke)) failures.push('staging smoke must not use fixed sleeps')

const stagingWorkflow = read('.github/workflows/frontend-staging-smoke.yml')
const dispatchBlock = yamlBlock(stagingWorkflow, 'on')
const dispatchKeys = directYamlKeys(dispatchBlock, 2)
if (dispatchKeys.length !== 1 || dispatchKeys[0] !== 'workflow_dispatch') {
  failures.push('staging workflow must be workflow_dispatch only')
}
requireMatch(dispatchBlock, /expected_version:\s*[\s\S]*?required:\s*true[\s\S]*?type:\s*string/, 'staging dispatch must require expected_version')
const permissionsBlock = yamlBlock(stagingWorkflow, 'permissions')
if (directYamlKeys(permissionsBlock, 2).join(',') !== 'contents') {
  failures.push('staging workflow permissions must only declare contents')
}
requireMatch(permissionsBlock, /contents:\s*read/, 'staging workflow permissions must be read-only')
const concurrencyBlock = yamlBlock(stagingWorkflow, 'concurrency')
requireMatch(concurrencyBlock, /group:\s*frontend-staging-smoke/, 'staging workflow must use a fixed concurrency group')
requireMatch(concurrencyBlock, /cancel-in-progress:\s*false/, 'staging workflow must not cancel a run during settings restore')
const refGuardJob = yamlBlock(stagingWorkflow, 'ref-guard')
requireMatch(refGuardJob, /refs\/heads\/main/, 'staging ref guard must reject non-main dispatches')
if (/environment:|secrets\.|vars\./.test(refGuardJob)) failures.push('staging ref guard must not read the environment or secrets')
const stagingJob = yamlBlock(stagingWorkflow, 'staging-smoke')
requireMatch(stagingJob, /needs:\s*ref-guard/, 'staging job must wait for the secret-free ref guard')
requireMatch(stagingJob, /environment:\s*staging/, 'staging job must use the staging environment')
for (const name of ['HOUFENG_STAGING_BASE_URL', 'HOUFENG_STAGING_USERNAME', 'HOUFENG_STAGING_PASSWORD', 'HOUFENG_EXPECTED_VERSION']) {
  if (!stagingJob.includes(name)) failures.push(`staging job must configure ${name}`)
}
requireMatch(stagingJob, /env -u NODE_ENV npm --prefix web ci --include=dev/, 'staging job must install dev dependencies reproducibly')
requireMatch(stagingJob, /playwright install[^\n]*chromium/, 'staging job must install lockfile-compatible Chromium')
requireMatch(stagingJob, /npm --prefix web run test:e2e:staging/, 'staging job must run the repository staging command')
requireMatch(stagingJob, /if:\s*always\(\)/, 'staging audit artifact must upload even when smoke fails')
requireMatch(stagingJob, /frontend-staging-audit-\$\{\{\s*github\.run_id\s*\}\}/, 'staging artifact name must include the run id')
requireMatch(stagingJob, /web\/test-results\/staging-audit/, 'staging artifact must use the sanitized audit directory')
requireMatch(stagingJob, /retention-days:\s*30/, 'staging audit artifact must retain evidence for 30 days')

if (failures.length > 0) {
  console.error('web quality gate contract failed:')
  for (const failure of failures) console.error(`- ${failure}`)
  process.exit(1)
}
EOF
