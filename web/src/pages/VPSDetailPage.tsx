import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Badge, Button, DataTable, Hostname, MonoDigits, Timestamp, type DataTableColumn } from '../components/atoms'
import { ApiError, getVPSAsset } from '../lib/api'
import { formatOptional } from '../lib/format'
import type { VPSAssetDetail, VPSNodeSummary } from '../lib/types'
import {
  AssetLabels,
  HealthBadge,
  LifecycleBadge,
  RenewalBadge,
  UsageBadge,
} from './assetPageBadges'

type PageState = {
  vpsId: string | null
  error: string | null
  detail: VPSAssetDetail | null
}

const INITIAL_STATE: PageState = {
  vpsId: null,
  error: null,
  detail: null,
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function DetailItem({ label, value }: { label: string; value: string | number | null | undefined }) {
  return (
    <div className="asset-detail-grid__item">
      <dt>{label}</dt>
      <dd>{formatOptional(value)}</dd>
    </div>
  )
}

export function VPSDetailPage() {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [state, setState] = useState<PageState>(INITIAL_STATE)

  useEffect(() => {
    if (!vpsId) {
      return
    }

    let cancelled = false

    getVPSAsset(vpsId)
      .then((detail) => {
        if (cancelled) return
        setState({ vpsId, error: null, detail })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          vpsId,
          error: describeError(error, '加载 VPS 详情失败'),
          detail: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [vpsId])

  const nodeColumns: DataTableColumn<VPSNodeSummary>[] = [
    {
      key: 'node',
      label: 'Node',
      render: (node) => (
        <div className="asset-table__identity">
          <strong>{node.display_name}</strong>
          <span>{node.node_id}</span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置 / Provider Hint',
      render: (node) => (
        <div className="asset-table__stack">
          <strong>{[node.region, node.city].filter(Boolean).join(' · ') || '—'}</strong>
          <span>{formatOptional(node.provider)}</span>
        </div>
      ),
    },
    {
      key: 'health',
      label: '监控状态',
      render: (node) => (
        <span className="asset-status-stack">
          <HealthBadge value={node.current_health_status} />
          <Badge variant="info" tone="neutral">{node.monitoring_status || '未知'}</Badge>
        </span>
      ),
    },
    {
      key: 'issue',
      label: '异常摘要',
      render: (node) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{node.current_active_incident_count}</MonoDigits> 个活跃异常</strong>
          <span>{formatOptional(node.current_primary_issue_summary)}</span>
        </div>
      ),
    },
    {
      key: 'heartbeat',
      label: '最近心跳',
      render: (node) => <Timestamp value={node.last_heartbeat_at} />,
    },
  ]

  if (!vpsId) {
    return (
      <div className="page-stack asset-page vps-detail-page">
        <section className="page-panel page-panel--inline">
          <div>
            <div className="page-panel__eyebrow">VPS DETAIL</div>
            <h1 className="page-panel__title">VPS 详情不可用</h1>
            <p className="page-panel__description">缺少 VPS ID</p>
          </div>
          <div className="page-panel__actions">
            <Button variant="secondary" onClick={() => navigate(-1)}>返回</Button>
          </div>
        </section>
      </div>
    )
  }

  const currentStateReady = state.vpsId === vpsId

  if (!currentStateReady) {
    return (
      <div className="page-stack asset-page vps-detail-page">
        <section className="page-panel">
          <div className="empty-state">正在加载 VPS 详情…</div>
        </section>
      </div>
    )
  }

  if (state.error || !state.detail) {
    return (
      <div className="page-stack asset-page vps-detail-page">
        <section className="page-panel page-panel--inline">
          <div>
            <div className="page-panel__eyebrow">VPS DETAIL</div>
            <h1 className="page-panel__title">VPS 详情不可用</h1>
            <p className="page-panel__description">{state.error ?? 'VPS 不存在'}</p>
          </div>
          <div className="page-panel__actions">
            <Button variant="secondary" onClick={() => navigate(-1)}>返回</Button>
          </div>
        </section>
      </div>
    )
  }

  const detail = state.detail

  return (
    <div className="page-stack asset-page vps-detail-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">VPS DETAIL</div>
          <h1 className="page-panel__title">{detail.display_name}</h1>
          <p className="page-panel__description">
            {formatOptional(detail.provider_name)} · {[detail.country, detail.region, detail.city].filter(Boolean).join(' · ') || '位置未确认'}
          </p>
          <div className="asset-hero-meta">
            <LifecycleBadge value={detail.lifecycle_status} />
            <UsageBadge value={detail.usage_status} />
            <RenewalBadge value={detail.renewal_decision} />
            <Badge variant="count" tone="neutral">{detail.active_node_link_count} 个 Node</Badge>
          </div>
        </div>
        <div className="page-panel__actions">
          <Button variant="secondary" onClick={() => navigate(-1)}>返回</Button>
          <Link className="btn btn--primary btn--md" to="/vps">VPS 列表</Link>
        </div>
      </section>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">FACTS</p>
            <h2>基础信息</h2>
          </div>
          <AssetLabels labels={detail.labels} />
        </div>
        <dl className="asset-detail-grid">
          <DetailItem label="VPS ID" value={detail.vps_id} />
          <DetailItem label="Provider ID" value={detail.provider_id} />
          <DetailItem label="产品名" value={detail.product_name} />
          <DetailItem label="订单号" value={detail.order_ref} />
          <DetailItem label="数据中心" value={detail.datacenter} />
          <DetailItem label="重要性" value={detail.importance} />
          <DetailItem label="IPv4" value={detail.ipv4} />
          <DetailItem label="IPv6" value={detail.ipv6} />
          <DetailItem label="SSH Host" value={detail.ssh_host} />
          <DetailItem label="SSH 端口" value={detail.ssh_port} />
          <DetailItem label="SSH 用户" value={detail.ssh_user} />
          <DetailItem label="操作系统" value={detail.os_name} />
          <DetailItem label="虚拟化" value={detail.virtualization} />
          <DetailItem label="备注" value={detail.note} />
        </dl>
      </section>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">OBSERVABILITY LINK</p>
            <h2>关联 Node 监控</h2>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{detail.node_links.length}</MonoDigits> 个 active link
          </span>
        </div>
        <DataTable
          className="asset-table vps-node-table"
          columns={nodeColumns}
          rows={detail.node_links}
          rowKey={(node) => node.node_id}
          emptyContent={<span className="empty-inline">尚未关联 Node</span>}
        />
      </section>

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
    </div>
  )
}
