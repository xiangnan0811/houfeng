import { describe, expect, it } from 'vitest'

import type { IPQualityServiceUnlock } from '../../lib/types'
import { serviceCardDescription, serviceTileDetail } from './ipQualityPresentation'

function unlock(patch: Partial<IPQualityServiceUnlock>): IPQualityServiceUnlock {
  return { service: 'reddit', status: 'unknown', probe_status: 'failure', ...patch } as IPQualityServiceUnlock
}

describe('serviceCardDescription', () => {
  it('turns HTTP probe failures into a short Chinese judgement instead of the raw agent text', () => {
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 403' }))).toBe('服务拒绝了探测请求（HTTP 403）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 401' }))).toBe('服务拒绝了探测请求（HTTP 401）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 404' }))).toBe('探测地址不存在（HTTP 404）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 429' }))).toBe('探测请求被限流（HTTP 429）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 503' }))).toBe('服务暂时不可用（HTTP 503）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: 'http status 418' }))).toBe('服务返回异常状态（HTTP 418）')
    expect(serviceCardDescription(unlock({ error_code: 'http_status', error_summary: '' }))).toBe('服务返回异常状态')
    // 旧数据可能只有 summary 没有 code。
    expect(serviceCardDescription(unlock({ error_summary: 'HTTP status 403' }))).toBe('服务拒绝了探测请求（HTTP 403）')
  })

  it('labels every error code the agent service probes emit, preferring the code over summary text', () => {
    expect(serviceCardDescription(unlock({ error_code: 'timeout', error_summary: 'context deadline exceeded' }))).toBe('探测超时')
    // agent 真实文本里带有 http status 200，也必须按 error_code 归类为响应无法识别。
    expect(serviceCardDescription(unlock({ error_code: 'non_json_response', error_summary: 'non_json_response: http status 200 content-type "text/html"' }))).toBe('服务响应无法识别')
    expect(serviceCardDescription(unlock({ error_code: 'request_failed', error_summary: 'dial tcp: connection refused' }))).toBe('探测请求失败')
    expect(serviceCardDescription(unlock({ error_code: 'read_failed', error_summary: 'unexpected EOF' }))).toBe('读取服务响应失败')
    expect(serviceCardDescription(unlock({ error_code: 'invalid_request', error_summary: 'parse "%%": invalid URL escape' }))).toBe('探测请求无法发出')
    expect(serviceCardDescription(unlock({ error_code: 'probe_failed', error_summary: 'panic recovered' }))).toBe('探测失败，未形成可靠结论')
    expect(serviceCardDescription(unlock({ probe_status: 'skipped', error_code: 'unsupported_service', error_summary: 'service is not supported by default IP quality probes' }))).toBe('默认探测暂不支持该服务')
  })

  it('does not rewrite summaries that merely mention an HTTP status', () => {
    expect(serviceCardDescription(unlock({ error_summary: '上次 http status 403 已恢复，等待复测' }))).toBe('上次 http status 403 已恢复，等待复测')
    expect(serviceCardDescription(unlock({ error_code: 'custom_check', error_summary: 'http status 403' }))).toBe('http status 403')
  })

  it('keeps existing safe summaries, configuration hints and status descriptions', () => {
    expect(serviceCardDescription(unlock({ error_code: 'custom', error_summary: '上游返回空结果' }))).toBe('上游返回空结果')
    expect(serviceCardDescription(unlock({ probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without optional service configuration' }))).toBe('默认探测暂不支持该服务')
    expect(serviceCardDescription(unlock({ status: 'unlocked', probe_status: 'success', region: 'JP' }))).toBe('区域 JP 可用')
    expect(serviceTileDetail(unlock({ error_code: 'http_status', error_summary: 'http status 403' }))).toBe('服务拒绝了探测请求（HTTP 403）')
    expect(serviceTileDetail(unlock({ status: 'blocked', probe_status: 'success', error_summary: 'http status 403' }))).toBeNull()
  })
})
