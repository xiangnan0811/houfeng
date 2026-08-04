import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './apiRequest'
import {
  archiveRecord,
  createRecord,
  createRecordDraft,
  createRecordRevision,
  discardRecordDraft,
  executeRecordPermanentDeletion,
  getRecord,
  getRecordDeletionOperation,
  getRecordDraft,
  getRecordRevision,
  listRecordDrafts,
  listRecordRevisions,
  listRecords,
  patchRecordDraft,
  previewRecordPermanentDeletion,
  restoreRecord,
  restoreRecordRevision,
} from './recordsApi'
import type {
  CreateRecordDraftInput,
  PublishRecordInput,
  PublishRecordRevisionInput,
  RecordDeletionExecuteInput,
  RecordDeletionOperation,
  RecordDeletionPreview,
  RecordDetail,
  RecordDraft,
  RecordDraftPayload,
  RecordLifecycleResult,
  RecordListResponse,
  RecordMutationResult,
  RecordRevision,
  RecordRevisionListResponse,
  RecordSubjectReference,
} from './types'

const requestDefaults = {
  headers: { Accept: 'application/json' },
  cache: 'no-store',
  credentials: 'include',
}

function mockResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const subjectReference = {
  registry_version: 1,
  kind: 'vps',
  role: 'affected',
  source_id: 'vps_contract',
  primary: true,
} satisfies RecordSubjectReference

const payload = {
  title: 'Provider incident',
  body_markdown: '# Incident',
  markdown_dialect_version: 1,
  record_type: 'troubleshooting',
  business_status: 'investigating',
  impact_level: 'high',
  occurred_at: '2026-08-03T10:00:00Z',
  visibility: {
    kind: 'restricted',
    allowed_roles: ['project_admin'],
    allowed_group_ids: ['rag_ops'],
  },
  subjects: [subjectReference],
  tags: ['provider'],
  owner_id: 'usr_owner',
  participant_ids: ['usr_participant'],
  follow_up_at: '2026-08-04T10:00:00Z',
  template: { id: 'tpl_incident', version: 1 },
  save_reason: 'investigation updated',
} satisfies RecordDraftPayload

const revision = {
  record_id: 'rec_contract',
  revision_id: 'rrv_contract',
  revision_no: 1,
  title: payload.title,
  body_markdown: payload.body_markdown,
  markdown_dialect_version: 1,
  record_type: 'troubleshooting',
  business_status: 'investigating',
  status_group: 'in_progress',
  impact_level: 'high',
  occurred_at: payload.occurred_at,
  visibility: payload.visibility,
  subjects: [{
    ...subjectReference,
    identity: {
      display_name: 'Contract VPS',
      provider: 'Example Cloud',
      region: 'ap-east',
      purpose: 'control-plane',
    },
  }],
  tags: payload.tags,
  owner_id: payload.owner_id,
  participants: [{ participant_id: 'usr_participant', display_name: 'Operator' }],
  follow_up_at: payload.follow_up_at,
  template: payload.template,
  author_id: 'usr_author',
  save_reason: payload.save_reason,
  created_at: '2026-08-03T10:10:00Z',
} satisfies RecordRevision

const record = {
  record_id: revision.record_id,
  lifecycle: 'active',
  current_revision_id: revision.revision_id,
  lock_version: 1,
  authorization_epoch: 1,
  current: revision,
  capabilities: {
    read: true,
    update: true,
    archive: true,
    restore: false,
    draft: true,
    permanent_delete: false,
  },
  created_at: revision.created_at,
  updated_at: revision.created_at,
} satisfies RecordDetail

const draft = {
  draft_id: 'rdf_contract',
  record_id: record.record_id,
  base_revision_id: revision.revision_id,
  payload,
  version: 2,
  etag: 'rdt1_contract',
  warning_at: '2026-10-25T10:00:00Z',
  created_at: '2026-08-03T10:00:00Z',
  updated_at: '2026-08-03T10:10:00Z',
  expires_at: '2026-11-01T10:00:00Z',
} satisfies RecordDraft

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Records API transport', () => {
  it('normalizes record list filters and preserves the server cursor response', async () => {
    const response = { items: [record], next_cursor: ' cursor-next ' } satisfies RecordListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(listRecords({
      q: '  provider incident  ',
      lifecycle: 'active',
      record_type: 'troubleshooting',
      sort: 'updated_at_desc',
      limit: 25,
      cursor: '  cursor-current  ',
    })).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/records?q=provider+incident&lifecycle=active&record_type=troubleshooting&sort=updated_at_desc&limit=25&cursor=cursor-current',
      requestDefaults,
    )
  })

  it('uses encoded record and revision read paths', async () => {
    const revisionList = { items: [revision] } satisfies RecordRevisionListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(200, record))
      .mockResolvedValueOnce(mockResponse(200, revisionList))
      .mockResolvedValueOnce(mockResponse(200, revision))

    await expect(getRecord('rec /contract')).resolves.toEqual(record)
    await expect(listRecordRevisions('rec /contract', { limit: 20 })).resolves.toEqual(revisionList)
    await expect(getRecordRevision('rec /contract', 'rrv /contract')).resolves.toEqual(revision)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/records/rec%20%2Fcontract', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/records/rec%20%2Fcontract/revisions?limit=20',
      requestDefaults,
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/records/rec%20%2Fcontract/revisions/rrv%20%2Fcontract',
      requestDefaults,
    )
  })

  it('keeps formal mutation idempotency headers and bodies exact', async () => {
    const mutation = {
      record_id: record.record_id,
      revision_id: revision.revision_id,
      revision_no: 1,
      lock_version: 1,
      authorization_epoch: 1,
      lifecycle: 'active',
      created: true,
      replayed: false,
      committed_at: revision.created_at,
    } satisfies RecordMutationResult
    const lifecycle = {
      record_id: record.record_id,
      current_revision_id: revision.revision_id,
      lock_version: 2,
      authorization_epoch: 2,
      lifecycle: 'archived',
      replayed: false,
      changed_at: '2026-08-03T11:00:00Z',
    } satisfies RecordLifecycleResult
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(200, lifecycle))
      .mockResolvedValueOnce(mockResponse(200, { ...lifecycle, lifecycle: 'active' }))
    const createInput = {
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
    } satisfies PublishRecordInput
    const revisionInput = {
      ...createInput,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
    } satisfies PublishRecordRevisionInput

    await createRecord(createInput, 'create-key')
    await createRecordRevision('rec /contract', revisionInput, 'revision-key')
    await restoreRecordRevision('rec /contract', 'rrv /contract', { save_reason: 'restore exact revision' }, 'restore-revision-key')
    await archiveRecord('rec /contract', 'archive-key')
    await restoreRecord('rec /contract', 'restore-record-key')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/records', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'create-key',
      },
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/records/rec%20%2Fcontract/revisions', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'revision-key',
      },
      body: JSON.stringify(revisionInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/records/rec%20%2Fcontract/revisions/rrv%20%2Fcontract/restore',
      {
        ...requestDefaults,
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': 'restore-revision-key',
        },
        body: JSON.stringify({ save_reason: 'restore exact revision' }),
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/records/rec%20%2Fcontract/archive', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Idempotency-Key': 'archive-key',
      },
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/records/rec%20%2Fcontract/restore', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Idempotency-Key': 'restore-record-key',
      },
    })
  })

  it('keeps draft routing fields on create and out of PATCH', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => Promise.resolve(mockResponse(200, draft)),
    )
    const createInput = {
      record_id: record.record_id,
      base_revision_id: revision.revision_id,
      payload,
    } satisfies CreateRecordDraftInput

    await listRecordDrafts({ limit: 25 })
    await createRecordDraft(createInput)
    await getRecordDraft('rdf /contract')
    await patchRecordDraft('rdf /contract', { payload }, draft.etag)
    await discardRecordDraft('rdf /contract')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/record-drafts?limit=25', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/record-drafts', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/record-drafts/rdf%20%2Fcontract', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/record-drafts/rdf%20%2Fcontract', {
      ...requestDefaults,
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': draft.etag,
      },
      body: JSON.stringify({ payload }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/record-drafts/rdf%20%2Fcontract', {
      ...requestDefaults,
      method: 'DELETE',
    })
  })

  it('omits both routing fields when creating a new-record draft', async () => {
    const input = { payload } satisfies CreateRecordDraftInput
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(201, draft))

    await createRecordDraft(input)

    expect(fetchMock).toHaveBeenCalledWith('/api/record-drafts', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ payload }),
    })
  })

  it('keeps the deletion request token header-only and reads operation status separately', async () => {
    const preview = {
      reservation_id: 'drs_contract',
      deletion_request_token: 'drt1_contract',
      expires_at: '2026-08-03T10:20:00Z',
      online_purge_scopes: [
        'record_core',
        'record_attachments',
        'record_evidence',
        'record_markdown_client',
        'record_search',
        'record_activity_projection',
        'record_comparison',
        'record_collaboration',
        'record_portability',
      ],
      surviving_copies: [{
        scope: 'record_attachments',
        kind: 'other_record',
        copy_count: 2,
      }],
      managed_backup: {
        retained_copy_count: 0,
        maximum_retention_days: 0,
        latest_expires_at: null,
      },
      ledger_health: 'healthy',
    } satisfies RecordDeletionPreview
    const operation = {
      operation_id: 'rpo_contract',
      state: 'witness_pending',
    } satisfies RecordDeletionOperation
    const executeInput = {
      reservation_id: preview.reservation_id,
      deletion_request_token: preview.deletion_request_token,
    } satisfies RecordDeletionExecuteInput
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(200, preview))
      .mockResolvedValueOnce(mockResponse(202, operation))
      .mockResolvedValueOnce(mockResponse(202, operation))

    await previewRecordPermanentDeletion('rec /contract')
    await executeRecordPermanentDeletion('rec /contract', executeInput)
    await getRecordDeletionOperation('rpo /contract')

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/records/rec%20%2Fcontract/permanent-delete-preview',
      { ...requestDefaults, method: 'POST' },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/records/rec%20%2Fcontract/permanent-delete',
      {
        ...requestDefaults,
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': preview.deletion_request_token,
        },
        body: JSON.stringify({ reservation_id: preview.reservation_id }),
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/record-deletions/rpo%20%2Fcontract',
      requestDefaults,
    )
  })

  it('surfaces stable 404, conflict recovery and unavailable codes', async () => {
    const recovery = {
      server_revision_id: revision.revision_id,
      server_lock_version: 4,
      server_authorization_epoch: 5,
      draft,
    }
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(404, {
        code: 'resource_not_found',
        message: 'resource not found',
        field_errors: [],
      }))
      .mockResolvedValueOnce(mockResponse(409, {
        code: 'record_revision_conflict',
        message: 'record revision changed',
        field_errors: [],
        recovery,
      }))
      .mockResolvedValueOnce(mockResponse(503, {
        code: 'deletion_safety_unavailable',
        message: 'deletion safety unavailable',
        field_errors: [],
      }))

    await expect(getRecord(record.record_id)).rejects.toMatchObject({
      status: 404,
      code: 'resource_not_found',
      field_errors: [],
    })
    const conflict = await createRecordRevision(record.record_id, {
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
    }, 'conflict-key').catch((reason: unknown) => reason)
    expect(conflict).toBeInstanceOf(ApiError)
    expect(conflict).toMatchObject({
      status: 409,
      code: 'record_revision_conflict',
      recovery,
    })
    await expect(previewRecordPermanentDeletion(record.record_id)).rejects.toMatchObject({
      status: 503,
      code: 'deletion_safety_unavailable',
      recovery: undefined,
    })
  })
})
