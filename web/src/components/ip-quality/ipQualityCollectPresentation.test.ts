import { describe, expect, it } from 'vitest'

import type { IPQualityCollectRequest, IPQualityCollectStatus } from '../../lib/types'
import {
  collectNotice,
  collectUnavailableAction,
  collectUnavailableLabel,
  isCollectRequestActive,
  safeCollectErrorSummary,
} from './ipQualityCollectPresentation'

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

describe('safeCollectErrorSummary', () => {
  it('translates timeouts and deadline errors to safe Chinese summary', () => {
    expect(safeCollectErrorSummary('lookup timeout')).toBe('检测请求超时，未能获取最新数据')
    expect(safeCollectErrorSummary('context deadline exceeded')).toBe('检测请求超时，未能获取最新数据')
  })

  it('translates connection and network errors to safe Chinese summary', () => {
    expect(safeCollectErrorSummary('dial tcp 1.2.3.4:443: connection refused')).toBe('网络连接失败，未能连接采集源')
    expect(safeCollectErrorSummary('connect: network unreachable')).toBe('网络连接失败，未能连接采集源')
  })

  it('translates non-JSON and parse errors to safe Chinese summary', () => {
    expect(safeCollectErrorSummary('non_json_response: http status 200 content-type text/html')).toBe('采集响应格式异常，未能解析有效结果')
    expect(safeCollectErrorSummary('unexpected EOF while parsing JSON')).toBe('采集响应格式异常，未能解析有效结果')
  })

  it('translates rate-limiting and server errors to safe Chinese summary', () => {
    expect(safeCollectErrorSummary('http status 429: rate limit exceeded')).toBe('采集请求被限流，未能获取有效结果')
    expect(safeCollectErrorSummary('http status 503 service unavailable')).toBe('采集源暂时不可用，未能获取有效结果')
  })

  it('never promotes raw or mixed Chinese/URL error content to primary summary and keeps it folded', () => {
    // 任何语言或混杂 URL 的原始内容均不作为主摘要，必须折叠在 details 中
    expect(safeCollectErrorSummary('https://example.com/api/test 发生未授权错误')).toBe('采集执行异常，未能获取有效报告')
    expect(safeCollectErrorSummary('上游服务拒绝访问：500 Internal Error')).toBe('采集源暂时不可用，未能获取有效结果')
    expect(safeCollectErrorSummary('纯中文未知异常内容')).toBe('采集执行异常，未能获取有效报告')
    expect(safeCollectErrorSummary('<!DOCTYPE html><script>alert(1)</script>')).toBe('采集响应格式异常，未能解析有效结果')
  })

  it('handles empty and unknown errors safely', () => {
    expect(safeCollectErrorSummary('')).toBe('采集未能完成，请稍后重试')
    expect(safeCollectErrorSummary(null)).toBe('采集未能完成，请稍后重试')
    expect(safeCollectErrorSummary('internal_unknown_code_99')).toBe('采集执行异常，未能获取有效报告')
  })
})

describe('collectUnavailableAction', () => {
  it('directs disabled collection to settings', () => {
    const action = collectUnavailableAction({ enabled: false, available: false, unavailable_reason: 'disabled' }, 'vps_001')
    expect(action).toEqual({ to: '/settings?tab=monitoring', label: '前往设置开启' })
    expect(collectUnavailableAction({ enabled: false, available: false, unavailable_reason: 'disabled' }, null)).toEqual({
      to: '/settings?tab=monitoring',
      label: '前往设置开启',
    })
  })

  it('directs unlinked or unbound agent to VPS monitoring workbench when vpsId is present', () => {
    const actionUnlinked = collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' }, 'vps_001')
    expect(actionUnlinked).toEqual({ to: '/vps/vps_001?workbench=monitoring', label: '前往监控工作台' })

    const actionUnbound = collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'agent_not_bound' }, 'vps_tokyo')
    expect(actionUnbound).toEqual({ to: '/vps/vps_tokyo?workbench=monitoring', label: '前往监控工作台' })
  })

  it('refuses to guess a workbench link when vpsId is missing or invalid', () => {
    expect(collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' }, null)).toBeUndefined()
    expect(collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' }, '')).toBeUndefined()
    expect(collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' }, '..')).toBeUndefined()
    expect(collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'agent_not_bound' }, 'invalid/id')).toBeUndefined()
  })

  it('directs paused monitoring to current MI with optional valid returnVPS', () => {
    // 带有有效 vpsId：附加 return_vps
    const actionWithVPS = collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' },
      'vps_001',
    )
    expect(actionWithVPS).toEqual({ to: '/monitoring/mi_001?return_vps=vps_001', label: '前往监控实例' })

    // 未提供 vpsId 或 vpsId 无效：仍然生成前往当前 MI 的有效链接，不带 return_vps
    const actionWithoutVPS = collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' },
      null,
    )
    expect(actionWithoutVPS).toEqual({ to: '/monitoring/mi_001', label: '前往监控实例' })

    const actionInvalidVPS = collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' },
      '//evil.com',
    )
    expect(actionInvalidVPS).toEqual({ to: '/monitoring/mi_001', label: '前往监控实例' })

    const actionAlias = collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'paused', monitoring_instance_id: 'mi_002' },
      'vps_002',
    )
    expect(actionAlias).toEqual({ to: '/monitoring/mi_002?return_vps=vps_002', label: '前往监控实例' })
  })

  it('refuses to fallback to historical request monitoring_instance_id and only yields none', () => {
    // 当前状态缺少 monitoring_instance_id，即使历史 request 中存在，也绝不回退
    expect(collectUnavailableAction(
      {
        enabled: true,
        available: false,
        unavailable_reason: 'monitoring_paused',
        request: {
          request_id: 'ipqc_stale',
          monitoring_instance_id: 'mi_stale',
          status: 'completed',
          requested_at: '2026-09-30T08:00:00Z',
          expires_at: '2026-09-30T08:10:00Z',
        },
      },
      'vps_001',
    )).toBeUndefined()
  })

  it('refuses to guess a monitoring link when current monitoring instance ID is missing or invalid', () => {
    // 缺少 monitoring_instance_id：不猜测 /monitoring
    expect(collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'monitoring_paused' },
      'vps_001',
    )).toBeUndefined()

    expect(collectUnavailableAction(
      { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: '..' },
      'vps_001',
    )).toBeUndefined()
  })

  it('returns undefined for unknown reasons', () => {
    expect(collectUnavailableAction({ enabled: true, available: false, unavailable_reason: 'future_reason' }, 'vps_001')).toBeUndefined()
    expect(collectUnavailableAction(null, 'vps_001')).toBeUndefined()
  })
})

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

    // 采集失败：提供安全中文主摘要，原始 error_summary 作为 rawError
    expect(collectNotice(status({ status: 'completed', report_status: 'failure', error_summary: 'timeout' }), 'ipqc_001', now)).toEqual({
      tone: 'warning',
      title: '本次采集失败，仍展示上一份有效报告',
      detail: '检测请求超时，未能获取最新数据',
      rawError: 'timeout',
    })
    expect(collectNotice(status({ status: 'completed', report_status: 'failure' }), 'ipqc_001', now)).toEqual({
      tone: 'warning',
      title: '本次采集失败，仍展示上一份有效报告',
      detail: '采集未能完成，请稍后重试',
      rawError: undefined,
    })
    expect(collectNotice(status({ status: 'expired' }), 'ipqc_001', now)?.title).toBe('agent 未在 10 分钟内返回结果')
  })

  it('never promotes mixed Chinese/URL error content to primary summary and keeps it folded', () => {
    const raw = 'https://example.com/api/test 发生未授权错误'
    const notice = collectNotice(
      status({ status: 'completed', report_status: 'failure', error_summary: raw }),
      'ipqc_001',
      now,
    )
    expect(notice).toEqual({
      tone: 'warning',
      title: '本次采集失败，仍展示上一份有效报告',
      detail: '采集执行异常，未能获取有效报告',
      rawError: raw,
    })
  })

  it('provides a valid link for paused monitoring without VPS context', () => {
    const notice = collectNotice(
      status(null, { available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' }),
      null,
      now,
    )
    expect(notice?.action).toEqual({ to: '/monitoring/mi_001', label: '前往监控实例' })
  })

  it('explains unavailable collection with actions when appropriate and stays quiet otherwise', () => {
    expect(collectNotice(status(null, { available: false, unavailable_reason: 'agent_not_bound' }), null, now, 'vps_001')).toEqual({
      tone: 'muted',
      title: 'agent 尚未完成绑定',
      action: { to: '/vps/vps_001?workbench=monitoring', label: '前往监控工作台' },
    })
    expect(collectNotice(status(null, { available: false, unavailable_reason: 'disabled' }), null, now)).toEqual({
      tone: 'muted',
      title: 'IP 质量采集已在设置中关闭',
      action: { to: '/settings?tab=monitoring', label: '前往设置开启' },
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
