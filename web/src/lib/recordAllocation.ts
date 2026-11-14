import { ApiError } from './apiRequest'
import type { RecordAllocation, RecordAllocationPurpose } from './types'

const RESERVED_RECORD_ID = /^rec2_[0-9a-f]{32}(?![\s\S])/
const DIGEST = /^[0-9a-f]{64}(?![\s\S])/
const PURPOSES: readonly RecordAllocationPurpose[] = ['record_create', 'evidence_preview', 'comparison_save']

/** Check the JSON numeric token before parsing can round it to a safe integer. */
export function decodeAllocationJSON(raw: string): unknown {
  const value: unknown = JSON.parse(raw)
  for (let index = 0; index < raw.length;) {
    if (raw[index] !== '"') { index += 1; continue }
    const start = index++
    while (index < raw.length) {
      if (raw[index] === '\\') { index += 2; continue }
      if (raw[index++] === '"') break
    }
    const name: unknown = JSON.parse(raw.slice(start, index))
    let next = index
    while (/\s/.test(raw[next] ?? '') && next < raw.length) next += 1
    if (name !== 'generation' || raw[next] !== ':') continue
    next += 1
    while (/\s/.test(raw[next] ?? '') && next < raw.length) next += 1
    const token = /^[^,}\]\s]+/.exec(raw.slice(next))?.[0] ?? ''
    if (!/^(0|[1-9][0-9]*)$/.test(token) || BigInt(token) > BigInt(Number.MAX_SAFE_INTEGER)) {
      throw new Error('记录预留响应无效')
    }
  }
  return value
}

/**
 * Validates a server reservation exactly as it will be stored and echoed back.
 * It checks shape and binding only: neither the ID domain nor `generation`
 * proves certification, so callers must never branch on them.
 */
export function parseRecordAllocation(
  value: unknown,
  expected: { purpose: RecordAllocationPurpose; idempotencyKey?: string; requestDigest?: string; recordId?: string },
): RecordAllocation {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('记录预留响应无效')
  const item = value as Partial<Record<keyof RecordAllocation, unknown>>
  const keys = Object.keys(value).sort().join(',')
  if (keys !== 'allocation_id,generation,idempotency_key,purpose,record_id,request_digest,reservation_id'
    || typeof item.allocation_id !== 'string' || !/^rqa_[0-9a-f]{32}(?![\s\S])/.test(item.allocation_id)
    || typeof item.reservation_id !== 'string' || !/^rba_[0-9a-f]{32}(?![\s\S])/.test(item.reservation_id)
    || typeof item.record_id !== 'string' || !RESERVED_RECORD_ID.test(item.record_id)
    || typeof item.purpose !== 'string' || !PURPOSES.includes(item.purpose as RecordAllocationPurpose)
    || typeof item.request_digest !== 'string' || !DIGEST.test(item.request_digest) || /^0+$/.test(item.request_digest)
    // Unsafe integers would round in JSON.parse; reject instead of storing a different value.
    || typeof item.generation !== 'number' || !Number.isSafeInteger(item.generation) || item.generation < 0
    || typeof item.idempotency_key !== 'string' || !validIdempotencyKey(item.idempotency_key)) {
    throw new Error('记录预留响应无效')
  }
  if (item.purpose !== expected.purpose
    || (expected.idempotencyKey !== undefined && item.idempotency_key !== expected.idempotencyKey)
    || (expected.requestDigest !== undefined && item.request_digest !== expected.requestDigest)
    || (expected.recordId !== undefined && item.record_id !== expected.recordId)) {
    throw new Error('记录预留与本次请求不一致')
  }
  return {
    allocation_id: item.allocation_id,
    reservation_id: item.reservation_id,
    record_id: item.record_id,
    purpose: item.purpose,
    request_digest: item.request_digest,
    generation: item.generation,
    idempotency_key: item.idempotency_key,
  }
}

export function sameRecordAllocation(left: RecordAllocation, right: RecordAllocation): boolean {
  return left.allocation_id === right.allocation_id && left.reservation_id === right.reservation_id
    && left.record_id === right.record_id && left.purpose === right.purpose
    && left.request_digest === right.request_digest && left.generation === right.generation
    && left.idempotency_key === right.idempotency_key
}

// Mirrors the server key grammar (unreserved URL characters, 1-200 bytes).
export function validIdempotencyKey(value: string): boolean {
  return /^[A-Za-z0-9._~-]{1,200}(?![\s\S])/.test(value)
}

function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalize)
  if (value !== null && typeof value === 'object') {
    const record = value as Record<string, unknown>
    return Object.fromEntries(Object.keys(record).sort().map((key) => [key, canonicalize(record[key])]))
  }
  return value
}

/** Lowercase SHA-256 of the canonical JSON intent the reservation binds to. */
export async function requestDigest(intent: unknown): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(canonicalize(intent))))
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/**
 * Explains a failed publication without claiming it did not commit. A 4xx
 * after the request was bound can follow a completed operation (read-fence
 * change, a parallel attempt committing between lookup and draft checks), so
 * every failure leaves the envelope in place for an exact replay.
 */
export function unresolvedPublicationMessage(cause: unknown, action: '发布' | '另存'): string {
  if (cause instanceof ApiError) {
    if (cause.code === 'publication_pending' || cause.code === 'record_operation_in_progress') {
      return `${action}仍在服务端确认中，请稍后重试确认`
    }
    if (cause.code === 'idempotency_conflict' || cause.code === 'idempotency_key_reused') {
      return `服务端认为本次重试与原${action}请求不一致，不能自动确认，请在记录列表核对`
    }
    if (cause.status === 403 || cause.status === 404 || cause.status === 410) {
      return `暂时无法确认上次${action}结果（访问或草稿状态已变化），可稍后重试确认`
    }
  }
  return `上次${action}结果尚未确认，重试会沿用原请求，不会重复创建`
}
