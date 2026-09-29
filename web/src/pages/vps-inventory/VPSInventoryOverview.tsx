import { renewalTimingLabel } from '../assetPageUtils'
import type { InventoryOverview, OverviewCount } from './inventoryOverview'

const RENEWAL_WARN_DAYS = 30

function CountList({ title, items }: { title: string; items: OverviewCount[] }) {
  return (
    <section className="vps-overview__group" aria-labelledby={`vps-overview-${title}`}>
      <h3 id={`vps-overview-${title}`} className="vps-overview__group-title">{title}</h3>
      <dl className="vps-overview__counts">
        {items.map((item) => (
          <div key={item.key} className="vps-overview__count">
            <dt>{item.label}</dt>
            <dd className="vps-mono">{item.count}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function RenewalSchedule({
  renewals,
  onSelect,
}: {
  renewals: InventoryOverview['renewals']
  onSelect: (vpsID: string) => void
}) {
  return (
    <section className="vps-overview__group vps-overview__group--wide" aria-labelledby="vps-overview-renewals">
      <h3 id="vps-overview-renewals" className="vps-overview__group-title">续费排期</h3>
      {renewals.status === 'loading' ? <p className="vps-overview__state">订阅加载中…</p> : null}
      {renewals.status === 'error' ? <p className="vps-overview__state">订阅加载失败，续费日未知</p> : null}
      {renewals.status === 'ready' ? (
        <>
          {renewals.items.length === 0 ? <p className="vps-overview__state">{`${renewals.undated} 台均无续费日`}</p> : (
            <ol className="vps-overview__renewals">
              {renewals.items.map((item) => (
                <li key={item.vpsId}>
                  <button type="button" className="vps-overview__renewal" onClick={() => onSelect(item.vpsId)}>
                    <span className="vps-overview__renewal-name">{item.name}</span>
                    <span className="vps-overview__renewal-date vps-mono">{item.date}</span>
                    <span className={item.days <= RENEWAL_WARN_DAYS ? 'vps-overview__renewal-days vps-tone-warn' : 'vps-overview__renewal-days'}>
                      {renewalTimingLabel(item.days)}
                    </span>
                  </button>
                </li>
              ))}
            </ol>
          )}
          {renewals.items.length > 0 && (renewals.hidden > 0 || renewals.undated > 0) ? (
            <p className="vps-overview__footnote">
              {[
                renewals.hidden > 0 ? `另有 ${renewals.hidden} 台排在之后` : '',
                renewals.undated > 0 ? `${renewals.undated} 台无续费日` : '',
              ].filter(Boolean).join(' · ')}
            </p>
          ) : null}
        </>
      ) : null}
    </section>
  )
}

export function VPSInventoryOverview({
  overview,
  notice,
  onSelect,
}: {
  overview: InventoryOverview
  notice: string | null
  onSelect: (vpsID: string) => void
}) {
  return (
    <div className="vps-overview">
      <header className="vps-overview__head">
        <h2 className="vps-inspector__title">当前列表</h2>
        <p className="vps-overview__total"><span className="vps-mono">{overview.total}</span> 台</p>
      </header>
      {notice ? <p className="vps-overview__notice">{notice}</p> : null}
      <div className="vps-overview__grid">
        <RenewalSchedule renewals={overview.renewals} onSelect={onSelect} />
        <CountList title="续费意向" items={overview.renewalDecisions} />
        <CountList title="用途" items={overview.usages} />
        <CountList title="服务商" items={overview.providers} />
        <CountList title="地区" items={overview.regions} />
      </div>
    </div>
  )
}
