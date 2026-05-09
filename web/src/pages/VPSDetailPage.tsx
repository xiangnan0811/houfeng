import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Badge, Button, DataTable, Hostname, Input, MonoDigits, Timestamp, type DataTableColumn } from '../components/atoms'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  getVPSAsset,
  getVPSTimeline,
  linkVPSNode,
  unlinkVPSNode,
  updateVPSAsset,
} from '../lib/api'
import { formatOptional } from '../lib/format'
import {
  VPS_RENEWAL_DECISION_LABELS,
  type VPSAssetDetail,
  type VPSNodeSummary,
  type VPSRenewalDecision,
  type VPSTimeline,
} from '../lib/types'
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
  timeline: VPSTimeline | null
}

const INITIAL_STATE: PageState = {
  vpsId: null,
  error: null,
  detail: null,
  timeline: null,
}

const RENEWAL_DECISION_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS) as Array<[
  VPSRenewalDecision,
  string,
]>

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
  const [decisionDraft, setDecisionDraft] = useState<{
    renewalDecision: VPSRenewalDecision
    reason: string
  }>({ renewalDecision: 'unreviewed', reason: '' })
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [linkDraft, setLinkDraft] = useState({ nodeId: '', note: '' })
  const [linkSubmitting, setLinkSubmitting] = useState(false)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkNotice, setLinkNotice] = useState<string | null>(null)
  const [unlinkingNodeId, setUnlinkingNodeId] = useState<string | null>(null)
  const [unlinkError, setUnlinkError] = useState<string | null>(null)

  useEffect(() => {
    if (!vpsId) {
      return
    }

    let cancelled = false

    Promise.all([getVPSAsset(vpsId), getVPSTimeline(vpsId)])
      .then(([detail, timeline]) => {
        if (cancelled) return
        setState({ vpsId, error: null, detail, timeline })
        setDecisionDraft({ renewalDecision: detail.renewal_decision, reason: '' })
        setDecisionError(null)
        setDecisionNotice(null)
        setLinkDraft({ nodeId: '', note: '' })
        setLinkError(null)
        setLinkNotice(null)
        setUnlinkError(null)
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          vpsId,
          error: describeError(error, '加载 VPS 详情失败'),
          detail: null,
          timeline: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [vpsId])

  async function refreshDetail(targetVPSId: string): Promise<VPSAssetDetail> {
    const detail = await getVPSAsset(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId || !current.timeline) return current
      return { ...current, error: null, detail }
    })
    return detail
  }

  async function refreshDetailAndTimeline(targetVPSId: string): Promise<VPSAssetDetail> {
    const [detail, timeline] = await Promise.all([getVPSAsset(targetVPSId), getVPSTimeline(targetVPSId)])
    setState({ vpsId: targetVPSId, error: null, detail, timeline })
    return detail
  }

  async function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    setDecisionError(null)
    setDecisionNotice(null)

    if (decisionDraft.renewalDecision === detail.renewal_decision) {
      setDecisionError('请选择一个不同的续费决策')
      return
    }

    const reason = decisionDraft.reason.trim()
    setDecisionSubmitting(true)
    try {
      await updateVPSAsset(detail.vps_id, {
        renewal_decision: decisionDraft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      })
      const refreshed = await refreshDetailAndTimeline(detail.vps_id)
      setDecisionDraft({ renewalDecision: refreshed.renewal_decision, reason: '' })
      setDecisionNotice('续费决策已更新，资产历史已刷新')
    } catch (error: unknown) {
      setDecisionError(describeError(error, '更新续费决策失败'))
    } finally {
      setDecisionSubmitting(false)
    }
  }

  async function handleLinkSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    const nodeId = linkDraft.nodeId.trim()
    if (!nodeId) {
      setLinkError('Node ID 不能为空')
      setLinkNotice(null)
      return
    }

    setLinkSubmitting(true)
    setLinkError(null)
    setLinkNotice(null)
    setUnlinkError(null)

    try {
      await linkVPSNode(detail.vps_id, {
        node_id: nodeId,
        note: linkDraft.note.trim(),
      })
      await refreshDetail(detail.vps_id)
      setLinkDraft({ nodeId: '', note: '' })
      setLinkNotice('Node 关联已更新')
    } catch (error: unknown) {
      setLinkError(describeError(error, '关联 Node 失败'))
    } finally {
      setLinkSubmitting(false)
    }
  }

  async function handleUnlinkNode(node: VPSNodeSummary) {
    const detail = state.detail
    if (!detail) return

    setUnlinkingNodeId(node.node_id)
    setUnlinkError(null)
    setLinkError(null)
    setLinkNotice(null)

    try {
      await unlinkVPSNode(detail.vps_id, {
        node_id: node.node_id,
        note: node.note,
      })
      await refreshDetail(detail.vps_id)
      setLinkNotice('Node 关联已解除')
    } catch (error: unknown) {
      setUnlinkError(describeError(error, '解除 Node 关联失败'))
    } finally {
      setUnlinkingNodeId(null)
    }
  }

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
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (node) => (
        <Button
          variant="ghost"
          size="sm"
          disabled={unlinkingNodeId !== null}
          onClick={() => void handleUnlinkNode(node)}
        >
          {unlinkingNodeId === node.node_id ? '解除中…' : '解除关联'}
        </Button>
      ),
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

  if (state.error || !state.detail || !state.timeline) {
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
  const timeline = state.timeline
  const decisionChanged = decisionDraft.renewalDecision !== detail.renewal_decision
  const linkControlsDisabled = linkSubmitting || unlinkingNodeId !== null

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

      <section className="page-panel asset-operation-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">OPERATIONS</p>
            <h2>资产操作</h2>
          </div>
          <span className="section-heading__meta">
            更新会立即写入资产台账
          </span>
        </div>
        <div className="asset-operation-grid">
          <form className="asset-operation-form" onSubmit={(event) => void handleDecisionSubmit(event)}>
            <div className="asset-operation-form__header">
              <div>
                <h3>续费决策</h3>
                <p>记录这台 VPS 下一次续费前的处理判断。</p>
              </div>
              <RenewalBadge value={detail.renewal_decision} />
            </div>
            <label className="asset-operation-field">
              <span>续费决策</span>
              <select
                value={decisionDraft.renewalDecision}
                onChange={(event) => {
                  setDecisionDraft((current) => ({
                    ...current,
                    renewalDecision: event.target.value as VPSRenewalDecision,
                  }))
                  setDecisionError(null)
                  setDecisionNotice(null)
                }}
              >
                {RENEWAL_DECISION_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label className="asset-operation-field asset-operation-field--wide">
              <span>决策理由</span>
              <textarea
                value={decisionDraft.reason}
                onChange={(event) => {
                  setDecisionDraft((current) => ({ ...current, reason: event.target.value }))
                  setDecisionError(null)
                  setDecisionNotice(null)
                }}
                placeholder="例如：价格上涨，迁移到首尔节点"
              />
            </label>
            {decisionError ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                {decisionError}
              </p>
            ) : decisionNotice ? (
              <p className="asset-operation-feedback" role="status">{decisionNotice}</p>
            ) : null}
            <div className="asset-operation-actions">
              <Button type="submit" disabled={decisionSubmitting || !decisionChanged}>
                {decisionSubmitting ? '保存中…' : '保存续费决策'}
              </Button>
            </div>
          </form>

          <form className="asset-operation-form" onSubmit={(event) => void handleLinkSubmit(event)}>
            <div className="asset-operation-form__header">
              <div>
                <h3>关联 Node</h3>
                <p>把资产台账中的 VPS 与观测系统中的 Node 对齐。</p>
              </div>
              <Badge variant="count" tone="neutral">{detail.node_links.length} 个 Node</Badge>
            </div>
            <Input
              label="Node ID"
              value={linkDraft.nodeId}
              onChange={(event) => {
                setLinkDraft((current) => ({ ...current, nodeId: event.target.value }))
                setLinkError(null)
                setLinkNotice(null)
              }}
              placeholder="nd_..."
              disabled={linkControlsDisabled}
            />
            <label className="asset-operation-field asset-operation-field--wide">
              <span>关联备注</span>
              <textarea
                value={linkDraft.note}
                onChange={(event) => {
                  setLinkDraft((current) => ({ ...current, note: event.target.value }))
                  setLinkError(null)
                  setLinkNotice(null)
                }}
                placeholder="例如：主业务 Node"
                disabled={linkControlsDisabled}
              />
            </label>
            {linkError || unlinkError ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                {linkError ?? unlinkError}
              </p>
            ) : linkNotice ? (
              <p className="asset-operation-feedback" role="status">{linkNotice}</p>
            ) : null}
            <div className="asset-operation-actions">
              <Button type="submit" disabled={linkControlsDisabled}>
                {linkSubmitting ? '关联中…' : '关联 Node'}
              </Button>
            </div>
          </form>
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

      <VPSTimelinePanel timeline={timeline} />

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
