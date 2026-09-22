import { Link } from 'react-router-dom'

import { Badge, DataTable, MonoDigits } from '../../components/atoms'
import type { SubscriptionRecord, VPSAssetRecord } from '../../lib/types'
import { formatDateTime } from '../../lib/format'
import { lifecycleLabel, renewalLabel, usageLabel, vpsLocationLabel } from '../assetPageUtils'
import { lifecycleTone, subscriptionMonthlySummary } from './archivePageHelpers'

type ArchiveVPSWorkspaceProps = {
  vpsRows: VPSAssetRecord[]
  subscriptionsState: {
    loading: boolean
    error: string | null
    data: SubscriptionRecord[]
  }
  onRetrySubscriptions: () => void
}

export function ArchiveVPSWorkspace({
  vpsRows,
  subscriptionsState,
  onRetrySubscriptions,
}: ArchiveVPSWorkspaceProps) {
  return (
    <section className="page-panel archive-page__workspace">
      <div className="section-heading">
        <div>
          <h2 className="section-heading__title">归档服务器清单</h2>
          <p className="section-heading__description">只读归档清单，点击详情进入单台历史事实。</p>
        </div>
      </div>

      {subscriptionsState.error ? (
        <div className="archive-page__sub-notice" role="status">
          <span>历史订阅数据加载失败（{subscriptionsState.error}），已保留 VPS 台账。</span>
          <button type="button" className="btn sm secondary" onClick={onRetrySubscriptions}>
            重试加载订阅
          </button>
        </div>
      ) : null}

      <div
        className="page-panel--scroll-x archive-page__table-panel"
        role="region"
        aria-label="归档 VPS 列表"
        tabIndex={0}
      >
        <DataTable
          className="archive-page__vps-table"
          rows={vpsRows}
          rowKey={(vps) => vps.vps_id}
          columns={[
            {
              key: 'identity',
              label: 'VPS 身份',
              width: '260px',
              render: (vps) => (
                <div className="asset-table__identity">
                  <strong>{vps.display_name}</strong>
                  <small>
                    {vps.provider_name || '服务商未记录'} · {vps.product_name || '产品未记录'} · {vpsLocationLabel(vps)}
                    {vps.datacenter ? ` (${vps.datacenter})` : ''}
                  </small>
                  <small className="mono-text">{vps.vps_id}</small>
                </div>
              ),
            },
            {
              key: 'status',
              label: '生命周期与决策',
              width: '180px',
              render: (vps) => (
                <div className="badge-row badge-row--wrap">
                  <Badge variant="state" tone={lifecycleTone(vps.lifecycle_status)}>
                    {lifecycleLabel(vps.lifecycle_status)}
                  </Badge>
                  <Badge variant="info" tone="neutral">{usageLabel(vps.usage_status)}</Badge>
                  <Badge variant="info" tone="neutral">{renewalLabel(vps.renewal_decision)}</Badge>
                </div>
              ),
            },
            {
              key: 'cost',
              label: '历史费用',
              width: '180px',
              render: (vps) => {
                if (subscriptionsState.loading) {
                  return <span className="text-muted">加载中…</span>
                }
                if (subscriptionsState.error) {
                  return <span className="text-muted">订阅未加载</span>
                }
                const matching = subscriptionsState.data.filter((s) => s.vps_id === vps.vps_id)
                if (matching.length === 0) {
                  return <span className="text-muted">—</span>
                }
                return <strong>{subscriptionMonthlySummary(matching)}</strong>
              },
            },
            {
              key: 'archived_at',
              label: '归档时间',
              width: '180px',
              render: (vps) => {
                if (vps.archived_at) {
                  return <MonoDigits>{formatDateTime(vps.archived_at)}</MonoDigits>
                }
                return (
                  <div className="asset-table__stack">
                    <span className="text-muted">未记录归档时间</span>
                    <small className="text-muted">更新于 <MonoDigits>{formatDateTime(vps.updated_at)}</MonoDigits></small>
                  </div>
                )
              },
            },
            {
              key: 'action',
              label: '详情',
              width: '128px',
              render: (vps) => (
                <Link className="btn sm secondary" to={`/archive/${encodeURIComponent(vps.vps_id)}`}>
                  查看归档详情
                </Link>
              ),
            },
          ]}
        />
      </div>
    </section>
  )
}
