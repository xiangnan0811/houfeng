import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, getVPSIPQualityCollectStatus, requestVPSIPQualityCollect } from '../../lib/api'
import type { IPQualityCollectRequest, IPQualityCollectStatus } from '../../lib/types'
import { isCollectRequestActive } from './ipQualityCollectPresentation'

export const COLLECT_POLL_INTERVAL_MS = 3000

export type IPQualityCollectController = {
  status: IPQualityCollectStatus | null
  // 本次页面会话发起或接手的请求；只有它结束时才提示结果并刷新报告。
  watchedRequestId: string | null
  submitting: boolean
  error: string | null
  active: boolean
  start: () => void
}

type State = {
  vpsId: string | null
  status: IPQualityCollectStatus | null
  watchedRequestId: string | null
  submitting: boolean
  error: string | null
  // 轮询失败时递增，驱动下一轮轮询。
  pollAttempt: number
}

function initialState(vpsId: string | null): State {
  return { vpsId, status: null, watchedRequestId: null, submitting: false, error: null, pollAttempt: 0 }
}

// 409 的具体原因由随后读取的状态给出，这里只处理其他失败。
function describeCollectError(error: unknown): string | null {
  if (error instanceof ApiError && error.status === 409) return null
  return '发起采集失败，请稍后重试'
}

type Ticket = { generation: number, sequence: number }

function isCurrentTicket(session: { generation: number }, ticket: Ticket | null): ticket is Ticket {
  return ticket != null && ticket.generation === session.generation
}

function justCompleted(prev: IPQualityCollectRequest | null, next: IPQualityCollectRequest | null): boolean {
  return Boolean(
    prev && next
    && prev.request_id === next.request_id
    && isCollectRequestActive(prev)
    && next.status === 'completed',
  )
}

// enabled 为 false 时（报告尚未加载成功）不读取采集状态，避免错误页发出无用请求。
export function useIPQualityCollect(vpsId: string | undefined, onCompleted: () => void, enabled = true): IPQualityCollectController {
  const key = vpsId ?? null
  const [state, setState] = useState<State>(() => initialState(key))
  const onCompletedRef = useRef(onCompleted)
  const lastRequestRef = useRef<IPQualityCollectRequest | null>(null)
  // 每次切换 VPS 开启新的会话代次；状态请求的序号全局单调，不随切换归零。
  // 回调只在代次一致且序号不早于已应用结果时生效：迟到的旧 GET 不能覆盖 POST，
  // 离开后又回到同一台 VPS 时，上一次访问的回调也不能污染本次会话。
  const sessionRef = useRef({ key, generation: 0, issued: 0, applied: 0 })

  useEffect(() => {
    onCompletedRef.current = onCompleted
  }, [onCompleted])

  // 必须先于下面发请求的 effect 执行，保证本次提交里发出的请求拿到新代次。
  useEffect(() => {
    const session = sessionRef.current
    if (session.key !== key) {
      sessionRef.current = { key, generation: session.generation + 1, issued: session.issued, applied: 0 }
    }
  }, [key])

  // vpsId 变化时丢弃上一台 VPS 的状态（渲染期重置，避免在 effect 里同步 setState）。
  const current = state.vpsId === key ? state : initialState(key)
  if (current !== state) setState(current)

  const issueTicket = useCallback((): Ticket | null => {
    const session = sessionRef.current
    if (session.key !== key) return null
    session.issued += 1
    return { generation: session.generation, sequence: session.issued }
  }, [key])

  const applyStatus = useCallback((next: IPQualityCollectStatus, requestedByUser: boolean, ticket: Ticket | null) => {
    const session = sessionRef.current
    if (!isCurrentTicket(session, ticket) || ticket.sequence < session.applied) return
    session.applied = ticket.sequence
    setState((prev) => {
      if (prev.vpsId !== key) return prev
      const request = next.request ?? null
      const watch = request && (requestedByUser || isCollectRequestActive(request))
      return {
        ...prev,
        status: next,
        watchedRequestId: watch ? request.request_id : prev.watchedRequestId,
        // 提交中状态只由 POST 自己结束，普通 GET 不能提前解除。
        submitting: requestedByUser ? false : prev.submitting,
        error: requestedByUser ? null : prev.error,
      }
    })
  }, [key])

  useEffect(() => {
    if (!key || !enabled) return
    let cancelled = false
    const ticket = issueTicket()
    getVPSIPQualityCollectStatus(key)
      .then((next) => {
        if (!cancelled) applyStatus(next, false, ticket)
      })
      .catch(() => {
        // 状态读取失败不影响报告展示；按钮保持可用，点击时由 POST 给出明确错误。
      })
    return () => { cancelled = true }
  }, [key, enabled, applyStatus, issueTicket])

  const request = current.status?.request ?? null
  // 采集变为不可用（关闭/暂停）时请求不会再下发，不再当作进行中轮询。
  const active = isCollectRequestActive(request) && current.status?.available !== false

  // 每次拿到新状态（或轮询失败）后安排下一轮，直到请求结束。
  useEffect(() => {
    if (!key || !active) return
    let cancelled = false
    const timer = window.setTimeout(() => {
      const ticket = issueTicket()
      getVPSIPQualityCollectStatus(key)
        .then((next) => {
          if (!cancelled) applyStatus(next, false, ticket)
        })
        .catch(() => {
          if (!cancelled) {
            setState((prev) => (prev.vpsId === key ? { ...prev, pollAttempt: prev.pollAttempt + 1 } : prev))
          }
        })
    }, COLLECT_POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [key, active, current.status, current.pollAttempt, applyStatus, issueTicket])

  useEffect(() => {
    const previous = lastRequestRef.current
    lastRequestRef.current = request
    if (request && current.watchedRequestId === request.request_id && justCompleted(previous, request)) {
      onCompletedRef.current()
    }
  }, [request, current.watchedRequestId])

  const busy = current.submitting || active
  const start = useCallback(() => {
    if (!key || busy) return
    setState((prev) => ({ ...prev, submitting: true, error: null }))
    const ticket = issueTicket()
    requestVPSIPQualityCollect(key)
      .then((next) => {
        if (!isCurrentTicket(sessionRef.current, ticket)) return
        // POST 结果即使被更新的 GET 取代，也要结束提交中状态。
        setState((prev) => (prev.vpsId === key ? { ...prev, submitting: false } : prev))
        applyStatus(next, true, ticket)
      })
      .catch((error: unknown) => {
        if (!isCurrentTicket(sessionRef.current, ticket)) return
        setState((prev) => (prev.vpsId === key ? { ...prev, submitting: false, error: describeCollectError(error) } : prev))
        const refreshTicket = issueTicket()
        getVPSIPQualityCollectStatus(key)
          .then((next) => applyStatus(next, false, refreshTicket))
          .catch(() => undefined)
      })
  }, [key, busy, applyStatus, issueTicket])

  return {
    status: current.status,
    watchedRequestId: current.watchedRequestId,
    submitting: current.submitting,
    error: current.error,
    active,
    start,
  }
}
