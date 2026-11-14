import { beforeEach, describe, expect, it, vi } from 'vitest'
import { recordAllocationFixture } from '../pages/records/testFixtures'
import { requestDigest } from './recordAllocation'
import {
  clearComparisonSaveAttempts,
  comparisonSaveRecordIDs,
  readComparisonSaveAttempt,
  readyComparisonSaveAttempt,
  findPendingComparisonSaveAttempt,
  writeComparisonSaveAttempt,
  type ComparisonSaveAttemptV2,
  type LegacyComparisonSaveAttempt,
} from './comparisonSaveAttempts'

const ALLOCATION = recordAllocationFixture('result', { purpose: 'comparison_save', idempotency_key: 'allocation-a', request_digest: 'e'.repeat(64) })

function attempt(overrides: Partial<ComparisonSaveAttemptV2> = {}): ComparisonSaveAttemptV2 {
  return {
    version: 2,
    userId: 'usr_a',
    digest: 'a'.repeat(64),
    generation: '0',
    sourceRecordIds: ['rec_source'],
    payloadDigest: 'f'.repeat(64),
    allocationKey: 'allocation-a',
    requestDigest: 'e'.repeat(64),
    idempotencyKey: 'request-a',
    ...overrides,
  }
}

function sent(overrides: Partial<ComparisonSaveAttemptV2> = {}): ComparisonSaveAttemptV2 {
  return attempt({
    allocation: ALLOCATION,
    request: { record_id: ALLOCATION.record_id, draft_id: 'rdf_sent', draft_etag: 'etag-sent', comparison_intent: 'cmp1.intent', allocation: ALLOCATION },
    ...overrides,
  })
}

function legacy(overrides: Partial<LegacyComparisonSaveAttempt> = {}): LegacyComparisonSaveAttempt {
  return { version: 1, userId: 'usr_a', digest: 'a'.repeat(64), recordId: 'rec_result', idempotencyKey: 'request-a', generation: '0', sourceRecordIds: ['rec_source'], ...overrides }
}

beforeEach(() => { vi.restoreAllMocks(); sessionStorage.clear() })

describe('owned comparison retry metadata', () => {
  it('preserves an exact retry identity across reads but never adopts legacy unowned data', () => {
    const own = sent()
    sessionStorage.setItem(`houfeng.comparison-save/${own.digest}`, JSON.stringify({ recordId: 'rec_legacy', idempotencyKey: 'legacy' }))
    expect(readComparisonSaveAttempt(own)).toBeNull()
    expect(sessionStorage.getItem(`houfeng.comparison-save/${own.digest}`)).toBeNull()
    writeComparisonSaveAttempt(own)
    expect(readComparisonSaveAttempt(own)).toEqual(own)
    expect(readComparisonSaveAttempt({ ...own, userId: 'usr_b' })).toBeNull()
    expect(readComparisonSaveAttempt(own)).toEqual(own)
  })

  it('retires a prior session generation and rejects changed provenance for the same digest', () => {
    const own = sent()
    writeComparisonSaveAttempt(own)
    expect(() => readComparisonSaveAttempt({ ...own, sourceRecordIds: ['rec_other'] })).toThrow('归属已变化')
    expect(readComparisonSaveAttempt({ ...own, generation: '1' })).toBeNull()
    expect(readComparisonSaveAttempt(own)).toBeNull()
  })

  it('parses a stored legacy envelope without rewriting its bytes and refuses to overwrite it', () => {
    const old = legacy()
    const raw = JSON.stringify(old)
    sessionStorage.setItem(`houfeng.comparison-save/usr_a/${old.digest}`, raw)
    expect(readComparisonSaveAttempt(old)).toEqual(old)
    expect(() => writeComparisonSaveAttempt(attempt())).toThrow('尚未确认')
    expect(sessionStorage.getItem(`houfeng.comparison-save/usr_a/${old.digest}`)).toBe(raw)
  })

  it('never replaces a sent request with a different key or body', () => {
    const own = sent()
    writeComparisonSaveAttempt(own)
    expect(() => writeComparisonSaveAttempt({ ...own, idempotencyKey: 'request-b' })).toThrow('尚未确认')
    expect(() => writeComparisonSaveAttempt(sent({ request: { ...own.request!, draft_etag: 'etag-edited' } }))).toThrow('尚未确认')
    expect(() => writeComparisonSaveAttempt(attempt())).toThrow('尚未确认')
    writeComparisonSaveAttempt({ ...own, savedRecordId: ALLOCATION.record_id })
    expect(readComparisonSaveAttempt(own)).toEqual({ ...own, savedRecordId: ALLOCATION.record_id })
  })

  it('refuses to replay a stored body whose immutable digest no longer matches', async () => {
    const original = sent()
    const publicationDigest = await requestDigest(original.request)
    writeComparisonSaveAttempt({ ...original, publicationDigest })
    const changed = { ...original, publicationDigest, request: { ...original.request!, draft_etag: 'different' } }
    sessionStorage.setItem(`houfeng.comparison-save/usr_a/${original.digest}`, JSON.stringify(changed))
    const stored = readComparisonSaveAttempt(original)
    if (stored?.version !== 2) throw new Error('expected current attempt')
    await expect(readyComparisonSaveAttempt(stored)).rejects.toThrow('摘要不匹配')
  })

  it('refuses to replace an allocation whose reply may have been lost', () => {
    const original = attempt({ allocation: ALLOCATION, queryKey: 'comparison-route' })
    writeComparisonSaveAttempt(original)
    const edited = { ...original, payloadDigest: '1'.repeat(64), allocationKey: 'allocation-b', idempotencyKey: 'request-b',
      allocation: { ...ALLOCATION, idempotency_key: 'allocation-b' } }
    expect(() => writeComparisonSaveAttempt(edited)).toThrow()
    expect(findPendingComparisonSaveAttempt('usr_a', 'comparison-route')).toEqual(original)
  })

  it.each([
    ['unsafe generation', { allocation: { ...ALLOCATION, generation: 2 ** 53 } }],
    ['other purpose', { allocation: { ...ALLOCATION, purpose: 'record_create' as const } }],
    ['other reservation key', { allocation: { ...ALLOCATION, idempotency_key: 'allocation-z' } }],
    ['request without allocation', { allocation: undefined }],
    ['request on another record', { request: { record_id: `rec2_${'9'.repeat(32)}`, draft_id: 'rdf_sent', draft_etag: 'etag-sent', comparison_intent: 'cmp1.intent', allocation: ALLOCATION } }],
  ])('refuses to persist a malformed envelope: %s', (_name, overrides) => {
    expect(() => writeComparisonSaveAttempt(sent(overrides as Partial<ComparisonSaveAttemptV2>))).toThrow()
    expect(sessionStorage.length).toBe(0)
  })

  it.each([`rec2_${'0'.repeat(31)}`, `rec2_${'0'.repeat(33)}`, 'rec_Result', 'rrv_result', ''])('refuses to persist malformed source identity %j', (malformed) => {
    expect(() => writeComparisonSaveAttempt(attempt({ sourceRecordIds: [malformed || 'rec_'] }))).toThrow('元数据损坏')
    expect(sessionStorage.length).toBe(0)
  })

  it('cleans source and destination retry metadata without changing independent saved result content or another user', () => {
    const source = sent()
    const destination = sent({ digest: 'b'.repeat(64), sourceRecordIds: ['rec_else'] })
    const independent = attempt({ digest: 'c'.repeat(64), sourceRecordIds: ['rec_independent'] })
    const otherUser = sent({ userId: 'usr_b' })
    for (const item of [source, destination, independent, otherUser]) writeComparisonSaveAttempt(item)
    const old = legacy({ digest: 'd'.repeat(64), recordId: 'rec_oldtarget', sourceRecordIds: ['rec_independent'] })
    sessionStorage.setItem(`houfeng.comparison-save/usr_a/${old.digest}`, JSON.stringify(old))
    sessionStorage.setItem('saved-result/rec_result', 'retained B evidence')
    expect(comparisonSaveRecordIDs('usr_a')).toEqual(['rec_else', 'rec_independent', 'rec_source'].sort())
    clearComparisonSaveAttempts(source.userId, ALLOCATION.record_id)
    expect(readComparisonSaveAttempt(source)).toBeNull()
    expect(readComparisonSaveAttempt(destination)).toBeNull()
    expect(readComparisonSaveAttempt(independent)).toEqual(independent)
    expect(readComparisonSaveAttempt(otherUser)).toEqual(otherUser)
    clearComparisonSaveAttempts(source.userId, 'rec_oldtarget')
    expect(readComparisonSaveAttempt(old)).toBeNull()
    expect(sessionStorage.getItem('saved-result/rec_result')).toBe('retained B evidence')
  })

  it('keeps unresolved envelopes for other comparisons and users', () => {
    const current = sent({ queryKey: 'current-route' })
    const previous = sent({ digest: 'b'.repeat(64), queryKey: 'other-route' })
    const otherUser = sent({ userId: 'usr_b', digest: previous.digest, queryKey: 'other-route' })
    for (const item of [current, previous, otherUser]) writeComparisonSaveAttempt(item)
    expect(findPendingComparisonSaveAttempt('usr_a', 'current-route')).toEqual(current)
    expect(findPendingComparisonSaveAttempt('usr_a', 'other-route')).toEqual(previous)
    expect(readComparisonSaveAttempt(otherUser)).toEqual(otherUser)
  })

  it('does not claim a durable request identity when browser storage rejects the write', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('quota', 'QuotaExceededError') })
    expect(() => writeComparisonSaveAttempt(sent())).toThrow('quota')
    expect(readComparisonSaveAttempt(sent())).toBeNull()
  })
})
