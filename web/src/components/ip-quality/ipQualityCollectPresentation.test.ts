import { describe, expect, it } from 'vitest'

import type { IPQualityCollectRequest, IPQualityCollectStatus } from '../../lib/types'
import {
  collectNotice,
  collectUnavailableAction,
  collectUnavailableLabel,
  isCollectRequestActive,
  safeCollectErrorSummary,
  type CollectNoticeView,
} from './ipQualityCollectPresentation'

function view(patch: Partial<CollectNoticeView> = {}): CollectNoticeView {
  return { ipVersion: 4, viewingHistory: false, reportCollectionID: null, reportCollectRequestID: null, refreshState: 'idle', hasReport: true, ...patch }
}

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
  it('describes an active request with its real family scope, stage and elapsed time', () => {
    expect(collectNotice(status({ status: 'pending', requested_ip_versions: [4, 6] }), null, now, view())).toEqual({
      tone: 'progress',
      title: '正在采集 IPv4 / IPv6',
      detail: '等待 agent 同步 · 已用 1 分 15 秒',
    })
    expect(collectNotice(status({ status: 'dispatched', requested_ip_versions: [6] }), null, now, view())).toMatchObject({
      title: '正在采集 IPv6',
      detail: 'agent 已接收，正在检测 · 已用 1 分 15 秒',
    })
    // 旧 Center 不返回族集合：按双栈描述，不猜测单族。
    expect(collectNotice(status({ status: 'pending' }), null, now, view())?.title).toBe('正在采集 IPv4 / IPv6')
  })

  it('reports a single-family round without claiming the other family report was updated', () => {
    const v6Only = (result: NonNullable<IPQualityCollectRequest['family_results']>[number]) => status({
      status: 'completed',
      report_status: result.report_status,
      requested_ip_versions: [6],
      collection_scope: 'single_family',
      collection_id: 'col_v6',
      family_results: [result],
    })
    const ok = v6Only({ ip_version: 6, availability: 'available', report_status: 'success' })
    // 查看 IPv4 时只采了 IPv6：短状态，不说当前报告已更新、不刷新为“本轮结果”。
    expect(collectNotice(ok, 'ipqc_001', now, view({ ipVersion: 4, reportCollectionID: 'col_old' }))).toEqual({ tone: 'success', title: 'IPv6 采集完成' })
    expect(collectNotice(ok, 'ipqc_001', now, view({ ipVersion: 4, viewingHistory: true }))?.title).toBe('IPv6 采集完成')
    expect(collectNotice(v6Only({ ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' }), 'ipqc_001', now, view({ ipVersion: 4 }))?.title)
      .toBe('本轮未检测到 IPv6 可用本地地址')
    expect(collectNotice(v6Only({ ip_version: 6, availability: 'available', report_status: 'failure', error_code: 'lookup_failed' }), 'ipqc_001', now, view({ ipVersion: 4 })))
      .toEqual({ tone: 'warning', title: 'IPv6 采集失败', detail: 'IPv6 出口探测失败' })
    // 查看 IPv6 时：只有展示报告属于本轮且刷新成功才是“报告已更新”。
    expect(collectNotice(ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_v6' }))?.title).toBe('采集完成，报告已更新')
    expect(collectNotice(ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_old' }))?.title).toBe('本轮采集已结束')
  })

  it('stops on failed requests with an actionable short error for agents without single-family support', () => {
    expect(collectNotice(status({ status: 'failed', requested_ip_versions: [4], error_code: 'unsupported_agent' }), 'ipqc_001', now, view())).toEqual({
      tone: 'warning',
      title: 'agent 版本不支持单独采集 IPv4',
      detail: '升级 agent，或改为采集 IPv4 / IPv6',
    })
    expect(collectNotice(status({ status: 'failed', requested_ip_versions: [6], error_code: 'future_code' }), 'ipqc_001', now, view())?.title).toBe('采集请求失败')
    // 失败请求不是进行中，也不在其他会话里打扰。
    expect(isCollectRequestActive(status({ status: 'failed' }).request)).toBe(false)
    expect(collectNotice(status({ status: 'failed', error_code: 'unsupported_agent' }), null, now, view())).toBeNull()
  })

  const dual = (familyResults: NonNullable<IPQualityCollectRequest['family_results']>, extra: Partial<IPQualityCollectRequest> = {}) => status({
    status: 'completed',
    report_status: 'partial',
    collection_scope: 'dual_stack',
    collection_id: 'col_new',
    family_results: familyResults,
    ...extra,
  })
  const ipv4FailedIPv6Ok = dual([
    { ip_version: 4, availability: 'available', report_status: 'failure', error_code: 'lookup_failed' },
    { ip_version: 6, availability: 'available', report_status: 'success' },
  ], { error_summary: 'ipv4 lookup timeout' })

  it('reports results only for the request watched in this session', () => {
    expect(collectNotice(ipv4FailedIPv6Ok, null, now, view())).toBeNull()
    expect(collectNotice(status({ status: 'expired' }), 'ipqc_001', now, view())?.title).toBe('agent 未在 10 分钟内返回结果')
  })

  it('separates the selected family result from the aggregate round', () => {
    // IPv4 失败 + IPv6 成功：IPv4 视图说失败并保留上次结果，不宣称已更新。
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 4, reportCollectionID: 'col_old' }))).toEqual({
      tone: 'warning',
      title: '本轮 IPv4 采集失败，仍展示上一份有效结果',
      detail: 'IPv4 出口探测失败',
      rawError: 'ipv4 lookup timeout',
    })
    // IPv6 视图只有展示报告属于本轮且刷新成功时才是“报告已更新”。
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_new' })))
      .toEqual({ tone: 'success', title: '采集完成，报告已更新' })
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_old' }))?.title).toBe('本轮采集已结束')
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_new', refreshState: 'refreshing' }))?.tone).toBe('progress')
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_new', refreshState: 'failed' }))?.title).toContain('刷新失败')

    const v6Missing = dual([
      { ip_version: 4, availability: 'available', report_status: 'success' },
      { ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' },
    ], { report_status: 'success' })
    expect(collectNotice(v6Missing, 'ipqc_001', now, view({ ipVersion: 6 }))?.title).toBe('本轮未检测到 IPv6 可用本地地址')
    expect(collectNotice(v6Missing, 'ipqc_001', now, view({ ipVersion: 4, reportCollectionID: 'col_new' }))?.title).toBe('采集完成，报告已更新')
    // partial 的族结果更新了报告，但注明部分来源缺失。
    expect(collectNotice(dual([{ ip_version: 4, availability: 'available', report_status: 'partial' }]), 'ipqc_001', now, view({ reportCollectionID: 'col_new' })))
      .toEqual({ tone: 'success', title: '采集完成，报告已更新', detail: 'IPv4 部分来源未返回结果' })
    // 未回传该族结果时不伪造结论。
    expect(collectNotice(dual([{ ip_version: 4, availability: 'available', report_status: 'success' }]), 'ipqc_001', now, view({ ipVersion: 6, reportCollectionID: 'col_new' }))?.title).toBe('本轮采集已结束')
  })

  it('keeps history views on history and never reports them as updated', () => {
    const notice = collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, view({ ipVersion: 6, viewingHistory: true, reportCollectionID: 'col_new' }))
    expect(notice?.title).toBe('本轮采集已结束')
    expect(notice?.detail).toContain('历史')
  })

  it('claims preserved results only when the selected family actually shows an older report', () => {
    const v6Missing = dual([
      { ip_version: 4, availability: 'available', report_status: 'success' },
      { ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' },
    ], { report_status: 'success' })
    // 有旧报告：可以说保留了历史结果。
    expect(collectNotice(v6Missing, 'ipqc_001', now, view({ ipVersion: 6 }))?.detail).toBeDefined()
    // 该族从未有过报告：只说本轮结果，不宣称保留/仍展示上一份结果。
    const noReport = (patch: Partial<CollectNoticeView> = {}) => view({ hasReport: false, ...patch })
    expect(collectNotice(v6Missing, 'ipqc_001', now, noReport({ ipVersion: 6 }))).toEqual({ tone: 'muted', title: '本轮未检测到 IPv6 可用本地地址' })
    const failed = collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, noReport({ ipVersion: 4 }))
    expect(failed?.tone).toBe('warning')
    expect(failed?.title).not.toMatch(/上一份|保留|仍展示/)
    expect(collectNotice(ipv4FailedIPv6Ok, 'ipqc_001', now, noReport({ ipVersion: 6, refreshState: 'failed' }))?.detail).toBeUndefined()
    const legacyFailed = collectNotice(status({ status: 'completed', report_status: 'failure', error_summary: 'timeout' }), 'ipqc_001', now, noReport())
    expect(legacyFailed?.tone).toBe('warning')
    expect(legacyFailed?.title).not.toMatch(/上一份|保留|仍展示/)
  })

  it('labels legacy single-family rounds and matches them by collect request ID', () => {
    const legacy = status({ status: 'completed', report_status: 'success', collection_scope: 'legacy_single_family' })
    expect(collectNotice(legacy, 'ipqc_001', now, view({ reportCollectRequestID: 'ipqc_001' })))
      .toEqual({ tone: 'success', title: '采集完成，报告已更新', detail: '本次仅收到单地址族报告，未验证另一地址族' })
    expect(collectNotice(legacy, 'ipqc_001', now, view({ reportCollectRequestID: 'ipqc_other' }))?.title).toBe('本轮采集已结束')

    // 采集失败：提供安全中文主摘要，原始 error_summary 作为 rawError
    expect(collectNotice(status({ status: 'completed', report_status: 'failure', error_summary: 'timeout' }), 'ipqc_001', now, view())).toEqual({
      tone: 'warning',
      title: '本次采集失败，仍展示上一份有效报告',
      detail: '检测请求超时，未能获取最新数据 · 本次仅收到单地址族报告，未验证另一地址族',
      rawError: 'timeout',
    })
  })

  it('never promotes mixed Chinese/URL error content to primary summary and keeps it folded', () => {
    const raw = 'https://example.com/api/test 发生未授权错误'
    const notice = collectNotice(status({ status: 'completed', report_status: 'failure', error_summary: raw }), 'ipqc_001', now, view())
    expect(notice?.detail).toContain('采集执行异常，未能获取有效报告')
    expect(notice?.detail).not.toContain('https://')
    expect(notice?.rawError).toBe(raw)
  })

  it('provides a valid link for paused monitoring without VPS context', () => {
    const notice = collectNotice(
      status(null, { available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' }),
      null,
      now,
      view(),
    )
    expect(notice?.action).toEqual({ to: '/monitoring/mi_001', label: '前往监控实例' })
  })

  it('explains unavailable collection with actions when appropriate and stays quiet otherwise', () => {
    expect(collectNotice(status(null, { available: false, unavailable_reason: 'agent_not_bound' }), null, now, view(), 'vps_001')).toEqual({
      tone: 'muted',
      title: 'agent 尚未完成绑定',
      action: { to: '/vps/vps_001?workbench=monitoring', label: '前往监控工作台' },
    })
    expect(collectNotice(status(null, { available: false, unavailable_reason: 'disabled' }), null, now, view())).toEqual({
      tone: 'muted',
      title: 'IP 质量采集已在设置中关闭',
      action: { to: '/settings?tab=monitoring', label: '前往设置开启' },
    })
    expect(collectNotice(status(null), null, now, view())).toBeNull()
    expect(collectNotice(null, null, now, view())).toBeNull()
  })

  it('maps reasons and active states', () => {
    expect(collectUnavailableLabel('disabled')).toBe('IP 质量采集已在设置中关闭')
    expect(collectUnavailableLabel('future_reason')).toBe('当前无法立即采集')
    expect(isCollectRequestActive(status({ status: 'dispatched' }).request)).toBe(true)
    expect(isCollectRequestActive(status({ status: 'expired' }).request)).toBe(false)
    expect(isCollectRequestActive(null)).toBe(false)
  })
})
