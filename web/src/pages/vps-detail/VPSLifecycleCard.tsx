import { Button, Timestamp } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import { VPS_LIFECYCLE_STATUS_LABELS, type VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge } from '../assetPageBadges'

type VPSLifecycleCardProps = {
  detail: VPSAssetDetail
  isArchived: boolean
  confirmingArchive: boolean
  submitting: boolean
  error: string | null
  notice: string | null
  onArchiveConfirmOpenChange: (open: boolean) => void
  onArchive: () => void
  onRestore: () => void
}

export function VPSLifecycleCard({
  detail,
  isArchived,
  confirmingArchive,
  submitting,
  error,
  notice,
  onArchiveConfirmOpenChange,
  onArchive,
  onRestore,
}: VPSLifecycleCardProps) {
  return (
    <div className="asset-operation-form asset-lifecycle-card">
      <div className="asset-operation-form__header">
        <div>
          <p className="asset-lifecycle-card__eyebrow">DANGER ZONE</p>
          <h3>生命周期危险区</h3>
          <p>
            {isArchived
              ? '这台 VPS 已退出当前工作集，可恢复为闲置后重新纳入台账处理。'
              : '归档是独立危险操作，不进入常规编辑 Drawer；它会让 VPS 退出当前工作集，但保留基础信息、历史与 Node 关联。'}
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
      {confirmingArchive ? (
        <section className="asset-lifecycle-confirm" role="alertdialog" aria-label="确认归档 VPS">
          <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
          <h4>确认归档 VPS</h4>
          <div className="asset-lifecycle-confirm__flow">
            <span>当前：{detail.display_name} 仍在当前资产工作集中。</span>
            <span>操作后：生命周期变为已归档，并记录归档时间。</span>
          </div>
          <div className="asset-lifecycle-confirm__callouts">
            <p>归档后它不会作为活跃 VPS 进入续费、迁移或成本核对队列。</p>
            <p>不会删除 VPS、订阅、Node 关联或资产历史。后续可恢复为闲置。</p>
          </div>
          <div className="asset-operation-actions">
            <Button
              type="button"
              variant="secondary"
              disabled={submitting}
              onClick={() => onArchiveConfirmOpenChange(false)}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={submitting}
              onClick={onArchive}
            >
              {submitting ? '归档中…' : '确认归档'}
            </Button>
          </div>
        </section>
      ) : (
        <>
          <p className="asset-lifecycle-card__note">
            {isArchived
              ? `已归档时间：${formatDateTime(detail.archived_at)}。恢复会把生命周期改为闲置，并由后端清空归档时间。`
              : '这是软归档，不是删除。归档后仍可通过 VPS 列表的“已归档”筛选找回。'}
          </p>
          <div className="asset-operation-actions">
            {isArchived ? (
              <Button
                type="button"
                variant="secondary"
                disabled={submitting}
                onClick={onRestore}
              >
                {submitting ? '恢复中…' : '恢复为闲置'}
              </Button>
            ) : (
              <Button
                type="button"
                variant="danger"
                disabled={submitting}
                onClick={() => onArchiveConfirmOpenChange(true)}
              >
                归档 VPS
              </Button>
            )}
          </div>
        </>
      )}
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
