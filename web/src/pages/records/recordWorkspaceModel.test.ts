import { describe, expect, it } from 'vitest'

import { emptyRecordDraftPayload } from './recordPayload'
import {
  applyRecordTypeChange,
  defaultBusinessStatus,
  insertMarkdownAroundSelection,
  insertMarkdownSnippet,
  patchPrimarySubject,
  templateMarkdownForType,
  typeSupportsBusinessStatus,
} from './recordWorkspaceModel'

describe('recordWorkspaceModel', () => {
  it('defaults a valid business status only for workflow types', () => {
    expect(defaultBusinessStatus('troubleshooting')).toBe('pending_investigation')
    expect(defaultBusinessStatus('maintenance')).toBe('planned')
    expect(defaultBusinessStatus('note')).toBe('')
    expect(typeSupportsBusinessStatus('billing')).toBe(true)
    expect(typeSupportsBusinessStatus('important_finding')).toBe(false)
  })

  it('changes type without rewriting existing markdown', () => {
    const payload = { ...emptyRecordDraftPayload('usr_1'), body_markdown: 'keep this' }
    expect(applyRecordTypeChange('troubleshooting')).toEqual({
      record_type: 'troubleshooting',
      business_status: 'pending_investigation',
    })
    expect(payload.body_markdown).toBe('keep this')
  })

  it('patches only the primary subject', () => {
    const subjects = [
      { registry_version: 1, kind: 'vps' as const, role: 'affected' as const, source_id: 'vps_old', primary: true },
      { registry_version: 1, kind: 'vps' as const, role: 'context' as const, source_id: 'vps_keep', primary: false },
    ]
    expect(patchPrimarySubject(subjects, 'vps_new')).toEqual([
      { ...subjects[0], source_id: 'vps_new' },
      subjects[1],
    ])
  })

  it('wraps the current selection and appends templates without clobbering body', () => {
    expect(insertMarkdownAroundSelection('keep title', 5, 10, '**', '**')).toEqual({
      value: 'keep **title**',
      selectionStart: 7,
      selectionEnd: 12,
    })
    expect(insertMarkdownSnippet('existing notes', templateMarkdownForType('note'))).toBe(
      'existing notes\n\n## 备忘',
    )
    expect(insertMarkdownSnippet('## 备忘\nalready', templateMarkdownForType('note'))).toBe('## 备忘\nalready')
  })
})
