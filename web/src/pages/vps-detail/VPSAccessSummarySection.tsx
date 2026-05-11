import { Hostname, MonoDigits } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'

type VPSAccessSummarySectionProps = {
  detail: VPSAssetDetail
}

export function VPSAccessSummarySection({ detail }: VPSAccessSummarySectionProps) {
  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">ACCESS</p>
          <h2>连接摘要</h2>
        </div>
      </div>
      <div className="asset-access-line">
        <Hostname>{detail.ssh_host || detail.ipv4 || detail.ipv6 || detail.display_name}</Hostname>
        <span>:</span>
        <MonoDigits>{detail.ssh_port}</MonoDigits>
        <span>{detail.ssh_user || 'root'}</span>
      </div>
    </section>
  )
}
