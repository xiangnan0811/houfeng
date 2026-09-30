import { describe, expect, it } from 'vitest'

import { emptyRecordDraftPayload, recordRevisionFixture } from '../testFixtures'
import { countClass, displaySubject, impactTone, statusGroupTone, subjectLabel } from './recordPresentation'

describe('recordPresentation', () => {
  it('keeps counts neutral at zero and highlights only positive counts', () => {
    expect(countClass(0)).toBe('record-count')
    expect(countClass(3)).toBe('record-count record-count--active')
  })

  it('maps only high free-text impact levels to alert tones', () => {
    expect(impactTone('critical')).toBe('critical')
    expect(impactTone(' High ')).toBe('alert')
    expect(impactTone('medium')).toBe('neutral')
    expect(impactTone('')).toBe('neutral')
  })

  it('maps status groups to state tones', () => {
    expect(statusGroupTone('completed')).toBe('normal')
    expect(statusGroupTone('waiting')).toBe('maintenance')
    expect(statusGroupTone(undefined)).toBe('neutral')
  })

  it('prefers the published identity name and falls back to the source id', () => {
    const revision = recordRevisionFixture()
    expect(subjectLabel(displaySubject(revision, emptyRecordDraftPayload('usr_1')))).toBe('VPS · VPS Alpha')
    const payload = { ...emptyRecordDraftPayload('usr_1'), subjects: [{
      registry_version: 1, kind: 'target' as const, role: 'affected' as const, source_id: 'tgt_01', primary: true,
    }] }
    expect(subjectLabel(displaySubject(null, payload))).toBe('探测目标 · tgt_01')
    expect(subjectLabel(undefined)).toBe('')
  })
})
