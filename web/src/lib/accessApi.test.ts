import { describe, expect, it } from 'vitest'
import { parseAccessMemberSummary } from './accessApi'
import { ApiError } from './apiRequest'

const user = {
  user_id: 'usr_ops',
  username: 'ops',
  display_name: '运维',
  role: 'admin',
  is_supervisor: false,
  disabled_at: null,
  created_at: '2026-01-01T00:00:00Z',
}

function omit(key: 'is_supervisor' | 'disabled_at'): Record<string, unknown> {
  const copy: Record<string, unknown> = { ...user }
  delete copy[key]
  return copy
}

describe('parseAccessMemberSummary', () => {
  it('keeps a normal row that includes is_supervisor false and disabled_at null', () => {
    expect(parseAccessMemberSummary(user)).toEqual(user)
  })

  it('keeps a normal row when missing is false', () => {
    expect(parseAccessMemberSummary({ ...user, missing: false })).toEqual(user)
  })

  it('accepts a dangling row that is exactly user_id and missing true', () => {
    expect(parseAccessMemberSummary({ user_id: 'usr_missing', missing: true })).toEqual({
      user_id: 'usr_missing',
      missing: true,
    })
  })

  it('rejects a dangling row that carries any other field', () => {
    expect(() => parseAccessMemberSummary({ user_id: 'usr_missing', missing: true, username: 'ops' })).toThrow(ApiError)
    expect(() => parseAccessMemberSummary({ ...user, missing: true })).toThrow(ApiError)
  })

  it('rejects a normal row that omits is_supervisor or disabled_at', () => {
    expect(() => parseAccessMemberSummary(omit('is_supervisor'))).toThrow(ApiError)
    expect(() => parseAccessMemberSummary(omit('disabled_at'))).toThrow(ApiError)
    expect(() => parseAccessMemberSummary({ ...omit('disabled_at'), missing: false })).toThrow(ApiError)
  })

  it('does not treat a partial row or a non-boolean missing as dangling', () => {
    expect(() => parseAccessMemberSummary({ user_id: 'usr_missing' })).toThrow(ApiError)
    expect(() => parseAccessMemberSummary({ user_id: 'usr_missing', missing: null })).toThrow(ApiError)
    expect(() => parseAccessMemberSummary({ user_id: 'usr_missing', missing: 'true' })).toThrow(ApiError)
  })
})
