import { Badge } from '../atoms'
import type { IPQualityServiceUnlock } from '../../lib/types'
import {
  serviceLabel,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusKind,
  unlockStatusLabel,
  unlockTone,
} from './ipQualityPresentation'

type IPQualityServiceGridProps = {
  unlocks: IPQualityServiceUnlock[]
}

const TITLE_ID = 'ip-quality-services-title'

export function IPQualityServiceGrid({ unlocks }: IPQualityServiceGridProps) {
  const counts = serviceUnlockCounts(unlocks)
  return (
    <section className="vps-detail-workspace__section ipq-services" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>服务解锁</h2>
        <p className="ipq-services__stats" aria-label="服务解锁状态统计">
          <span className={counts.unlocked > 0 ? 'ipq-services__stat--unlocked' : undefined}>{counts.unlocked} 可用</span>
          <span className={counts.blocked > 0 ? 'ipq-services__stat--blocked' : undefined}>{counts.blocked} 受阻</span>
          {counts.partial > 0 ? <span className="ipq-services__stat--partial">{counts.partial} 部分</span> : null}
          <span>{counts.unknown} 未知</span>
        </p>
      </div>
      {unlocks.length > 0 ? (
        <ul className="ipq-services__grid">
          {unlocks.map((unlock) => {
            const detail = serviceTileDetail(unlock)
            return (
              <li
                key={`${unlock.service}:${unlock.source ?? ''}`}
                className={`ipq-service ipq-service--${unlockStatusKind(unlock.status)}`}
              >
                <div className="ipq-service__head">
                  <span className="ipq-service__name">{serviceLabel(unlock.service)}</span>
                  <Badge variant="info" tone={unlockTone(unlock.status)}>{unlockStatusLabel(unlock.status, unlock.region)}</Badge>
                </div>
                {detail ? <p className="ipq-service__detail">{detail}</p> : null}
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="ipq-empty">本次未检测服务解锁。</p>
      )}
    </section>
  )
}
