import { parseRecordAllocation, requestDigest, sameRecordAllocation, validIdempotencyKey } from './recordAllocation'
import { isRecordId } from './recordIdentity'
import { registerRecordClientAllocation, type RecordClientAllocationReference } from './recordSecurity'
import type { RecordAllocation, RecordDraftPayload, SaveComparisonRecordInput } from './types'

const STORAGE_PREFIX = 'houfeng.comparison-save/'
const DIGEST = /^[a-f0-9]{64}(?![\s\S])/

type ComparisonSaveScope = {
  userId: string
  digest: string
  generation: string
  sourceRecordIds: readonly string[]
}

/**
 * Earlier clients stored no draft. They can only ask for their completed result
 * with the original key and a freshly authenticated matching comparison intent.
 */
export type LegacyComparisonSaveAttempt = ComparisonSaveScope & {
  version: 1
  recordId: string
  idempotencyKey: string
}

/**
 * `allocationKey`/`requestDigest` are fixed before the reservation request;
 * `request` is the exact save body, fixed before the first save request and
 * replayed byte-for-byte with `idempotencyKey` until it succeeds. A request
 * that has been sent is never treated as uncommitted because of an error.
 */
export type ComparisonSaveAttemptV2 = ComparisonSaveScope & {
  version: 2
  payloadDigest: string
  payload?: RecordDraftPayload
  /** A draft creation was dispatched without a recoverable reply. */
  draftPending?: boolean
  /** Canonical comparison route, so a sent request can be found before evaluation. */
  queryKey?: string
  allocationKey: string
  requestDigest: string
  idempotencyKey: string
  allocation?: RecordAllocation
  request?: SaveComparisonRecordInput & { allocation: RecordAllocation }
  publicationDigest?: string
  savedRecordId?: string
}

export type ComparisonSaveAttempt = LegacyComparisonSaveAttempt | ComparisonSaveAttemptV2

export type ComparisonSaveIdentity = ComparisonSaveScope

function storageKey(userId: string, digest: string): string {
  return `${STORAGE_PREFIX}${encodeURIComponent(userId)}/${digest}`
}

function corrupt(): never {
  throw new Error('比较重试元数据损坏，请重新评估')
}

function decodeAttempt(raw: string): ComparisonSaveAttempt {
  const value: unknown = JSON.parse(raw)
  if (!value || typeof value !== 'object' || Array.isArray(value)) corrupt()
  const item = value as Partial<Omit<LegacyComparisonSaveAttempt, 'version'>> & Partial<Omit<ComparisonSaveAttemptV2, 'version'>> & { version?: unknown }
  if (typeof item.userId !== 'string' || !item.userId
    || typeof item.digest !== 'string' || !DIGEST.test(item.digest)
    || typeof item.generation !== 'string' || !/^(0|[1-9][0-9]*)(?![\s\S])/.test(item.generation)
    || !Array.isArray(item.sourceRecordIds) || item.sourceRecordIds.length === 0
    || item.sourceRecordIds.some((id) => typeof id !== 'string' || !isRecordId(id))
    || item.sourceRecordIds.some((id, index) => index > 0 && id <= item.sourceRecordIds![index - 1]!)
    || typeof item.idempotencyKey !== 'string' || !item.idempotencyKey) {
    corrupt()
  }
  if (item.version === 1) {
    if (typeof item.recordId !== 'string' || !item.recordId.startsWith('rec_') || !isRecordId(item.recordId)) corrupt()
    return value as LegacyComparisonSaveAttempt
  }
  if (item.version !== 2
    || typeof item.payloadDigest !== 'string' || !DIGEST.test(item.payloadDigest)
    || (item.queryKey !== undefined && (typeof item.queryKey !== 'string' || !item.queryKey))
    || (item.payload !== undefined && (!item.payload || typeof item.payload !== 'object' || Array.isArray(item.payload)))
    || (item.draftPending !== undefined && item.draftPending !== true)
    || (item.publicationDigest !== undefined && (typeof item.publicationDigest !== 'string' || !DIGEST.test(item.publicationDigest)))
    || typeof item.allocationKey !== 'string' || !validIdempotencyKey(item.allocationKey)
    || typeof item.requestDigest !== 'string' || !DIGEST.test(item.requestDigest)
    || !validIdempotencyKey(item.idempotencyKey)
    || (item.savedRecordId !== undefined && (!item.request || item.savedRecordId !== item.request.record_id))) {
    corrupt()
  }
  const expected = { purpose: 'comparison_save' as const, idempotencyKey: item.allocationKey, requestDigest: item.requestDigest }
  const allocation = item.allocation === undefined ? undefined : parseRecordAllocation(item.allocation, expected)
  if (item.request !== undefined) {
    const request = item.request
    if (!allocation || !request || typeof request !== 'object'
      || Object.keys(request).sort().join(',') !== 'allocation,comparison_intent,draft_etag,draft_id,record_id'
      || request.record_id !== allocation.record_id
      || !sameRecordAllocation(parseRecordAllocation(request.allocation, expected), allocation)
      || typeof request.draft_id !== 'string' || !request.draft_id
      || typeof request.draft_etag !== 'string' || !request.draft_etag
      || typeof request.comparison_intent !== 'string' || !request.comparison_intent) {
      corrupt()
    }
  }
  return value as ComparisonSaveAttemptV2
}

/** Old digest-only entries have no owner; discard metadata, never adopt it. */
export function discardUnownedComparisonSaveAttempts(): void {
  const keys: string[] = []
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (key?.startsWith(STORAGE_PREFIX) && !key.slice(STORAGE_PREFIX.length).includes('/')) keys.push(key)
  }
  for (const key of keys) sessionStorage.removeItem(key)
}

export function readComparisonSaveAttempt(identity: ComparisonSaveIdentity): ComparisonSaveAttempt | null {
  discardUnownedComparisonSaveAttempts()
  const key = storageKey(identity.userId, identity.digest)
  const raw = sessionStorage.getItem(key)
  if (raw === null) return null
  const attempt = decodeAttempt(raw)
  if (attempt.userId !== identity.userId || attempt.digest !== identity.digest) throw new Error('比较重试元数据归属不匹配')
  if (attempt.generation !== identity.generation) {
    sessionStorage.removeItem(key)
    return null
  }
  if (attempt.sourceRecordIds.length !== identity.sourceRecordIds.length
    || attempt.sourceRecordIds.some((id, index) => id !== identity.sourceRecordIds[index])) {
    throw new Error('比较来源归属已变化，请重新评估')
  }
  return attempt
}

/** Old envelopes lack a route: matching sources with a different digest are an unresolved recovery gap. */
export function findLegacyComparisonSaveGap(identity: ComparisonSaveIdentity): LegacyComparisonSaveAttempt | null {
  const prefix = `${STORAGE_PREFIX}${encodeURIComponent(identity.userId)}/`
  const matches: LegacyComparisonSaveAttempt[] = []
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (!key?.startsWith(prefix)) continue
    const raw = sessionStorage.getItem(key)
    if (raw === null) continue
    const attempt = decodeAttempt(raw)
    if (attempt.userId !== identity.userId || key !== storageKey(identity.userId, attempt.digest)) throw new Error('比较重试元数据归属不匹配')
    if (attempt.version === 1 && attempt.generation === identity.generation && attempt.digest !== identity.digest
      && attempt.sourceRecordIds.length === identity.sourceRecordIds.length
      && attempt.sourceRecordIds.every((id, position) => id === identity.sourceRecordIds[position])) matches.push(attempt)
  }
  return matches.sort((left, right) => left.digest.localeCompare(right.digest))[0] ?? null
}

/** Find an unresolved current request before fetching a new comparison intent. */
export function findPendingComparisonSaveAttempt(userId: string, queryKey: string): ComparisonSaveAttemptV2 | null {
  const prefix = `${STORAGE_PREFIX}${encodeURIComponent(userId)}/`
  let found: ComparisonSaveAttemptV2 | null = null
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (!key?.startsWith(prefix)) continue
    const raw = sessionStorage.getItem(key)
    if (raw === null) continue
    const attempt = decodeAttempt(raw)
    if (attempt.userId !== userId || key !== storageKey(userId, attempt.digest)) throw new Error('比较重试元数据归属不匹配')
    if (attempt.version !== 2 || attempt.queryKey !== queryKey || attempt.savedRecordId) continue
    if (found) throw new Error('此比较有多次未确认的另存，请先核对记录列表')
    found = attempt
  }
  return found
}

/** Only current envelopes are written; a legacy entry keeps its original bytes. */
export function writeComparisonSaveAttempt(attempt: ComparisonSaveAttemptV2): void {
  const raw = JSON.stringify(attempt)
  decodeAttempt(raw)
  const key = storageKey(attempt.userId, attempt.digest)
  const previous = sessionStorage.getItem(key)
  if (previous !== null) {
    const stored = decodeAttempt(previous)
    // Reservation and draft creation may have committed even without a reply.
    // Only monotonic updates to the same fixed request are accepted.
    if (stored.version === 1 || stored.idempotencyKey !== attempt.idempotencyKey
      || stored.allocationKey !== attempt.allocationKey || stored.requestDigest !== attempt.requestDigest
      || stored.payloadDigest !== attempt.payloadDigest || stored.generation !== attempt.generation
      || stored.queryKey !== attempt.queryKey || JSON.stringify(stored.payload) !== JSON.stringify(attempt.payload)
      || JSON.stringify(stored.sourceRecordIds) !== JSON.stringify(attempt.sourceRecordIds)
      || (stored.allocation && JSON.stringify(stored.allocation) !== JSON.stringify(attempt.allocation))
      || (stored.request && JSON.stringify(stored.request) !== JSON.stringify(attempt.request))
      || (stored.publicationDigest && stored.publicationDigest !== attempt.publicationDigest)
      || (stored.draftPending && !attempt.draftPending && !attempt.request)
      || (stored.savedRecordId && stored.savedRecordId !== attempt.savedRecordId)) {
      throw new Error('上次另存结果尚未确认，不能改写重试状态')
    }
  }
  // Storage failure must stop publication: memory alone cannot preserve a
  // successful or response-lost request identity across navigation/reload.
  sessionStorage.setItem(key, raw)
  if (attempt.allocation && !attempt.savedRecordId) {
    registerRecordClientAllocation(attempt.userId, { record_id: attempt.allocation.record_id, allocation_id: attempt.allocation.allocation_id })
  }
}

/** Validate the persisted request before replay; older V2 entries gain only the digest. */
export async function readyComparisonSaveAttempt(attempt: ComparisonSaveAttemptV2): Promise<ComparisonSaveAttemptV2> {
  if (!attempt.request) throw new Error('比较另存请求尚未绑定')
  const digest = await requestDigest(attempt.request)
  if (attempt.publicationDigest && attempt.publicationDigest !== digest) throw new Error('比较另存请求摘要不匹配')
  const raw = sessionStorage.getItem(storageKey(attempt.userId, attempt.digest))
  if (raw === null) throw new Error('本地比较重试状态已清理')
  const current = decodeAttempt(raw)
  if (current.version !== 2 || current.idempotencyKey !== attempt.idempotencyKey
    || JSON.stringify(current.request) !== JSON.stringify(attempt.request)) throw new Error('比较重试状态已变化')
  if (current.publicationDigest && current.publicationDigest !== digest) throw new Error('比较另存请求摘要不匹配')
  if (current.publicationDigest) return current
  const completed = { ...current, publicationDigest: digest }
  writeComparisonSaveAttempt(completed)
  return completed
}

export function retireComparisonSaveAttempt(userId: string, digest: string): void {
  sessionStorage.removeItem(storageKey(userId, digest))
}


/** The saved target (if any) and its sources: every record the envelope depends on. */
function attemptRecordIds(attempt: ComparisonSaveAttempt): string[] {
  const target = attempt.version === 1 ? attempt.recordId : attempt.allocation?.record_id
  return target ? [target, ...attempt.sourceRecordIds] : [...attempt.sourceRecordIds]
}

/** Clears only retry metadata; no record content or saved result is touched. */
export function clearComparisonSaveAttempts(userId: string, recordId?: string): void {
  discardUnownedComparisonSaveAttempts()
  const prefix = `${STORAGE_PREFIX}${encodeURIComponent(userId)}/`
  const keys: string[] = []
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (!key?.startsWith(prefix)) continue
    if (!recordId) {
      keys.push(key)
      continue
    }
    const raw = sessionStorage.getItem(key)
    if (raw === null) continue
    let attempt: ComparisonSaveAttempt
    try {
      attempt = decodeAttempt(raw)
    } catch {
      // Unreadable retry metadata cannot prove independence from this source.
      keys.push(key)
      continue
    }
    if (attemptRecordIds(attempt).includes(recordId)) keys.push(key)
  }
  for (const key of keys) sessionStorage.removeItem(key)
}

/** Existing sources and confirmed targets can use root authorization reconciliation. */
export function comparisonSaveRecordIDs(userId: string): string[] {
  discardUnownedComparisonSaveAttempts()
  const prefix = `${STORAGE_PREFIX}${encodeURIComponent(userId)}/`
  const ids = new Set<string>()
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (!key?.startsWith(prefix)) continue
    const raw = sessionStorage.getItem(key)
    if (raw === null) continue
    const attempt = decodeAttempt(raw)
    if (attempt.userId !== userId || key !== storageKey(userId, attempt.digest)) throw new Error('比较重试元数据归属不匹配')
    for (const recordId of attempt.sourceRecordIds) ids.add(recordId)
    if (attempt.version === 2 && attempt.savedRecordId) ids.add(attempt.savedRecordId)
  }
  return [...ids].sort()
}

/** Pending targets are reconciled through allocation ownership, not root reads. */
export function comparisonSaveAllocationReferences(userId: string): RecordClientAllocationReference[] {
  const prefix = `${STORAGE_PREFIX}${encodeURIComponent(userId)}/`
  const references: RecordClientAllocationReference[] = []
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)
    if (!key?.startsWith(prefix)) continue
    const raw = sessionStorage.getItem(key)
    if (raw === null) continue
    const attempt = decodeAttempt(raw)
    if (attempt.userId !== userId || key !== storageKey(userId, attempt.digest)) throw new Error('比较重试元数据归属不匹配')
    if (attempt.version === 2 && attempt.allocation && !attempt.savedRecordId) {
      references.push({ record_id: attempt.allocation.record_id, allocation_id: attempt.allocation.allocation_id })
    }
  }
  return references
}
