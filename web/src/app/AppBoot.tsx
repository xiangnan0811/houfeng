import { useEffect } from 'react'

/**
 * Marks the document body as booted shortly after the first paint. Combined
 * with the `.app-booted .animate-in{animation:none}` rule, this lets the
 * entrance animation play once on initial load but suppresses the replay that
 * would otherwise fire on every client-side navigation (each route remounts
 * the page component).
 */
export function AppBoot() {
  useEffect(() => {
    const timer = window.setTimeout(() => {
      document.body.classList.add('app-booted')
    }, 600)
    return () => window.clearTimeout(timer)
  }, [])
  return null
}
