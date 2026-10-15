import { useCallback, useEffect, useRef, useState } from 'react'

import { getVPSIPQualityReport } from '../../lib/api'
import type { VPSIPQualityReport } from '../../lib/types'

export const ENRICHMENT_POLL_INTERVAL_MS = 3000

export type IPQualityEnrichmentController = {
  failed: boolean
  retry: () => void
}

type Options = {
  vpsId: string | undefined
  // 仍在补充资料（pending/running）的所选报告 ID；为 null 时不轮询。
  reportId: string | null
  // 所选报告当前的请求身份；切族/切 VPS/历史切换后旧轮询结果不得写回。
  requestKey: string | null
  onReport: (requestKey: string, report: VPSIPQualityReport) => void
}

function documentVisible(): boolean {
  return typeof document === 'undefined' || document.visibilityState !== 'hidden'
}

// 只对同一份所选报告在页面可见时每 3 秒读取一次 detail；终态、卸载或选择变化即停止。
// 失败后停止并暴露一次手动重试，不做无界后台重试。
export function useIPQualityEnrichment({ vpsId, reportId, requestKey, onReport }: Options): IPQualityEnrichmentController {
  const [failure, setFailure] = useState<string | null>(null)
  const [retries, setRetries] = useState(0)
  const onReportRef = useRef(onReport)
  // 手动重试后的首轮立即读取；其余情况等待一个间隔。
  const immediateRef = useRef(false)
  const target = vpsId && reportId && requestKey ? `${requestKey}|${reportId}` : null
  const failed = failure != null && failure === target

  useEffect(() => {
    onReportRef.current = onReport
  }, [onReport])

  useEffect(() => {
    if (!vpsId || !reportId || !requestKey || failed) return
    let cancelled = false
    let timer: number | undefined
    let inFlight = false
    const key = `${requestKey}|${reportId}`

    const schedule = () => {
      if (cancelled || timer !== undefined || inFlight || !documentVisible()) return
      timer = window.setTimeout(poll, ENRICHMENT_POLL_INTERVAL_MS)
    }
    const poll = () => {
      timer = undefined
      if (cancelled || inFlight || !documentVisible()) return
      inFlight = true
      getVPSIPQualityReport(vpsId, reportId)
        .then((report) => {
          if (cancelled) return
          inFlight = false
          onReportRef.current(requestKey, report)
          // 若仍未终结，父组件保持同一 reportId，本 effect 继续排下一轮；终结后 reportId 变为 null 并清理。
          schedule()
        })
        .catch(() => {
          if (cancelled) return
          inFlight = false
          setFailure(key)
        })
    }
    const onVisibility = () => {
      if (documentVisible()) {
        schedule()
        return
      }
      clearTimeout(timer)
      timer = undefined
    }

    if (immediateRef.current) {
      immediateRef.current = false
      poll()
    } else {
      schedule()
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      cancelled = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [vpsId, reportId, requestKey, failed, retries])

  const retry = useCallback(() => {
    immediateRef.current = true
    setFailure(null)
    setRetries((value) => value + 1)
  }, [])

  return { failed, retry }
}
