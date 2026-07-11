/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import postcss, {
  type AnyNode,
  type Declaration,
  type Root,
  type Rule,
} from 'postcss'
import { describe, expect, it } from 'vitest'

function normalizeWhitespace(value: string) {
  return value.replace(/\s+/g, ' ').trim()
}

function ruleContext(rule: Rule) {
  const contexts: string[] = []
  let parent: AnyNode | undefined = rule.parent
  while (parent) {
    if (parent.type === 'atrule') {
      const params = normalizeWhitespace(parent.params)
      contexts.unshift(`@${parent.name}${params === '' ? '' : ` ${params}`}`)
    }
    parent = parent.parent
  }
  return contexts.length === 0 ? 'root' : contexts.join(' > ')
}

function localImportPath(params: string) {
  const normalized = params.trim()
  const quote = normalized[0]
  if ((quote !== "'" && quote !== '"') || normalized.at(-1) !== quote) {
    throw new Error(`index CSS contract only accepts quoted local imports: ${params}`)
  }
  const path = normalized.slice(1, -1)
  if (!path.startsWith('.')) {
    throw new Error(`index CSS contract only accepts local imports: ${path}`)
  }
  return path
}

function resolveImportedCss(filePath: string, seen = new Set<string>()): Root {
  const absolutePath = resolve(filePath)
  if (seen.has(absolutePath)) {
    throw new Error(`CSS import cycle detected at ${absolutePath}`)
  }
  seen.add(absolutePath)

  const parsed = postcss.parse(readFileSync(absolutePath, 'utf8'), { from: absolutePath })
  const resolvedRoot = postcss.root()

  for (const node of parsed.nodes) {
    if (node.type === 'atrule' && node.name === 'import') {
      const importedPath = resolve(dirname(absolutePath), localImportPath(node.params))
      const importedRoot = resolveImportedCss(importedPath, seen)
      for (const importedNode of importedRoot.nodes) resolvedRoot.append(importedNode.clone())
    } else {
      resolvedRoot.append(node.clone())
    }
  }

  seen.delete(absolutePath)
  return resolvedRoot
}

function matchingRules(root: Root, selector: string) {
  const normalizedSelector = normalizeWhitespace(selector)
  const matches: Rule[] = []
  root.walkRules((rule) => {
    if (normalizeWhitespace(rule.selector) === normalizedSelector) matches.push(rule)
  })
  return matches
}

function requireSelectorContexts(root: Root, selector: string, expectedContexts: string[]) {
  const matches = matchingRules(root, selector)
  const actualContexts = matches.map(ruleContext)
  expect(actualContexts, `${selector} contexts`).toEqual(expectedContexts)
  return matches
}

function requireUniqueRule(root: Root, selector: string, context = 'root') {
  const matches = matchingRules(root, selector).filter((rule) => ruleContext(rule) === context)
  if (matches.length !== 1) {
    throw new Error(
      `${selector} must have exactly one rule in ${context}; found ${matches.length}`,
    )
  }
  const match = matches[0]
  if (!match) throw new Error(`${selector} is missing from ${context}`)
  return match
}

function declaration(rule: Rule, property: string) {
  const matches = rule.nodes.filter(
    (node): node is Declaration => node.type === 'decl' && node.prop === property,
  )
  if (matches.length !== 1) {
    throw new Error(
      `${rule.selector} must declare ${property} exactly once in ${ruleContext(rule)}; found ${matches.length}`,
    )
  }
  const match = matches[0]
  if (!match) throw new Error(`${rule.selector} is missing ${property}`)
  return normalizeWhitespace(match.value)
}

const indexCss = resolveImportedCss('src/index.css')
const indexManifest = readFileSync('src/index.css', 'utf8')
const loginPageCss = postcss.parse(readFileSync('src/pages/LoginPage.css', 'utf8'), {
  from: 'src/pages/LoginPage.css',
})
const cssOwners = JSON.parse(readFileSync('css-owners.json', 'utf8')) as {
  owners: Record<string, string[]>
}

describe('PostCSS contract helpers', () => {
  it('rejects a correct first match followed by a conflicting rule in the same context', () => {
    const fixture = postcss.parse('.command { display: block } .command { display: none }')

    expect(() => requireUniqueRule(fixture, '.command')).toThrow(
      '.command must have exactly one rule in root; found 2',
    )
  })

  it('accepts only the explicitly declared at-rule context set', () => {
    const fixture = postcss.parse(
      '.command { display: flex } @media (max-width: 40rem) { .command { display: grid } }',
    )

    expect(requireSelectorContexts(fixture, '.command', [
      'root',
      '@media (max-width: 40rem)',
    ])).toHaveLength(2)
    expect(() => requireSelectorContexts(fixture, '.command', ['root'])).toThrow()
  })
})

describe('index.css modernization contracts', () => {
  it('groups every non-empty partial under one explicit owner without catch-all files', () => {
    const expectedOwners = [
      'app-shell',
      'assets',
      'dashboard',
      'observability',
      'settings-subscriptions',
      'shared-atoms-page',
      'vps',
    ]
    const expectedManifestOrder = [
      'shared-atoms-page',
      'app-shell',
      'dashboard',
      'assets',
      'vps',
      'observability',
      'settings-subscriptions',
    ]
    const sectionOwners: string[] = []
    const importedPartials: string[] = []
    let currentOwner = ''

    expect(Object.keys(cssOwners.owners).sort()).toEqual(expectedOwners)

    for (const line of indexManifest.split('\n')) {
      const ownerMatch = line.match(/^\/\* owner: ([a-z-]+) \*\/$/)
      if (ownerMatch) {
        const owner = ownerMatch[1]
        if (!owner) throw new Error(`invalid CSS owner marker: ${line}`)
        currentOwner = owner
        sectionOwners.push(currentOwner)
        continue
      }

      const importMatch = line.match(/^@import\s+((?:'|").*(?:'|"));$/)
      if (!importMatch) continue
      expect(currentOwner, `${line} must follow an owner marker`).not.toBe('')

      const importParams = importMatch[1]
      if (!importParams) throw new Error(`invalid CSS import: ${line}`)
      const importedPath = resolve(dirname('src/index.css'), localImportPath(importParams))
      const ownerPath = relative('.', importedPath)
      importedPartials.push(ownerPath)
      expect(cssOwners.owners[currentOwner], `${ownerPath} owner`).toContain(ownerPath)

      const parsed = postcss.parse(readFileSync(importedPath, 'utf8'), { from: importedPath })
      expect(
        parsed.nodes.some((node) => node.type !== 'comment'),
        `${ownerPath} must not be comment-only`,
      ).toBe(true)
    }

    const ownedPartials = Object.values(cssOwners.owners)
      .flat()
      .filter((path) => path.startsWith('src/styles/partials/'))
      .sort()
    const retiredCatchAlls = [
      'src/styles/partials/legacy-dashboard.css',
      'src/styles/partials/legacy-misc.css',
      'src/styles/partials/misc.css',
    ]

    expect(sectionOwners).toEqual(expectedManifestOrder)
    expect(importedPartials.sort()).toEqual(ownedPartials)
    expect(Object.values(cssOwners.owners).flat()).not.toEqual(
      expect.arrayContaining(retiredCatchAlls),
    )
  })

  it('does not retain CSS owners for Task 3 and Task 8 surfaces removed from production', () => {
    const retiredClassPrefixes = [
      'events-filter-panel',
      'events-filter-drawer__value',
      'events-filter-drawer__hint',
      'list-command-band',
      'dashboard-empty-state',
      'summary-grid--strip',
      'summary-grid--numeric',
      'asset-hero-meta',
      'asset-cancel-workbench__hint',
      'asset-drawer-context',
      'asset-workbench-summary',
      'asset-decision-board__summary',
      'asset-decision-board__context',
      'asset-decision-actions',
      'asset-decision-row',
      'asset-decision-signal',
      'asset-decision-quality',
      'asset-decision-path',
      'asset-decision-deeplink-notice',
      'asset-decision-support-surface',
      'asset-decision-closed-loop',
      'asset-decision-next-work',
      'asset-decision-groups-table',
      'asset-decision-templates-table',
      'asset-decision-comparison-overview',
      'asset-decision-detail__summary',
      'asset-decision-detail-nav',
      'asset-decision-detail-actionbar',
      'asset-decision-detail-directory',
      'asset-decision-comparison-matrix',
      'asset-decision-saved-evidence',
      'asset-decision-progression-branch',
      'asset-decision-progress-panel',
      'asset-decision-comparison-card',
      'asset-decision-progress-item',
      'asset-decision-detail-command__body',
      'asset-decision-detail-command__checks',
      'asset-decision-detail-command__readiness',
      'asset-decision-member-preview',
      'asset-decision-member-list',
      'asset-decision-member-card',
      'asset-decision-record-member-summary',
      'asset-decision-detail__evidence',
      'asset-decision-detail__issue',
      'asset-decision-record-form__members',
      'asset-decision-record-form__member',
      'asset-decision-manual-member-form',
      'asset-decision-record-detail__lead',
      'asset-decision-execution-card__facts',
      'asset-quality-list',
      'asset-quality-pill',
      'asset-create-form',
    ]
    const violations: string[] = []

    indexCss.walkRules((rule) => {
      for (const classPrefix of retiredClassPrefixes) {
        if (rule.selector.includes(`.${classPrefix}`)) {
          violations.push(`${classPrefix} in ${ruleContext(rule)}: ${rule.selector}`)
        }
      }
    })

    expect(violations).toEqual([])
  })

  it('anchors section title accent markers to the title itself', () => {
    const rule = requireUniqueRule(indexCss, '.section-title')
    expect(declaration(rule, 'position')).toBe('relative')
  })

  it('only shows watchtower h1 eyebrow pseudo elements for explicit variants', () => {
    expect(
      declaration(
        requireUniqueRule(indexCss, '.watchtower-header__title-block h1::before'),
        'display',
      ),
    ).toBe('none')
    expect(
      declaration(
        requireUniqueRule(
          indexCss,
          '.watchtower-header[aria-label="VPS 身份与操作"] .watchtower-header__title-block h1::before',
        ),
        'display',
      ),
    ).toBe('block')
    expect(
      declaration(
        requireUniqueRule(
          indexCss,
          '.provider-directory .watchtower-header__title-block h1::before',
        ),
        'display',
      ),
    ).toBe('block')
  })

  it('stacks the login card and footer vertically in both global and route CSS', () => {
    expect(declaration(requireUniqueRule(indexCss, '.login-page'), 'flex-direction')).toBe(
      'column',
    )
    expect(
      declaration(requireUniqueRule(loginPageCss, '.login-page'), 'flex-direction'),
    ).toBe('column')
  })

  it('keeps responsive tabs readable within their unique owner rules', () => {
    for (const variant of ['underline', 'pill']) {
      const tabs = requireUniqueRule(indexCss, `.tabs--${variant}`)
      const tab = requireUniqueRule(indexCss, `.tabs--${variant} .tab`)

      expect(declaration(tabs, 'max-width')).toBe('100%')
      expect(declaration(tabs, 'overflow-x')).toBe('auto')
      expect(declaration(tab, 'flex')).toBe('0 0 auto')
      expect(declaration(tab, 'white-space')).toBe('nowrap')
    }
  })

  it('keeps the asset support strip title readable and owns one responsive override', () => {
    const title = requireUniqueRule(indexCss, '.asset-decision-support-strip__title')
    expect(declaration(title, 'white-space')).toBe('normal')
    expect(declaration(title, 'overflow')).toBe('visible')
    expect(declaration(title, 'text-overflow')).toBe('clip')

    const [base, responsive] = requireSelectorContexts(
      indexCss,
      '.asset-decision-support-strip',
      ['root', '@media (max-width:920px)'],
    )
    if (!base || !responsive) {
      throw new Error('asset decision support strip must define base and responsive rules')
    }
    expect(declaration(base, 'display')).toBe('flex')
    expect(declaration(responsive, 'display')).toBe('grid')
    expect(declaration(responsive, 'grid-template-columns')).toBe(
      'repeat(2,minmax(0,1fr))',
    )
  })

  it('isolates provider table overflow and keeps entry labels visible', () => {
    const table = requireUniqueRule(indexCss, '.provider-directory-table')
    const scrollRegion = requireUniqueRule(indexCss, '.provider-directory-table-scroll')
    const focusRing = requireUniqueRule(
      indexCss,
      '.provider-directory-table-scroll:focus-visible',
    )
    const entryLinks = requireUniqueRule(indexCss, '.provider-directory-entry-links')
    const entryLink = requireUniqueRule(indexCss, '.provider-directory-entry-link')

    expect(declaration(table, 'min-width')).toBe('1000px')
    expect(declaration(scrollRegion, 'max-width')).toBe('100%')
    expect(declaration(scrollRegion, 'overflow-x')).toBe('auto')
    expect(declaration(scrollRegion, 'scrollbar-gutter')).toBe('stable')
    expect(declaration(focusRing, 'outline')).toBe(
      'var(--border-w-strong) solid var(--accent)',
    )
    expect(declaration(entryLinks, 'flex-wrap')).toBe('wrap')
    expect(declaration(entryLinks, 'overflow')).toBe('visible')
    expect(declaration(entryLink, 'max-width')).toBe('none')
    expect(declaration(entryLink, 'overflow')).toBe('visible')
    expect(declaration(entryLink, 'text-overflow')).toBe('clip')
  })

  it('keeps small state text readable on tinted surfaces', () => {
    const criticalInfo = requireUniqueRule(indexCss, '.badge--info.tone--critical')
    const assetGroupContext = requireUniqueRule(
      indexCss,
      '.asset-decision-group-card__head span:not(.badge)',
    )

    expect(declaration(criticalInfo, 'color')).toBe(
      'color-mix(in srgb,var(--color-state-critical) 78%,var(--text-primary))',
    )
    expect(declaration(assetGroupContext, 'color')).toBe(
      'color-mix(in srgb,var(--text-secondary) 90%,var(--text-primary))',
    )
  })
})
