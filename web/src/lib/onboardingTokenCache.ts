import type { NodeEnrollmentTokenIssue } from './types'

const keyPrefix = 'houfeng:onboarding-token:'

function getStorageKey(nodeId: string) {
  return `${keyPrefix}${nodeId}`
}

function isValidCacheEntry(value: unknown): value is NodeEnrollmentTokenIssue {
  if (typeof value !== 'object' || value === null) {
    return false
  }

  const token = (value as Record<string, unknown>).token
  const issuedAt = (value as Record<string, unknown>).issued_at

  return (
    typeof token === 'string' &&
    token.trim().length > 0 &&
    typeof issuedAt === 'string' &&
    issuedAt.trim().length > 0
  )
}

export function getOnboardingTokenCache(nodeId: string): NodeEnrollmentTokenIssue | null {
  const storageKey = getStorageKey(nodeId)
  const raw = window.sessionStorage.getItem(storageKey)
  if (!raw) {
    return null
  }

  try {
    const parsed = JSON.parse(raw) as unknown
    if (isValidCacheEntry(parsed)) {
      return parsed
    }
  } catch {
    // Invalid session storage data is treated as a cache miss.
  }

  window.sessionStorage.removeItem(storageKey)
  return null
}

export function setOnboardingTokenCache(nodeId: string, value: NodeEnrollmentTokenIssue) {
  window.sessionStorage.setItem(getStorageKey(nodeId), JSON.stringify(value))
}

export function clearOnboardingTokenCache(nodeId: string) {
  window.sessionStorage.removeItem(getStorageKey(nodeId))
}
