import type { IPQualityCollectRequest, IPQualityCollectStatus } from '../../lib/types'
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

// collectNotice 只描述本次页面会话中发起或正在进行的请求；历史上已结束的请求不打扰用户。
export function collectNotice(
  status: IPQualityCollectStatus | null,
  watchedRequestId: string | null,
  now: Date,
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
      title: '正在采集最新 IP 质量',
      detail: elapsed ? `${stage} · 已用 ${elapsed}` : stage,
    }
  }
  if (request && watchedRequestId === request.request_id) {
    if (request.status === 'completed') {
      if (request.report_status === 'failure') {
        const rawError = request.error_summary?.trim() || undefined
        return {
          tone: 'warning',
          title: '本次采集失败，仍展示上一份有效报告',
          detail: safeCollectErrorSummary(rawError),
          ...(rawError ? { rawError } : {}),
        }
      }
      return { tone: 'success', title: '采集完成，报告已更新' }
    }
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
