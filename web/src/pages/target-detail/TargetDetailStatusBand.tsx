import { Link, useLocation } from 'react-router-dom'

import { MonoDigits, Timestamp } from '../../components/atoms'
import type { AssetContextForTarget, ProbeItemRecord } from '../../lib/types'
import {
  assetContextHasAttention,
  assetContextPrimarySummary,
  vpsLifecycleLabel,
} from '../assetContextSummary'

type Props = {
  probeItems: ProbeItemRecord[]
  latestObservationAt: string | null
  assetContext: AssetContextForTarget | null
  assetContextError: string | null
}

function probeCoverage(probeItems: ProbeItemRecord[]) {
  if (probeItems.length === 0) return '尚未配置'
  const enabled = probeItems.filter((item) => item.enabled).length
  return (
    <>
      <MonoDigits>{enabled}</MonoDigits> 启用 / <MonoDigits>{probeItems.length}</MonoDigits> 配置
    </>
  )
}

function LinkedVpsValue({
  assetContext,
  assetContextError,
}: {
  assetContext: AssetContextForTarget | null
  assetContextError: string | null
}) {
  const location = useLocation()
  if (assetContextError && !assetContext) {
    return (
      <span className="target-detail-status-band__muted" title={assetContextError}>
        暂不可用
      </span>
    )
  }
  const primary = assetContextPrimarySummary(assetContext)
  if (!primary) {
    return <span className="target-detail-status-band__muted">未关联</span>
  }
  const attention = assetContextHasAttention(assetContext)
  return (
    <>
      <Link className="text-link" to={`/vps/${primary.vps_id}`} state={location.state}>
        {primary.display_name}
      </Link>
      {' · '}
      {vpsLifecycleLabel(primary.lifecycle_status)}
      {attention ? (
        <>
          {' · '}
          <Link
            className="text-link"
            to={`/vps/${primary.vps_id}?workbench=cancellation`}
            state={location.state}
          >
            打开工作台
          </Link>
        </>
      ) : null}
    </>
  )
}

export function TargetDetailStatusBand({
  probeItems,
  latestObservationAt,
  assetContext,
  assetContextError,
}: Props) {
  return (
    <dl className="target-detail-status-band" aria-label="目标状态">
      <div className="target-detail-status-band__item">
        <dt>探测方式</dt>
        <dd>{probeCoverage(probeItems)}</dd>
      </div>
      <div className="target-detail-status-band__item">
        <dt>最近观测</dt>
        <dd>
          {latestObservationAt ? (
            <Timestamp value={latestObservationAt} mode="relative" />
          ) : (
            <span className="target-detail-status-band__muted">暂无</span>
          )}
        </dd>
      </div>
      <div className="target-detail-status-band__item">
        <dt>关联 VPS</dt>
        <dd>
          <LinkedVpsValue assetContext={assetContext} assetContextError={assetContextError} />
        </dd>
      </div>
    </dl>
  )
}
