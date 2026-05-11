import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  createVPSDomain,
  createVPSService,
  createVPSExperienceLog,
  getVPSAsset,
  getVPSTimeline,
  linkVPSNode,
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
  UpdateVPSAssetInput,
  VPSAssetDetail,
  VPSNodeSummary,
} from '../lib/types'
import { VPSAccessSummarySection } from './vps-detail/VPSAccessSummarySection'
import { VPSDetailErrorPanel } from './vps-detail/VPSDetailErrorPanel'
import { VPSDetailHero } from './vps-detail/VPSDetailHero'
import { VPSDetailLoading } from './vps-detail/VPSDetailLoading'
import { VPSDetailMissingID } from './vps-detail/VPSDetailMissingID'
import { VPSDomainsSection } from './vps-detail/VPSDomainsSection'
import { VPSFactsSection } from './vps-detail/VPSFactsSection'
import { VPSNodeLinksSection } from './vps-detail/VPSNodeLinksSection'
import { VPSOperationsPanel } from './vps-detail/VPSOperationsPanel'
import { VPSServicesSection } from './vps-detail/VPSServicesSection'
import type {
  DecisionDraftState,
  DomainDraftState,
  ExperienceDraftState,
  FactEditFormState,
  LinkDraftState,
  ServiceDraftState,
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
  const [factEditOpen, setFactEditOpen] = useState(false)
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

    Promise.all([getVPSAsset(vpsId), getVPSTimeline(vpsId), listVPSServices(vpsId), listVPSDomains(vpsId)])
      .then(([detail, timeline, services, domains]) => {
        if (cancelled) return
        setState({ vpsId, error: null, detail, timeline, services, domains })
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
    const [detail, timeline, services, domains] = await Promise.all([
      getVPSAsset(targetVPSId),
      getVPSTimeline(targetVPSId),
      listVPSServices(targetVPSId),
      listVPSDomains(targetVPSId),
    ])
    setState({ vpsId: targetVPSId, error: null, detail, timeline, services, domains })
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

  return (
    <div className="page-stack asset-page vps-detail-page">
      <VPSDetailHero detail={detail} onBack={() => navigate(-1)} />

      <VPSOperationsPanel
        detail={detail}
        timeline={timeline}
        decisionDraft={decisionDraft}
        decisionSubmitting={decisionSubmitting}
        decisionError={decisionError}
        decisionNotice={decisionNotice}
        decisionChanged={decisionChanged}
        onDecisionDraftChange={handleDecisionDraftChange}
        onDecisionFeedbackClear={clearDecisionFeedback}
        onDecisionSubmit={(event) => void handleDecisionSubmit(event)}
        linkDraft={linkDraft}
        linkControlsDisabled={linkControlsDisabled}
        linkSubmitting={linkSubmitting}
        linkFeedback={linkFeedback}
        linkFeedbackIsError={linkFeedbackIsError}
        onLinkDraftChange={handleLinkDraftChange}
        onLinkFeedbackClear={clearLinkFormFeedback}
        onLinkSubmit={(event) => void handleLinkSubmit(event)}
        isArchived={isArchived}
        lifecycleConfirmingArchive={lifecycleConfirmingArchive}
        lifecycleSubmitting={lifecycleSubmitting}
        lifecycleError={lifecycleError}
        lifecycleNotice={lifecycleNotice}
        onLifecycleConfirmingArchiveChange={handleLifecycleConfirmingArchiveChange}
        onArchive={() => void handleArchiveVPS()}
        onRestore={() => void handleRestoreVPS()}
        experienceDraft={experienceDraft}
        experienceSubmitting={experienceSubmitting}
        experienceError={experienceError}
        experienceNotice={experienceNotice}
        onExperienceDraftChange={handleExperienceDraftChange}
        onExperienceFeedbackClear={clearExperienceFeedback}
        onExperienceSubmit={(event) => void handleExperienceSubmit(event)}
      />

      <VPSFactsSection
        detail={detail}
        editOpen={factEditOpen}
        draft={factDraft}
        submitting={factSubmitting}
        error={factError}
        notice={factNotice}
        onToggleEdit={() => toggleFactEdit(detail)}
        onDraftChange={handleFactDraftChange}
        onSubmit={(event) => void handleFactSubmit(event)}
      />

      <VPSNodeLinksSection
        nodes={detail.node_links}
        unlinkingNodeId={unlinkingNodeId}
        onUnlinkNode={(node) => void handleUnlinkNode(node)}
      />

      <VPSServicesSection
        services={state.services}
        draft={serviceDraft}
        submitting={serviceSubmitting}
        error={serviceError}
        notice={serviceNotice}
        onDraftChange={handleServiceDraftChange}
        onFeedbackClear={clearServiceFeedback}
        onSubmit={(event) => void handleServiceSubmit(event)}
      />

      <VPSDomainsSection
        domains={state.domains}
        draft={domainDraft}
        submitting={domainSubmitting}
        error={domainError}
        notice={domainNotice}
        onDraftChange={handleDomainDraftChange}
        onFeedbackClear={clearDomainFeedback}
        onSubmit={(event) => void handleDomainSubmit(event)}
      />

      <VPSTimelinePanel timeline={timeline} />

      <VPSAccessSummarySection detail={detail} />
    </div>
  )
}
