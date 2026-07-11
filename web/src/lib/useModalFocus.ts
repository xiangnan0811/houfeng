import { useEffect, useRef } from 'react'

import {
  acquireBodyScrollLock,
  isTopModal,
  registerModal,
} from './modalStack'

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function useModalFocus<T extends HTMLElement>(
  active: boolean,
  onClose: () => void,
  modalId: string,
  dismissOnEscape = true,
  parentModalId: string | null = null,
) {
  const containerRef = useRef<T | null>(null)
  const onCloseRef = useRef(onClose)
  const dismissOnEscapeRef = useRef(dismissOnEscape)
  const restoreFocusRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    onCloseRef.current = onClose
    dismissOnEscapeRef.current = dismissOnEscape
  }, [dismissOnEscape, onClose])

  useEffect(() => {
    if (!active) return

    const previousActive = document.activeElement
    restoreFocusRef.current = previousActive instanceof HTMLElement ? previousActive : null

    const container = containerRef.current
    if (!container) return

    const unregisterModal = registerModal({
      id: modalId,
      container,
      restoreTarget: restoreFocusRef.current,
      parentId: resolveParentModalId(
        parentModalId,
        restoreFocusRef.current,
        modalId,
      ),
    })
    const releaseBodyScrollLock = acquireBodyScrollLock()
    if (isTopModal(modalId)) {
      const [firstFocusable] = getFocusableElements(container)
      ;(firstFocusable ?? container).focus()
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented || !isTopModal(modalId)) return

      if (event.key === 'Escape') {
        event.preventDefault()
        if (dismissOnEscapeRef.current) onCloseRef.current()
        return
      }

      if (event.key !== 'Tab') return

      const container = containerRef.current
      if (!container) return

      const [firstFocusable, ...remainingFocusable] = getFocusableElements(container)
      if (!firstFocusable) {
        event.preventDefault()
        container.focus()
        return
      }

      const lastFocusable = remainingFocusable.at(-1) ?? firstFocusable
      const activeElement = document.activeElement

      if (event.shiftKey) {
        if (activeElement === firstFocusable || !container.contains(activeElement)) {
          event.preventDefault()
          lastFocusable.focus()
        }
        return
      }

      if (activeElement === lastFocusable || !container.contains(activeElement)) {
        event.preventDefault()
        firstFocusable.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown)

    return () => {
      const wasTopModal = isTopModal(modalId)
      document.removeEventListener('keydown', handleKeyDown)
      unregisterModal()
      releaseBodyScrollLock()

      const restoreTarget = restoreFocusRef.current
      restoreFocusRef.current = null
      if (wasTopModal && restoreTarget && document.contains(restoreTarget)) {
        restoreModalFocus(restoreTarget)
      }
    }
  }, [active, modalId, parentModalId])

  return containerRef
}

function getFocusableElements(container: HTMLElement) {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => {
      if (element.tabIndex < 0) return false
      if (element.hidden || element.closest('[hidden]')) return false
      if (element.closest('[aria-hidden="true"]')) return false

      const style = window.getComputedStyle(element)
      return style.display !== 'none' && style.visibility !== 'hidden'
    },
  )
}

function resolveParentModalId(
  explicitParentId: string | null,
  restoreTarget: HTMLElement | null,
  modalId: string,
) {
  if (explicitParentId) return explicitParentId

  const candidate = restoreTarget?.closest<HTMLElement>('[data-modal-stack-id]')
  const candidateId = candidate?.getAttribute('data-modal-stack-id') ?? null
  if (!candidate || !candidateId || isDescendantOf(candidate, modalId)) return null
  return candidateId
}

function isDescendantOf(container: HTMLElement, ancestorId: string) {
  const visited = new Set<string>()
  let parentId = container.getAttribute('data-modal-stack-parent-id')

  while (parentId && !visited.has(parentId)) {
    if (parentId === ancestorId) return true
    visited.add(parentId)
    parentId = findModalContainer(parentId)?.getAttribute('data-modal-stack-parent-id') ?? null
  }

  return false
}

function findModalContainer(id: string) {
  return Array.from(
    document.querySelectorAll<HTMLElement>('[data-modal-stack-id]'),
  ).find((container) => container.getAttribute('data-modal-stack-id') === id)
}

function restoreModalFocus(target: HTMLElement) {
  if (!target.closest('[inert]')) {
    target.focus()
    return
  }

  queueMicrotask(() => {
    if (!document.contains(target) || target.closest('[inert]')) return

    const activeElement = document.activeElement
    if (
      activeElement instanceof HTMLElement &&
      activeElement !== document.body &&
      activeElement.isConnected &&
      !activeElement.closest('[inert]')
    ) {
      return
    }

    target.focus()
  })
}
