import { Fragment, useState } from 'react'

import { Badge, Button, ScrollRegion, Timestamp } from '../atoms'
import type { IPQualityBlacklistResult, IPQualityProductView } from '../../lib/types'
import { probeErrorLabel } from './ipQualityPresentation'
import {
  blacklistCategoryLabel,
  blacklistSourceLabel,
  blacklistStatus,
  enrichmentGap,
  safeReferenceURL,
} from './ipQualityProductPresentation'

type IPQualityBlacklistTableProps = {
  product: IPQualityProductView | null
}

const TITLE_ID = 'ip-quality-blacklist-title'

function BlacklistDetail({ row }: { row: IPQualityBlacklistResult }) {
  const reference = safeReferenceURL(row.reference_url)
  return (
    <dl className="ipq-inline-facts">
      <div><dt>方式</dt><dd>{row.query_method === 'dataset' ? '数据集' : 'DNSBL'}</dd></div>
      <div><dt>查询时间</dt><dd><Timestamp value={row.observed_at} /></dd></div>
      {row.dataset_at ? <div><dt>数据版本</dt><dd><Timestamp value={row.dataset_at} /></dd></div> : null}
      {row.codes.length > 0 ? <div><dt>返回码</dt><dd className="mono">{row.codes.join(', ')}</dd></div> : null}
      {row.error_code ? <div><dt>原因</dt><dd>{probeErrorLabel(row.error_code) ?? row.error_code}</dd></div> : null}
      {reference ? <div><dt>说明</dt><dd><a className="text-link" href={reference} target="_blank" rel="noreferrer noopener">来源页面</a></dd></div> : null}
    </dl>
  )
}

// 黑名单：只有实际已知的 listed/not_listed 计入已查；未知不算正常。
export function IPQualityBlacklistTable({ product }: IPQualityBlacklistTableProps) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const rows = product?.blacklists ?? []

  return (
    <section className="vps-detail-workspace__section ipq-blacklist" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>黑名单</h2>
        {product && rows.length > 0 ? (
          <span className={product.summary.blacklist_listed_count > 0 ? 'ipq-count ipq-count--alert' : 'ipq-count'}>
            命中 {product.summary.blacklist_listed_count} / 已查 {product.summary.blacklist_checked_count}
          </span>
        ) : null}
      </div>
      {rows.length > 0 ? (
        <ScrollRegion
          className="ipq-table-scroll"
          labelledBy={TITLE_ID}
          hintId="ip-quality-blacklist-hint"
          hintClassName="ipq-table-hint"
          hint="横向滚动查看完整列"
        >
          <table className="data-table data-table--compact ipq-blacklist-table">
            <thead className="data-table__head">
              <tr><th scope="col">来源</th><th scope="col">类别</th><th scope="col">结果</th><th scope="col">匹配范围</th><th scope="col"><span className="visually-hidden">详情</span></th></tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const status = blacklistStatus(row.status)
                const key = `${row.source}:${row.query_method}`
                const expanded = open[key] === true
                return (
                  <Fragment key={key}>
                    <tr className="data-table__row">
                      <td className="data-table__cell ipq-provider-table__name">{blacklistSourceLabel(row.source)}</td>
                      <td className="data-table__cell">{blacklistCategoryLabel(row.category)}</td>
                      <td className="data-table__cell"><Badge variant="info" tone={status.tone}>{status.label}</Badge></td>
                      <td className="data-table__cell mono">{row.matched_range || '—'}</td>
                      <td className="data-table__cell ipq-mail-table__toggle">
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-expanded={expanded}
                          aria-label={`${blacklistSourceLabel(row.source)} 详情`}
                          onClick={() => setOpen((current) => ({ ...current, [key]: !expanded }))}
                        >
                          {expanded ? '收起' : '详情'}
                        </Button>
                      </td>
                    </tr>
                    {expanded ? (
                      <tr className="data-table__row ipq-row-detail">
                        <td className="data-table__cell" colSpan={5}><BlacklistDetail row={row} /></td>
                      </tr>
                    ) : null}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </ScrollRegion>
      ) : (
        <p className="ipq-empty">{enrichmentGap(product)}</p>
      )}
    </section>
  )
}
