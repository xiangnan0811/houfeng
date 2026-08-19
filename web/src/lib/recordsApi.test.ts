import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './apiRequest'
import {
  archiveRecord,
  completeAttachmentUpload,
  captureEvidencePreview,
  createAttachmentUpload,
  createRecord,
  createRecordDraft,
  createRecordRevision,
  discardRecordDraft,
  executeRecordPermanentDeletion,
  getAttachmentContent,
  getAttachmentMetadata,
  getEvidenceSnapshot,
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
  uploadAttachmentContent,
} from './recordsApi'
import type {
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  EvidenceSnapshotRead,
  CreateRecordDraftInput,
  PublishRecordInput,
  PublishRecordRevisionInput,
  RecordDeletionExecuteInput,
  RecordDeletionOperation,
  RecordDeletionPreview,
  RecordDetail,
  RecordDraft,
  RecordDraftListResponse,
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
  attachment_ids: ['att_contract_first', 'att_contract_second'],
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
  attachment_ids: payload.attachment_ids,
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

type EvidenceEnvelopeFixture = Omit<
  EvidenceCapturePreview,
  'capture_intent_id' | 'estimated_canonical_bytes' | 'previewed_at' | 'valid_until'
>

const evidenceEnvelope: EvidenceEnvelopeFixture = {
  record_id: 'rec_contract',
  snapshot_id: 'evs_contract',
  kind: 'monitoring.host',
  schema_version: 1,
  subject: { type: 'vps', id: 'vps_contract', display_name: 'Contract VPS' },
  source: { type: 'monitoring_instance', id: 'mon_contract', display_name: 'Primary monitor' },
  requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  actual_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  observed_at: '2026-08-16T02:00:00Z',
  source_revision: 'source-revision',
  source_watermark: 'source-watermark',
  producer_version: 'producer-v1',
  calculation_version: 'calculation-v1',
  units: { status: 'applicable', values: { cpu_usage_pct: 'percent' } },
  quality: {
    status: 'complete',
    sample_count: 12,
    gap_count: 0,
    maintenance_count: 0,
    backfilled_count: 0,
    bucket_count: 12,
    data_point_count: 12,
    peak_count: 1,
    truncated: false,
    partial: false,
  },
  sensitivity: 'normal',
  actual_precision_seconds: 300,
  bucket_width_seconds: 300,
  quota: { status: 'allowed' },
  retention: {
    immutable: true,
    scope: 'record_revision',
    source_deletion: 'snapshot_retained_source_unavailable',
  },
  redaction: [],
  renderer_version: 'monitoring_host_v1',
}

const evidencePreviewResponse = {
  ...evidenceEnvelope,
  capture_intent_id: 'eci_contract',
  estimated_canonical_bytes: 4096,
  previewed_at: '2026-08-16T02:00:01Z',
  valid_until: '2026-08-16T02:05:01Z',
} satisfies EvidenceCapturePreview

const evidenceReadResponse = {
  ...evidenceEnvelope,
  captured_at: '2026-08-16T02:00:01Z',
  referenced_at: '2026-08-16T02:01:00Z',
  source_available: true,
  title: 'Monitoring host evidence',
  read_model: { version: 'monitoring_host_read_model/v1' },
} satisfies EvidenceSnapshotRead

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Records API transport', () => {
  it('captures an evidence preview with an allowlisted body and abort signal', async () => {
    const input = {
      record_id: 'rec_contract',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon /contract',
      requested_window: {
        start: '2026-08-16T01:00:00Z',
        end: '2026-08-16T02:00:00Z',
      },
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
      payload: 'must-not-leave-the-client',
      authorization: 'must-not-leave-the-client',
    } satisfies EvidenceCapturePreviewInput & { payload: string; authorization: string }
    const response = evidencePreviewResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(201, response))
    const controller = new AbortController()

    await expect(captureEvidencePreview(input, controller.signal)).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [path, init] = fetchMock.mock.calls[0] ?? []
    expect(path).toBe('/api/evidence/capture-previews')
    expect(init).toMatchObject({
      method: 'POST',
      signal: controller.signal,
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    })
    expect(JSON.parse(String(init?.body))).toEqual({
      record_id: 'rec_contract',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon /contract',
      requested_window: input.requested_window,
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
    })
  })

  it('reads an encoded evidence snapshot through the shared records transport', async () => {
    const response = { ...evidenceReadResponse, snapshot_id: 'evs /contract' }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))
    const controller = new AbortController()

    await expect(getEvidenceSnapshot('evs /contract', controller.signal)).resolves.toEqual(response)
    expect(fetchMock).toHaveBeenCalledWith('/api/evidence/evs%20%2Fcontract', {
      ...requestDefaults,
      signal: controller.signal,
    })
  })

  it.each([
    [409, 'evidence_preview_stale'],
    [503, 'evidence_service_unavailable'],
  ])('keeps evidence failure metadata allowlisted for HTTP %i', async (status, code) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(status, {
      code,
      message: 'evidence unavailable',
      metadata: 'must-not-enter-error',
      authorization: 'must-not-enter-error',
    }))

    await expect(captureEvidencePreview({
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon_contract',
      requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
    })).rejects.toMatchObject({ status, code, message: 'evidence unavailable' })
  })

  it('preserves non-null ordered attachment IDs in draft and revision DTOs', () => {
    expect(draft.payload.attachment_ids).toEqual([
      'att_contract_first',
      'att_contract_second',
    ])
    expect(revision.attachment_ids).toEqual(draft.payload.attachment_ids)
    expect({ ...draft.payload, attachment_ids: [] }.attachment_ids).toEqual([])
  })

  it('follows a local attachment instruction through complete and metadata polling', async () => {
    const session = {
      upload_id: 'aup_contract',
      attachment_id: 'att_contract',
      state: 'created',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 12,
        physical_bytes: 0,
        effective_record_bytes: 12,
        project_warning: false,
      },
      target: {
        transport: 'local',
        upload_url: '/api/attachment-uploads/aup_contract/content',
        method: 'PUT',
        required_headers: ['X-Houfeng-Draft-ID', 'X-Content-SHA256'],
      },
    } satisfies AttachmentUploadSession
    const completion = {
      upload_id: session.upload_id,
      attachment_id: session.attachment_id,
      state: 'quarantined',
      quota: session.quota,
    } satisfies AttachmentUploadCompletion
    const metadata = {
      attachment_id: session.attachment_id,
      state: 'available',
      display_name: 'incident.txt',
      media_type: 'text/plain',
      size_bytes: 12,
      preview_available: true,
    } satisfies AttachmentMetadata
    const content = new Blob(['safe content'], { type: 'text/plain' })
    const sha256 = 'a'.repeat(64)
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(201, session))
      .mockResolvedValueOnce(mockResponse(200, {
        upload_id: session.upload_id,
        attachment_id: session.attachment_id,
        size_bytes: content.size,
        sha256,
      }))
      .mockResolvedValueOnce(mockResponse(202, completion))
      .mockResolvedValueOnce(mockResponse(200, metadata))

    await expect(createAttachmentUpload({
      draft_id: 'rdf_contract',
      display_name: metadata.display_name,
      media_type: metadata.media_type,
      declared_size_bytes: content.size,
    })).resolves.toEqual(session)
    await uploadAttachmentContent(session, 'rdf_contract', sha256, content)
    await expect(completeAttachmentUpload(session.upload_id, 'rdf_contract')).resolves.toEqual(completion)
    await expect(getAttachmentMetadata(session.attachment_id)).resolves.toEqual(metadata)

    expect(fetchMock).toHaveBeenNthCalledWith(2, session.target.upload_url, {
      cache: 'no-store',
      credentials: 'include',
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'X-Houfeng-Draft-ID': 'rdf_contract',
        'X-Content-SHA256': sha256,
      },
      body: content,
    })
  })

  it('uses an S3 instruction without forwarding first-party credentials', async () => {
    const session = {
      upload_id: 'aup_s3contract',
      attachment_id: 'att_s3contract',
      state: 'uploading',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 4,
        physical_bytes: 0,
        effective_record_bytes: 4,
        project_warning: false,
      },
      target: {
        transport: 's3',
        upload_url: 'https://objects.example.test/private-upload',
        method: 'PUT',
        required_headers: [],
        temporary_object_key: 'temporary/' + 'b'.repeat(64),
      },
    } satisfies AttachmentUploadSession
    const content = new Blob(['safe'], { type: 'text/plain' })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    await uploadAttachmentContent(session, 'rdf_contract', 'c'.repeat(64), content)

    expect(fetchMock).toHaveBeenCalledWith(session.target.upload_url, {
      cache: 'no-store',
      credentials: 'omit',
      method: 'PUT',
      headers: {},
      body: content,
    })
  })

  it('fetches authorized attachment bytes through the shared transport', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('preview', {
      status: 200,
      headers: { 'Content-Type': 'text/plain' },
    }))

    const result = await getAttachmentContent('att /contract', 'preview')

    expect(result.type).toBe('text/plain')
    await expect(result.text()).resolves.toBe('preview')
    expect(fetchMock).toHaveBeenCalledWith('/api/attachments/att%20%2Fcontract/content?variant=preview', {
      cache: 'no-store',
      credentials: 'include',
      headers: { Accept: 'application/octet-stream' },
    })
  })

  it('decodes an opaque authorized attachment denial through the shared error contract', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(404, {
      code: 'resource_not_found',
      message: 'resource not found',
      field_errors: [],
    }))

    await expect(getAttachmentContent('att_denied')).rejects.toMatchObject({
      status: 404,
      code: 'resource_not_found',
      message: 'resource not found',
    })
  })

  it('fails a rejected S3 instruction without converting it into a successful upload', async () => {
    const session = {
      upload_id: 'aup_s3denied',
      attachment_id: 'att_s3denied',
      state: 'uploading',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 4,
        physical_bytes: 0,
        effective_record_bytes: 4,
        project_warning: false,
      },
      target: {
        transport: 's3',
        upload_url: 'https://objects.example.test/rejected-upload',
        method: 'PUT',
        required_headers: [],
        temporary_object_key: 'temporary/' + 'd'.repeat(64),
      },
    } satisfies AttachmentUploadSession
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 503 }))

    await expect(uploadAttachmentContent(
      session,
      'rdf_contract',
      'e'.repeat(64),
      new Blob(['safe']),
    )).rejects.toMatchObject({
      status: 503,
      message: 'Request failed: 503',
    })
  })

  it('normalizes record list filters and preserves the server cursor response', async () => {
    const response = { items: [record], next_cursor: ' cursor-next ' } satisfies RecordListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(listRecords({
      sort: 'updated_at_desc',
      limit: 25,
      cursor: '  cursor-current  ',
    })).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/records?sort=updated_at_desc&limit=25&cursor=cursor-current',
      requestDefaults,
    )
  })

  it('sends the draft list cursor and preserves the one the server returns', async () => {
    const response = {
      items: [],
      next_cursor: ' draft-cursor-next ',
    } satisfies RecordDraftListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(listRecordDrafts({
      limit: 25,
      cursor: '  draft-cursor-current  ',
    })).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/record-drafts?limit=25&cursor=draft-cursor-current',
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
