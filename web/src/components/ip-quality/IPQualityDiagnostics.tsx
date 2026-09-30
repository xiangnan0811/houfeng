import { useId } from 'react'

import { ScrollRegion, Timestamp } from '../atoms'
import { formatLatency, formatOptional } from '../../lib/format'
import type { IPQualityProviderResult, IPQualityServiceUnlock, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityJSONBlock } from './IPQualityJSONBlock'
import { serviceLabel } from './ipQualityPresentation'

type IPQualityDiagnosticsProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
}

const SOURCE_STATUS_LABELS: Record<string, string> = {
  success: '成功',
  failure: '失败',
  skipped: '跳过',
  not_configured: '未配置',
}

const REPORT_STATUS_LABELS: Record<string, string> = {
  success: '采集成功',
  partial: '部分采集',
  failure: '采集失败',
}

function sourceStatusLabel(value?: string): string {
  const normalized = (value ?? 'success').trim().toLowerCase()
  return SOURCE_STATUS_LABELS[normalized] ?? normalized
}

function reportStatusLabel(value?: string): string {
  const normalized = (value ?? '').trim().toLowerCase()
  return REPORT_STATUS_LABELS[normalized] ?? (value || '未知')
}

function assignmentModeLabel(mode?: string, ambiguous?: boolean): string {
  const normalized = (mode ?? '').trim().toLowerCase()
  if (normalized === 'link' || normalized === 'monitoring_link') return '监控关联'
  if (normalized === 'ip_match' || normalized === 'ip' || normalized === 'ipv4' || normalized === 'ipv6') return ambiguous ? '出口 IP 匹配（多台 VPS）' : '出口 IP 匹配'
  return normalized || '—'
}

function errorText(code?: string, summary?: string): string {
  return [code, summary].map((value) => (value ?? '').trim()).filter(Boolean).join(' · ') || '—'
}

function extraJSONMap<T extends { extra_json?: unknown }>(rows: T[], name: (row: T) => string): Record<string, unknown> | null {
  const entries = rows.filter((row) => row.extra_json != null).map((row) => [name(row), row.extra_json] as const)
  return entries.length > 0 ? Object.fromEntries(entries) : null
}

function providerKey(result: IPQualityProviderResult): string {
  return `${result.provider}:${result.source_type ?? ''}:${result.status ?? ''}`
}

function serviceKey(unlock: IPQualityServiceUnlock): string {
  return `${unlock.service}:${unlock.source ?? ''}`
}

// 采集诊断默认折叠：只服务排障，不参与上方结论。
export function IPQualityDiagnostics({ report, summary }: IPQualityDiagnosticsProps) {
  const idPrefix = useId()
  const latest = report.latest_report ?? null
  const providers = report.provider_results
  const services = report.service_unlocks
  const failedSources = providers.filter((row) => sourceStatusLabel(row.status) !== '成功').length
  const coordinates = latest?.latitude != null && latest?.longitude != null ? `${latest.latitude}, ${latest.longitude}` : '—'
  const providerExtra = extraJSONMap(providers, (row) => row.provider)
  const serviceExtra = extraJSONMap(services, (row) => row.service)
  const providerTitleId = `${idPrefix}-providers`
  const serviceTitleId = `${idPrefix}-services`

  return (
    <section className="vps-detail-workspace__section ipq-diagnostics" aria-label="采集诊断">
      <details className="ipq-diagnostics__fold">
        <summary className="ipq-diagnostics__summary">
          <h2>采集诊断</h2>
          <span className="ipq-diagnostics__hint">
            {providers.length} 个来源 · {services.length} 项服务探测{failedSources > 0 ? ` · ${failedSources} 个来源未成功` : ''}
          </span>
        </summary>
        <div className="ipq-diagnostics__body">
          <dl className="ipq-facts">
            <div><dt>报告标识</dt><dd className="mono">{latest?.report_id || summary.report_id || '—'}</dd></div>
            <div><dt>Agent 版本</dt><dd className="mono">{formatOptional(latest?.agent_version)}</dd></div>
            <div><dt>采集状态</dt><dd>{reportStatusLabel(latest?.status || summary.status)}</dd></div>
            <div><dt>归属方式</dt><dd>{assignmentModeLabel(summary.assignment_mode, summary.ambiguous)}</dd></div>
            <div><dt>接收时间</dt><dd><Timestamp value={latest?.received_at} /></dd></div>
            <div><dt>坐标</dt><dd className="mono">{coordinates}</dd></div>
            <div><dt>数据来源</dt><dd>{latest?.is_backfilled ? '离线补传' : '实时上报'}</dd></div>
            {latest?.error_summary || latest?.error_code ? (
              <div className="ipq-facts__wide"><dt>错误</dt><dd>{errorText(latest.error_code, latest.error_summary)}</dd></div>
            ) : null}
          </dl>

          {providers.length > 0 ? (
            <div className="ipq-diagnostics__group">
              <h3 id={providerTitleId}>来源明细</h3>
              <ScrollRegion
                className="ipq-table-scroll"
                labelledBy={providerTitleId}
                hintId={`${providerTitleId}-hint`}
                hintClassName="ipq-table-hint"
                hint="横向滚动查看完整列"
              >
                <table className="data-table data-table--compact ipq-diagnostics__table">
                  <thead className="data-table__head">
                    <tr><th>来源</th><th>类型</th><th>状态</th><th>耗时</th><th>错误</th></tr>
                  </thead>
                  <tbody>
                    {providers.map((row) => (
                      <tr key={providerKey(row)} className="data-table__row">
                        <td className="data-table__cell">{row.provider}</td>
                        <td className="data-table__cell mono">{row.source_type || 'default'}</td>
                        <td className="data-table__cell">{sourceStatusLabel(row.status)}</td>
                        <td className="data-table__cell mono">{row.latency_ms != null ? formatLatency(row.latency_ms) : '—'}</td>
                        <td className="data-table__cell ipq-diagnostics__error">{errorText(row.error_code, row.error_summary)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </ScrollRegion>
            </div>
          ) : null}

          {services.length > 0 ? (
            <div className="ipq-diagnostics__group">
              <h3 id={serviceTitleId}>服务探测</h3>
              <ScrollRegion
                className="ipq-table-scroll"
                labelledBy={serviceTitleId}
                hintId={`${serviceTitleId}-hint`}
                hintClassName="ipq-table-hint"
                hint="横向滚动查看完整列"
              >
                <table className="data-table data-table--compact ipq-diagnostics__table">
                  <thead className="data-table__head">
                    <tr><th>服务</th><th>探测方式</th><th>状态</th><th>耗时</th><th>错误</th></tr>
                  </thead>
                  <tbody>
                    {services.map((row) => (
                      <tr key={serviceKey(row)} className="data-table__row">
                        <td className="data-table__cell">{serviceLabel(row.service)}</td>
                        <td className="data-table__cell mono">{row.source || '—'}</td>
                        <td className="data-table__cell">{sourceStatusLabel(row.probe_status)}</td>
                        <td className="data-table__cell mono">{row.latency_ms != null ? formatLatency(row.latency_ms) : '—'}</td>
                        <td className="data-table__cell ipq-diagnostics__error">{errorText(row.error_code, row.error_summary)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </ScrollRegion>
            </div>
          ) : null}

          <div className="ipq-diagnostics__group">
            <h3>原始数据</h3>
            <div className="ipq-diagnostics__json">
              {latest?.diagnostics_json != null ? <IPQualityJSONBlock title="采集诊断 JSON" value={latest.diagnostics_json} /> : null}
              {latest?.raw_json != null ? <IPQualityJSONBlock title="原始报告 JSON" value={latest.raw_json} /> : null}
              {providerExtra ? <IPQualityJSONBlock title="来源附加数据" value={providerExtra} /> : null}
              {serviceExtra ? <IPQualityJSONBlock title="服务附加数据" value={serviceExtra} /> : null}
              {latest?.diagnostics_json == null && latest?.raw_json == null && !providerExtra && !serviceExtra ? (
                <p className="ipq-empty">本报告没有原始数据。</p>
              ) : null}
            </div>
          </div>
        </div>
      </details>
    </section>
  )
}
