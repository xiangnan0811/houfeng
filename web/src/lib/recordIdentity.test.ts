import { describe, expect, it } from 'vitest'

import { isRecordId } from './recordIdentity'

describe('record identity grammar', () => {
  it('accepts the full legacy domain and both halves of the reserved domain', () => {
    for (const id of [
      'rec_a', 'rec_0', `rec_${'z'.repeat(64)}`, 'rec_0123456789abcdef', 'rec_e2e001',
      `rec2_${'0'.repeat(32)}`, `rec2_7${'f'.repeat(31)}`, `rec2_8${'0'.repeat(31)}`, `rec2_${'f'.repeat(32)}`,
    ]) expect(isRecordId(id), id).toBe(true)
  })

  it('rejects out-of-bound lengths and characters in either domain', () => {
    for (const id of [
      '', 'rec_', `rec_${'a'.repeat(65)}`, 'rec_A', 'rec_a-b', 'rec_a_b', 'rec_a ', ' rec_a', 'rec_a\n', 'rec_é',
      'rec2_', `rec2_${'0'.repeat(31)}`, `rec2_${'0'.repeat(33)}`, `rec2_${'A'.repeat(32)}`,
      `rec2_${'g'.repeat(32)}`, `rec2_${'0'.repeat(31)}\n`, `rec2-${'0'.repeat(32)}`,
    ]) expect(isRecordId(id), JSON.stringify(id)).toBe(false)
  })

  it('does not admit neighbouring prefixes', () => {
    for (const id of [
      `rec3_${'0'.repeat(32)}`, `rec_2_${'0'.repeat(32)}`, `REC2_${'0'.repeat(32)}`, 'REC_a', 'rect_a',
      'rrv_a', 'rpo_a', 'rcm_a', 'ract_a', 'usr_a', 'evs_a', 'rdf_a',
    ]) expect(isRecordId(id), id).toBe(false)
  })
})
