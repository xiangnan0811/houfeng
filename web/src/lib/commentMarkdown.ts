export const COMMENT_MARKDOWN_VERSION_V1 = 'comment_markdown/v1' as const

const MAX_COMMENT_RENDER_NODES = 512
const MAX_COMMENT_RENDER_DEPTH = 8
const MAX_COMMENT_LINK_BYTES = 2_048

export type CommentTextNodeV1 = Readonly<{ type: 'text'; text: string }>
export type CommentLineBreakNodeV1 = Readonly<{ type: 'line_break' }>
export type CommentInlineCodeNodeV1 = Readonly<{ type: 'inline_code'; text: string }>
export type CommentInlineContainerNodeV1 = Readonly<{
  type: 'emphasis' | 'strong' | 'strikethrough'
  children: readonly CommentInlineNodeV1[]
}>
export type CommentLinkNodeV1 = Readonly<{
  type: 'link'
  href: string
  children: readonly CommentInlineNodeV1[]
}>
export type CommentInlineNodeV1 =
  | CommentTextNodeV1
  | CommentLineBreakNodeV1
  | CommentInlineCodeNodeV1
  | CommentInlineContainerNodeV1
  | CommentLinkNodeV1

export type CommentParagraphNodeV1 = Readonly<{
  type: 'paragraph'
  children: readonly CommentInlineNodeV1[]
}>
export type CommentFencedCodeNodeV1 = Readonly<{ type: 'fenced_code'; text: string }>
export type CommentListNodeV1 = Readonly<{
  type: 'ordered_list' | 'unordered_list'
  start?: number
  items: readonly (readonly CommentInlineNodeV1[])[]
}>
export type CommentRenderNodeV1 = CommentParagraphNodeV1 | CommentFencedCodeNodeV1 | CommentListNodeV1

export type CommentRenderModelV1 = Readonly<{
  version: typeof COMMENT_MARKDOWN_VERSION_V1
  nodes: readonly CommentRenderNodeV1[]
}>

export class InvalidCommentRenderModelError extends Error {
  constructor() {
    super('invalid comment_markdown/v1 render model')
    this.name = 'InvalidCommentRenderModelError'
  }
}

type DecoderState = { count: number }
type UnknownObject = Record<string, unknown>

export function decodeCommentRenderModelV1(value: unknown): CommentRenderModelV1 {
  const model = expectObject(value)
  expectExactKeys(model, ['nodes', 'version'])
  if (model.version !== COMMENT_MARKDOWN_VERSION_V1 || !Array.isArray(model.nodes) || model.nodes.length === 0) {
    invalid()
  }
  const state: DecoderState = { count: 0 }
  return {
    version: COMMENT_MARKDOWN_VERSION_V1,
    nodes: model.nodes.map((node) => decodeBlockNode(node, 1, state)),
  }
}

function decodeBlockNode(value: unknown, depth: number, state: DecoderState): CommentRenderNodeV1 {
  const node = countedNode(value, depth, state)
  switch (node.type) {
    case 'paragraph':
      expectExactKeys(node, ['children', 'type'])
      return { type: 'paragraph', children: decodeInlineChildren(node.children, depth + 1, state) }
    case 'fenced_code':
      expectExactKeys(node, ['text', 'type'])
      return { type: 'fenced_code', text: expectString(node.text, true) }
    case 'ordered_list':
      expectExactKeys(node, ['items', 'start', 'type'])
      if (!Number.isSafeInteger(node.start) || Number(node.start) <= 0) invalid()
      return {
        type: 'ordered_list',
        start: Number(node.start),
        items: decodeListItems(node.items, depth + 1, state),
      }
    case 'unordered_list':
      expectExactKeys(node, ['items', 'type'])
      return { type: 'unordered_list', items: decodeListItems(node.items, depth + 1, state) }
    default:
      return invalid()
  }
}

function decodeInlineNode(value: unknown, depth: number, state: DecoderState): CommentInlineNodeV1 {
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
    case 'emphasis':
    case 'strong':
    case 'strikethrough':
      expectExactKeys(node, ['children', 'type'])
      return { type: node.type, children: decodeInlineChildren(node.children, depth + 1, state) }
    case 'link':
      expectExactKeys(node, ['children', 'href', 'type'])
      return {
        type: 'link',
        href: expectCanonicalHTTPLink(node.href),
        children: decodeInlineChildren(node.children, depth + 1, state),
      }
    default:
      return invalid()
  }
}

function decodeInlineChildren(value: unknown, depth: number, state: DecoderState): readonly CommentInlineNodeV1[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((child) => decodeInlineNode(child, depth, state))
}

function decodeListItems(value: unknown, depth: number, state: DecoderState): readonly (readonly CommentInlineNodeV1[])[] {
  if (!Array.isArray(value) || value.length === 0) invalid()
  return value.map((item) => decodeInlineChildren(item, depth, state))
}

function countedNode(value: unknown, depth: number, state: DecoderState): UnknownObject {
  state.count += 1
  if (depth > MAX_COMMENT_RENDER_DEPTH || state.count > MAX_COMMENT_RENDER_NODES) invalid()
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

function expectString(value: unknown, allowEmpty: boolean): string {
  if (typeof value !== 'string' || (!allowEmpty && value.length === 0) || !isWellFormedUnicode(value)) invalid()
  return value
}

function expectCanonicalHTTPLink(value: unknown): string {
  const href = expectString(value, false)
  if (new TextEncoder().encode(href).length > MAX_COMMENT_LINK_BYTES || /[\s\\"'<>\0]/u.test(href)) invalid()
  for (let index = 0; index < href.length; index += 1) {
    const code = href.charCodeAt(index)
    if (code < 0x21 || code > 0x7f) invalid()
    if (href[index] === '%') {
      if (!/^[0-9A-F]{2}$/u.test(href.slice(index + 1, index + 3))) invalid()
      index += 2
    }
  }
  const authorityMatch = /^(https?):\/\/([^/?#]+)(.*)$/u.exec(href)
  if (authorityMatch === null) invalid()
  const scheme = authorityMatch[1]
  const authority = authorityMatch[2]
  const suffix = authorityMatch[3]
  if (authority === undefined || scheme === undefined || suffix === undefined || authority.includes('@') || authority !== authority.toLowerCase()) invalid()
  if ((scheme === 'http' && hasPort(authority, '80')) || (scheme === 'https' && hasPort(authority, '443'))) invalid()
  if (hasDotPathSegment(suffix)) invalid()
  let parsed: URL
  try {
    parsed = new URL(href)
  } catch {
    return invalid()
  }
  if (parsed.protocol !== `${scheme}:` || parsed.username !== '' || parsed.password !== '' || parsed.hostname === '') invalid()
  const browserCanonical = suffix === '' || suffix.startsWith('?') || suffix.startsWith('#')
    ? `${scheme}://${authority}/${suffix}`
    : href
  if (parsed.href !== browserCanonical) invalid()
  return href
}

function hasDotPathSegment(suffix: string): boolean {
  const path = suffix.split(/[?#]/u, 1)[0] ?? ''
  return path.split('/').some((segment) => {
    const normalized = segment.replaceAll('%2E', '.')
    return normalized === '.' || normalized === '..'
  })
}

function hasPort(authority: string, port: string): boolean {
  if (authority.startsWith('[')) return authority.endsWith(`]:${port}`)
  return authority.endsWith(`:${port}`)
}

function isWellFormedUnicode(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1)
      if (next < 0xdc00 || next > 0xdfff) return false
      index += 1
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return false
    }
  }
  return true
}

function invalid(): never {
  throw new InvalidCommentRenderModelError()
}
