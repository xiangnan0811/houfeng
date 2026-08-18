import type { RecordDetail, RecordRevision } from '../../lib/types'
import { DOCUMENT_MARKDOWN_VERSION_V1 } from '../../lib/documentMarkdown'

export { emptyRecordDraftPayload, payloadFromRevision } from './recordPayload'

export function recordRevisionFixture(overrides: Partial<RecordRevision> = {}): RecordRevision {
  return {
    record_id: 'rec_001',
    revision_id: 'rrv_001',
    revision_no: 1,
    title: 'Database outage',
    body_markdown: '# Details\nRecovered.',
    markdown_dialect_version: 1,
    record_type: 'note',
    impact_level: 'high',
    visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
    subjects: [{
      registry_version: 1,
      kind: 'vps',
      role: 'affected',
      source_id: 'vps_0123456789abcdef',
      primary: true,
      identity: { display_name: 'VPS Alpha', provider: 'Example Cloud' },
    }],
    tags: [],
    attachment_ids: ['att_httpfirst'],
    owner_id: 'usr_1',
    participants: [{ participant_id: 'usr_2', display_name: '值班' }],
    author_id: 'usr_1',
    save_reason: 'initial record',
    created_at: '2026-08-03T14:00:00Z',
    render_model: {
      version: DOCUMENT_MARKDOWN_VERSION_V1,
      nodes: [
        { type: 'heading', level: 1, children: [{ type: 'text', text: 'Details' }] },
        { type: 'paragraph', children: [{ type: 'text', text: 'Recovered.' }] },
      ],
    },
    ...overrides,
  }
}

export function recordDetailFixture(overrides: Partial<RecordDetail> = {}): RecordDetail {
  const current = recordRevisionFixture(overrides.current)
  return {
    record_id: 'rec_001',
    lifecycle: 'active',
    current_revision_id: current.revision_id,
    lock_version: 7,
    authorization_epoch: 5,
    capabilities: {
      read: true,
      update: true,
      archive: true,
      restore: true,
      draft: true,
      permanent_delete: false,
    },
    created_at: '2026-08-03T14:00:00Z',
    updated_at: '2026-08-03T14:00:00Z',
    ...overrides,
    current,
  }
}
