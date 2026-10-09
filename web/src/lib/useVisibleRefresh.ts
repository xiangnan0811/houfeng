import { useCallback, useEffect, useLayoutEffect, useRef } from 'react'

/** Visible list/detail/summary refresh interval. Hidden pages do not request. */
export const VISIBLE_REFRESH_INTERVAL_MS = 30_000

export type VisibleRefreshContext = {
  /** False after invalidate, refreshKey change, or unmount for the request that observed it. */
  isCurrent: () => boolean
}

export type VisibleRefreshOptions = {
  /** Gates the interval and visibility/focus wakes. Manual refresh still runs. */
  enabled?: boolean
  /** Changing this makes an in-flight callback report isCurrent() === false. */
  refreshKey?: string
}

export type VisibleRefreshController = {
  refresh: () => Promise<void>
  invalidate: () => void
}

/**
 * One visible-refresh lane. Does not read on mount: the caller starts the
 * initial request through refresh(), or keeps its own first read and then
 * uses this lane for later reads. A later refresh() shares the in-flight
 * request. After invalidate, a refresh() requested while that obsolete
 * request is still running is queued once and starts when it finishes.
 */
export function useVisibleRefresh(
  callback: (context: VisibleRefreshContext) => Promise<void>,
  options?: VisibleRefreshOptions,
): VisibleRefreshController {
  const enabled = options?.enabled ?? true
  const refreshKey = options?.refreshKey
  const callbackRef = useRef(callback)
  const enabledRef = useRef(enabled)
  const generationRef = useRef(0)
  const mountedRef = useRef(true)
  const inflightRef = useRef<{ promise: Promise<void>; generation: number } | null>(null)
  const followupRef = useRef<Promise<void> | null>(null)
  const refreshRef = useRef<() => Promise<void>>(async () => {})
  const keyRef = useRef(refreshKey)


  const refresh = useCallback((): Promise<void> => {
    if (!mountedRef.current) return Promise.resolve()
    const inflight = inflightRef.current
    if (inflight) {
      if (inflight.generation === generationRef.current) return inflight.promise
      if (!followupRef.current) {
        const startQueuedRefresh = (): Promise<void> => {
          followupRef.current = null
          if (!mountedRef.current) return Promise.resolve()
          return refreshRef.current()
        }
        followupRef.current = inflight.promise.then(
          () => startQueuedRefresh(),
          () => startQueuedRefresh(),
        )
      }
      return followupRef.current
    }

    const generation = generationRef.current
    const context: VisibleRefreshContext = {
      isCurrent: () => mountedRef.current && generationRef.current === generation,
    }
    const promise = Promise.resolve(callbackRef.current(context)).finally(() => {
      if (inflightRef.current?.promise === promise) inflightRef.current = null
    })
    inflightRef.current = { promise, generation }
    return promise
  }, [])


  useLayoutEffect(() => {
    callbackRef.current = callback
    enabledRef.current = enabled
    refreshRef.current = refresh
  }, [callback, enabled, refresh])

  const invalidate = useCallback(() => {
    generationRef.current += 1
  }, [])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      generationRef.current += 1
      followupRef.current = null
    }
  }, [])

  useEffect(() => {
    if (Object.is(keyRef.current, refreshKey)) return
    keyRef.current = refreshKey
    generationRef.current += 1
  }, [refreshKey])

  useEffect(() => {
    if (!enabled) return
    let timer: number | undefined

    const arm = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        if (document.visibilityState === 'visible') void refresh()
        arm()
      }, VISIBLE_REFRESH_INTERVAL_MS)
    }

    const onWake = () => {
      if (!enabledRef.current) return
      if (document.visibilityState !== 'visible') {
        window.clearTimeout(timer)
        timer = undefined
        return
      }
      void refresh()
      arm()
    }

    arm()
    document.addEventListener('visibilitychange', onWake)
    window.addEventListener('focus', onWake)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onWake)
      window.removeEventListener('focus', onWake)
    }
  }, [enabled, refresh])

  return { refresh, invalidate }
}
