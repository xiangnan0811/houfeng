import type { VPSOverviewFact, VPSOverviewIdentity } from '../../lib/types'
import { VPSFactList } from './VPSFactList'
import { modernOverviewFactRows } from './vpsFactPresentation'

type Props = {
  facts: VPSOverviewFact[]
  identity?: VPSOverviewIdentity
  heading?: string
}

export function VPSOverviewFacts({ facts, identity, heading = '稳定事实' }: Props) {
  const rows = modernOverviewFactRows(facts, identity)
  return (
    <section className="vps-overview-facts" aria-label={heading || '稳定事实'}>
      {heading ? <h2 id="vps-overview-facts-title">{heading}</h2> : null}
      <VPSFactList
        facts={rows}
        ariaLabel={heading || '稳定事实'}
        emptyLabel="暂无稳定事实"
      />
    </section>
  )
}
