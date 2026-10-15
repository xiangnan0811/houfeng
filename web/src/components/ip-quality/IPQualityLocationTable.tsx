import type { ReactNode } from 'react'

import { ScrollRegion, Timestamp } from '../atoms'
import type { IPQualityProviderResult } from '../../lib/types'
import { IP_QUALITY_LOCATION_ANCHOR } from './IPQualityIdentityPanel'
import { FieldStateMark } from './IPQualityProviderTable'
import { agreedCountry, agreedValue, locationRows, type FieldCellState } from './ipQualityProductPresentation'

function StateValue({ state, label, children }: { state: FieldCellState, label: string, children: ReactNode }) {
  return state === 'known' ? <>{children}</> : <FieldStateMark state={state} label={label} />
}

type IPQualityLocationTableProps = {
  results: IPQualityProviderResult[]
}

// 多源定位：每个来源一行，注册地/ASN 国家不混入使用地列；不同来源不串值。
export function IPQualityLocationTable({ results }: IPQualityLocationTableProps) {
  const rows = locationRows(results)
  const discrepant = [
    agreedCountry(rows),
    agreedValue(rows.map((row) => row.region)),
    agreedValue(rows.map((row) => row.city)),
  ].some((value) => value.discrepant)

  return (
    <section className="vps-detail-workspace__section ipq-location" aria-labelledby={IP_QUALITY_LOCATION_ANCHOR}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={IP_QUALITY_LOCATION_ANCHOR}>多源定位</h2>
        {discrepant ? <span className="ipq-muted">来源有分歧</span> : null}
      </div>
      {rows.length > 0 ? (
        <ScrollRegion
          className="ipq-table-scroll"
          labelledBy={IP_QUALITY_LOCATION_ANCHOR}
          hintId="ip-quality-location-hint"
          hintClassName="ipq-table-hint"
          hint="横向滚动查看完整列"
        >
          <table className="data-table data-table--compact ipq-location-table">
            <thead className="data-table__head">
              <tr><th>来源</th><th>国家</th><th>省 / 州</th><th>城市</th><th>经纬度</th><th>采集时间</th></tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.key} className="data-table__row">
                  <td className="data-table__cell ipq-provider-table__name">
                    <span className="ipq-source">
                      <span className="ipq-source__name">{row.provider}</span>
                      {row.method ? <span className="ipq-source__method">{row.method}</span> : null}
                    </span>
                  </td>
                  <td className="data-table__cell"><StateValue state={row.states.country} label="国家">{row.country}</StateValue></td>
                  <td className="data-table__cell"><StateValue state={row.states.region} label="省 / 州">{row.region}</StateValue></td>
                  <td className="data-table__cell"><StateValue state={row.states.city} label="城市">{row.city}</StateValue></td>
                  <td className="data-table__cell mono"><StateValue state={row.states.coordinates} label="经纬度">{row.coordinates}</StateValue></td>
                  <td className="data-table__cell">{row.observedAt ? <Timestamp value={row.observedAt} /> : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollRegion>
      ) : (
        <p className="ipq-empty">暂无定位结果。</p>
      )}
    </section>
  )
}
