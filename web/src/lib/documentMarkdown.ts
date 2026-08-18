import { InvalidCommentRenderModelError, validateCanonicalHTTPLink } from './commentMarkdown'
import type { RecordRenderModelStatus } from './types'

export const DOCUMENT_MARKDOWN_VERSION_V1 = 'houfeng_markdown/v1' as const

const MAX_DOCUMENT_RENDER_NODES = 4096
const MAX_DOCUMENT_RENDER_DEPTH = 16

export type DocumentTextNodeV1 = Readonly<{ type: 'text'; text: string }>
export type DocumentLineBreakNodeV1 = Readonly<{ type: 'line_break' }>
export type DocumentInlineCodeNodeV1 = Readonly<{ type: 'inline_code'; text: string }>
export type DocumentFootnoteRefNodeV1 = Readonly<{ type: 'footnote_ref'; text: string }>
export type DocumentInlineContainerNodeV1 = Readonly<{
  type: 'emphasis' | 'strong' | 'strikethrough'
  children: readonly DocumentInlineNodeV1[]
}>
export type DocumentLinkNodeV1 = Readonly<{
  type: 'link'
  href: string
  children: readonly DocumentInlineNodeV1[]
}>
export type DocumentInlineNodeV1 =
  | DocumentTextNodeV1
  | DocumentLineBreakNodeV1
  | DocumentInlineCodeNodeV1
  | DocumentFootnoteRefNodeV1
  | DocumentInlineContainerNodeV1
  | DocumentLinkNodeV1

export type DocumentParagraphNodeV1 = Readonly<{
  type: 'paragraph'
  children: readonly DocumentInlineNodeV1[]
}>
export type DocumentHeadingNodeV1 = Readonly<{
  type: 'heading'
  level: number
  children: readonly DocumentInlineNodeV1[]
}>
export type DocumentBlockquoteNodeV1 = Readonly<{
  type: 'blockquote'
  children: readonly DocumentRenderNodeV1[]
}>
export type DocumentThematicBreakNodeV1 = Readonly<{ type: 'thematic_break' }>
export type DocumentFencedCodeNodeV1 = Readonly<{ type: 'fenced_code'; text: string }>
export type DocumentListNodeV1 = Readonly<{
  type: 'ordered_list' | 'unordered_list'
  start?: number
  items: readonly (readonly DocumentInlineNodeV1[])[]
}>
export type DocumentTaskItemV1 = Readonly<{
  checked: boolean
  children: readonly DocumentInlineNodeV1[]
}>
export type DocumentTaskListNodeV1 = Readonly<{
  type: 'task_list'
  items: readonly DocumentTaskItemV1[]
}>
export type DocumentTableNodeV1 = Readonly<{
  type: 'table'
  header: readonly (readonly DocumentInlineNodeV1[])[]
  rows: readonly (readonly (readonly DocumentInlineNodeV1[])[])[]
}>
export type DocumentFootnoteDefNodeV1 = Readonly<{
  type: 'footnote_def'
  text: string
  children: readonly DocumentRenderNodeV1[]
}>
export type DocumentReferenceNodeV1 = Readonly<{
  type: 'reference'
  kind: 'evidence' | 'attachment'
  id: string
  children: readonly DocumentInlineNodeV1[]
}>

export type DocumentRenderNodeV1 =
  | DocumentParagraphNodeV1
  | DocumentHeadingNodeV1
  | DocumentBlockquoteNodeV1
  | DocumentThematicBreakNodeV1
  | DocumentFencedCodeNodeV1
  | DocumentListNodeV1
  | DocumentTaskListNodeV1
  | DocumentTableNodeV1
  | DocumentFootnoteDefNodeV1
  | DocumentReferenceNodeV1

export type DocumentRenderModelV1 = Readonly<{
  version: typeof DOCUMENT_MARKDOWN_VERSION_V1
  nodes: readonly DocumentRenderNodeV1[]
}>

export type DocumentReference = Readonly<{ kind: 'evidence' | 'attachment'; id: string }>

export class InvalidDocumentRenderModelError extends Error {
  constructor() {
    super('invalid houfeng_markdown/v1 render model')
    this.name = 'InvalidDocumentRenderModelError'
  }
}

type DecoderState = { count: number }
type UnknownObject = Record<string, unknown>

export function decodeDocumentRenderModelV1(value: unknown): DocumentRenderModelV1 {
  const model = expectObject(value)
  expectExactKeys(model, ['nodes', 'version'])
  if (model.version !== DOCUMENT_MARKDOWN_VERSION_V1 || !Array.isArray(model.nodes)) invalid()
  const state: DecoderState = { count: 0 }
  return {
    version: DOCUMENT_MARKDOWN_VERSION_V1,
    nodes: model.nodes.map((node) => decodeBlockNode(node, 1, state)),
  }
}

/**
 * The render status arrives on an untyped revision payload, so it is narrowed here
 * rather than trusted. An unrecognised value is reported as undefined so the reader
 * stays silent instead of claiming the body fell back when it did not.
 */
export function decodeRenderModelStatusV1(value: unknown): RecordRenderModelStatus | undefined {
  return value === 'ready' || value === 'unsupported' ? value : undefined
}

export function formatHoufengReference(kind: DocumentReference['kind'], id: string, label: string): string {
  return `<!-- houfeng-ref:v1 ${kind} ${id} -->\n[${label}](houfeng-${kind}:${id})`
}

export function insertMaterialToken(source: string, item: DocumentReference & { label: string }): string {
  const token = formatHoufengReference(item.kind, item.id, item.label)
  if (source.includes(`houfeng-${item.kind}:${item.id}`)) return source
  return source.trim().length === 0 ? token : `${source.trimEnd()}\n\n${token}`
}

export function extractTaskItems(source: string): readonly { checked: boolean; text: string }[] {
  return source.split('\n').flatMap((line) => {
    const match = /^[ \t]{0,3}(?:[-*+]|\d+\.) \[([ xX])\][ \t]+(.+)$/u.exec(line)
    return match ? [{ checked: match[1] !== ' ', text: match[2] ?? '' }] : []
  })
}

function decodeBlockNode(value: unknown, depth: number, state: DecoderState): DocumentRenderNodeV1 {
  const node = countedNode(value, depth, state)
  switch (node.type) {
    case 'paragraph':
      expectExactKeys(node, ['children', 'type'])
      return { type: 'paragraph', children: decodeInlineChildren(node.children, depth + 1, state) }
    case 'heading':
      expectExactKeys(node, ['children', 'level', 'type'])
      if (!Number.isSafeInteger(node.level) || Number(node.level) < 1 || Number(node.level) > 6) invalid()
      return { type: 'heading', level: Number(node.level), children: decodeInlineChildren(node.children, depth + 1, state) }
    case 'blockquote':
      expectExactKeys(node, ['children', 'type'])
      return { type: 'blockquote', children: decodeBlockChildren(node.children, depth + 1, state) }
    case 'thematic_break':
      expectExactKeys(node, ['type'])
      return { type: 'thematic_break' }
    case 'fenced_code':
      expectExactKeys(node, ['text', 'type'])
      return { type: 'fenced_code', text: expectString(node.text, true) }
    case 'ordered_list':
      expectExactKeys(node, ['items', 'start', 'type'])
      if (!Number.isSafeInteger(node.start) || Number(node.start) <= 0) invalid()
      return { type: 'ordered_list', start: Number(node.start), items: decodeListItems(node.items, depth + 1, state) }
    case 'unordered_list':
      expectExactKeys(node, ['items', 'type'])
      return { type: 'unordered_list', items: decodeListItems(node.items, depth + 1, state) }
    case 'task_list':
      expectExactKeys(node, ['items', 'type'])
      return { type: 'task_list', items: decodeTaskItems(node.items, depth + 1, state) }
    case 'table':
      expectExactKeys(node, ['header', 'rows', 'type'])
      return decodeTable(node, depth, state)
    case 'footnote_def':
      expectExactKeys(node, ['children', 'text', 'type'])
      return {
        type: 'footnote_def',
        text: expectString(node.text, false),
        children: decodeBlockChildren(node.children, depth + 1, state),
      }
    case 'reference':
      expectExactKeys(node, ['children', 'id', 'kind', 'type'])
      if (node.kind !== 'evidence' && node.kind !== 'attachment') invalid()
      return {
        type: 'reference',
        kind: node.kind,
        id: expectString(node.id, false),
        children: decodeInlineChildren(node.children, depth + 1, state),
      }
    default:
      return invalid()
  }
}

function decodeTable(node: UnknownObject, depth: number, state: DecoderState): DocumentTableNodeV1 {
  if (!Array.isArray(node.header) || node.header.length === 0 || !Array.isArray(node.rows)) invalid()
  const header = node.header.map((cell) => decodeInlineChildren(cell, depth + 1, state))
  const rows = node.rows.map((row) => {
    if (!Array.isArray(row) || row.length !== header.length) invalid()
    return row.map((cell) => decodeInlineChildren(cell, depth + 1, state))
  })
  return { type: 'table', header, rows }
}

function decodeInlineNode(value: unknown, depth: number, state: DecoderState): DocumentInlineNodeV1 {
  const node = countedNode(value, depth, state)
  switch (node.type) {
    case 'text':
      expectExactKeys(node, ['text', 'type'])
      return { type: 'text', text: expectString(node.text, false) }
    case 'line_break':
      expectExactKeys(node, ['type'])
      return { type: 'line_break' }
    case 'inline_code':
      expectExactKeys(node, ['text', 'type'])
      return { type: 'inline_code', text: expectString(node.text, false) }
    case 'footnote_ref':
      expectExactKeys(node, ['text', 'type'])
      return { type: 'footnote_ref', text: expectString(node.text, false) }
    case 'emphasis':
    case 'strong':
    case 'strikethrough':
      expectExactKeys(node, ['children', 'type'])
      return { type: node.type, children: decodeInlineChildren(node.children, depth + 1, state) }
    case 'link':
      expectExactKeys(node, ['children', 'href', 'type'])
      return {
        type: 'link',
        href: validateDocumentHTTPLink(node.href),
        children: decodeInlineChildren(node.children, depth + 1, state),
      }
    default:
      return invalid()
  }
}

function decodeBlockChildren(value: unknown, depth: number, state: DecoderState): readonly DocumentRenderNodeV1[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((child) => decodeBlockNode(child, depth, state))
}

function decodeInlineChildren(value: unknown, depth: number, state: DecoderState): readonly DocumentInlineNodeV1[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((child) => decodeInlineNode(child, depth, state))
}

function decodeListItems(value: unknown, depth: number, state: DecoderState): readonly (readonly DocumentInlineNodeV1[])[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((item) => decodeInlineChildren(item, depth, state))
}

function decodeTaskItems(value: unknown, depth: number, state: DecoderState): readonly DocumentTaskItemV1[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((item) => {
    const node = expectObject(item)
    expectExactKeys(node, ['checked', 'children'])
    if (typeof node.checked !== 'boolean') invalid()
    return { checked: node.checked, children: decodeInlineChildren(node.children, depth, state) }
  })
}

function countedNode(value: unknown, depth: number, state: DecoderState): UnknownObject {
  state.count += 1
  if (depth > MAX_DOCUMENT_RENDER_DEPTH || state.count > MAX_DOCUMENT_RENDER_NODES) invalid()
  const node = expectObject(value)
  if (typeof node.type !== 'string') invalid()
  return node
}

function expectObject(value: unknown): UnknownObject {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) invalid()
  return value as UnknownObject
}

function expectExactKeys(value: UnknownObject, expected: readonly string[]): void {
  const keys = Object.keys(value).sort()
  if (keys.length !== expected.length || keys.some((key, index) => key !== expected[index])) invalid()
}

function validateDocumentHTTPLink(value: unknown): string {
  try {
    return validateCanonicalHTTPLink(value)
  } catch (error) {
    if (error instanceof InvalidCommentRenderModelError) invalid()
    throw error
  }
}

function expectString(value: unknown, allowEmpty: boolean): string {
  if (typeof value !== 'string' || (!allowEmpty && value.length === 0)) invalid()
  return value
}

function invalid(): never {
  throw new InvalidDocumentRenderModelError()
}
