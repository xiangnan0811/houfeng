import { Fragment, useState } from 'react'

import { Badge, Button, ScrollRegion } from '../atoms'
import type { IPQualityMailResult, IPQualityProductView } from '../../lib/types'
import { mailErrorLabel, mailProviderLabel, mailStatus } from './ipQualityProductPresentation'

type IPQualityMailTableProps = {
  product: IPQualityProductView | null
}

const TITLE_ID = 'ip-quality-mail-title'

function MailDetail({ row }: { row: IPQualityMailResult }) {
  const error = mailErrorLabel(row.error_code)
  const facts = [
    row.mx_host ? { label: 'MX', value: row.mx_preference != null ? `${row.mx_host}（优先级 ${row.mx_preference}）` : row.mx_host } : null,
    row.target_ip ? { label: '目标地址', value: row.target_ip } : null,
    row.greeting_code != null ? { label: '响应码', value: String(row.greeting_code) } : null,
    error ? { label: '原因', value: error } : null,
  ].filter((fact): fact is { label: string, value: string } => fact != null)
  return (
    <dl className="ipq-inline-facts">
      {facts.length > 0
        ? facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd className="mono">{fact.value}</dd></div>)
        : <div><dt>MX</dt><dd>—</dd></div>}
    </dl>
  )
}

// 邮件连通性：只读取 SMTP 首个问候，不发信；220 不代表送达或信誉。
export function IPQualityMailTable({ product }: IPQualityMailTableProps) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const rows = product?.mail ?? []
  const reachable = rows.filter((row) => row.status === 'reachable').length

  return (
    <section className="vps-detail-workspace__section ipq-mail" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>邮件连通性</h2>
        {rows.length > 0 ? <span className="ipq-muted">可连接 {reachable}/{rows.length}</span> : null}
      </div>
      {rows.length > 0 ? (
        <ScrollRegion
          className="ipq-table-scroll"
          labelledBy={TITLE_ID}
          hintId="ip-quality-mail-hint"
          hintClassName="ipq-table-hint"
          hint="横向滚动查看完整列"
        >
          <table className="data-table data-table--compact ipq-mail-table">
            <thead className="data-table__head">
              <tr><th>邮局</th><th>端口</th><th>结果</th><th>延迟</th><th><span className="visually-hidden">详情</span></th></tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const status = mailStatus(row.status)
                const key = `${row.provider}:${row.domain}`
                const expanded = open[key] === true
                return (
                  <Fragment key={key}>
                    <tr className="data-table__row">
                      <td className="data-table__cell">
                        <span className="ipq-source">
                          <span className="ipq-source__name">{mailProviderLabel(row.provider)}</span>
                          <span className="ipq-source__method mono">{row.domain}</span>
                        </span>
                      </td>
                      <td className="data-table__cell mono">25</td>
                      <td className="data-table__cell"><Badge variant="info" tone={status.tone}>{status.label}</Badge></td>
                      <td className="data-table__cell mono">{row.latency_ms != null ? `${row.latency_ms} ms` : '—'}</td>
                      <td className="data-table__cell ipq-mail-table__toggle">
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-expanded={expanded}
                          aria-label={`${mailProviderLabel(row.provider)} MX 详情`}
                          onClick={() => setOpen((current) => ({ ...current, [key]: !expanded }))}
                        >
                          {expanded ? '收起' : 'MX'}
                        </Button>
                      </td>
                    </tr>
                    {expanded ? (
                      <tr className="data-table__row ipq-row-detail">
                        <td className="data-table__cell" colSpan={5}><MailDetail row={row} /></td>
                      </tr>
                    ) : null}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </ScrollRegion>
      ) : (
        <p className="ipq-empty">{product ? '本报告未检测邮件连通性。' : '旧版报告无此项。'}</p>
      )}
    </section>
  )
}
