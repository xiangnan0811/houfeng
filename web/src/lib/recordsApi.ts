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
  ComparisonCandidateRequest,
  ComparisonCandidateResponse,
  ComparisonEvaluateRequest,
  ComparisonEvaluateResponse,
  ComparisonFixedItemInput,
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
  RecordExportPreview,
  RecordExportPreviewInput,
  RecordExportView,
  RecordImportApplyResult,
  RecordImportPlan,
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
  SaveComparisonRecordInput,
  SaveComparisonRevisionInput,
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

function postIdempotentJSON<T>(
  path: string,
  body: unknown,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<T> {
  const init = jsonBodyInit('POST', body, {
    'Idempotency-Key': idempotencyKey,
  })
  if (signal) init.signal = signal
  return requestJSON<T>(path, init)
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

type InvalidVPSOverviewResponseReason = 'malformed_json' | 'invalid_shape'
type VPSOverviewWireObject = Record<string, unknown>

const VPS_OVERVIEW_SECTION_STATES = ['ready', 'stale', 'unavailable'] as const
const VPS_OVERVIEW_ANOMALY_RULES = [
  'monitoring.health.abnormal.v1',
  'monitoring.incidents.open.v1',
  'ip_quality.risk.elevated.v1',
  'ip_quality.stale.v1',
  'ip_quality.partial.v1',
  'renewal.subscription.missing.v1',
  'renewal.due.soon.v1',
  'lifecycle.blocker.v1',
  'source.unavailable.v1',
] as const
const VPS_OVERVIEW_ANOMALY_ACTIONS = [
  'open_monitoring',
  'open_incidents',
  'open_ip_quality',
  'open_subscription',
  'open_renewal_decision',
  'open_management',
  'retry_overview',
] as const
const SUBJECT_ACTIVITY_SOURCE_KINDS = [
  'record_domain',
  'evidence_snapshot',
  'asset_history',
  'monitoring_event',
  'command_audit',
] as const
const SUBJECT_ACTIVITY_EVENT_KINDS = [
  'record_created',
  'record_revised',
  'record_restored',
  'record_archived',
  'record_unarchived',
  'record_owner_changed',
  'record_participant_changed',
  'record_follow_up_changed',
  'comment_created',
  'comment_edited',
  'comment_redacted',
  'action_created',
  'action_updated',
  'action_completed',
  'action_cancelled',
  'action_reopened',
  'evidence_captured',
  'asset_fact_changed',
  'monitoring_state_changed',
  'command_executed',
] as const
const RECORD_SUBJECT_KINDS = ['vps', 'monitoring_instance', 'target'] as const
const SUBJECT_ACTIVITY_ROLES = ['affected', 'context', 'evidence_source'] as const
const SUBJECT_ACTIVITY_IDENTITY_FIELDS = {
  vps: ['provider', 'region', 'purpose'],
  monitoring_instance: ['version'],
  target: ['target_type'],
} as const satisfies Record<RecordSubjectKind, readonly string[]>
const VPS_OVERVIEW_RELATIONS = [
  { kind: 'monitoring_instances', route: false },
  { kind: 'subscriptions', route: true },
  { kind: 'services', route: false },
  { kind: 'domains', route: false },
] as const

export class InvalidVPSOverviewResponseError extends Error {
  readonly reason: InvalidVPSOverviewResponseReason

  constructor(reason: InvalidVPSOverviewResponseReason) {
    super('Invalid VPS overview response')
    Object.defineProperty(this, 'name', { value: 'InvalidVPSOverviewResponseError' })
    this.reason = reason
  }
}

function invalidVPSOverview(): never {
  throw new InvalidVPSOverviewResponseError('invalid_shape')
}

function overviewObject(value: unknown): VPSOverviewWireObject {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) invalidVPSOverview()
  return value as VPSOverviewWireObject
}

function overviewArray(value: unknown): unknown[] {
  if (!Array.isArray(value)) invalidVPSOverview()
  return value
}

function overviewString(value: unknown): string {
  if (typeof value !== 'string') invalidVPSOverview()
  return value
}

function optionalOverviewString(
  object: VPSOverviewWireObject,
  key: string,
): string | undefined {
  if (!Object.prototype.hasOwnProperty.call(object, key)) return undefined
  return overviewString(object[key])
}

function overviewBoolean(value: unknown): boolean {
  if (typeof value !== 'boolean') invalidVPSOverview()
  return value
}

function overviewEnum<const T extends string>(value: unknown, allowed: readonly T[]): T {
  if (typeof value !== 'string' || !allowed.includes(value as T)) invalidVPSOverview()
  return value as T
}

function overviewStringArray(value: unknown): string[] {
  return overviewArray(value).map(overviewString)
}

function isLeapYear(year: number): boolean {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
}

function daysInMonth(year: number, month: number): number {
  if (month === 2) return isLeapYear(year) ? 29 : 28
  return [4, 6, 9, 11].includes(month) ? 30 : 31
}

function overviewTimestamp(value: unknown): string {
  const timestamp = overviewString(value)
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|([+-])(\d{2}):(\d{2}))$/u.exec(timestamp)
  if (!match) invalidVPSOverview()
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const hour = Number(match[4])
  const minute = Number(match[5])
  const second = Number(match[6])
  const offsetHour = match[8] === undefined ? 0 : Number(match[8])
  const offsetMinute = match[9] === undefined ? 0 : Number(match[9])
  if (
    month < 1
    || month > 12
    || day < 1
    || day > daysInMonth(year, month)
    || hour > 23
    || minute > 59
    || second > 59
    || offsetHour > 23
    || offsetMinute > 59
  ) invalidVPSOverview()
  return timestamp
}

function nullableOverviewTimestamp(value: unknown): string | null {
  return value === null ? null : overviewTimestamp(value)
}

function optionalNullableOverviewTimestamp(
  object: VPSOverviewWireObject,
  key: string,
): string | null | undefined {
  if (!Object.prototype.hasOwnProperty.call(object, key)) return undefined
  return nullableOverviewTimestamp(object[key])
}

function decodeOverviewSection(value: unknown): VPSOverview['recent_activity']['section'] {
  const section = overviewObject(value)
  return {
    state: overviewEnum(section.state, VPS_OVERVIEW_SECTION_STATES),
    observed_at: nullableOverviewTimestamp(section.observed_at),
    last_success_at: nullableOverviewTimestamp(section.last_success_at),
    reason_code: overviewString(section.reason_code),
  }
}

function decodeOverviewSummaryCell(value: unknown): VPSOverview['summary']['overall'] {
  const cell = overviewObject(value)
  const detail = optionalOverviewString(cell, 'detail')
  const decoded: VPSOverview['summary']['overall'] = {
    status: overviewString(cell.status),
    section: decodeOverviewSection(cell.section),
  }
  if (detail !== undefined) decoded.detail = detail
  return decoded
}

function decodeOverviewIdentity(value: unknown): VPSOverview['identity'] {
  const identity = overviewObject(value)
  return {
    vps_id: overviewString(identity.vps_id),
    display_name: overviewString(identity.display_name),
    provider_name: overviewString(identity.provider_name),
    product_name: overviewString(identity.product_name),
    country: overviewString(identity.country),
    region: overviewString(identity.region),
    city: overviewString(identity.city),
    datacenter: overviewString(identity.datacenter),
    ipv4: overviewString(identity.ipv4),
    ipv6: overviewString(identity.ipv6),
    lifecycle_status: overviewString(identity.lifecycle_status),
    usage_status: overviewString(identity.usage_status),
    renewal_decision: overviewString(identity.renewal_decision),
    importance: overviewString(identity.importance),
    labels: overviewStringArray(identity.labels),
    updated_at: overviewTimestamp(identity.updated_at),
  }
}

function decodeOverviewAction(value: unknown): VPSOverview['anomalies'][number]['secondary_actions'][number] {
  const action = overviewObject(value)
  const route = optionalOverviewString(action, 'route')
  const decoded: VPSOverview['anomalies'][number]['secondary_actions'][number] = {
    id: overviewEnum(action.id, VPS_OVERVIEW_ANOMALY_ACTIONS),
    label: overviewString(action.label),
  }
  if (route !== undefined) decoded.route = route
  return decoded
}

function decodeOverviewAnomaly(value: unknown): VPSOverview['anomalies'][number] {
  const anomaly = overviewObject(value)
  const detail = optionalOverviewString(anomaly, 'detail')
  const eventAt = optionalNullableOverviewTimestamp(anomaly, 'event_at')
  const hasPrimary = Object.prototype.hasOwnProperty.call(anomaly, 'primary_action')
  const decoded: VPSOverview['anomalies'][number] = {
    rule_id: overviewEnum(anomaly.rule_id, VPS_OVERVIEW_ANOMALY_RULES),
    severity: overviewString(anomaly.severity),
    title: overviewString(anomaly.title),
    source: overviewString(anomaly.source),
    secondary_actions: overviewArray(anomaly.secondary_actions).map(decodeOverviewAction),
  }
  if (detail !== undefined) decoded.detail = detail
  if (eventAt !== undefined) decoded.event_at = eventAt
  if (hasPrimary) {
    decoded.primary_action = anomaly.primary_action === null
      ? null
      : decodeOverviewAction(anomaly.primary_action)
  }
  return decoded
}

function decodeOverviewActor(value: unknown): NonNullable<SubjectActivityItem['actor']> {
  const actor = overviewObject(value)
  const displayName = optionalOverviewString(actor, 'display_name')
  const decoded: NonNullable<SubjectActivityItem['actor']> = {
    actor_id: overviewString(actor.actor_id),
  }
  if (displayName !== undefined) decoded.display_name = displayName
  return decoded
}

function decodeOverviewIdentityMap(
  value: unknown,
  kind: RecordSubjectKind,
): Record<string, string> {
  const identity = overviewObject(value)
  const decoded: Record<string, string> = {}
  const displayName = optionalOverviewString(identity, 'display_name')
  if (displayName !== undefined) decoded.display_name = displayName
  for (const field of SUBJECT_ACTIVITY_IDENTITY_FIELDS[kind]) {
    const item = optionalOverviewString(identity, field)
    if (item !== undefined) decoded[field] = item
  }
  return decoded
}

function decodeOverviewSubject(value: unknown): SubjectActivityItem['subjects'][number] {
  const subject = overviewObject(value)
  const kind = overviewEnum(subject.kind, RECORD_SUBJECT_KINDS)
  const liveRoute = optionalOverviewString(subject, 'live_route')
  const decoded: SubjectActivityItem['subjects'][number] = {
    kind,
    source_id: overviewString(subject.source_id),
    role: overviewEnum(subject.role, SUBJECT_ACTIVITY_ROLES),
    primary: overviewBoolean(subject.primary),
    identity: decodeOverviewIdentityMap(subject.identity, kind),
    tombstoned: overviewBoolean(subject.tombstoned),
  }
  if (liveRoute !== undefined) decoded.live_route = liveRoute
  return decoded
}

function decodeOverviewPresentation(value: unknown): SubjectActivityItem['presentation'] {
  const presentation = overviewObject(value)
  if (presentation.version !== 1) invalidVPSOverview()
  const summary = optionalOverviewString(presentation, 'summary')
  const decoded: SubjectActivityItem['presentation'] = {
    version: 1,
    title: overviewString(presentation.title),
  }
  if (summary !== undefined) decoded.summary = summary
  return decoded
}

function decodeOverviewActivityItem(value: unknown): SubjectActivityItem {
  const item = overviewObject(value)
  const actor = Object.prototype.hasOwnProperty.call(item, 'actor')
    ? decodeOverviewActor(item.actor)
    : undefined
  const correctsActivityID = optionalOverviewString(item, 'corrects_activity_id')
  const decoded: SubjectActivityItem = {
    activity_id: overviewString(item.activity_id),
    event_kind: overviewEnum(item.event_kind, SUBJECT_ACTIVITY_EVENT_KINDS),
    event_at: overviewTimestamp(item.event_at),
    recorded_at: overviewTimestamp(item.recorded_at),
    source_kind: overviewEnum(item.source_kind, SUBJECT_ACTIVITY_SOURCE_KINDS),
    backfilled: overviewBoolean(item.backfilled),
    subjects: overviewArray(item.subjects).map(decodeOverviewSubject),
    presentation: decodeOverviewPresentation(item.presentation),
  }
  if (actor !== undefined) decoded.actor = actor
  if (correctsActivityID !== undefined) decoded.corrects_activity_id = correctsActivityID
  return decoded
}

function decodeOverviewRecentActivity(value: unknown): VPSOverview['recent_activity'] {
  const activity = overviewObject(value)
  const items = overviewArray(activity.items)
  if (items.length > 5) invalidVPSOverview()
  const snapshotCursor = optionalOverviewString(activity, 'snapshot_cursor')
  const decoded: VPSOverview['recent_activity'] = {
    section: decodeOverviewSection(activity.section),
    items: items.map(decodeOverviewActivityItem),
  }
  if (snapshotCursor !== undefined) decoded.snapshot_cursor = snapshotCursor
  return decoded
}

function decodeOverviewFact(value: unknown): VPSOverview['facts'][number] {
  const fact = overviewObject(value)
  return {
    key: overviewString(fact.key),
    label: overviewString(fact.label),
    value: overviewString(fact.value),
  }
}

function decodeOverviewRelations(value: unknown): VPSOverview['relations'] {
  const relations = overviewArray(value)
  if (relations.length !== VPS_OVERVIEW_RELATIONS.length) invalidVPSOverview()
  return relations.map((candidate, index) => {
    const relation = overviewObject(candidate)
    const contract = VPS_OVERVIEW_RELATIONS[index]
    if (!contract || relation.kind !== contract.kind) invalidVPSOverview()
    if (!Number.isSafeInteger(relation.count) || Number(relation.count) < 0) invalidVPSOverview()
    const hasRoute = Object.prototype.hasOwnProperty.call(relation, 'route')
    if (hasRoute !== contract.route) invalidVPSOverview()
    const status = optionalOverviewString(relation, 'status')
    const route = contract.route ? overviewString(relation.route) : undefined
    const decoded: VPSOverview['relations'][number] = {
      kind: contract.kind,
      count: Number(relation.count),
      label: overviewString(relation.label),
      section: decodeOverviewSection(relation.section),
    }
    if (status !== undefined) decoded.status = status
    if (route !== undefined) decoded.route = route
    return decoded
  })
}

function decodeVPSOverview(value: unknown): VPSOverview {
  const overview = overviewObject(value)
  const summary = overviewObject(overview.summary)
  return {
    generated_at: overviewTimestamp(overview.generated_at),
    identity: decodeOverviewIdentity(overview.identity),
    anomalies: overviewArray(overview.anomalies).map(decodeOverviewAnomaly),
    summary: {
      overall: decodeOverviewSummaryCell(summary.overall),
      monitoring: decodeOverviewSummaryCell(summary.monitoring),
      ip_quality: decodeOverviewSummaryCell(summary.ip_quality),
      renewal: decodeOverviewSummaryCell(summary.renewal),
    },
    recent_activity: decodeOverviewRecentActivity(overview.recent_activity),
    facts: overviewArray(overview.facts).map(decodeOverviewFact),
    relations: decodeOverviewRelations(overview.relations),
    capabilities: overviewStringArray(overview.capabilities),
  }
}

/** Reads and fully validates the request-scoped VPS overview read model. */
export async function getVPSOverview(vpsId: string): Promise<VPSOverview> {
  let response: unknown
  try {
    response = await requestJSON<unknown>(`/api/vps/${encoded(vpsId)}/overview`)
  } catch (error) {
    if (error instanceof SyntaxError) {
      throw new InvalidVPSOverviewResponseError('malformed_json')
    }
    throw error
  }
  const overview = decodeVPSOverview(response)
  if (overview.identity.vps_id !== vpsId) invalidVPSOverview()
  return overview
}

export function overviewHasRecordsV2Read(overview: VPSOverview): boolean {
  return overview.capabilities.includes('records_v2_read')
}

export function resolveComparisonCandidates(
  input: ComparisonCandidateRequest,
  signal?: AbortSignal,
): Promise<ComparisonCandidateResponse> {
  const body: ComparisonCandidateRequest = {
    subjects: input.subjects.map((subject) => ({ kind: subject.kind, id: subject.id })),
    requested_window: {
      start: input.requested_window.start,
      end: input.requested_window.end,
    },
  }
  if (input.kinds?.length) {
    body.kinds = input.kinds.map((key) => ({
      kind: key.kind,
      schema_version: key.schema_version,
    }))
  }
  const init = jsonBodyInit('POST', body)
  if (signal) init.signal = signal
  return requestJSON<ComparisonCandidateResponse>('/api/evidence/comparison-candidates', init)
}

export function evaluateFixedComparison(
  input: ComparisonEvaluateRequest,
  signal?: AbortSignal,
): Promise<ComparisonEvaluateResponse> {
  const body: ComparisonEvaluateRequest = {
    items: input.items.map((item): ComparisonFixedItemInput => {
      if (item.snapshot_id) return { snapshot_id: item.snapshot_id }
      const revision: ComparisonFixedItemInput = {}
      if (item.record_id) revision.record_id = item.record_id
      if (item.revision_id) revision.revision_id = item.revision_id
      if (item.snapshot_ids?.length) revision.snapshot_ids = [...item.snapshot_ids]
      return revision
    }),
    baseline_index: input.baseline_index,
    alignment: input.alignment,
    requested_window: {
      start: input.requested_window.start,
      end: input.requested_window.end,
    },
    tolerance_seconds: input.tolerance_seconds,
  }
  if (input.bucket_seconds != null) body.bucket_seconds = input.bucket_seconds
  if (input.detail) {
    body.detail = {
      kind: input.detail.kind,
      schema_version: input.detail.schema_version,
      ...(input.detail.metric ? { metric: input.detail.metric } : {}),
    }
  }
  const init = jsonBodyInit('POST', body)
  if (signal) init.signal = signal
  return requestJSON<ComparisonEvaluateResponse>('/api/evidence/comparisons', init)
}

export function saveComparisonRecord(
  input: SaveComparisonRecordInput,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>('/api/records', {
    record_id: input.record_id,
    draft_id: input.draft_id,
    draft_etag: input.draft_etag,
    comparison_intent: input.comparison_intent,
  }, idempotencyKey, signal)
}

export function previewRecordExport(
  input: RecordExportPreviewInput,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RecordExportPreview> {
  return postIdempotentJSON<RecordExportPreview>('/api/record-export-previews', input, idempotencyKey, signal)
}

export function createRecordExport(
  input: { preview_id: string; preview_token: string; inventory_digest: string },
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RecordExportView> {
  return postIdempotentJSON<RecordExportView>('/api/record-exports', input, idempotencyKey, signal)
}

export function getRecordExport(exportId: string, signal?: AbortSignal): Promise<RecordExportView> {
  const init: RequestInit = {}
  if (signal) init.signal = signal
  return requestJSON<RecordExportView>(`/api/record-exports/${encoded(exportId)}`, init)
}

export function dryRunRecordImport(
  archive: Blob,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RecordImportPlan> {
  const init: RequestInit = {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/zip',
      'Idempotency-Key': idempotencyKey,
    },
    body: archive,
  }
  if (signal) init.signal = signal
  return requestJSON<RecordImportPlan>('/api/record-imports/dry-run', init)
}

export function applyRecordImport(
  planId: string,
  lockVersion: number,
  signal?: AbortSignal,
): Promise<RecordImportApplyResult> {
  const init = jsonBodyInit('POST', { lock_version: lockVersion })
  if (signal) init.signal = signal
  return requestJSON<RecordImportApplyResult>(`/api/record-imports/${encoded(planId)}/apply`, init)
}

export function downloadRecordExportContent(exportId: string, signal?: AbortSignal): Promise<Blob> {
  const init: RequestInit = { headers: { Accept: 'application/octet-stream' } }
  if (signal) init.signal = signal
  return requestBlob(`/api/record-exports/${encoded(exportId)}/content`, init)
}

export function saveComparisonRevision(
  recordId: string,
  input: SaveComparisonRevisionInput,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>(`/api/records/${encoded(recordId)}/revisions`, {
    draft_id: input.draft_id,
    draft_etag: input.draft_etag,
    base_revision_id: input.base_revision_id,
    lock_version: input.lock_version,
    authorization_epoch: input.authorization_epoch,
    comparison_intent: input.comparison_intent,
  }, idempotencyKey, signal)
}
