import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { DOCUMENT_MARKDOWN_VERSION_V1, decodeDocumentRenderModelV1 } from '../../../lib/documentMarkdown'

type Corpus = {
  contract_version: string
  cases: Array<{ name: string; valid: boolean; source?: string; model?: unknown }>
}

const corpus = JSON.parse(readFileSync('../testdata/markdown/houfeng-v1.json', 'utf8')) as Corpus

describe('markdown golden corpus', () => {
  it('shares the same document contract as the Go renderer', () => {
    expect(corpus.contract_version).toBe(DOCUMENT_MARKDOWN_VERSION_V1)
    const heading = corpus.cases.find((testCase) => testCase.name === 'heading paragraph and list')
    expect(heading?.valid).toBe(true)
    const model = decodeDocumentRenderModelV1(heading?.model)
    expect(model.nodes[0]).toMatchObject({ type: 'heading', level: 1 })
  })

  it('decodes every valid golden model and rejects invalid source as not a usable model', () => {
    for (const testCase of corpus.cases) {
      if (testCase.valid) {
        expect(testCase.model, testCase.name).toBeDefined()
        expect(decodeDocumentRenderModelV1(testCase.model), testCase.name).toEqual(testCase.model)
        continue
      }
      if (testCase.model) {
        expect(() => decodeDocumentRenderModelV1(testCase.model), testCase.name).toThrow()
      }
    }
  })
})
