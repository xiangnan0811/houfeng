import type { IPQualityCollectIPVersions, IPQualityCollectRequest, IPQualityCollectStatus, IPQualityIPVersion } from '../../lib/types'
import { validateReturnVPSId, withReturnVPSQuery } from '../../pages/monitoring-detail/monitoringDetailHelpers'

export type CollectNoticeTone = 'progress' | 'success' | 'warning' | 'muted'

export type CollectNoticeAction = {
  to: string
  label: string
}

export type CollectNotice = {
  tone: CollectNoticeTone
  title: string
  detail?: string
  action?: CollectNoticeAction
  rawError?: string
}

const UNAVAILABLE_LABELS: Record<string, string> = {
  disabled: 'IP 质量采集已在设置中关闭',
  no_monitoring_instance: '该 VPS 尚未接入监控 agent',
  agent_not_bound: 'agent 尚未完成绑定',
  monitoring_paused: '监控已暂停',
  paused: '监控已暂停',
}

export function collectUnavailableLabel(reason?: string): string {
  return UNAVAILABLE_LABELS[(reason ?? '').trim()] ?? '当前无法立即采集'
}

export function collectUnavailableAction(
  status: IPQualityCollectStatus | null,
  vpsId?: string | null,
): CollectNoticeAction | undefined {
  if (!status) return undefined
  const reason = (status.unavailable_reason ?? '').trim()
  if (reason === 'disabled') {
    return { to: '/settings?tab=monitoring', label: '前往设置开启' }
  }
  if (reason === 'no_monitoring_instance' || reason === 'agent_not_bound') {
    const validVpsId = validateReturnVPSId(vpsId)
    if (!validVpsId || validVpsId === '.' || validVpsId === '..') return undefined
    return {
      to: `/vps/${encodeURIComponent(validVpsId)}?workbench=monitoring`,
      label: '前往监控工作台',
    }
  }
  if (reason === 'monitoring_paused' || reason === 'paused') {
    // 暂停动作仅认当前状态中的 monitoring_instance_id，绝不回退至历史 request；vpsId 仅作为可选返回上下文
    const miId = validateReturnVPSId(status.monitoring_instance_id)
    if (!miId || miId === '.' || miId === '..') return undefined
    return {
      to: withReturnVPSQuery(`/monitoring/${encodeURIComponent(miId)}`, vpsId),
      label: '前往监控实例',
    }
  }
  return undefined
}

export function safeCollectErrorSummary(rawError?: string | null): string {
  const trimmed = (rawError ?? '').trim()
  if (!trimmed) {
    return '采集未能完成，请稍后重试'
  }
  const lower = trimmed.toLowerCase()
  if (lower.includes('timeout') || lower.includes('deadline')) {
    return '检测请求超时，未能获取最新数据'
  }
  if (
    lower.includes('connection refused')
    || lower.includes('connect:')
    || lower.includes('dial tcp')
    || lower.includes('network')
  ) {
    return '网络连接失败，未能连接采集源'
  }
  if (lower.includes('non_json') || lower.includes('html') || /pars(?:e|ing)/.test(lower)) {
    return '采集响应格式异常，未能解析有效结果'
  }
  if (lower.includes('429') || lower.includes('rate limit')) {
    return '采集请求被限流，未能获取有效结果'
  }
  if (lower.includes('500') || lower.includes('502') || lower.includes('503') || lower.includes('504')) {
    return '采集源暂时不可用，未能获取有效结果'
  }
  return '采集执行异常，未能获取有效报告'
}

export function isCollectRequestActive(request?: IPQualityCollectRequest | null): boolean {
  return request?.status === 'pending' || request?.status === 'dispatched'
}

export const COLLECT_SCOPE_OPTIONS: ReadonlyArray<{ ipVersions: IPQualityCollectIPVersions, label: string }> = [
  { ipVersions: [4], label: '采集 IPv4' },
  { ipVersions: [6], label: '采集 IPv6' },
  { ipVersions: [4, 6], label: '采集 IPv4 / IPv6' },
]

// 请求的族集合：只认 [4]、[6]、[4,6]；旧 Center 未返回该字段时按双栈处理。
export function requestedIPVersions(request: IPQualityCollectRequest): IPQualityCollectIPVersions {
  const versions = request.requested_ip_versions
  if (Array.isArray(versions) && versions.length === 1 && (versions[0] === 4 || versions[0] === 6)) {
    return versions[0] === 4 ? [4] : [6]
  }
  return [4, 6]
}

export function collectScopeLabel(versions: IPQualityCollectIPVersions): string {
  return versions.length === 2 ? 'IPv4 / IPv6' : `IPv${versions[0]}`
}

// 请求是否覆盖该族：只有覆盖时，完成后才需要刷新该族报告。
export function collectRequestCoversFamily(request: IPQualityCollectRequest, family: IPQualityIPVersion | null): boolean {
  if (family == null) return true
  return (requestedIPVersions(request) as readonly number[]).includes(family)
}

function elapsedSeconds(from: string, now: Date): number | null {
  const started = new Date(from).getTime()
  if (Number.isNaN(started)) return null
  return Math.max(0, Math.round((now.getTime() - started) / 1000))
}

function formatElapsed(seconds: number | null): string {
  if (seconds == null) return ''
  if (seconds < 60) return `${seconds} 秒`
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}

// 当前页面选择：完成提示只对“当前所选族的当前报告”下结论。
export type CollectNoticeView = {
  ipVersion: IPQualityIPVersion | null
  viewingHistory: boolean
  // 当前展示报告 diagnostics 里的 collection_id / collect_request_id；没有则为 null。
  reportCollectionID: string | null
  reportCollectRequestID: string | null
  // idle：最近一次读取成功；refreshing：采集结束后的刷新进行中；failed：刷新失败，仍展示旧报告。
  refreshState: 'idle' | 'refreshing' | 'failed'
  // 当前是否展示所选族的一份真实报告；没有时不得宣称“保留/仍展示上一份结果”。
  hasReport: boolean
}

export const LEGACY_SINGLE_FAMILY_NOTICE = '本次仅收到单地址族报告，未验证另一地址族'

function familyFailureDetail(errorCode: string | undefined, ipVersion: IPQualityIPVersion): string {
  if ((errorCode ?? '').trim() === 'lookup_failed') return `IPv${ipVersion} 出口探测失败`
  return `IPv${ipVersion} 未形成有效报告`
}

// 本轮只采了另一族：给出该族的短结论，不宣称当前展示的报告已更新。
function otherFamilyNotice(request: IPQualityCollectRequest, family: IPQualityIPVersion): CollectNotice {
  const result = request.family_results?.find((item) => item.ip_version === family)
  if (!result) return { tone: 'muted', title: `IPv${family} 采集已结束`, detail: `本轮未收到 IPv${family} 结果` }
  if (result.availability === 'unavailable') return { tone: 'muted', title: `本轮未检测到 IPv${family} 可用本地地址` }
  if (result.report_status !== 'success' && result.report_status !== 'partial') {
    return { tone: 'warning', title: `IPv${family} 采集失败`, detail: familyFailureDetail(result.error_code, family) }
  }
  return { tone: 'success', title: `IPv${family} 采集完成` }
}

// failed 是 Center 判定的终态（例如 agent 不支持单族采集），不会再下发；给可操作的短错误。
function failedNotice(request: IPQualityCollectRequest): CollectNotice {
  const code = (request.error_code ?? '').trim()
  if (code === 'unsupported_agent') {
    return {
      tone: 'warning',
      title: `agent 版本不支持单独采集 ${collectScopeLabel(requestedIPVersions(request))}`,
      detail: '升级 agent，或改为采集 IPv4 / IPv6',
    }
  }
  return { tone: 'warning', title: '采集请求失败', detail: safeCollectErrorSummary(request.error_summary) }
}

// 采集结束 ≠ 当前报告已更新：只有当前族成功、非历史视图、刷新成功且展示报告属于本轮，才说“报告已更新”。
function completedNotice(request: IPQualityCollectRequest, view: CollectNoticeView): CollectNotice {
  const rawError = request.error_summary?.trim() || undefined
  // dual_stack / single_family 按族结果判定；其余（含缺失）为旧 agent 单族报告。
  const legacy = request.collection_scope !== 'dual_stack' && request.collection_scope !== 'single_family'
  const versions = requestedIPVersions(request)
  if (!legacy && view.ipVersion != null && !collectRequestCoversFamily(request, view.ipVersion)) {
    return otherFamilyNotice(request, versions[0])
  }
  if (view.viewingHistory) {
    return {
      tone: 'muted',
      title: '本轮采集已结束',
      detail: legacy ? `仍在查看历史报告 · ${LEGACY_SINGLE_FAMILY_NOTICE}` : '仍在查看历史报告，可切换到最新报告查看本轮结果',
    }
  }

  if (legacy) {
    if (request.report_status === 'failure') {
      return {
        tone: 'warning',
        title: view.hasReport ? '本次采集失败，仍展示上一份有效报告' : '本次采集失败',
        detail: `${safeCollectErrorSummary(rawError)} · ${LEGACY_SINGLE_FAMILY_NOTICE}`,
        ...(rawError ? { rawError } : {}),
      }
    }
    if (view.refreshState === 'refreshing') return { tone: 'progress', title: '本轮采集已结束，正在刷新报告', detail: LEGACY_SINGLE_FAMILY_NOTICE }
    if (view.refreshState === 'failed') {
      return { tone: 'warning', title: '本轮采集已结束，报告刷新失败', detail: view.hasReport ? `仍展示刷新前的报告 · ${LEGACY_SINGLE_FAMILY_NOTICE}` : LEGACY_SINGLE_FAMILY_NOTICE }
    }
    if (view.reportCollectRequestID === request.request_id) {
      return { tone: 'success', title: '采集完成，报告已更新', detail: LEGACY_SINGLE_FAMILY_NOTICE }
    }
    return { tone: 'muted', title: '本轮采集已结束', detail: LEGACY_SINGLE_FAMILY_NOTICE }
  }

  const family = view.ipVersion
  const result = family == null ? undefined : request.family_results?.find((item) => item.ip_version === family)
  if (family == null || !result) {
    return { tone: 'muted', title: '本轮采集已结束', detail: family == null ? '未确定当前地址族' : `本轮未收到 IPv${family} 结果` }
  }
  if (result.availability === 'unavailable') {
    const title = `本轮未检测到 IPv${family} 可用本地地址`
    return view.hasReport ? { tone: 'muted', title, detail: '已保留该地址族的历史结果' } : { tone: 'muted', title }
  }
  if (result.report_status !== 'success' && result.report_status !== 'partial') {
    return {
      tone: 'warning',
      title: view.hasReport ? `本轮 IPv${family} 采集失败，仍展示上一份有效结果` : `本轮 IPv${family} 采集失败`,
      detail: familyFailureDetail(result.error_code, family),
      ...(rawError ? { rawError } : {}),
    }
  }
  if (view.refreshState === 'refreshing') return { tone: 'progress', title: '本轮采集已结束，正在刷新报告' }
  if (view.refreshState === 'failed') {
    return view.hasReport
      ? { tone: 'warning', title: '本轮采集已结束，报告刷新失败', detail: '仍展示刷新前的报告' }
      : { tone: 'warning', title: '本轮采集已结束，报告刷新失败' }
  }
  if (request.collection_id && view.reportCollectionID === request.collection_id) {
    return result.report_status === 'partial'
      ? { tone: 'success', title: '采集完成，报告已更新', detail: `IPv${family} 部分来源未返回结果` }
      : { tone: 'success', title: '采集完成，报告已更新' }
  }
  return { tone: 'muted', title: '本轮采集已结束', detail: '当前展示的报告不是本轮结果' }
}

// collectNotice 只描述本次页面会话中发起或正在进行的请求；历史上已结束的请求不打扰用户。
export function collectNotice(
  status: IPQualityCollectStatus | null,
  watchedRequestId: string | null,
  now: Date,
  view: CollectNoticeView,
  vpsId?: string | null,
): CollectNotice | null {
  const request = status?.request ?? null
  // 采集变为不可用时请求不会再下发，优先说明原因而不是继续显示“正在采集”。
  if (status && !status.available) {
    const action = collectUnavailableAction(status, vpsId)
    return {
      tone: 'muted',
      title: collectUnavailableLabel(status.unavailable_reason),
      ...(action ? { action } : {}),
    }
  }
  if (request && isCollectRequestActive(request)) {
    const elapsed = formatElapsed(elapsedSeconds(request.requested_at, now))
    const stage = request.status === 'dispatched' ? 'agent 已接收，正在检测' : '等待 agent 同步'
    return {
      tone: 'progress',
      title: `正在采集 ${collectScopeLabel(requestedIPVersions(request))}`,
      detail: elapsed ? `${stage} · 已用 ${elapsed}` : stage,
    }
  }
  if (request && watchedRequestId === request.request_id) {
    if (request.status === 'completed') return completedNotice(request, view)
    if (request.status === 'failed') return failedNotice(request)
    if (request.status === 'expired') {
      return {
        tone: 'warning',
        title: 'agent 未在 10 分钟内返回结果',
        detail: 'agent 可能离线，或版本过旧不支持立即采集。',
      }
    }
  }
  return null
}
