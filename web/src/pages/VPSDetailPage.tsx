import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { Drawer } from '../components/atoms'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  createVPSDomain,
  createVPSService,
  createVPSExperienceLog,
  getVPSAsset,
  getVPSTimeline,
  linkVPSNode,
  listSubscriptions,
  listVPSDomains,
  listVPSServices,
  unlinkVPSNode,
  updateVPSAsset,
} from '../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  CreateVPSExperienceLogInput,
  SubscriptionRecord,
  UpdateVPSAssetInput,
  VPSAssetDetail,
  VPSNodeSummary,
} from '../lib/types'
import { VPSAccessSummarySection } from './vps-detail/VPSAccessSummarySection'
import { VPSDecisionWorkbench } from './vps-detail/VPSDecisionWorkbench'
import { VPSDetailErrorPanel } from './vps-detail/VPSDetailErrorPanel'
import { VPSDetailHero } from './vps-detail/VPSDetailHero'
import { VPSDetailLoading } from './vps-detail/VPSDetailLoading'
import { VPSDetailMissingID } from './vps-detail/VPSDetailMissingID'
import { VPSDomainsForm } from './vps-detail/VPSDomainsForm'
import { VPSDomainsSection } from './vps-detail/VPSDomainsSection'
import { VPSExperienceLogForm } from './vps-detail/VPSExperienceLogForm'
import { VPSFactsEditForm } from './vps-detail/VPSFactsEditForm'
import { VPSFactsSection } from './vps-detail/VPSFactsSection'
import { VPSLifecycleCard } from './vps-detail/VPSLifecycleCard'
import { VPSNodeLinkForm } from './vps-detail/VPSNodeLinkForm'
import { VPSNodeLinksSection } from './vps-detail/VPSNodeLinksSection'
import { VPSRenewalDecisionForm } from './vps-detail/VPSRenewalDecisionForm'
import { VPSServicesForm } from './vps-detail/VPSServicesForm'
import { VPSServicesSection } from './vps-detail/VPSServicesSection'
import type {
  DecisionDraftState,
  DomainDraftState,
  ExperienceDraftState,
  FactEditFormState,
  LinkDraftState,
  ServiceDraftState,
  VPSDetailDrawerMode,
} from './vps-detail/types'
import {
  buildDomainInput,
  buildExperienceLogInput,
  buildFactEditInput,
  buildServiceInput,
  detailToFactEditForm,
  INITIAL_DOMAIN_DRAFT,
  INITIAL_EXPERIENCE_DRAFT,
  INITIAL_SERVICE_DRAFT,
  INITIAL_STATE,
} from './vps-detail/vpsDetailHelpers'

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

async function loadSubscriptions(targetVPSId: string): Promise<{
  subscriptions: SubscriptionRecord[]
  subscriptionsError: string | null
}> {
  try {
    const subscriptions = await listSubscriptions({
      vps_id: targetVPSId,
      sort: 'renew_at',
      order: 'asc',
    })
    return { subscriptions, subscriptionsError: null }
  } catch (error: unknown) {
    return {
      subscriptions: [],
      subscriptionsError: describeError(error, '加载 VPS 订阅失败'),
    }
  }
}

function selectPrimarySubscription(subscriptions: SubscriptionRecord[]): SubscriptionRecord | null {
  return subscriptions[0] ?? null
}

export function VPSDetailPage() {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [state, setState] = useState(INITIAL_STATE)
  const [decisionDraft, setDecisionDraft] = useState<DecisionDraftState>({
    renewalDecision: 'unreviewed',
    reason: '',
  })
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [activeDrawer, setActiveDrawer] = useState<VPSDetailDrawerMode>(null)
  const [factDraft, setFactDraft] = useState<FactEditFormState | null>(null)
  const [factSubmitting, setFactSubmitting] = useState(false)
  const [factError, setFactError] = useState<string | null>(null)
  const [factNotice, setFactNotice] = useState<string | null>(null)
  const [linkDraft, setLinkDraft] = useState<LinkDraftState>({ nodeId: '', note: '' })
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
  const [serviceDraft, setServiceDraft] = useState<ServiceDraftState>(INITIAL_SERVICE_DRAFT)
  const [serviceSubmitting, setServiceSubmitting] = useState(false)
  const [serviceError, setServiceError] = useState<string | null>(null)
  const [serviceNotice, setServiceNotice] = useState<string | null>(null)
  const [domainDraft, setDomainDraft] = useState<DomainDraftState>(INITIAL_DOMAIN_DRAFT)
  const [domainSubmitting, setDomainSubmitting] = useState(false)
  const [domainError, setDomainError] = useState<string | null>(null)
  const [domainNotice, setDomainNotice] = useState<string | null>(null)

  useEffect(() => {
    if (!vpsId) {
      return
    }

    let cancelled = false

    Promise.all([
      getVPSAsset(vpsId),
      getVPSTimeline(vpsId),
      listVPSServices(vpsId),
      listVPSDomains(vpsId),
      loadSubscriptions(vpsId),
    ])
      .then(([detail, timeline, services, domains, subscriptionState]) => {
        if (cancelled) return
        setState({
          vpsId,
          error: null,
          detail,
          timeline,
          services,
          domains,
          subscriptions: subscriptionState.subscriptions,
          subscriptionsError: subscriptionState.subscriptionsError,
        })
        setDecisionDraft({ renewalDecision: detail.renewal_decision, reason: '' })
        setDecisionError(null)
        setDecisionNotice(null)
        setActiveDrawer(null)
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
        setServiceDraft(INITIAL_SERVICE_DRAFT)
        setServiceError(null)
        setServiceNotice(null)
        setDomainDraft(INITIAL_DOMAIN_DRAFT)
        setDomainError(null)
        setDomainNotice(null)
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          vpsId,
          error: describeError(error, '加载 VPS 详情失败'),
          detail: null,
          timeline: null,
          services: [],
          domains: [],
          subscriptions: [],
          subscriptionsError: null,
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
    const [detail, timeline, services, domains, subscriptionState] = await Promise.all([
      getVPSAsset(targetVPSId),
      getVPSTimeline(targetVPSId),
      listVPSServices(targetVPSId),
      listVPSDomains(targetVPSId),
      loadSubscriptions(targetVPSId),
    ])
    setState({
      vpsId: targetVPSId,
      error: null,
      detail,
      timeline,
      services,
      domains,
      subscriptions: subscriptionState.subscriptions,
      subscriptionsError: subscriptionState.subscriptionsError,
    })
    return detail
  }

  async function refreshServices(targetVPSId: string): Promise<AssetServiceRecord[]> {
    const services = await listVPSServices(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId) return current
      return { ...current, services }
    })
    return services
  }

  async function refreshDomains(targetVPSId: string): Promise<AssetDomainRecord[]> {
    const domains = await listVPSDomains(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId) return current
      return { ...current, domains }
    })
    return domains
  }

  function clearDecisionFeedback() {
    setDecisionError(null)
    setDecisionNotice(null)
  }

  function handleDecisionDraftChange(draft: DecisionDraftState) {
    setDecisionDraft(draft)
  }

  function handleFactDraftChange(draft: FactEditFormState) {
    setFactDraft(draft)
  }

  function handleLinkDraftChange(draft: LinkDraftState) {
    setLinkDraft(draft)
  }

  function clearLinkFormFeedback() {
    setLinkError(null)
    setLinkNotice(null)
  }

  function clearExperienceFeedback() {
    setExperienceError(null)
    setExperienceNotice(null)
  }

  function handleExperienceDraftChange(draft: ExperienceDraftState) {
    setExperienceDraft(draft)
  }

  function clearServiceFeedback() {
    setServiceError(null)
    setServiceNotice(null)
  }

  function handleServiceDraftChange(draft: ServiceDraftState) {
    setServiceDraft(draft)
  }

  function clearDomainFeedback() {
    setDomainError(null)
    setDomainNotice(null)
  }

  function handleDomainDraftChange(draft: DomainDraftState) {
    setDomainDraft(draft)
  }

  function handleLifecycleConfirmingArchiveChange(open: boolean) {
    setLifecycleConfirmingArchive(open)
    setLifecycleError(null)
    if (open) {
      setLifecycleNotice(null)
    }
  }

  function openDrawer(mode: NonNullable<VPSDetailDrawerMode>) {
    if (mode === 'decision') {
      clearDecisionFeedback()
    }
    if (mode === 'node-link') {
      clearLinkFormFeedback()
      setUnlinkError(null)
    }
    if (mode === 'experience') {
      clearExperienceFeedback()
    }
    if (mode === 'service') {
      clearServiceFeedback()
    }
    if (mode === 'domain') {
      clearDomainFeedback()
    }
    setActiveDrawer(mode)
  }

  function closeDrawer() {
    setActiveDrawer(null)
  }

  async function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearDecisionFeedback()

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
      setActiveDrawer(null)
    } catch (error: unknown) {
      setDecisionError(describeError(error, '更新续费决策失败'))
    } finally {
      setDecisionSubmitting(false)
    }
  }

  function openFactEdit(detail: VPSAssetDetail) {
    setFactDraft(detailToFactEditForm(detail))
    setFactError(null)
    setFactNotice(null)
    setActiveDrawer('facts')
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
      setActiveDrawer(null)
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
      setActiveDrawer(null)
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

    clearExperienceFeedback()

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
      setActiveDrawer(null)
    } catch (error: unknown) {
      setExperienceError(describeError(error, '创建经验记录失败'))
    } finally {
      setExperienceSubmitting(false)
    }
  }

  async function handleServiceSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearServiceFeedback()

    let input: CreateAssetServiceInput
    try {
      input = buildServiceInput(serviceDraft)
    } catch (error: unknown) {
      setServiceError(describeError(error, '服务输入无效'))
      return
    }

    setServiceSubmitting(true)
    try {
      await createVPSService(detail.vps_id, input)
      await refreshServices(detail.vps_id)
      setServiceDraft(INITIAL_SERVICE_DRAFT)
      setServiceNotice('服务记录已创建')
      setActiveDrawer(null)
    } catch (error: unknown) {
      setServiceError(describeError(error, '创建服务记录失败'))
    } finally {
      setServiceSubmitting(false)
    }
  }

  async function handleDomainSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearDomainFeedback()

    let input: CreateAssetDomainInput
    try {
      input = buildDomainInput(domainDraft)
    } catch (error: unknown) {
      setDomainError(describeError(error, '域名输入无效'))
      return
    }

    setDomainSubmitting(true)
    try {
      await createVPSDomain(detail.vps_id, input)
      await refreshDomains(detail.vps_id)
      setDomainDraft(INITIAL_DOMAIN_DRAFT)
      setDomainNotice('域名记录已创建')
      setActiveDrawer(null)
    } catch (error: unknown) {
      setDomainError(describeError(error, '创建域名记录失败'))
    } finally {
      setDomainSubmitting(false)
    }
  }

  if (!vpsId) {
    return <VPSDetailMissingID onBack={() => navigate(-1)} />
  }

  const currentStateReady = state.vpsId === vpsId

  if (!currentStateReady) {
    return <VPSDetailLoading />
  }

  if (state.error || !state.detail || !state.timeline) {
    return <VPSDetailErrorPanel error={state.error} onBack={() => navigate(-1)} />
  }

  const detail = state.detail
  const timeline = state.timeline
  const decisionChanged = decisionDraft.renewalDecision !== detail.renewal_decision
  const linkControlsDisabled = linkSubmitting || unlinkingNodeId !== null
  const isArchived = detail.lifecycle_status === 'archived'
  const linkFeedback = linkError ?? unlinkError ?? linkNotice
  const linkFeedbackIsError = linkError !== null || unlinkError !== null
  const primarySubscription = selectPrimarySubscription(state.subscriptions)
  const subscriptionLoadFailed = state.subscriptionsError !== null

  function drawerTitle(): string {
    if (activeDrawer === 'decision') return '续费决策'
    if (activeDrawer === 'facts') return '编辑基础信息'
    if (activeDrawer === 'node-link') return '关联 Node'
    if (activeDrawer === 'experience') return '经验记录'
    if (activeDrawer === 'service') return '新增服务'
    if (activeDrawer === 'domain') return '新增域名'
    return 'VPS 操作'
  }

  function renderDrawerContent(): ReactNode {
    if (activeDrawer === 'decision') {
      return (
        <VPSRenewalDecisionForm
          detail={detail}
          draft={decisionDraft}
          submitting={decisionSubmitting}
          error={decisionError}
          notice={decisionNotice}
          decisionChanged={decisionChanged}
          onDraftChange={handleDecisionDraftChange}
          onFeedbackClear={clearDecisionFeedback}
          onSubmit={(event) => void handleDecisionSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'facts') {
      return factDraft ? (
        <VPSFactsEditForm
          draft={factDraft}
          submitting={factSubmitting}
          error={factError}
          notice={factNotice}
          onCancel={closeDrawer}
          onDraftChange={handleFactDraftChange}
          onSubmit={(event) => void handleFactSubmit(event)}
        />
      ) : null
    }
    if (activeDrawer === 'node-link') {
      return (
        <VPSNodeLinkForm
          detail={detail}
          draft={linkDraft}
          controlsDisabled={linkControlsDisabled}
          submitting={linkSubmitting}
          error={linkError}
          notice={linkNotice}
          onDraftChange={handleLinkDraftChange}
          onFeedbackClear={clearLinkFormFeedback}
          onSubmit={(event) => void handleLinkSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'experience') {
      return (
        <VPSExperienceLogForm
          timeline={timeline}
          draft={experienceDraft}
          submitting={experienceSubmitting}
          error={experienceError}
          notice={experienceNotice}
          onDraftChange={handleExperienceDraftChange}
          onFeedbackClear={clearExperienceFeedback}
          onSubmit={(event) => void handleExperienceSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'service') {
      return (
        <VPSServicesForm
          draft={serviceDraft}
          submitting={serviceSubmitting}
          error={serviceError}
          notice={serviceNotice}
          onDraftChange={handleServiceDraftChange}
          onFeedbackClear={clearServiceFeedback}
          onSubmit={(event) => void handleServiceSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'domain') {
      return (
        <VPSDomainsForm
          draft={domainDraft}
          submitting={domainSubmitting}
          error={domainError}
          notice={domainNotice}
          onDraftChange={handleDomainDraftChange}
          onFeedbackClear={clearDomainFeedback}
          onSubmit={(event) => void handleDomainSubmit(event)}
        />
      )
    }
    return null
  }

  return (
    <div className="page-stack asset-page vps-detail-page">
      <VPSDetailHero
        detail={detail}
        onBack={() => navigate(-1)}
        onDecisionEdit={() => openDrawer('decision')}
        onFactEdit={() => openFactEdit(detail)}
      />

      <VPSDecisionWorkbench
        detail={detail}
        timeline={timeline}
        primarySubscription={primarySubscription}
        subscriptionLoadFailed={subscriptionLoadFailed}
        servicesCount={state.services.length}
        domainsCount={state.domains.length}
        onDecisionEdit={() => openDrawer('decision')}
        onExperienceLog={() => openDrawer('experience')}
        onNodeLink={() => openDrawer('node-link')}
      />

      {state.subscriptionsError ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {state.subscriptionsError}
        </p>
      ) : null}
      {decisionNotice ? (
        <p className="asset-operation-feedback" role="status">{decisionNotice}</p>
      ) : null}
      {experienceNotice ? (
        <p className="asset-operation-feedback" role="status">{experienceNotice}</p>
      ) : null}

      <VPSTimelinePanel timeline={timeline} />

      <div className="page-stack">
        <VPSLifecycleCard
          detail={detail}
          isArchived={isArchived}
          confirmingArchive={lifecycleConfirmingArchive}
          submitting={lifecycleSubmitting}
          error={lifecycleError}
          notice={lifecycleNotice}
          onArchiveConfirmOpenChange={handleLifecycleConfirmingArchiveChange}
          onArchive={() => void handleArchiveVPS()}
          onRestore={() => void handleRestoreVPS()}
        />

        <VPSFactsSection
          detail={detail}
          error={activeDrawer === 'facts' ? null : factError}
          notice={factNotice}
          onEdit={() => openFactEdit(detail)}
        />

        <VPSNodeLinksSection
          nodes={detail.node_links}
          unlinkingNodeId={unlinkingNodeId}
          linkFeedback={activeDrawer === 'node-link' ? null : linkFeedback}
          linkFeedbackIsError={linkFeedbackIsError}
          onOpenLink={() => openDrawer('node-link')}
          onUnlinkNode={(node) => void handleUnlinkNode(node)}
        />

        <VPSServicesSection
          services={state.services}
          error={activeDrawer === 'service' ? null : serviceError}
          notice={serviceNotice}
          onCreate={() => openDrawer('service')}
        />

        <VPSDomainsSection
          domains={state.domains}
          error={activeDrawer === 'domain' ? null : domainError}
          notice={domainNotice}
          onCreate={() => openDrawer('domain')}
        />

        <VPSAccessSummarySection detail={detail} />
      </div>

      <Drawer
        open={activeDrawer !== null}
        onClose={closeDrawer}
        title={drawerTitle()}
        ariaLabel={drawerTitle()}
      >
        <div className="vps-detail-drawer">
          {renderDrawerContent()}
        </div>
      </Drawer>
    </div>
  )
}
