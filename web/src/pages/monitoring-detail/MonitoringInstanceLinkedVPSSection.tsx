import type { RefObject } from 'react'
import { Link } from 'react-router-dom'

import { DetailSection } from '../../components/DetailSection'
import { Badge, DataTable, Timestamp, type DataTableColumn } from '../../components/atoms'
import type { VPSSummary } from '../../lib/types'
import { vpsLifecycleLabel, vpsRenewalDecisionLabel } from '../assetContextSummary'
import { AssetLabels, LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'
import { formatAssetLocation } from './monitoringDetailHelpers'

type MonitoringInstanceLinkedVPSSectionProps = {
  sectionRef: RefObject<HTMLDivElement | null>
  records: VPSSummary[]
  loading: boolean
  loaded: boolean
  error: string | null
}

export function MonitoringInstanceLinkedVPSSection({
  sectionRef,
  records,
  loading,
  loaded,
  error,
}: MonitoringInstanceLinkedVPSSectionProps) {
  const lifecycleContext = records.find((vps) =>
    vps.lifecycle_status === 'to_cancel' ||
    vps.lifecycle_status === 'cancelled' ||
    vps.renewal_decision === 'cancel' ||
    vps.renewal_decision === 'auto_renew_cancelled',
  )
  const linkedVPSColumns: DataTableColumn<VPSSummary>[] = [
    {
      key: 'vps',
      label: 'VPS',
      render: (vps) => (
        <div className="asset-table__identity">
          <strong>
            <Link className="text-link" to={`/vps/${vps.vps_id}`}>{vps.display_name}</Link>
          </strong>
          <span>{vps.vps_id}</span>
        </div>
      ),
    },
    {
      key: 'provider',
      label: 'Provider / 位置',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong>{vps.provider_name || 'Provider 未确认'}</strong>
          <span>{formatAssetLocation(vps)}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '资产状态',
      render: (vps) => (
        <span className="asset-status-stack">
          <LifecycleBadge value={vps.lifecycle_status} />
          <UsageBadge value={vps.usage_status} />
          <RenewalBadge value={vps.renewal_decision} />
        </span>
      ),
    },
    {
      key: 'link',
      label: '关联',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong><Timestamp value={vps.linked_at} /></strong>
          <span>{vps.note || '无关联备注'}</span>
        </div>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (vps) => <AssetLabels labels={vps.labels} />,
    },
  ]

  return (
    <div ref={sectionRef}>
      <DetailSection
        eyebrow="ASSET LEDGER"
        title="关联 VPS"
        aside={loading ? '加载中' : loaded ? `${records.length} 台` : '待同步'}
      >
        <p className="monitoring-instance-vps-table__hint">
          VPS 是资产账本里的购买、续费与归属对象；监控实例是 agent 接入后的运行实例。
          {!loading && loaded && records.length === 0 ? (
            <>
              {' '}
              <Link className="text-link" to="/vps?view=unlinked">去 VPS 库存选择并关联</Link>
            </>
          ) : null}
        </p>
        {error ? <p className="empty-inline" role="alert">{error}</p> : null}
        {lifecycleContext ? (
          <div className="asset-context-panel">
            <div>
              <span className="asset-context-panel__label">取消/过期上下文</span>
              <strong>关联 VPS 已进入取消/退役流程</strong>
              <small>
                {lifecycleContext.display_name} · {vpsLifecycleLabel(lifecycleContext.lifecycle_status)} ·
                续费 {vpsRenewalDecisionLabel(lifecycleContext.renewal_decision)}
              </small>
            </div>
            <Badge variant="state" tone="notice">
              需联动处理
            </Badge>
            <Link className="btn sm secondary" to={`/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement&vps_id=${encodeURIComponent(lifecycleContext.vps_id)}`}>
              组合决策
            </Link>
            <Link className="btn sm secondary" to={`/vps/${lifecycleContext.vps_id}?workbench=cancellation`}>
              打开工作台
            </Link>
          </div>
        ) : null}
        <DataTable
          className="asset-table monitoringInstance-vps-table"
          columns={linkedVPSColumns}
          rows={records}
          rowKey={(vps) => vps.vps_id}
          emptyContent={
            <span className="empty-inline">
              {loading
                ? '正在加载关联 VPS…'
                : loaded
                  ? '尚未关联 VPS'
                  : '关联 VPS 待同步'}
            </span>
          }
        />
      </DetailSection>
    </div>
  )
}
