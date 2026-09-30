import { Badge, ScrollRegion } from '../atoms'
import type { IPQualityProviderResult } from '../../lib/types'
import {
  providerTableRows,
  riskFlags,
  riskLevelLabel,
  riskSignalHits,
  riskTone,
  type RiskFlag,
} from './ipQualityPresentation'

type IPQualityProviderTableProps = {
  results: IPQualityProviderResult[]
}

const TITLE_ID = 'ip-quality-provider-table-title'

// 表头顺序：先负面信号，机房属性放最后，它本身不是负面风险。
const FLAG_ORDER: RiskFlag['key'][] = ['proxy', 'vpn', 'tor', 'abuse', 'robot', 'server']
const FLAG_HEADERS: Record<RiskFlag['key'], string> = {
  proxy: 'Proxy',
  vpn: 'VPN',
  tor: 'Tor',
  abuse: 'Abuse',
  robot: 'Robot',
  server: '机房',
}

function orderedFlags(result: IPQualityProviderResult): RiskFlag[] {
  const flags = riskFlags(result)
  return FLAG_ORDER.map((key) => flags.find((flag) => flag.key === key) as RiskFlag)
}

function FlagCell({ flag }: { flag: RiskFlag }) {
  if (flag.active === true) {
    return <Badge variant="info" tone={flag.negative ? 'alert' : 'neutral'}>是</Badge>
  }
  if (flag.active === false) return <span className="ipq-flag ipq-flag--no">否</span>
  return <span className="ipq-flag ipq-flag--unknown" aria-label="未知">—</span>
}

function RiskCell({ result }: { result: IPQualityProviderResult }) {
  const level = (result.risk_level ?? '').trim()
  const score = (result.risk_score ?? '').trim()
  if (!level && !score) return <span className="ipq-flag ipq-flag--unknown" aria-label="未评级">—</span>
  return (
    <span className="ipq-risk-cell">
      {level ? <Badge variant="info" tone={riskTone(level)}>{riskLevelLabel(level)}</Badge> : <span className="ipq-risk-cell__label">风险分</span>}
      {score ? <span className="ipq-risk-cell__score mono">{score}</span> : null}
    </span>
  )
}

function textCell(value?: string): string {
  const normalized = (value ?? '').trim()
  return normalized || '—'
}

export function IPQualityProviderTable({ results }: IPQualityProviderTableProps) {
  const { rows, emptyCount, unavailable } = providerTableRows(results)
  const hits = riskSignalHits(results)

  return (
    <section className="vps-detail-workspace__section ipq-providers" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>IP 数据库判断</h2>
        <div className="ipq-signal-hits" aria-label="风险信号命中">
          {hits.length > 0 ? hits.map((hit) => (
            <Badge key={hit.key} variant="info" tone={hit.negative ? 'alert' : 'neutral'}>
              {hit.key === 'server' ? '机房' : hit.label} {hit.hits}/{hit.total}
            </Badge>
          )) : <span className="ipq-muted">未发现代理、VPN、Tor、滥用信号</span>}
        </div>
      </div>
      {rows.length > 0 ? (
        <ScrollRegion
          className="ipq-table-scroll"
          labelledBy={TITLE_ID}
          hintId="ip-quality-provider-table-hint"
          hintClassName="ipq-table-hint"
          hint="横向滚动查看完整列"
        >
          <table className="data-table data-table--compact ipq-provider-table">
            <thead className="data-table__head">
              <tr>
                <th>数据库</th>
                <th>使用类型</th>
                <th>公司类型</th>
                <th>风险</th>
                <th>地区</th>
                {FLAG_ORDER.map((key) => <th key={key} className="ipq-provider-table__flag">{FLAG_HEADERS[key]}</th>)}
              </tr>
            </thead>
            <tbody>
              {rows.map((result) => (
                <tr key={`${result.provider}:${result.source_type ?? ''}`} className="data-table__row">
                  <td className="data-table__cell ipq-provider-table__name">{result.provider}</td>
                  <td className="data-table__cell">{textCell(result.usage_type)}</td>
                  <td className="data-table__cell">{textCell(result.company_type)}</td>
                  <td className="data-table__cell"><RiskCell result={result} /></td>
                  <td className="data-table__cell">{textCell(result.region_code || result.region_name)}</td>
                  {orderedFlags(result).map((flag) => (
                    <td key={flag.key} className="data-table__cell ipq-provider-table__flag"><FlagCell flag={flag} /></td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollRegion>
      ) : (
        <p className="ipq-empty">暂无可展示的数据库判断。</p>
      )}
      {emptyCount > 0 || unavailable.length > 0 ? (
        <p className="ipq-providers__footnote">
          {emptyCount > 0 ? <span>另有 {emptyCount} 个数据库未给出风险判断</span> : null}
          {unavailable.length > 0 ? (
            <span className="ipq-providers__unavailable" aria-label="未返回结果的数据库">
              未返回结果：
              {unavailable.map((gap) => <Badge key={gap.provider} variant="info" tone={gap.tone}>{gap.label}</Badge>)}
            </span>
          ) : null}
        </p>
      ) : null}
    </section>
  )
}
