import { Timestamp } from '../../components/atoms'
import { VPS_LIFECYCLE_STATUS_LABELS, type VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge } from '../assetPageBadges'
import { type VPSLifecycleAction, vpsLifecycleConfirmationCopy } from './vpsLifecycleConfirmationCopy'

type VPSLifecycleCardProps = {
  detail: VPSAssetDetail
  action: VPSLifecycleAction
  error: string | null
  notice: string | null
}

export function VPSLifecycleCard({
  detail,
  action,
  error,
  notice,
}: VPSLifecycleCardProps) {
  const isRestore = action === 'restore'
  const dialogLabel = vpsLifecycleConfirmationCopy(detail, action).title

  return (
    <div className="asset-operation-form asset-lifecycle-card">
      <div className="asset-operation-form__header">
        <div>
          <p className="asset-lifecycle-card__eyebrow">DANGER ZONE</p>
          <h3>{dialogLabel}</h3>
          <p>
            {isRestore
              ? '恢复是独立生命周期操作，确认后会把已归档 VPS 重新放回闲置资产工作集。'
              : '归档是独立危险操作，不进入常规编辑弹窗；它会让 VPS 退出当前工作集，但保留基础信息、历史与监控实例关联。'}
          </p>
        </div>
        <LifecycleBadge value={detail.lifecycle_status} />
      </div>
      <dl className="asset-lifecycle-card__facts">
        <div>
          <dt>当前状态</dt>
          <dd>{VPS_LIFECYCLE_STATUS_LABELS[detail.lifecycle_status]}</dd>
        </div>
        <div>
          <dt>归档时间</dt>
          <dd>{detail.archived_at ? <Timestamp value={detail.archived_at} /> : '—'}</dd>
        </div>
      </dl>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
    </div>
  )
}
