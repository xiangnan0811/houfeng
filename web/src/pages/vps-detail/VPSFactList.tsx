import { VPSCopyValueButton } from './VPSCopyValueButton'
import type { VPSFactDisplay } from './vpsFactPresentation'

type Props = {
  facts: VPSFactDisplay[]
  ariaLabel: string
  emptyLabel?: string
}

export function VPSFactList({ facts, ariaLabel, emptyLabel }: Props) {
  if (facts.length === 0) {
    return emptyLabel ? <p className="vps-overview-facts__empty">{emptyLabel}</p> : null
  }

  return (
    <dl className="vps-overview-facts__list vps-detail-overview__facts" aria-label={ariaLabel}>
      {facts.map((fact) => {
        const copyValue = fact.copyValue ?? null
        const full = fact.layout === 'full'
        const note = fact.key === 'note' || fact.label === '备注'
        return (
          <div
            key={fact.key}
            className={[
              'vps-overview-facts__row',
              'vps-detail-overview__fact',
              full ? 'vps-overview-facts__row--full' : '',
              note ? 'vps-overview-facts__row--note' : '',
              fact.tone ? `vps-detail-overview__fact--${fact.tone}` : '',
            ].filter(Boolean).join(' ')}
          >
            <dt>{fact.label}</dt>
            <dd>
              <span className={copyValue ? 'mono vps-overview-facts__value' : note ? 'vps-overview-facts__note' : 'vps-overview-facts__value'}>
                {fact.value || '—'}
              </span>
              {copyValue ? (
                <span className="vps-overview-facts__copy">
                  <VPSCopyValueButton value={copyValue} label={fact.label} />
                </span>
              ) : null}
            </dd>
            {fact.meta ? <small>{fact.meta}</small> : null}
          </div>
        )
      })}
    </dl>
  )
}
