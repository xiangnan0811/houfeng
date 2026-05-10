import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Badge, Button, DataTable, Hostname, Input, MonoDigits, Timestamp, type DataTableColumn } from '../components/atoms'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  createVPSExperienceLog,
  getVPSAsset,
  getVPSTimeline,
  linkVPSNode,
  unlinkVPSNode,
  updateVPSAsset,
} from '../lib/api'
import { formatDateTime, formatOptional } from '../lib/format'
import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type CreateVPSExperienceLogInput,
  type UpdateVPSAssetInput,
  type VPSAssetDetail,
  type VPSExperienceCategory,
  type VPSExperienceSeverity,
  type VPSLifecycleStatus,
  type VPSNodeSummary,
  type VPSRenewalDecision,
  type VPSTimeline,
  type VPSUsageStatus,
} from '../lib/types'
import {
  AssetLabels,
  HealthBadge,
  LifecycleBadge,
  RenewalBadge,
  UsageBadge,
} from './assetPageBadges'
import { parseLabels } from './assetPageUtils'

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
const LIFECYCLE_OPTIONS = Object.entries(VPS_LIFECYCLE_STATUS_LABELS) as Array<[
  VPSLifecycleStatus,
  string,
]>
const USAGE_OPTIONS = Object.entries(VPS_USAGE_STATUS_LABELS) as Array<[
  VPSUsageStatus,
  string,
]>
const EXPERIENCE_CATEGORY_OPTIONS = Object.entries(VPS_EXPERIENCE_CATEGORY_LABELS) as Array<[
  VPSExperienceCategory,
  string,
]>
const EXPERIENCE_SEVERITY_OPTIONS = Object.entries(VPS_EXPERIENCE_SEVERITY_LABELS) as Array<[
  VPSExperienceSeverity,
  string,
]>

type FactEditFormState = {
  displayName: string
  providerID: string
  providerName: string
  productName: string
  orderRef: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  sshHost: string
  sshPort: string
  sshUser: string
  osName: string
  virtualization: string
  lifecycleStatus: VPSLifecycleStatus
  usageStatus: VPSUsageStatus
  importance: string
  labels: string
  note: string
}

type ExperienceDraftState = {
  category: VPSExperienceCategory
  severity: VPSExperienceSeverity
  summary: string
  details: string
  occurredAt: string
}

const INITIAL_EXPERIENCE_DRAFT: ExperienceDraftState = {
  category: 'note',
  severity: 'info',
  summary: '',
  details: '',
  occurredAt: '',
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

function detailToFactEditForm(detail: VPSAssetDetail): FactEditFormState {
  return {
    displayName: detail.display_name,
    providerID: detail.provider_id ?? '',
    providerName: detail.provider_name,
    productName: detail.product_name,
    orderRef: detail.order_ref,
    country: detail.country,
    region: detail.region,
    city: detail.city,
    datacenter: detail.datacenter,
    ipv4: detail.ipv4,
    ipv6: detail.ipv6,
    sshHost: detail.ssh_host,
    sshPort: String(detail.ssh_port),
    sshUser: detail.ssh_user,
    osName: detail.os_name,
    virtualization: detail.virtualization,
    lifecycleStatus: detail.lifecycle_status,
    usageStatus: detail.usage_status,
    importance: detail.importance,
    labels: detail.labels.join(', '),
    note: detail.note,
  }
}

function buildFactEditInput(form: FactEditFormState): UpdateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
  }
  const sshPort = Number.parseInt(form.sshPort.trim(), 10)
  if (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535) {
    throw new Error('SSH 端口必须为 1 到 65535。')
  }

  return {
    display_name: form.displayName.trim(),
    provider_id: form.providerID.trim() || null,
    provider_name: form.providerName.trim(),
    product_name: form.productName.trim(),
    order_ref: form.orderRef.trim(),
    country: form.country.trim(),
    region: form.region.trim(),
    city: form.city.trim(),
    datacenter: form.datacenter.trim(),
    ipv4: form.ipv4.trim(),
    ipv6: form.ipv6.trim(),
    ssh_host: form.sshHost.trim(),
    ssh_port: sshPort,
    ssh_user: form.sshUser.trim(),
    os_name: form.osName.trim(),
    virtualization: form.virtualization.trim(),
    lifecycle_status: form.lifecycleStatus,
    usage_status: form.usageStatus,
    importance: form.importance.trim() || 'normal',
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function buildExperienceLogInput(form: ExperienceDraftState): CreateVPSExperienceLogInput {
  const summary = form.summary.trim()
  if (!summary) {
    throw new Error('经验摘要不能为空。')
  }
  const occurredAt = form.occurredAt.trim()
  const occurredAtISO = occurredAt ? new Date(occurredAt).toISOString() : null

  return {
    category: form.category,
    severity: form.severity,
    summary,
    details: form.details.trim(),
    occurred_at: occurredAtISO,
  }
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
  const [factEditOpen, setFactEditOpen] = useState(false)
  const [factDraft, setFactDraft] = useState<FactEditFormState | null>(null)
  const [factSubmitting, setFactSubmitting] = useState(false)
  const [factError, setFactError] = useState<string | null>(null)
  const [factNotice, setFactNotice] = useState<string | null>(null)
  const [linkDraft, setLinkDraft] = useState({ nodeId: '', note: '' })
  const [linkSubmitting, setLinkSubmitting] = useState(false)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkNotice, setLinkNotice] = useState<string | null>(null)
  const [unlinkingNodeId, setUnlinkingNodeId] = useState<string | null>(null)
  const [unlinkError, setUnlinkError] = useState<string | null>(null)
  const [lifecycleConfirmingArchive, setLifecycleConfirmingArchive] = useState(false)
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState(false)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [lifecycleNotice, setLifecycleNotice] = useState<string | null>(null)
  const [experienceDraft, setExperienceDraft] = useState<ExperienceDraftState>(INITIAL_EXPERIENCE_DRAFT)
  const [experienceSubmitting, setExperienceSubmitting] = useState(false)
  const [experienceError, setExperienceError] = useState<string | null>(null)
  const [experienceNotice, setExperienceNotice] = useState<string | null>(null)

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
        setFactEditOpen(false)
        setFactDraft(detailToFactEditForm(detail))
        setFactError(null)
        setFactNotice(null)
        setLinkDraft({ nodeId: '', note: '' })
        setLinkError(null)
        setLinkNotice(null)
        setUnlinkError(null)
        setLifecycleConfirmingArchive(false)
        setLifecycleError(null)
        setLifecycleNotice(null)
        setExperienceDraft(INITIAL_EXPERIENCE_DRAFT)
        setExperienceError(null)
        setExperienceNotice(null)
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

  function toggleFactEdit(detail: VPSAssetDetail) {
    setFactEditOpen((open) => {
      const next = !open
      if (next) {
        setFactDraft(detailToFactEditForm(detail))
        setFactError(null)
        setFactNotice(null)
      }
      return next
    })
  }

  async function handleFactSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail || !factDraft) return

    setFactError(null)
    setFactNotice(null)

    let input: UpdateVPSAssetInput
    try {
      input = buildFactEditInput(factDraft)
    } catch (error: unknown) {
      setFactError(describeError(error, 'VPS 基础信息输入无效'))
      return
    }

    setFactSubmitting(true)
    try {
      await updateVPSAsset(detail.vps_id, input)
      const refreshed = await refreshDetailAndTimeline(detail.vps_id)
      setFactDraft(detailToFactEditForm(refreshed))
      setFactEditOpen(false)
      setFactNotice('基础信息已更新，资产历史已刷新')
    } catch (error: unknown) {
      setFactError(describeError(error, '更新基础信息失败'))
    } finally {
      setFactSubmitting(false)
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

  async function handleArchiveVPS() {
    const detail = state.detail
    if (!detail) return

    setLifecycleSubmitting(true)
    setLifecycleError(null)
    setLifecycleNotice(null)

    try {
      await updateVPSAsset(detail.vps_id, { lifecycle_status: 'archived' })
      const refreshed = await refreshDetailAndTimeline(detail.vps_id)
      setFactDraft(detailToFactEditForm(refreshed))
      setLifecycleConfirmingArchive(false)
      setLifecycleNotice('VPS 已归档，资产历史已刷新')
    } catch (error: unknown) {
      setLifecycleError(describeError(error, '归档 VPS 失败'))
    } finally {
      setLifecycleSubmitting(false)
    }
  }

  async function handleRestoreVPS() {
    const detail = state.detail
    if (!detail) return

    setLifecycleSubmitting(true)
    setLifecycleError(null)
    setLifecycleNotice(null)

    try {
      await updateVPSAsset(detail.vps_id, { lifecycle_status: 'idle' })
      const refreshed = await refreshDetailAndTimeline(detail.vps_id)
      setFactDraft(detailToFactEditForm(refreshed))
      setLifecycleNotice('VPS 已恢复为闲置，资产历史已刷新')
    } catch (error: unknown) {
      setLifecycleError(describeError(error, '恢复 VPS 失败'))
    } finally {
      setLifecycleSubmitting(false)
    }
  }

  async function handleExperienceSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    setExperienceError(null)
    setExperienceNotice(null)

    let input: CreateVPSExperienceLogInput
    try {
      input = buildExperienceLogInput(experienceDraft)
    } catch (error: unknown) {
      setExperienceError(describeError(error, '经验记录输入无效'))
      return
    }

    setExperienceSubmitting(true)
    try {
      await createVPSExperienceLog(detail.vps_id, input)
      await refreshDetailAndTimeline(detail.vps_id)
      setExperienceDraft(INITIAL_EXPERIENCE_DRAFT)
      setExperienceNotice('经验记录已写入资产历史')
    } catch (error: unknown) {
      setExperienceError(describeError(error, '创建经验记录失败'))
    } finally {
      setExperienceSubmitting(false)
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
  const isArchived = detail.lifecycle_status === 'archived'

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

          <div className="asset-operation-form asset-lifecycle-card">
            <div className="asset-operation-form__header">
              <div>
                <h3>生命周期</h3>
                <p>
                  {isArchived
                    ? '这台 VPS 已退出当前工作集，可恢复为闲置后重新纳入台账处理。'
                    : '归档会让 VPS 退出当前工作集，但保留基础信息、历史与 Node 关联。'}
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
            {lifecycleConfirmingArchive ? (
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
                    disabled={lifecycleSubmitting}
                    onClick={() => {
                      setLifecycleConfirmingArchive(false)
                      setLifecycleError(null)
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    type="button"
                    variant="danger"
                    disabled={lifecycleSubmitting}
                    onClick={() => void handleArchiveVPS()}
                  >
                    {lifecycleSubmitting ? '归档中…' : '确认归档'}
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
                      disabled={lifecycleSubmitting}
                      onClick={() => void handleRestoreVPS()}
                    >
                      {lifecycleSubmitting ? '恢复中…' : '恢复为闲置'}
                    </Button>
                  ) : (
                    <Button
                      type="button"
                      variant="danger"
                      disabled={lifecycleSubmitting}
                      onClick={() => {
                        setLifecycleConfirmingArchive(true)
                        setLifecycleError(null)
                        setLifecycleNotice(null)
                      }}
                    >
                      归档 VPS
                    </Button>
                  )}
                </div>
              </>
            )}
            {lifecycleError ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                {lifecycleError}
              </p>
            ) : lifecycleNotice ? (
              <p className="asset-operation-feedback" role="status">{lifecycleNotice}</p>
            ) : null}
          </div>

          <form className="asset-operation-form" onSubmit={(event) => void handleExperienceSubmit(event)}>
            <div className="asset-operation-form__header">
              <div>
                <h3>经验记录</h3>
                <p>补充这台 VPS 的稳定性、网络、账单或迁移原因。</p>
              </div>
              <Badge variant="count" tone="neutral">{timeline.experience_logs.length} 条</Badge>
            </div>
            <label className="asset-operation-field">
              <span>分类</span>
              <select
                value={experienceDraft.category}
                onChange={(event) => {
                  setExperienceDraft((current) => ({
                    ...current,
                    category: event.target.value as VPSExperienceCategory,
                  }))
                  setExperienceError(null)
                  setExperienceNotice(null)
                }}
              >
                {EXPERIENCE_CATEGORY_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label className="asset-operation-field">
              <span>级别</span>
              <select
                value={experienceDraft.severity}
                onChange={(event) => {
                  setExperienceDraft((current) => ({
                    ...current,
                    severity: event.target.value as VPSExperienceSeverity,
                  }))
                  setExperienceError(null)
                  setExperienceNotice(null)
                }}
              >
                {EXPERIENCE_SEVERITY_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <Input
              label="摘要"
              value={experienceDraft.summary}
              onChange={(event) => {
                setExperienceDraft((current) => ({ ...current, summary: event.target.value }))
                setExperienceError(null)
                setExperienceNotice(null)
              }}
              placeholder="例如：晚高峰丢包明显"
            />
            <Input
              label="发生时间"
              type="datetime-local"
              value={experienceDraft.occurredAt}
              onChange={(event) => {
                setExperienceDraft((current) => ({ ...current, occurredAt: event.target.value }))
                setExperienceError(null)
                setExperienceNotice(null)
              }}
            />
            <label className="asset-operation-field asset-operation-field--wide">
              <span>详情</span>
              <textarea
                value={experienceDraft.details}
                onChange={(event) => {
                  setExperienceDraft((current) => ({ ...current, details: event.target.value }))
                  setExperienceError(null)
                  setExperienceNotice(null)
                }}
                placeholder="例如：连续三天晚高峰 tcp probe 抖动，已向服务商提交工单"
              />
            </label>
            {experienceError ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                {experienceError}
              </p>
            ) : experienceNotice ? (
              <p className="asset-operation-feedback" role="status">{experienceNotice}</p>
            ) : null}
            <div className="asset-operation-actions">
              <Button type="submit" disabled={experienceSubmitting}>
                {experienceSubmitting ? '记录中…' : '写入经验记录'}
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
          <div className="section-heading__actions">
            <AssetLabels labels={detail.labels} />
            <Button variant={factEditOpen ? 'secondary' : 'primary'} size="sm" onClick={() => toggleFactEdit(detail)}>
              {factEditOpen ? '收起编辑' : '编辑基础信息'}
            </Button>
          </div>
        </div>
        {factError ? (
          <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
            {factError}
          </p>
        ) : factNotice ? (
          <p className="asset-operation-feedback" role="status">{factNotice}</p>
        ) : null}
        {factEditOpen && factDraft && (
          <form className="asset-facts-edit-form" onSubmit={(event) => void handleFactSubmit(event)}>
            <Input label="VPS 名称" value={factDraft.displayName} onChange={(event) => setFactDraft({ ...factDraft, displayName: event.target.value })} />
            <Input label="Provider ID" value={factDraft.providerID} onChange={(event) => setFactDraft({ ...factDraft, providerID: event.target.value })} />
            <Input label="服务商名称快照" value={factDraft.providerName} onChange={(event) => setFactDraft({ ...factDraft, providerName: event.target.value })} />
            <Input label="产品名" value={factDraft.productName} onChange={(event) => setFactDraft({ ...factDraft, productName: event.target.value })} />
            <Input label="订单号" value={factDraft.orderRef} onChange={(event) => setFactDraft({ ...factDraft, orderRef: event.target.value })} />
            <Input label="国家 / 地区" value={factDraft.country} onChange={(event) => setFactDraft({ ...factDraft, country: event.target.value })} />
            <Input label="区域" value={factDraft.region} onChange={(event) => setFactDraft({ ...factDraft, region: event.target.value })} />
            <Input label="城市" value={factDraft.city} onChange={(event) => setFactDraft({ ...factDraft, city: event.target.value })} />
            <Input label="数据中心" value={factDraft.datacenter} onChange={(event) => setFactDraft({ ...factDraft, datacenter: event.target.value })} />
            <Input label="IPv4" value={factDraft.ipv4} onChange={(event) => setFactDraft({ ...factDraft, ipv4: event.target.value })} />
            <Input label="IPv6" value={factDraft.ipv6} onChange={(event) => setFactDraft({ ...factDraft, ipv6: event.target.value })} />
            <Input label="SSH Host" value={factDraft.sshHost} onChange={(event) => setFactDraft({ ...factDraft, sshHost: event.target.value })} />
            <Input label="SSH 端口" type="number" min="1" max="65535" value={factDraft.sshPort} onChange={(event) => setFactDraft({ ...factDraft, sshPort: event.target.value })} />
            <Input label="SSH 用户" value={factDraft.sshUser} onChange={(event) => setFactDraft({ ...factDraft, sshUser: event.target.value })} />
            <Input label="操作系统" value={factDraft.osName} onChange={(event) => setFactDraft({ ...factDraft, osName: event.target.value })} />
            <Input label="虚拟化" value={factDraft.virtualization} onChange={(event) => setFactDraft({ ...factDraft, virtualization: event.target.value })} />
            <label className="input-field">
              <span className="input-field__label">生命周期</span>
              <select className="input" value={factDraft.lifecycleStatus} onChange={(event) => setFactDraft({ ...factDraft, lifecycleStatus: event.target.value as VPSLifecycleStatus })}>
                {LIFECYCLE_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label className="input-field">
              <span className="input-field__label">用途状态</span>
              <select className="input" value={factDraft.usageStatus} onChange={(event) => setFactDraft({ ...factDraft, usageStatus: event.target.value as VPSUsageStatus })}>
                {USAGE_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <Input label="重要性" value={factDraft.importance} onChange={(event) => setFactDraft({ ...factDraft, importance: event.target.value })} />
            <Input label="标签" hint="用逗号分隔" value={factDraft.labels} onChange={(event) => setFactDraft({ ...factDraft, labels: event.target.value })} />
            <Input label="备注" value={factDraft.note} onChange={(event) => setFactDraft({ ...factDraft, note: event.target.value })} />
            <div className="page-form-actions">
              <Button type="button" variant="secondary" disabled={factSubmitting} onClick={() => toggleFactEdit(detail)}>
                取消编辑
              </Button>
              <Button type="submit" disabled={factSubmitting}>
                {factSubmitting ? '保存中…' : '保存基础信息'}
              </Button>
            </div>
          </form>
        )}
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
          <DetailItem label="归档时间" value={detail.archived_at ? formatDateTime(detail.archived_at) : null} />
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
