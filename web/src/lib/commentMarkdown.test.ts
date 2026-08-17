import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import {
  COMMENT_MARKDOWN_VERSION_V1,
  InvalidCommentRenderModelError,
  decodeCommentRenderModelV1,
} from './commentMarkdown'

type Corpus = {
  contract_version: string
  cases: Array<{ name: string; valid: boolean; model?: unknown }>
  hostile_models: Array<{ name: string; model: unknown }>
}

const corpus = JSON.parse(readFileSync(
  '../internal/center/recordcollaboration/testdata/comment_markdown_v1.json',
  'utf8',
)) as Corpus

describe('comment_markdown/v1 Web decoder', () => {
  it('consumes every server-owned golden render model without reinterpretation', () => {
    expect(corpus.contract_version).toBe(COMMENT_MARKDOWN_VERSION_V1)
    for (const testCase of corpus.cases.filter((candidate) => candidate.valid)) {
      expect(testCase.model, testCase.name).toBeDefined()
      expect(decodeCommentRenderModelV1(testCase.model), testCase.name).toEqual(testCase.model)
    }
  })

  it('rejects every server-owned hostile render shape', () => {
    for (const testCase of corpus.hostile_models) {
      expect(
        () => decodeCommentRenderModelV1(testCase.model),
        testCase.name,
      ).toThrow(InvalidCommentRenderModelError)
    }
  })

  it('enforces node, depth, and canonical HTTP(S) link bounds', () => {
    const text = { type: 'text', text: 'x' }
    expect(() => decodeCommentRenderModelV1({
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: Array.from({ length: 513 }, () => ({ type: 'paragraph', children: [text] })),
    })).toThrow(InvalidCommentRenderModelError)

    let nested: unknown = text
    for (let index = 0; index < 8; index += 1) {
      nested = { type: 'strong', children: [nested] }
    }
    expect(() => decodeCommentRenderModelV1({
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: [{ type: 'paragraph', children: [nested] }],
    })).toThrow(InvalidCommentRenderModelError)

    for (const href of [
      'javascript:alert(1)',
      'https://user:pass@example.com/path',
      'https://EXAMPLE.com/path',
      'https://example.com:443/path',
      `https://example.com/${'a'.repeat(2030)}`,
    ]) {
      expect(() => decodeCommentRenderModelV1({
        version: COMMENT_MARKDOWN_VERSION_V1,
        nodes: [{ type: 'paragraph', children: [{ type: 'link', href, children: [text] }] }],
      }), href).toThrow(InvalidCommentRenderModelError)
    }
  })

  it('returns an owned clone rather than retaining untrusted input objects', () => {
    const input = {
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: [{ type: 'paragraph', children: [{ type: 'text', text: 'safe' }] }],
    }
    const decoded = decodeCommentRenderModelV1(input)
    input.nodes[0]!.children[0]!.text = 'changed'
    expect(decoded.nodes[0]).toEqual({
      type: 'paragraph',
      children: [{ type: 'text', text: 'safe' }],
    })
  })
})
