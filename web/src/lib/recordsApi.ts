import allowlistedApiError from './apiError'
import {
  ApiError,
  jsonBodyInit,
  requestBlob as transportRequestBlob,
  requestEmpty as transportRequestEmpty,
  requestExternalEmpty,
  requestJSON as transportRequestJSON,
  withQuery,
} from './apiRequest'
import type {
  AttachmentContentVariant,
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
  CreateAttachmentUploadInput,
  CreateRecordDraftInput,
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  EvidenceSnapshotRead,
  PatchRecordDraftInput,
  PublishRecordInput,
  PublishRecordRevisionInput,
  RecordDeletionExecuteInput,
  RecordDeletionOperation,
  RecordDeletionPreview,
  RecordDetail,
  RecordDraft,
  RecordDraftListFilter,
  RecordDraftListResponse,
  RecordLifecycleResult,
  RecordListFilter,
  RecordListResponse,
  RecordMutationResult,
  RecordRevision,
  RecordRevisionListFilter,
  RecordRevisionListResponse,
  RecordSearchFilter,
  RecordSearchResponse,
  RecordSearchSubjectFilter,
  RestoreRecordRevisionInput,
  SubjectActivityFilter,
  SubjectActivityListResponse,
  SubjectActivityItem,
  SubjectActivitySourceStatus,
  RecordSubjectKind,
  VPSOverview,
} from './types'

function encoded(value: string): string {
  return encodeURIComponent(value)
}

function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  return transportRequestJSON<T>(path, init, allowlistedApiError)
}

function requestEmpty(path: string, init?: RequestInit): Promise<void> {
  return transportRequestEmpty(path, init, allowlistedApiError)
}

function requestBlob(path: string, init?: RequestInit): Promise<Blob> {
  return transportRequestBlob(path, init, allowlistedApiError)
}

function attachmentUploadHeaders(
  requiredHeaders: readonly string[],
  draftId: string,
  sha256: string,
  content: Blob,
): Record<string, string> {
  const headers: Record<string, string> = {}
  for (const header of requiredHeaders) {
    switch (header.toLowerCase()) {
      case 'x-houfeng-draft-id':
        headers[header] = draftId
        break
      case 'x-content-sha256':
        headers[header] = sha256
        break
      case 'content-type':
        headers[header] = content.type || 'application/octet-stream'
        break
      default:
        throw new ApiError(503, 'unsupported attachment upload instruction', {
          code: 'attachment_service_unavailable',
        })
    }
  }
  return headers
}

function postIdempotentJSON<T>(path: string, body: unknown, idempotencyKey: string): Promise<T> {
  return requestJSON<T>(path, jsonBodyInit('POST', body, {
    'Idempotency-Key': idempotencyKey,
  }))
}

function changeRecordLifecycle(
  recordId: string,
  action: 'archive' | 'restore',
  idempotencyKey: string,
): Promise<RecordLifecycleResult> {
  return requestJSON<RecordLifecycleResult>(`/api/records/${encoded(recordId)}/${action}`, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  })
}

export function listRecords(filter?: RecordListFilter): Promise<RecordListResponse> {
  return requestJSON<RecordListResponse>(withQuery('/api/records', filter
    ? {
        sort: filter.sort,
        limit: filter.limit,
        cursor: filter.cursor,
      }
    : undefined))
}

/**
 * The server reads a subject filter by position, so every segment keeps its slot
 * even when empty. Trimming trailing separators would shift `role` into
 * `source_id` and silently ask a different question.
 */
function encodeRecordSearchSubject(subject: RecordSearchSubjectFilter): string {
  return [
    subject.kind ?? '',
    subject.source_id ?? '',
    subject.role ?? '',
    subject.placement ?? '',
  ].join(':')
}

export function searchRecords(filter?: RecordSearchFilter): Promise<RecordSearchResponse> {
  return requestJSON<RecordSearchResponse>(withQuery('/api/records/search', filter
    ? {
        q: filter.q,
        type: filter.type,
        status: filter.status,
        status_group: filter.status_group,
        lifecycle: filter.lifecycle,
        owner: filter.owner,
        participant: filter.participant,
        tag: filter.tag,
        subject: filter.subject?.map(encodeRecordSearchSubject),
        follow_up: filter.follow_up,
        action: filter.action,
        occurred_from: filter.occurred_from,
        occurred_to: filter.occurred_to,
        updated_from: filter.updated_from,
        updated_to: filter.updated_to,
        sort: filter.sort,
        limit: filter.limit,
        cursor: filter.cursor,
      }
    : undefined))
}

export function captureEvidencePreview(
  input: EvidenceCapturePreviewInput,
  signal?: AbortSignal,
): Promise<EvidenceCapturePreview> {
  const body: EvidenceCapturePreviewInput = {
    kind: input.kind,
    schema_version: input.schema_version,
    source_type: input.source_type,
    source_id: input.source_id,
    requested_window: {
      start: input.requested_window.start,
      end: input.requested_window.end,
    },
    metrics: [...input.metrics],
    precision_seconds: input.precision_seconds,
    sensitive_topology_fields: [...input.sensitive_topology_fields],
  }
  if (input.record_id !== undefined) body.record_id = input.record_id
  const init = jsonBodyInit('POST', body)
  if (signal) init.signal = signal
  return requestJSON<EvidenceCapturePreview>('/api/evidence/capture-previews', init)
}

export function getEvidenceSnapshot(
  snapshotId: string,
  signal?: AbortSignal,
): Promise<EvidenceSnapshotRead> {
  const init: RequestInit = {}
  if (signal) init.signal = signal
  return requestJSON<EvidenceSnapshotRead>(`/api/evidence/${encoded(snapshotId)}`, init)
}

export function createAttachmentUpload(
  input: CreateAttachmentUploadInput,
  signal?: AbortSignal,
): Promise<AttachmentUploadSession> {
  const init = jsonBodyInit('POST', input)
  if (signal) init.signal = signal
  return requestJSON<AttachmentUploadSession>(
    '/api/attachment-uploads',
    init,
  )
}

export async function uploadAttachmentContent(
  session: AttachmentUploadSession,
  draftId: string,
  sha256: string,
  content: Blob,
  signal?: AbortSignal,
): Promise<void> {
  const headers = attachmentUploadHeaders(session.target.required_headers, draftId, sha256, content)
  if (session.target.transport === 'local') headers.Accept = 'application/json'
  const init: RequestInit = {
    method: session.target.method,
    headers,
    body: content,
  }
  if (signal) init.signal = signal

  if (session.target.transport === 's3') {
    await requestExternalEmpty(session.target.upload_url, init)
    return
  }
  await requestJSON<unknown>(session.target.upload_url, init)
}

export function completeAttachmentUpload(
  uploadId: string,
  draftId: string,
  signal?: AbortSignal,
): Promise<AttachmentUploadCompletion> {
  const init = jsonBodyInit('POST', { draft_id: draftId })
  if (signal) init.signal = signal
  return requestJSON<AttachmentUploadCompletion>(
    `/api/attachment-uploads/${encoded(uploadId)}/complete`,
    init,
  )
}

export function getAttachmentMetadata(
  attachmentId: string,
  signal?: AbortSignal,
): Promise<AttachmentMetadata> {
  const init: RequestInit = {}
  if (signal) init.signal = signal
  return requestJSON<AttachmentMetadata>(`/api/attachments/${encoded(attachmentId)}`, init)
}

export function getAttachmentContent(
  attachmentId: string,
  variant: AttachmentContentVariant = 'original',
  signal?: AbortSignal,
): Promise<Blob> {
  const init: RequestInit = { headers: { Accept: 'application/octet-stream' } }
  if (signal) init.signal = signal
  return requestBlob(withQuery(
    `/api/attachments/${encoded(attachmentId)}/content`,
    variant === 'original' ? undefined : { variant },
  ), init)
}

export function getRecord(recordId: string): Promise<RecordDetail> {
  return requestJSON<RecordDetail>(`/api/records/${encoded(recordId)}`)
}

export function createRecord(input: PublishRecordInput, idempotencyKey: string): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>('/api/records', input, idempotencyKey)
}

export function listRecordRevisions(
  recordId: string,
  filter?: RecordRevisionListFilter,
): Promise<RecordRevisionListResponse> {
  return requestJSON<RecordRevisionListResponse>(withQuery(
    `/api/records/${encoded(recordId)}/revisions`,
    filter ? { limit: filter.limit } : undefined,
  ))
}

export function getRecordRevision(recordId: string, revisionId: string): Promise<RecordRevision> {
  return requestJSON<RecordRevision>(
    `/api/records/${encoded(recordId)}/revisions/${encoded(revisionId)}`,
  )
}

export function createRecordRevision(
  recordId: string,
  input: PublishRecordRevisionInput,
  idempotencyKey: string,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>(
    `/api/records/${encoded(recordId)}/revisions`,
    input,
    idempotencyKey,
  )
}

export function restoreRecordRevision(
  recordId: string,
  revisionId: string,
  input: RestoreRecordRevisionInput,
  idempotencyKey: string,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>(
    `/api/records/${encoded(recordId)}/revisions/${encoded(revisionId)}/restore`,
    input,
    idempotencyKey,
  )
}

export function archiveRecord(recordId: string, idempotencyKey: string): Promise<RecordLifecycleResult> {
  return changeRecordLifecycle(recordId, 'archive', idempotencyKey)
}

export function restoreRecord(recordId: string, idempotencyKey: string): Promise<RecordLifecycleResult> {
  return changeRecordLifecycle(recordId, 'restore', idempotencyKey)
}

export function listRecordDrafts(filter?: RecordDraftListFilter): Promise<RecordDraftListResponse> {
  return requestJSON<RecordDraftListResponse>(withQuery(
    '/api/record-drafts',
    filter ? { limit: filter.limit, cursor: filter.cursor } : undefined,
  ))
}

export function createRecordDraft(input: CreateRecordDraftInput): Promise<RecordDraft> {
  return requestJSON<RecordDraft>('/api/record-drafts', jsonBodyInit('POST', input))
}

export function getRecordDraft(draftId: string): Promise<RecordDraft> {
  return requestJSON<RecordDraft>(`/api/record-drafts/${encoded(draftId)}`)
}

export function patchRecordDraft(
  draftId: string,
  input: PatchRecordDraftInput,
  etag: string,
): Promise<RecordDraft> {
  return requestJSON<RecordDraft>(
    `/api/record-drafts/${encoded(draftId)}`,
    jsonBodyInit('PATCH', input, { 'If-Match': etag }),
  )
}

export function discardRecordDraft(draftId: string): Promise<void> {
  return requestEmpty(`/api/record-drafts/${encoded(draftId)}`, { method: 'DELETE' })
}

export function previewRecordPermanentDeletion(recordId: string): Promise<RecordDeletionPreview> {
  return requestJSON<RecordDeletionPreview>(
    `/api/records/${encoded(recordId)}/permanent-delete-preview`,
    { method: 'POST' },
  )
}

export function executeRecordPermanentDeletion(
  recordId: string,
  input: RecordDeletionExecuteInput,
): Promise<RecordDeletionOperation> {
  return requestJSON<RecordDeletionOperation>(
    `/api/records/${encoded(recordId)}/permanent-delete`,
    jsonBodyInit('POST', { reservation_id: input.reservation_id }, {
      'Idempotency-Key': input.deletion_request_token,
    }),
  )
}

export function getRecordDeletionOperation(operationId: string): Promise<RecordDeletionOperation> {
  return requestJSON<RecordDeletionOperation>(`/api/record-deletions/${encoded(operationId)}`)
}

const ACTIVITY_GLOBAL_HEAD_KEYS = [
  'projection_generation',
  'as_of_ingest_sequence',
  'current_ingest_sequence',
  'published_ingest_sequence',
  'ingest_sequence',
  'checkpoint',
  'global_checkpoint',
] as const

function stripActivityGlobalHeadFields(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(stripActivityGlobalHeadFields)
  }
  if (!value || typeof value !== 'object') return value
  const out: Record<string, unknown> = {}
  for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
    if ((ACTIVITY_GLOBAL_HEAD_KEYS as readonly string[]).includes(key)) continue
    out[key] = stripActivityGlobalHeadFields(nested)
  }
  return out
}

function normalizeActivityItems(raw: unknown): SubjectActivityItem[] {
  if (!Array.isArray(raw)) return []
  return raw.map((item) => {
    const row = (item && typeof item === 'object' ? item : {}) as SubjectActivityItem
    return {
      ...row,
      subjects: Array.isArray(row.subjects) ? row.subjects : [],
      presentation: row.presentation ?? { version: 1, title: '' },
    }
  })
}

function normalizeActivitySourceStatuses(raw: unknown): SubjectActivitySourceStatus[] {
  return Array.isArray(raw) ? raw as SubjectActivitySourceStatus[] : []
}

function normalizeSubjectActivityResponse(raw: unknown): SubjectActivityListResponse {
  const stripped = stripActivityGlobalHeadFields(raw) as Partial<SubjectActivityListResponse> & {
    subject?: Partial<SubjectActivityListResponse['subject']>
    freshness?: Partial<SubjectActivityListResponse['freshness']>
  }
  const subject = stripped.subject ?? {
    kind: 'vps' as RecordSubjectKind,
    source_id: '',
    identity: {},
    status: 'live' as const,
  }
  return {
    subject: {
      kind: subject.kind ?? 'vps',
      source_id: subject.source_id ?? '',
      identity: subject.identity && typeof subject.identity === 'object' ? subject.identity : {},
      ...(subject.live_route ? { live_route: subject.live_route } : {}),
      status: subject.status === 'tombstoned' ? 'tombstoned' : 'live',
    },
    view: stripped.view === 'records' || stripped.view === 'evidence' ? stripped.view : 'activity',
    snapshot_cursor: typeof stripped.snapshot_cursor === 'string' ? stripped.snapshot_cursor.trim() : '',
    freshness: {
      state: stripped.freshness?.state ?? '',
      visible_observed_at: stripped.freshness?.visible_observed_at ?? null,
      new_items_available: Boolean(stripped.freshness?.new_items_available),
      reason_code: stripped.freshness?.reason_code ?? '',
    },
    items: normalizeActivityItems(stripped.items),
    source_statuses: normalizeActivitySourceStatuses(stripped.source_statuses),
    ...(typeof stripped.next_cursor === 'string' && stripped.next_cursor.trim()
      ? { next_cursor: stripped.next_cursor.trim() }
      : {}),
  }
}

/**
 * Lists one subject's fixed-watermark activity page. Cursors are opaque: the
 * client only stores and returns them. Global projector head fields are stripped
 * if a buggy server ever includes them.
 */
export function listSubjectActivity(
  kind: RecordSubjectKind,
  sourceId: string,
  filter?: SubjectActivityFilter,
): Promise<SubjectActivityListResponse> {
  const path = `/api/subjects/${encoded(kind)}/${encoded(sourceId)}/activity`
  return requestJSON<unknown>(withQuery(path, filter
    ? {
        view: filter.view === 'activity' ? undefined : filter.view,
        source: filter.source,
        event_kind: filter.event_kind,
        from: filter.from,
        to: filter.to,
        versions: filter.versions === 'history' ? undefined : filter.versions,
        limit: filter.limit === 50 ? undefined : filter.limit,
        cursor: filter.cursor,
      }
    : undefined)).then(normalizeSubjectActivityResponse)
}

function emptySectionState(): VPSOverview['recent_activity']['section'] {
  return {
    state: '',
    observed_at: null,
    last_success_at: null,
    reason_code: '',
  }
}

function normalizeOverviewSection(
  raw: Partial<VPSOverview['recent_activity']['section']> | undefined,
): VPSOverview['recent_activity']['section'] {
  return {
    state: raw?.state ?? '',
    observed_at: raw?.observed_at ?? null,
    last_success_at: raw?.last_success_at ?? null,
    reason_code: raw?.reason_code ?? '',
  }
}

function normalizeVPSOverview(raw: unknown): VPSOverview {
  const stripped = stripActivityGlobalHeadFields(raw) as Partial<VPSOverview>
  const identity = stripped.identity ?? {
    vps_id: '',
    display_name: '',
    provider_name: '',
    product_name: '',
    country: '',
    region: '',
    city: '',
    datacenter: '',
    ipv4: '',
    ipv6: '',
    lifecycle_status: '',
    usage_status: '',
    renewal_decision: '',
    importance: '',
    labels: [],
    updated_at: '',
  }
  const summary = stripped.summary
  return {
    generated_at: typeof stripped.generated_at === 'string' ? stripped.generated_at : '',
    identity: {
      ...identity,
      labels: Array.isArray(identity.labels) ? identity.labels : [],
    },
    anomalies: Array.isArray(stripped.anomalies)
      ? stripped.anomalies.map((anomaly) => ({
          ...anomaly,
          secondary_actions: Array.isArray(anomaly.secondary_actions)
            ? anomaly.secondary_actions
            : [],
        }))
      : [],
    summary: {
      overall: {
        status: summary?.overall?.status ?? '',
        ...(summary?.overall?.detail ? { detail: summary.overall.detail } : {}),
        section: normalizeOverviewSection(summary?.overall?.section),
      },
      monitoring: {
        status: summary?.monitoring?.status ?? '',
        ...(summary?.monitoring?.detail ? { detail: summary.monitoring.detail } : {}),
        section: normalizeOverviewSection(summary?.monitoring?.section),
      },
      ip_quality: {
        status: summary?.ip_quality?.status ?? '',
        ...(summary?.ip_quality?.detail ? { detail: summary.ip_quality.detail } : {}),
        section: normalizeOverviewSection(summary?.ip_quality?.section),
      },
      renewal: {
        status: summary?.renewal?.status ?? '',
        ...(summary?.renewal?.detail ? { detail: summary.renewal.detail } : {}),
        section: normalizeOverviewSection(summary?.renewal?.section),
      },
    },
    recent_activity: {
      section: normalizeOverviewSection(stripped.recent_activity?.section) || emptySectionState(),
      items: normalizeActivityItems(stripped.recent_activity?.items),
      ...(typeof stripped.recent_activity?.snapshot_cursor === 'string'
        && stripped.recent_activity.snapshot_cursor.trim()
        ? { snapshot_cursor: stripped.recent_activity.snapshot_cursor.trim() }
        : {}),
    },
    facts: Array.isArray(stripped.facts) ? stripped.facts : [],
    relations: Array.isArray(stripped.relations) ? stripped.relations : [],
    capabilities: Array.isArray(stripped.capabilities) ? stripped.capabilities : [],
  }
}

/** Reads the request-scoped VPS overview read model. */
export function getVPSOverview(vpsId: string): Promise<VPSOverview> {
  return requestJSON<unknown>(`/api/vps/${encoded(vpsId)}/overview`).then(normalizeVPSOverview)
}

export function overviewHasRecordsV2Read(overview: VPSOverview): boolean {
  return overview.capabilities.includes('records_v2_read')
}
