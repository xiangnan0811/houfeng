import { Button, Timestamp } from '../../components/atoms'
import { VPS_LIFECYCLE_STATUS_LABELS, type VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge } from '../assetPageBadges'

type VPSLifecycleAction = 'archive' | 'restore'

type VPSLifecycleCardProps = {
  detail: VPSAssetDetail
  action: VPSLifecycleAction
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onArchive: () => void
  onRestore: () => void
}

export function VPSLifecycleCard({
  detail,
  action,
  submitting,
  error,
  notice,
  onCancel,
  onArchive,
  onRestore,
}: VPSLifecycleCardProps) {
  const isRestore = action === 'restore'
  const dialogLabel = isRestore ? '确认恢复 VPS' : '确认归档 VPS'

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
      <section className="asset-lifecycle-confirm" role="alertdialog" aria-label={dialogLabel}>
        <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
        <h4>{dialogLabel}</h4>
        <div className="asset-lifecycle-confirm__flow">
          <span>
            {isRestore
              ? `当前：${detail.display_name} 已归档，不在当前资产工作集中。`
              : `当前：${detail.display_name} 仍在当前资产工作集中。`}
          </span>
          <span>
            {isRestore
              ? '操作后：生命周期变为闲置，并由后端清空归档时间。'
              : '操作后：生命周期变为已归档，并记录归档时间。'}
          </span>
        </div>
        <div className="asset-lifecycle-confirm__callouts">
          {isRestore ? (
            <>
              <p>恢复后它会重新进入 VPS 台账的人工核对范围，但不会自动改变续费决策。</p>
              <p>不会删除或重建 VPS、订阅、监控实例关联或资产历史。</p>
            </>
          ) : (
            <>
              <p>归档后它不会作为活跃 VPS 进入续费、迁移或成本核对队列。</p>
              <p>不会删除 VPS、订阅、监控实例关联或资产历史。后续可恢复为闲置。</p>
            </>
          )}
        </div>
        <div className="asset-operation-actions">
          <Button
            type="button"
            variant="secondary"
            disabled={submitting}
            onClick={onCancel}
          >
            取消
          </Button>
          <Button
            type="button"
            variant={isRestore ? 'secondary' : 'danger'}
            disabled={submitting}
            onClick={isRestore ? onRestore : onArchive}
          >
            {isRestore ? (submitting ? '恢复中…' : '确认恢复') : (submitting ? '归档中…' : '确认归档')}
          </Button>
        </div>
      </section>
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
