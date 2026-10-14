import { Badge } from '../atoms'
import type { IPQualityServiceUnlock } from '../../lib/types'
import {
  effectiveUnlockStatus,
  serviceLabel,
  serviceRowUsesAnonymousProbe,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusLabel,
  unlockStatusText,
  unlockTone,
} from './ipQualityPresentation'

type IPQualityServiceGridProps = {
  unlocks: IPQualityServiceUnlock[]
  // 所选报告 diagnostics.service_probe_revision === 2：免登录服务探测范围。
  anonymousProbe?: boolean
}

const TITLE_ID = 'ip-quality-services-title'

export function IPQualityServiceGrid({ unlocks, anonymousProbe = false }: IPQualityServiceGridProps) {
  const counts = serviceUnlockCounts(unlocks)
  return (
    <section className="vps-detail-workspace__section ipq-services" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>{anonymousProbe ? '服务可用性（免登录探测）' : '服务解锁'}</h2>
        <p className="ipq-services__stats" aria-label="服务状态统计">
          <span className={counts.unlocked > 0 ? 'ipq-services__stat--unlocked' : undefined}>
            {counts.unlocked} {unlockStatusText('unlocked', anonymousProbe)}
          </span>
          <span className={counts.blocked > 0 ? 'ipq-services__stat--blocked' : undefined}>
            {counts.blocked} {unlockStatusText('blocked', anonymousProbe)}
          </span>
          {counts.partial > 0 ? (
            <span className="ipq-services__stat--partial">{counts.partial} {unlockStatusText('partial', anonymousProbe)}</span>
          ) : null}
          <span>{counts.unknown} 未知</span>
        </p>
      </div>
      {unlocks.length > 0 ? (
        <ul className="ipq-services__grid">
          {unlocks.map((unlock) => {
            const kind = effectiveUnlockStatus(unlock)
            const detail = serviceTileDetail(unlock)
            return (
              <li key={`${unlock.service}:${unlock.source ?? ''}`} className={`ipq-service ipq-service--${kind}`}>
                <div className="ipq-service__head">
                  <span className="ipq-service__name">{serviceLabel(unlock.service)}</span>
                  <Badge variant="info" tone={unlockTone(kind)}>
                    {unlockStatusLabel(unlock, anonymousProbe || serviceRowUsesAnonymousProbe(unlock))}
                  </Badge>
                </div>
                {detail ? <p className="ipq-service__detail">{detail}</p> : null}
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="ipq-empty">本次未检测服务。</p>
      )}
    </section>
  )
}
