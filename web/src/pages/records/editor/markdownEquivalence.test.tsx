import { readFileSync } from 'node:fs'
import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { MarkdownPreview } from './MarkdownPreview'
import type { DocumentReference } from '../../../lib/documentMarkdown'

type CorpusCase = {
  name: string
  valid: boolean
  source?: string
  model?: unknown
  authorized_references?: DocumentReference[]
}

const corpus = JSON.parse(readFileSync('../testdata/markdown/houfeng-v1.json', 'utf8')) as {
  cases: CorpusCase[]
}

/**
 * The record body has two render paths: the server render model and, when that model
 * is unavailable, react-markdown over the raw source. This suite is the contract that
 * keeps them saying the same thing, comparing a normalized projection of both DOMs.
 *
 * Normalization erases differences that are presentational only:
 * - the model path wraps every text run in a bare span
 * - the two renderers use different class names and input flags for task lists
 * - remark-gfm collects footnote definitions into a trailing section with backlinks,
 *   while the model path renders each definition where it appears
 */
describe('document markdown render equivalence', () => {
  const renderable = corpus.cases.filter((testCase) => testCase.valid && (testCase.source ?? '') !== '')

  it('covers the corpus constructs that both paths must agree on', () => {
    expect(renderable.length).toBeGreaterThanOrEqual(6)
  })

  for (const testCase of renderable) {
    it(`renders "${testCase.name}" the same from the model and from source`, () => {
      const references = testCase.authorized_references ?? []
      const fromModel = render(<MarkdownPreview model={testCase.model} references={references} />)
      const model = normalize(fromModel.container.firstElementChild)
      fromModel.unmount()

      const fromSource = render(<MarkdownPreview source={testCase.source} references={references} />)
      const live = normalize(fromSource.container.firstElementChild)
      fromSource.unmount()

      expect(model).not.toBe('')
      expect(model).toEqual(live)
    })
  }
})

type NormalNode = { tag: string; attributes: Record<string, string>; children: NormalNode[]; text: string }

const keptAttributes = ['data-ref-kind', 'data-ref-id', 'type', 'checked', 'start']
const footnoteBacklink = '↩'

function normalize(root: Element | null): string {
  if (!root) return ''
  return JSON.stringify(collectFootnotes(element(root)))
}

function element(node: Element): NormalNode {
  const attributes: Record<string, string> = {}
  for (const name of keptAttributes) {
    const value = node.getAttribute(name)
    if (value !== null) attributes[name] = value
  }
  if (node.tagName.toLowerCase() === 'input' && node instanceof HTMLInputElement) {
    attributes.checked = String(node.checked)
  }
  return {
    tag: node.tagName.toLowerCase(),
    attributes,
    children: children(node),
    text: '',
  }
}

function children(node: Element): NormalNode[] {
  const collected: NormalNode[] = []
  for (const child of Array.from(node.childNodes)) {
    if (child.nodeType === child.TEXT_NODE) {
      pushText(collected, child.textContent ?? '')
      continue
    }
    if (!(child instanceof Element)) continue
    if (isScreenReaderLabel(child)) continue
    const normalized = element(child)
    if (isTransparentWrapper(child, normalized)) {
      for (const inner of normalized.children) {
        if (inner.tag === '#text') pushText(collected, inner.text)
        else collected.push(inner)
      }
      continue
    }
    collected.push(normalized)
  }
  return collected
}

function pushText(collected: NormalNode[], raw: string) {
  const text = raw.replace(/\s+/gu, ' ')
  if (text.trim() === '') return
  const previous = collected[collected.length - 1]
  if (previous && previous.tag === '#text') {
    previous.text = `${previous.text}${text}`.replace(/\s+/gu, ' ')
    return
  }
  collected.push({ tag: '#text', attributes: {}, children: [], text })
}

// A span with nothing but text is a wrapper the model path adds; unwrapping it lets
// the comparison see the same inline content the live path produces.
function isTransparentWrapper(node: Element, normalized: NormalNode): boolean {
  return node.tagName.toLowerCase() === 'span' && Object.keys(normalized.attributes).length === 0
}

function isScreenReaderLabel(node: Element): boolean {
  return node.classList.contains('sr-only')
}

// Both paths carry the same footnote definitions in different shapes, so each is
// projected to `label -> text` and compared as an unordered set at the root.
function collectFootnotes(root: NormalNode): { blocks: NormalNode[]; footnotes: string[] } {
  const blocks: NormalNode[] = []
  const footnotes: string[] = []
  for (const child of root.children) {
    if (child.tag === 'section') {
      footnotes.push(...liveFootnotes(child))
      continue
    }
    if (child.tag === 'aside') {
      footnotes.push(modelFootnote(child))
      continue
    }
    blocks.push(child)
  }
  return { blocks, footnotes: footnotes.sort() }
}

function liveFootnotes(section: NormalNode): string[] {
  const list = section.children.find((child) => child.tag === 'ol')
  return (list?.children ?? []).map((item, index) => `${index + 1}:${plainText(item)}`)
}

function modelFootnote(aside: NormalNode): string {
  const label = aside.children.find((child) => child.tag === 'strong')
  const body = aside.children.filter((child) => child !== label)
  return `${plainText(label ?? { tag: '#text', attributes: {}, children: [], text: '' })}:${body.map(plainText).join(' ')}`
}

function plainText(node: NormalNode): string {
  if (node.tag === '#text') return node.text
  return node.children
    .map(plainText)
    .join('')
    .split(footnoteBacklink)
    .join('')
    .replace(/\s+/gu, ' ')
    .trim()
}
