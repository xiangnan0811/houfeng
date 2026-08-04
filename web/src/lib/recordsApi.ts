import allowlistedApiError from './apiError'
import {
  jsonBodyInit,
  requestEmpty as transportRequestEmpty,
  requestJSON as transportRequestJSON,
  withQuery,
} from './apiRequest'
import type {
  CreateRecordDraftInput,
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
  RestoreRecordRevisionInput,
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
        q: filter.q,
        lifecycle: filter.lifecycle,
        record_type: filter.record_type,
        sort: filter.sort,
        limit: filter.limit,
        cursor: filter.cursor,
      }
    : undefined))
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
    filter ? { limit: filter.limit } : undefined,
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
