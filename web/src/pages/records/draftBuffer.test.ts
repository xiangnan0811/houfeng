import { describe, expect, it } from 'vitest'

import type { RecordDraftPayload } from '../../lib/types'
import {
  DRAFT_BUFFER_TTL_MS,
  draftBufferKey,
  memoryDraftBufferStore,
  readUnsyncedDraft,
  discardUserDrafts,
  writeUnsyncedDraft,
} from './draftBuffer'

function payload(title: string): RecordDraftPayload {
  return {
    title,
    body_markdown: 'body',
    markdown_dialect_version: 1,
    record_type: 'note',
    business_status: '',
    impact_level: 'medium',
    visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
    subjects: [],
    tags: [],
    attachment_ids: ['att_1'],
    owner_id: 'usr_1',
    participant_ids: [],
    save_reason: 'edit',
  }
}

describe('draftBuffer', () => {
  it('keeps unsynced payload for 24h and drops expired copies', async () => {
    const store = memoryDraftBufferStore()
    const key = draftBufferKey('usr_1', 'rec_001')
    await writeUnsyncedDraft(store, {
      key,
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: payload('local'),
      updatedAt: 1,
    })
    await expect(readUnsyncedDraft(store, key, 2)).resolves.toMatchObject({ payload: { title: 'local' } })
    await expect(readUnsyncedDraft(store, key, 1 + DRAFT_BUFFER_TTL_MS + 1)).resolves.toBeUndefined()
  })

  it('clears only the current user on logout', async () => {
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: payload('mine'),
      updatedAt: 1,
    })
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_2'),
      userId: 'usr_2',
      payload: payload('theirs'),
      updatedAt: 1,
    })
    await discardUserDrafts('usr_1', store)
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1'), 2)).resolves.toBeUndefined()
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_2'), 2)).resolves.toMatchObject({ payload: { title: 'theirs' } })
  })
})
