import { useEffect, useLayoutEffect, useRef, useState } from 'react'

import { ApiError, getSubscriptionExchangeRateStatus, refreshSubscriptionExchangeRates } from '../../lib/api'
import type { ExchangeRatePairStatus, ExchangeRateStatusSnapshot } from '../../lib/types'
import {
  describeExchangeRateStatus,
  exchangeRateRefreshActive,
  exchangeRateRefreshSucceeded,
  type ExchangeRateNotice,
} from './exchangeRatePresentation'

/** Short poll while a refresh is queued or running. The first check follows the snapshot immediately. */
export const EXCHANGE_RATE_POLL_MS = 1000

/** Status transport failed. Amounts stay on the last cost payload; the text never includes the URL or response body. */
const STATUS_READ_ERROR = '汇率状态暂不可读'

export type ExchangeRateRefreshController = {
  notice: ExchangeRateNotice | null
  /** The status document could not be read. Costs stay as they were; retry with readStatus, not a refresh POST. */
  statusUnavailable: boolean
  refreshing: boolean
  buttonLabel: string
  refresh: () => void
  /** Read-only status recheck. Does not request another refresh. */
  readStatus: () => void
}

function describeError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error && err.message.trim()) return err.message
  return fallback
}

function isAbortError(err: unknown): boolean {
  return (err instanceof DOMException || err instanceof Error) && err.name === 'AbortError'
}

function snapshotItems(snapshot: ExchangeRateStatusSnapshot | null | undefined): ExchangeRatePairStatus[] {
  return Array.isArray(snapshot?.items) ? snapshot.items : []
}

function pageVisible(): boolean {
  return document.visibilityState === 'visible'
}

export function useExchangeRateRefresh(onSuccess: () => void): ExchangeRateRefreshController {
  const [items, setItems] = useState<ExchangeRatePairStatus[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [userNotice, setUserNotice] = useState<ExchangeRateNotice | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [pollEpoch, setPollEpoch] = useState(0)
  const itemsRef = useRef(items)
  const onSuccessRef = useRef(onSuccess)
  const statusErrorRef = useRef<string | null>(null)
  const submittingRef = useRef(false)
  const manualPending = useRef(false)
  const appliedSeq = useRef(0)
  const requestSeq = useRef(0)
  const generation = useRef(0)
  const statusAbort = useRef<AbortController | null>(null)
  const refreshAbort = useRef<AbortController | null>(null)
  const readStatusRef = useRef<() => void>(() => {})

  useEffect(() => {
    onSuccessRef.current = onSuccess
  }, [onSuccess])

  useEffect(() => () => {
    generation.current += 1
    statusAbort.current?.abort()
    refreshAbort.current?.abort()
  }, [])

  function issue(): { seq: number; generation: number } {
    const current = generation.current
    requestSeq.current += 1
    return { seq: requestSeq.current, generation: current }
  }

  function beginStatusRequest(): AbortSignal {
    statusAbort.current?.abort()
    const controller = new AbortController()
    statusAbort.current = controller
    return controller.signal
  }

  function clearStatusReadError() {
    if (statusErrorRef.current == null) return
    statusErrorRef.current = null
    setStatusError(null)
  }

  function reportStatusReadFailure(err: unknown, signal: AbortSignal) {
    if (signal.aborted || isAbortError(err)) return
    statusErrorRef.current = STATUS_READ_ERROR
    setStatusError(STATUS_READ_ERROR)
  }

  function applySnapshot(
    ticket: { seq: number; generation: number },
    next: ExchangeRatePairStatus[],
    source: 'load' | 'refresh' | 'poll',
  ): boolean {
    if (ticket.generation !== generation.current || ticket.seq < appliedSeq.current) return false
    appliedSeq.current = ticket.seq
    const previous = itemsRef.current
    itemsRef.current = next
    setItems(next)
    clearStatusReadError()
    const failed = next.some((item) => item.refresh_status === 'failed')
    const settled = !exchangeRateRefreshActive(next)
    // A pair that left queued/running is a completed refresh. HTTP 202 is only acceptance:
    // a body that is already the idle cache (worker finished before the response) must
    // still reload costs, without calling that acceptance a success by itself.
    if (source !== 'load' && exchangeRateRefreshSucceeded(previous, next)) {
      manualPending.current = false
      onSuccessRef.current()
      if (!failed) setUserNotice({ tone: 'status', text: '汇率已更新' })
      return true
    }
    if (manualPending.current && source !== 'load' && settled) {
      manualPending.current = false
      if (!failed) {
        onSuccessRef.current()
        if (!describeExchangeRateStatus(next)) setUserNotice({ tone: 'status', text: source === 'refresh' ? '汇率刷新已受理' : '汇率状态已读取' })
      }
    }
    return true
  }

  function readStatus() {
    if (submittingRef.current) return
    const wasActive = exchangeRateRefreshActive(itemsRef.current)
    const ticket = issue()
    const signal = beginStatusRequest()
    getSubscriptionExchangeRateStatus(signal)
      .then((snapshot) => {
        if (ticket.generation !== generation.current || signal.aborted) return
        const next = snapshotItems(snapshot)
        const applied = applySnapshot(ticket, next, wasActive ? 'poll' : 'load')
        // Replacing the poll's request leaves that loop stopped. Restart it while work remains.
        if (applied && wasActive && exchangeRateRefreshActive(itemsRef.current)) setPollEpoch((epoch) => epoch + 1)
      })
      .catch((err: unknown) => {
        if (ticket.generation !== generation.current) return
        reportStatusReadFailure(err, signal)
        if (!signal.aborted && !isAbortError(err) && wasActive) setPollEpoch((epoch) => epoch + 1)
      })
  }

  useLayoutEffect(() => {
    readStatusRef.current = readStatus
  })

  useEffect(() => {
    let cancelled = false
    const onWake = () => {
      if (cancelled) return
      if (!pageVisible()) {
        statusAbort.current?.abort()
        return
      }
      if (appliedSeq.current === 0 || statusErrorRef.current != null) readStatusRef.current()
    }
    onWake()
    document.addEventListener('visibilitychange', onWake)
    window.addEventListener('focus', onWake)
    return () => {
      cancelled = true
      statusAbort.current?.abort()
      document.removeEventListener('visibilitychange', onWake)
      window.removeEventListener('focus', onWake)
    }
  }, [])

  const active = exchangeRateRefreshActive(items)
  useEffect(() => {
    if (!active) return
    let cancelled = false
    let timer = 0
    let inFlight = false

    const schedule = (delay: number) => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => { void run() }, delay)
    }

    const run = async () => {
      if (cancelled || inFlight || !pageVisible()) return
      const ticket = issue()
      const signal = beginStatusRequest()
      inFlight = true
      try {
        const snapshot = await getSubscriptionExchangeRateStatus(signal)
        if (!cancelled && !signal.aborted) applySnapshot(ticket, snapshotItems(snapshot), 'poll')
      } catch (err: unknown) {
        if (cancelled || signal.aborted || isAbortError(err)) return
        // Keep the last snapshot (and the cost page) and keep polling.
        statusErrorRef.current = STATUS_READ_ERROR
        setStatusError(STATUS_READ_ERROR)
      } finally {
        inFlight = false
        if (!cancelled) {
          if (signal.aborted) {
            if (pageVisible() && statusAbort.current?.signal === signal) schedule(0)
          } else if (pageVisible()) {
            schedule(EXCHANGE_RATE_POLL_MS)
          }
        }
      }
    }

    const onWake = () => {
      if (!pageVisible()) {
        window.clearTimeout(timer)
        statusAbort.current?.abort()
        return
      }
      if (!inFlight) schedule(0)
    }
    document.addEventListener('visibilitychange', onWake)
    window.addEventListener('focus', onWake)
    schedule(0)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
      statusAbort.current?.abort()
      document.removeEventListener('visibilitychange', onWake)
      window.removeEventListener('focus', onWake)
    }
    // Restarting this effect on every snapshot would overlap polls.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, pollEpoch])

  const derived = describeExchangeRateStatus(items)
  const notice = userNotice?.tone === 'error'
    ? userNotice
    : statusError
      ? { tone: 'error' as const, text: statusError }
      : (derived ?? userNotice)
  const refreshing = submitting || active
  const statusUnavailable = statusError != null && userNotice?.tone !== 'error'

  function refresh() {
    if (refreshing) return
    const ticket = issue()
    submittingRef.current = true
    manualPending.current = true
    statusAbort.current?.abort()
    // Drop the aborted read so its retry cannot start beside this POST.
    statusAbort.current = null
    refreshAbort.current?.abort()
    const controller = new AbortController()
    refreshAbort.current = controller
    setSubmitting(true)
    setUserNotice(null)
    refreshSubscriptionExchangeRates(controller.signal)
      .then((snapshot) => {
        if (ticket.generation !== generation.current || controller.signal.aborted) return
        // Cost reload runs inside the snapshot commit, which can flush before this promise's finally.
        submittingRef.current = false
        applySnapshot(ticket, snapshotItems(snapshot), 'refresh')
      })
      .catch((err: unknown) => {
        if (ticket.generation !== generation.current || isAbortError(err)) return
        manualPending.current = false
        setUserNotice({ tone: 'error', text: describeError(err, '汇率刷新失败') })
      })
      .finally(() => {
        if (ticket.generation !== generation.current || refreshAbort.current !== controller) return
        submittingRef.current = false
        setSubmitting(false)
      })
  }

  const failed = items.some((item) => item.refresh_status === 'failed')
  return {
    notice,
    statusUnavailable,
    refreshing,
    buttonLabel: refreshing ? '补取中…' : failed ? '重试汇率' : '刷新汇率',
    refresh,
    readStatus,
  }
}
