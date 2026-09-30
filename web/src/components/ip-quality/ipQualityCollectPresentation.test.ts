import { describe, expect, it } from 'vitest'

import type { IPQualityCollectRequest, IPQualityCollectStatus } from '../../lib/types'
import { collectNotice, collectUnavailableLabel, isCollectRequestActive } from './ipQualityCollectPresentation'

const now = new Date('2026-09-30T08:01:15Z')

function status(request: Partial<IPQualityCollectRequest> | null, extra: Partial<IPQualityCollectStatus> = {}): IPQualityCollectStatus {
  return {
    enabled: true,
    available: true,
    request: request
      ? {
          request_id: 'ipqc_001',
          monitoring_instance_id: 'mi_001',
          status: 'pending',
          requested_at: '2026-09-30T08:00:00Z',
          expires_at: '2026-09-30T08:10:00Z',
          ...request,
        }
      : null,
    ...extra,
  }
}

describe('collectNotice', () => {
  it('describes an active request with its stage and elapsed time', () => {
    expect(collectNotice(status({ status: 'pending' }), null, now)).toEqual({
      tone: 'progress',
      title: '正在采集最新 IP 质量',
      detail: '等待 agent 同步 · 已用 1 分 15 秒',
    })
    expect(collectNotice(status({ status: 'dispatched' }), null, now)?.detail).toBe('agent 已接收，正在检测 · 已用 1 分 15 秒')
  })

  it('reports results only for the request watched in this session', () => {
    const completed = status({ status: 'completed', report_status: 'success' })
    expect(collectNotice(completed, null, now)).toBeNull()
    expect(collectNotice(completed, 'ipqc_001', now)).toEqual({ tone: 'success', title: '采集完成，报告已更新' })
    expect(collectNotice(status({ status: 'completed', report_status: 'failure', error_summary: 'timeout' }), 'ipqc_001', now)).toEqual({
      tone: 'warning',
      title: '本次采集失败，仍展示上一份有效报告',
      detail: 'timeout',
    })
    expect(collectNotice(status({ status: 'expired' }), 'ipqc_001', now)?.title).toBe('agent 未在 10 分钟内返回结果')
  })

  it('explains unavailable collection and stays quiet otherwise', () => {
    expect(collectNotice(status(null, { available: false, unavailable_reason: 'agent_not_bound' }), null, now)).toEqual({
      tone: 'muted',
      title: 'agent 尚未完成绑定',
    })
    expect(collectNotice(status(null), null, now)).toBeNull()
    expect(collectNotice(null, null, now)).toBeNull()
  })

  it('maps reasons and active states', () => {
    expect(collectUnavailableLabel('disabled')).toBe('IP 质量采集已在设置中关闭')
    expect(collectUnavailableLabel('future_reason')).toBe('当前无法立即采集')
    expect(isCollectRequestActive(status({ status: 'dispatched' }).request)).toBe(true)
    expect(isCollectRequestActive(status({ status: 'expired' }).request)).toBe(false)
    expect(isCollectRequestActive(null)).toBe(false)
  })
})
