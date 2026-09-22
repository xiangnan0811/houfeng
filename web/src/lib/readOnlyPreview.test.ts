import { describe, expect, it } from 'vitest'

import { READ_ONLY_PREVIEW } from './readOnlyPreview'

describe('READ_ONLY_PREVIEW', () => {
  it('is false unless VITE_READ_ONLY_PREVIEW is explicitly true', () => {
    expect(READ_ONLY_PREVIEW).toBe(false)
  })
})
