import { useEffect, useRef } from 'react'

const AUTO_REFRESH_OPTIONS = [
  { label: '关闭', value: null },
  { label: '30s', value: 30_000 },
  { label: '60s', value: 60_000 },
  { label: '5m', value: 300_000 },
] as const

export type AutoRefreshOption = number | null

export { AUTO_REFRESH_OPTIONS }

/**
 * Calls `callback` at the given interval. Pauses when the page is hidden.
 * Pass `null` as interval to disable. Cleans up on unmount.
 */
export function useAutoRefresh(interval: number | null, callback: () => void) {
  const callbackRef = useRef(callback)

  useEffect(() => {
    callbackRef.current = callback
  })

  useEffect(() => {
    if (interval == null) return

    let id: ReturnType<typeof setInterval> | null = null

    function start() {
      if (id != null) return
      id = setInterval(() => callbackRef.current(), interval!)
    }

    function stop() {
      if (id == null) return
      clearInterval(id)
      id = null
    }

    function onVisibilityChange() {
      if (document.visibilityState === 'visible') start()
      else stop()
    }

    start()
    document.addEventListener('visibilitychange', onVisibilityChange)

    return () => {
      stop()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [interval])
}
