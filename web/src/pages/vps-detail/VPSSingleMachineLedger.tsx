import { Button } from '../../components/atoms'
import { formatDate } from '../../lib/format'
import type { VPSSingleMachineLedgerModel } from './vpsDetailOverviewModel'
import type { VPSDetailModalMode } from './types'

type VPSSingleMachineLedgerProps = {
  ledger: VPSSingleMachineLedgerModel
  onOpenModal: (mode: NonNullable<VPSDetailModalMode>) => void
}

export function VPSSingleMachineLedger({ ledger, onOpenModal }: VPSSingleMachineLedgerProps) {
  return (
    <section className="page-panel vps-single-ledger" aria-labelledby="vps-single-ledger-title">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">Ledger</p>
          <h2 id="vps-single-ledger-title" className="section-heading__title">单机台账</h2>
        </div>
        <div className="section-heading__actions">
          <Button variant="secondary" size="sm" onClick={() => onOpenModal('experience')}>记录经验</Button>
        </div>
      </div>

      <div className="vps-single-ledger__body">
        <dl className="vps-single-ledger__facts" aria-label="运维字段">
          {ledger.operationFacts.map((fact) => (
            <div key={fact.label}>
              <dt>{fact.label}</dt>
              <dd>{fact.value}</dd>
            </div>
          ))}
        </dl>

        <div className="vps-single-ledger__columns">
          <section className="vps-ledger-group" aria-labelledby="vps-ledger-records-title">
            <div className="vps-ledger-group__head">
              <h3 id="vps-ledger-records-title">近期记录</h3>
              <button type="button" className="text-link" onClick={() => onOpenModal('timeline-detail')}>资产历史</button>
            </div>
            {ledger.records.length > 0 ? (
              <ul className="vps-ledger-list" aria-label="近期记录">
                {ledger.records.map((record) => (
                  <li key={record.key}>
                    <button type="button" onClick={() => onOpenModal('timeline-detail')}>
                      <strong>{record.summary}</strong>
                      <span>{record.kind} · {formatDate(record.date)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="empty-inline">尚无记录</p>
            )}
          </section>

          <section className="vps-ledger-group" aria-labelledby="vps-ledger-carriers-title">
            <div className="vps-ledger-group__head">
              <h3 id="vps-ledger-carriers-title">承载清单</h3>
              <div>
                <button type="button" className="text-link" onClick={() => onOpenModal('services-detail')}>服务</button>
                <button type="button" className="text-link" onClick={() => onOpenModal('domains-detail')}>域名</button>
              </div>
            </div>
            {ledger.carriers.length > 0 ? (
              <ul className="vps-ledger-list" aria-label="承载清单">
                {ledger.carriers.map((carrier) => (
                  <li key={carrier.key}>
                    <button type="button" onClick={() => onOpenModal(carrier.kind === 'service' ? 'services-detail' : 'domains-detail')}>
                      <strong>{carrier.name}</strong>
                      <span>{carrier.kind === 'service' ? '服务' : '域名'} · {carrier.meta}</span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="empty-inline">未记录服务或域名</p>
            )}
          </section>

          <section className="vps-ledger-group" aria-labelledby="vps-ledger-changes-title">
            <h3 id="vps-ledger-changes-title">关键变化</h3>
            {ledger.changes.length > 0 ? (
              <ul className="vps-ledger-list" aria-label="关键变化">
                {ledger.changes.map((change) => (
                  <li key={change.key}>
                    <button type="button" onClick={() => onOpenModal('timeline-detail')}>
                      <strong>{change.summary}</strong>
                      <span>{change.meta}</span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="empty-inline">近期无关键变化</p>
            )}
          </section>
        </div>
      </div>
    </section>
  )
}
