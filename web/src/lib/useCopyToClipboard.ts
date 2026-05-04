import { useCallback, useEffect, useRef, useState } from 'react'

type CopyFn = (text: string) => Promise<boolean>

/**
 * Wraps `navigator.clipboard.writeText` with a fallback to the legacy
 * `document.execCommand('copy')` path so it still works under HTTP / older
 * browsers / sandboxed iframes where the async clipboard API is unavailable.
 *
 * After a successful copy, `copied` flips to `true` for `resetMs` milliseconds.
 * The reset timer is cleaned up on unmount and on subsequent copies, so the
 * flag never leaks past component lifetime.
 */
export function useCopyToClipboard(resetMs = 1500): {
  copy: CopyFn
  copied: boolean
} {
  const [copied, setCopied] = useState(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(
    () => () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current)
        timeoutRef.current = null
      }
    },
    [],
  )

  const copy = useCallback<CopyFn>(
    async (text) => {
      let ok = false
      try {
        if (
          typeof navigator !== 'undefined' &&
          navigator.clipboard &&
          typeof navigator.clipboard.writeText === 'function' &&
          typeof window !== 'undefined' &&
          window.isSecureContext
        ) {
          await navigator.clipboard.writeText(text)
          ok = true
        } else if (typeof document !== 'undefined') {
          // Fallback for non-secure contexts (HTTP / older Chromium / iframes).
          const ta = document.createElement('textarea')
          ta.value = text
          ta.setAttribute('readonly', '')
          ta.style.position = 'fixed'
          ta.style.top = '0'
          ta.style.left = '0'
          ta.style.opacity = '0'
          document.body.appendChild(ta)
          ta.select()
          ta.setSelectionRange(0, text.length)
          try {
            ok = document.execCommand('copy')
          } catch {
            ok = false
          }
          document.body.removeChild(ta)
        }
      } catch {
        ok = false
      }
      if (ok) {
        setCopied(true)
        if (timeoutRef.current) {
          clearTimeout(timeoutRef.current)
        }
        timeoutRef.current = setTimeout(() => {
          setCopied(false)
          timeoutRef.current = null
        }, resetMs)
      }
      return ok
    },
    [resetMs],
  )

  return { copy, copied }
}
