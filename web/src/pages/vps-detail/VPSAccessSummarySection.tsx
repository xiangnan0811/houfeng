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
          <p className="section-heading__eyebrow">ACCESS SUMMARY</p>
          <h2>访问摘要</h2>
          <p className="section-heading__description">仅呈现当前资产记录中的 SSH/IP 入口，便于人工核对。</p>
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
