import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  acquireBodyScrollLock,
  getModalDepth,
  isTopModal,
  registerModal,
  subscribeModalStack,
} from './modalStack'

const cleanups: Array<() => void> = []

afterEach(() => {
  while (cleanups.length > 0) cleanups.pop()?.()
  document.body.style.overflow = ''
})

function register(
  id: string,
  container = document.createElement('div'),
  parentId: string | null = null,
) {
  const cleanup = registerModal({ id, container, restoreTarget: null, parentId })
  cleanups.push(cleanup)
  return cleanup
}

describe('modalStack', () => {
  it('tracks one-based depth and exposes only the latest modal as top', () => {
    const unregisterParent = register('parent')
    expect(getModalDepth('parent')).toBe(1)
    expect(isTopModal('parent')).toBe(true)

    const unregisterChild = register('child')
    expect(getModalDepth('child')).toBe(2)
    expect(isTopModal('parent')).toBe(false)
    expect(isTopModal('child')).toBe(true)

    unregisterChild()
    expect(getModalDepth('child')).toBe(0)
    expect(isTopModal('parent')).toBe(true)

    unregisterParent()
    expect(getModalDepth('parent')).toBe(0)
  })

  it('keeps a replacement registration when stale cleanup runs', () => {
    const staleCleanup = register('shared')
    const currentCleanup = register('shared')

    expect(getModalDepth('shared')).toBe(1)
    staleCleanup()
    expect(isTopModal('shared')).toBe(true)

    currentCleanup()
    expect(isTopModal('shared')).toBe(false)
  })

  it('keeps descendants above ancestors that register later', () => {
    register('child', document.createElement('div'), 'parent')
    expect(getModalDepth('child')).toBe(1)

    register('parent')
    expect(getModalDepth('parent')).toBe(1)
    expect(getModalDepth('child')).toBe(2)
    expect(isTopModal('child')).toBe(true)
  })

  it('notifies subscribers only when the effective stack changes', () => {
    const listener = vi.fn()
    const unsubscribe = subscribeModalStack(listener)

    const cleanup = register('notified')
    expect(listener).toHaveBeenCalledTimes(1)

    cleanup()
    cleanup()
    expect(listener).toHaveBeenCalledTimes(2)

    unsubscribe()
  })

  it('holds body scroll lock until the final owner releases it', () => {
    document.body.style.overflow = 'clip'
    const releaseParent = acquireBodyScrollLock()
    const releaseChild = acquireBodyScrollLock()
    cleanups.push(releaseParent, releaseChild)

    expect(document.body).toHaveStyle({ overflow: 'hidden' })
    releaseChild()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    releaseParent()
    releaseParent()
    expect(document.body).toHaveStyle({ overflow: 'clip' })
  })
})
