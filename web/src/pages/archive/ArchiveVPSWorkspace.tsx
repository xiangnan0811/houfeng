import { Badge, DataTable, MonoDigits } from '../../components/atoms'
import type { SubscriptionRecord, VPSAssetRecord } from '../../lib/types'
import { formatDate, formatDateTime, formatMoney } from '../../lib/format'
import { lifecycleLabel, renewalLabel, subscriptionStatusLabel, usageLabel, vpsLocationLabel } from '../assetPageUtils'
import { lifecycleTone, subscriptionMonthlySummary } from './archivePageHelpers'

type ArchiveVPSWorkspaceProps = {
  vpsRows: VPSAssetRecord[]
  selectedVPS: VPSAssetRecord | null
  subscriptions: SubscriptionRecord[]
  onSelectVPS: (vpsID: string) => void
}

export function ArchiveVPSWorkspace({
  vpsRows,
  selectedVPS,
  subscriptions,
  onSelectVPS,
}: ArchiveVPSWorkspaceProps) {
  return (
    <section className="page-panel archive-page__workspace">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">READ ONLY</p>
          <h2 className="section-heading__title">归档服务器</h2>
          <p className="section-heading__description">点击行切换右侧历史上下文；本页不提供编辑、恢复、取消、创建或关联操作。</p>
        </div>
      </div>
      <div className="archive-page__workspace-grid">
        <div className="page-panel page-panel--scroll-x archive-page__table-panel">
          <DataTable
            className="archive-page__vps-table"
            rows={vpsRows}
            rowKey={(vps) => vps.vps_id}
            onRowClick={(vps) => onSelectVPS(vps.vps_id)}
            columns={[
              {
                key: 'identity',
                label: 'VPS',
                width: '240px',
                render: (vps) => (
                  <div className="asset-table__identity">
                    <strong>{vps.display_name}</strong>
                    <small>{vps.provider_name || '服务商缺失'} · {vps.product_name || '产品缺失'}</small>
                    <small>{vps.vps_id}</small>
                  </div>
                ),
              },
              {
                key: 'status',
                label: '状态',
                width: '136px',
                render: (vps) => (
                  <div className="badge-row badge-row--wrap">
                    <Badge variant="state" tone={lifecycleTone(vps.lifecycle_status)}>
                      {lifecycleLabel(vps.lifecycle_status)}
                    </Badge>
                    <Badge variant="info" tone="neutral">{usageLabel(vps.usage_status)}</Badge>
                  </div>
                ),
              },
              {
                key: 'location',
                label: '位置',
                width: '180px',
                render: (vps) => (
                  <div className="asset-table__stack">
                    <strong>{vpsLocationLabel(vps)}</strong>
                    <small>{vps.datacenter || '机房缺失'}</small>
                  </div>
                ),
              },
              {
                key: 'archived_at',
                label: '归档时间',
                width: '156px',
                render: (vps) => <MonoDigits>{formatDateTime(vps.archived_at ?? vps.updated_at)}</MonoDigits>,
              },
            ]}
          />
        </div>

        <aside className="archive-page__detail" aria-label="归档资产详情">
          {selectedVPS ? (
            <>
              <div className="archive-page__detail-card">
                <div className="archive-page__detail-head">
                  <div>
                    <p>ARCHIVED VPS</p>
                    <h2>{selectedVPS.display_name}</h2>
                    <span>{selectedVPS.provider_name || '服务商缺失'} · {selectedVPS.product_name || '产品缺失'}</span>
                  </div>
                  <Badge variant="state" tone={lifecycleTone(selectedVPS.lifecycle_status)}>
                    {lifecycleLabel(selectedVPS.lifecycle_status)}
                  </Badge>
                </div>
                <dl className="asset-detail-grid">
                  <div className="asset-detail-grid__item">
                    <dt>续费判断</dt>
                    <dd>{renewalLabel(selectedVPS.renewal_decision)}</dd>
                  </div>
                  <div className="asset-detail-grid__item">
                    <dt>访问入口</dt>
                    <dd>{selectedVPS.ssh_host || selectedVPS.ipv4 || selectedVPS.ipv6 || '—'}</dd>
                  </div>
                  <div className="asset-detail-grid__item">
                    <dt>归档时间</dt>
                    <dd>{formatDateTime(selectedVPS.archived_at ?? selectedVPS.updated_at)}</dd>
                  </div>
                  <div className="asset-detail-grid__item">
                    <dt>备注</dt>
                    <dd>{selectedVPS.note || '—'}</dd>
                  </div>
                </dl>
              </div>

              <div className="archive-page__detail-card">
                <div className="archive-page__detail-head">
                  <div>
                    <p>BILLING HISTORY</p>
                    <h2>历史订阅</h2>
                    <span>
                      <MonoDigits>{subscriptions.length}</MonoDigits> 次记录 · <span>{subscriptionMonthlySummary(subscriptions)}</span>
                    </span>
                  </div>
                </div>
                <DataTable
                  className="archive-page__subscription-table"
                  rows={subscriptions}
                  rowKey={(subscription) => subscription.subscription_id}
                  emptyContent={<span className="empty-inline">暂无历史订阅</span>}
                  columns={[
                    {
                      key: 'period',
                      label: '周期',
                      width: '160px',
                      render: (subscription) => (
                        <div className="asset-subscription-cell">
                          <strong>{formatMoney(subscription.monthly_price, subscription.currency)}/月</strong>
                          <span>{formatDate(subscription.started_at)} {'->'} {formatDate(subscription.renew_at)}</span>
                        </div>
                      ),
                    },
                    {
                      key: 'status',
                      label: '状态',
                      width: '108px',
                      render: (subscription) => (
                        <Badge variant="state" tone="offline">{subscriptionStatusLabel(subscription.status)}</Badge>
                      ),
                    },
                    {
                      key: 'note',
                      label: '备注',
                      render: (subscription) => (
                        <span className="asset-table__muted">{subscription.note || subscription.payment_method || '—'}</span>
                      ),
                    },
                  ]}
                />
              </div>
            </>
          ) : null}
        </aside>
      </div>
    </section>
  )
}
