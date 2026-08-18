import type { RecordDraftPayload, RecordRevision } from '../../lib/types'

export function emptyRecordDraftPayload(ownerId = ''): RecordDraftPayload {
  return {
    title: '',
    body_markdown: '',
    markdown_dialect_version: 1,
    record_type: 'note',
    business_status: '',
    impact_level: 'medium',
    visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
    subjects: [{
      registry_version: 1,
      kind: 'vps',
      role: 'affected',
      source_id: '',
      primary: true,
    }],
    tags: [],
    attachment_ids: [],
    owner_id: ownerId,
    participant_ids: [],
    save_reason: '',
  }
}

export function payloadFromRevision(revision: RecordRevision): RecordDraftPayload {
  return {
    title: revision.title,
    body_markdown: revision.body_markdown,
    markdown_dialect_version: 1,
    record_type: revision.record_type,
    business_status: revision.business_status ?? '',
    impact_level: revision.impact_level,
    occurred_at: revision.occurred_at ?? null,
    completed_at: revision.completed_at ?? null,
    visibility: revision.visibility,
    subjects: revision.subjects.map((subject) => ({
      registry_version: subject.registry_version,
      kind: subject.kind,
      role: subject.role,
      source_id: subject.source_id,
      primary: subject.primary,
    })),
    tags: [...revision.tags],
    attachment_ids: [...revision.attachment_ids],
    owner_id: revision.owner_id ?? '',
    participant_ids: revision.participants.map((participant) => participant.participant_id),
    follow_up_at: revision.follow_up_at ?? null,
    template: revision.template ?? null,
    save_reason: '',
  }
}
