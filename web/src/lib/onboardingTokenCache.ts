import type { NodeEnrollmentTokenIssue } from './types'

const keyPrefix = 'houfeng:onboarding-token:'

function getStorageKey(nodeId: string) {
  return `${keyPrefix}${nodeId}`
}

export function getOnboardingTokenCache(nodeId: string): NodeEnrollmentTokenIssue | null {
  const raw = window.sessionStorage.getItem(getStorageKey(nodeId))
  if (!raw) {
    return null
  }

  return JSON.parse(raw) as NodeEnrollmentTokenIssue
}

export function setOnboardingTokenCache(nodeId: string, value: NodeEnrollmentTokenIssue) {
  window.sessionStorage.setItem(getStorageKey(nodeId), JSON.stringify(value))
}

export function clearOnboardingTokenCache(nodeId: string) {
  window.sessionStorage.removeItem(getStorageKey(nodeId))
}
