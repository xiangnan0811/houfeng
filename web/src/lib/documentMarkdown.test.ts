import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import {
  DOCUMENT_MARKDOWN_VERSION_V1,
  InvalidDocumentRenderModelError,
  decodeDocumentRenderModelV1,
  decodeRenderModelStatusV1,
  extractTaskItems,
  formatHoufengReference,
  insertMaterialToken,
} from './documentMarkdown'

type Corpus = {
  contract_version: string
  cases: Array<{ name: string; valid: boolean; model?: unknown }>
  hostile_models: Array<{ name: string; model: unknown }>
}

const corpus = JSON.parse(readFileSync('../testdata/markdown/houfeng-v1.json', 'utf8')) as Corpus

describe('houfeng_markdown/v1 Web decoder', () => {
  it('consumes every server-owned golden render model', () => {
    expect(corpus.contract_version).toBe(DOCUMENT_MARKDOWN_VERSION_V1)
    for (const testCase of corpus.cases.filter((candidate) => candidate.valid)) {
      expect(testCase.model, testCase.name).toBeDefined()
      expect(decodeDocumentRenderModelV1(testCase.model), testCase.name).toEqual(testCase.model)
    }
  })

  it('rejects every server-owned hostile render shape', () => {
    for (const testCase of corpus.hostile_models) {
      expect(() => decodeDocumentRenderModelV1(testCase.model), testCase.name).toThrow(InvalidDocumentRenderModelError)
    }
  })

  // The revision payload is not schema-decoded, so the status is narrowed at use.
  it('narrows the render model status and drops anything else', () => {
    expect(decodeRenderModelStatusV1('ready')).toBe('ready')
    expect(decodeRenderModelStatusV1('unsupported')).toBe('unsupported')
    for (const value of [undefined, null, '', 'READY', 'partial', 1, true, {}, ['ready']]) {
      expect(decodeRenderModelStatusV1(value), String(value)).toBeUndefined()
    }
  })

  it('formats stable reference tokens and extracts checklist items', () => {
    expect(formatHoufengReference('evidence', 'ev_7K2P', '系统证据：第三晚 TCP 观测')).toBe(
      '<!-- houfeng-ref:v1 evidence ev_7K2P -->\n[系统证据：第三晚 TCP 观测](houfeng-evidence:ev_7K2P)',
    )
    expect(extractTaskItems('- [x] execute\n- [ ] verify\nplain')).toEqual([
      { checked: true, text: 'execute' },
      { checked: false, text: 'verify' },
    ])
    expect(insertMaterialToken('', { kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测' })).toContain('houfeng-evidence:ev_7K2P')
  })
})
