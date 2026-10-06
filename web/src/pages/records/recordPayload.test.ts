import { describe, expect, it } from 'vitest'

import type { RecordEvidenceItemInput } from '../../lib/types'
import { existingEvidenceItems } from './recordPayload'

describe('existingEvidenceItems', () => {
  it('declares every existing snapshot in revision order', () => {
    expect(existingEvidenceItems({ evidence_snapshot_ids: ['evs_b', 'evs_a'] })).toEqual([
      { existing_snapshot_id: 'evs_b' },
      { existing_snapshot_id: 'evs_a' },
    ])
    expect(existingEvidenceItems({ evidence_snapshot_ids: [] })).toEqual([])
    expect(existingEvidenceItems({})).toEqual([])
  })

  it('keeps each evidence item to exactly one reference kind', () => {
    const items: RecordEvidenceItemInput[] = [{ capture_intent_id: 'evi_1' }, { existing_snapshot_id: 'evs_1' }]
    // @ts-expect-error 同时声明两种引用会被后端拒绝，类型上也不允许。
    const both: RecordEvidenceItemInput = { capture_intent_id: 'evi_1', existing_snapshot_id: 'evs_1' }
    expect([...items, both]).toHaveLength(3)
  })
})
