import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { Button, Modal } from '../components/atoms'
import { VPSCancellationWorkbench } from '../components/VPSCancellationWorkbench'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  applyVPSCancellation,
  createVPSDomain,
  createVPSService,
  createVPSExperienceLog,
  getVPSAsset,
  getVPSCancellationPreview,
  getVPSTimeline,
  linkVPSNode,
  listNodes,
  listProviders,
  listSubscriptions,
  listTargets,
  listVPSDomains,
  listVPSServices,
  unlinkVPSNode,
  updateVPSAsset,
} from '../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  ApplyCancellationInput,
  CancellationPreview,
  LifecycleActionResult,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  CreateVPSExperienceLogInput,
  RenewalSubscriptionLinkage,
  SubscriptionRecord,
  UpdateVPSAssetInput,
  VPSAssetDetail,
  VPSNodeSummary,
} from '../lib/types'
import { VPSDecisionBoard } from './vps-detail/VPSDecisionBoard'
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
  INITIAL_SELECTOR_STATE,
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

async function loadCancellationPreview(targetVPSId: string): Promise<{
  cancellationPreview: CancellationPreview | null
  cancellationPreviewError: string | null
}> {
  try {
    const cancellationPreview = await getVPSCancellationPreview(targetVPSId)
    return { cancellationPreview, cancellationPreviewError: null }
  } catch (error: unknown) {
    return {
      cancellationPreview: null,
      cancellationPreviewError: describeError(error, '加载取消/退役预览失败'),
    }
  }
}

function selectPrimarySubscription(subscriptions: SubscriptionRecord[]): SubscriptionRecord | null {
  return subscriptions[0] ?? null
}

function normalizeVPSDetail(detail: VPSAssetDetail): VPSAssetDetail {
  return {
    ...detail,
    node_links: detail.node_links ?? [],
  }
}

function subscriptionLinkageNotice(linkage?: RenewalSubscriptionLinkage | null): string {
  if (!linkage || linkage.status === 'none') {
    return '续费决策已更新，资产历史已刷新'
  }
  return `续费决策已更新，资产历史已刷新。${linkage.message}`
}

function subscriptionLinkageAction(linkage: RenewalSubscriptionLinkage | null | undefined, vpsID: string): { to: string; label: string } | null {
  if (!linkage) return null
  if (linkage.status === 'no_active_subscription') {
    if (linkage.candidate_count > 0) {
      return { to: `/vps/${encodeURIComponent(vpsID)}?workbench=cancellation`, label: '打开取消/退役工作台' }
    }
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsID)}&create=1`, label: '创建该 VPS 订阅' }
  }
  if (linkage.status === 'multiple_active_subscriptions') {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsID)}`, label: '去订阅页选择处理' }
  }
  if (linkage.subscription_id) {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsID)}`, label: '查看关联订阅' }
  }
  return null
}

export function VPSDetailPage() {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const openCancellationFromQuery = searchParams.get('workbench') === 'cancellation'
  const [state, setState] = useState(INITIAL_STATE)
  const [selectors, setSelectors] = useState(INITIAL_SELECTOR_STATE)
  const [decisionDraft, setDecisionDraft] = useState<DecisionDraftState>({
    renewalDecision: 'unreviewed',
    reason: '',
  })
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [decisionAction, setDecisionAction] = useState<{ to: string; label: string } | null>(null)
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
  const [lifecycleConfirmingAction, setLifecycleConfirmingAction] = useState<'archive' | 'restore' | null>(null)
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState(false)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [lifecycleNotice, setLifecycleNotice] = useState<string | null>(null)
  const [cancellationSubmitting, setCancellationSubmitting] = useState(false)
  const [cancellationError, setCancellationError] = useState<string | null>(null)
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
      openCancellationFromQuery ? loadCancellationPreview(vpsId) : Promise.resolve({ cancellationPreview: null, cancellationPreviewError: null }),
    ])
      .then(([detail, timeline, services, domains, subscriptionState, cancellationState]) => {
        if (cancelled) return
        const normalizedDetail = normalizeVPSDetail(detail)
        setState({
          vpsId,
          error: null,
          detail: normalizedDetail,
          timeline,
          services,
          domains,
          subscriptions: subscriptionState.subscriptions,
          subscriptionsError: subscriptionState.subscriptionsError,
          cancellationPreview: cancellationState.cancellationPreview,
          cancellationPreviewError: cancellationState.cancellationPreviewError,
          cancellationResult: null,
        })
        setDecisionDraft({ renewalDecision: normalizedDetail.renewal_decision, reason: '' })
        setDecisionError(null)
        setDecisionNotice(null)
        setDecisionAction(null)
        setFactDraft(detailToFactEditForm(normalizedDetail))
        setFactError(null)
        setFactNotice(null)
        setLinkDraft({ nodeId: '', note: '' })
        setLinkError(null)
        setLinkNotice(null)
        setUnlinkError(null)
        setLifecycleConfirmingAction(null)
        setLifecycleError(null)
        setLifecycleNotice(null)
        setCancellationError(null)
        setExperienceDraft(INITIAL_EXPERIENCE_DRAFT)
        setExperienceError(null)
        setExperienceNotice(null)
        setServiceDraft(INITIAL_SERVICE_DRAFT)
        setServiceError(null)
        setServiceNotice(null)
        setDomainDraft(INITIAL_DOMAIN_DRAFT)
        setDomainError(null)
        setDomainNotice(null)
        setActiveDrawer(openCancellationFromQuery ? 'cancellation' : null)
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
          cancellationPreview: null,
          cancellationPreviewError: null,
          cancellationResult: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [openCancellationFromQuery, vpsId])

  const refreshCancellationPreview = useCallback(async (targetVPSId: string) => {
    const cancellationState = await loadCancellationPreview(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId) return current
      return {
        ...current,
        cancellationPreview: cancellationState.cancellationPreview,
        cancellationPreviewError: cancellationState.cancellationPreviewError,
      }
    })
  }, [])

  async function refreshDetail(targetVPSId: string): Promise<VPSAssetDetail> {
    const detail = normalizeVPSDetail(await getVPSAsset(targetVPSId))
    setState((current) => {
      if (current.vpsId !== targetVPSId || !current.timeline) return current
      return { ...current, error: null, detail }
    })
    return detail
  }

  async function refreshDetailAndTimeline(targetVPSId: string): Promise<VPSAssetDetail> {
    const [detailResult, timeline, services, domains, subscriptionState] = await Promise.all([
      getVPSAsset(targetVPSId),
      getVPSTimeline(targetVPSId),
      listVPSServices(targetVPSId),
      listVPSDomains(targetVPSId),
      loadSubscriptions(targetVPSId),
    ])
    const detail = normalizeVPSDetail(detailResult)
    setState((current) => {
      const keepCancellationResult = current.vpsId === targetVPSId ? current.cancellationResult : null
      const keepCancellationPreview = current.vpsId === targetVPSId ? current.cancellationPreview : null
      const keepCancellationPreviewError = current.vpsId === targetVPSId ? current.cancellationPreviewError : null
      return {
        vpsId: targetVPSId,
        error: null,
        detail,
        timeline,
        services,
        domains,
        subscriptions: subscriptionState.subscriptions,
        subscriptionsError: subscriptionState.subscriptionsError,
        cancellationPreview: keepCancellationPreview,
        cancellationPreviewError: keepCancellationPreviewError,
        cancellationResult: keepCancellationResult,
      }
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
    setDecisionAction(null)
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

  function closeLifecycleConfirmation() {
    setLifecycleConfirmingAction(null)
    setLifecycleError(null)
  }

  function ensureNodesLoaded() {
    if (selectors.nodesLoading || selectors.nodes.length > 0) return
    setSelectors((current) => ({ ...current, nodesLoading: true, nodesError: null }))
    listNodes()
      .then((nodes) => setSelectors((current) => ({ ...current, nodesLoading: false, nodesError: null, nodes })))
      .catch((error: unknown) => setSelectors((current) => ({ ...current, nodesLoading: false, nodesError: describeError(error, '加载 Node 列表失败'), nodes: [] })))
  }

  function ensureProvidersLoaded() {
    if (selectors.providersLoading || selectors.providers.length > 0) return
    setSelectors((current) => ({ ...current, providersLoading: true, providersError: null }))
    listProviders()
      .then((providers) => setSelectors((current) => ({ ...current, providersLoading: false, providersError: null, providers })))
      .catch((error: unknown) => setSelectors((current) => ({ ...current, providersLoading: false, providersError: describeError(error, '加载服务商列表失败'), providers: [] })))
  }

  function ensureTargetsLoaded() {
    if (selectors.targetsLoading || selectors.targets.length > 0) return
    setSelectors((current) => ({ ...current, targetsLoading: true, targetsError: null }))
    listTargets()
      .then((targets) => setSelectors((current) => ({ ...current, targetsLoading: false, targetsError: null, targets })))
      .catch((error: unknown) => setSelectors((current) => ({ ...current, targetsLoading: false, targetsError: describeError(error, '加载 Target 列表失败'), targets: [] })))
  }

  function openDrawer(mode: NonNullable<VPSDetailDrawerMode>) {
    if (mode === 'decision') {
      clearDecisionFeedback()
    }
    if (mode === 'node-link') {
      clearLinkFormFeedback()
      setUnlinkError(null)
      ensureNodesLoaded()
    }
    if (mode === 'experience') {
      clearExperienceFeedback()
    }
    if (mode === 'service') {
      clearServiceFeedback()
      ensureTargetsLoaded()
    }
    if (mode === 'domain') {
      clearDomainFeedback()
      ensureTargetsLoaded()
    }
    if (mode === 'cancellation') {
      setCancellationError(null)
      setState((current) => ({ ...current, cancellationResult: null }))
      if (state.detail && !state.cancellationPreview && !state.cancellationPreviewError) {
        void refreshCancellationPreview(state.detail.vps_id)
      }
    }
    setActiveDrawer(mode)
  }

  function closeDrawer() {
    if (activeDrawer === 'decision') {
      if (state.detail) {
        setDecisionDraft({ renewalDecision: state.detail.renewal_decision, reason: '' })
      }
      clearDecisionFeedback()
    }
    if (activeDrawer === 'facts') {
      if (state.detail) {
        setFactDraft(detailToFactEditForm(state.detail))
      }
      setFactError(null)
      setFactNotice(null)
    }
    if (activeDrawer === 'node-link') {
      setLinkDraft({ nodeId: '', note: '' })
      clearLinkFormFeedback()
    }
    if (activeDrawer === 'experience') {
      setExperienceDraft(INITIAL_EXPERIENCE_DRAFT)
      clearExperienceFeedback()
    }
    if (activeDrawer === 'service') {
      setServiceDraft(INITIAL_SERVICE_DRAFT)
      clearServiceFeedback()
    }
    if (activeDrawer === 'domain') {
      setDomainDraft(INITIAL_DOMAIN_DRAFT)
      clearDomainFeedback()
    }
    if (activeDrawer === 'node-evidence') {
      setUnlinkError(null)
    }
    if (activeDrawer === 'cancellation') {
      setCancellationError(null)
    }
    setActiveDrawer(null)
  }

  function openLifecycleConfirmation(action: 'archive' | 'restore') {
    setLifecycleConfirmingAction(action)
    setLifecycleError(null)
    setLifecycleNotice(null)
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
      const updated = await updateVPSAsset(detail.vps_id, {
        renewal_decision: decisionDraft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      })
      const refreshed = await refreshDetailAndTimeline(detail.vps_id)
      setDecisionDraft({ renewalDecision: refreshed.renewal_decision, reason: '' })
      setDecisionNotice(subscriptionLinkageNotice(updated.renewal_subscription_linkage))
      setDecisionAction(subscriptionLinkageAction(updated.renewal_subscription_linkage, detail.vps_id))
      setActiveDrawer(null)
    } catch (error: unknown) {
      setDecisionError(describeError(error, '更新续费决策失败'))
    } finally {
      setDecisionSubmitting(false)
    }
  }

  function openFactEdit(detail: VPSAssetDetail) {
    ensureProvidersLoaded()
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
      setLinkError('请选择要关联的 Node。')
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
      setLifecycleConfirmingAction(null)
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
      setLifecycleConfirmingAction(null)
      setLifecycleNotice('VPS 已恢复为闲置，资产历史已刷新')
    } catch (error: unknown) {
      setLifecycleError(describeError(error, '恢复 VPS 失败'))
    } finally {
      setLifecycleSubmitting(false)
    }
  }

  async function handleCancellationSubmit(input: ApplyCancellationInput) {
    const detail = state.detail
    if (!detail) return

    setCancellationSubmitting(true)
    setCancellationError(null)
    setLifecycleNotice(null)
    setLifecycleError(null)

    try {
      const result: LifecycleActionResult = await applyVPSCancellation(detail.vps_id, input)
      setState((current) => {
        if (current.vpsId !== detail.vps_id) return current
        return { ...current, cancellationResult: result }
      })
      await refreshDetailAndTimeline(detail.vps_id)
      const cancellationState = await loadCancellationPreview(detail.vps_id)
      setState((current) => {
        if (current.vpsId !== detail.vps_id) return current
        return {
          ...current,
          cancellationPreview: cancellationState.cancellationPreview,
          cancellationPreviewError: cancellationState.cancellationPreviewError,
          cancellationResult: result,
        }
      })
      setLifecycleNotice(`取消/退役动作已完成，写入 ${result.steps.length} 个审计步骤`)
    } catch (error: unknown) {
      setCancellationError(describeError(error, '执行取消/退役失败'))
    } finally {
      setCancellationSubmitting(false)
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
    if (activeDrawer === 'cancellation') return '取消/退役工作台'
    if (activeDrawer === 'facts') return '编辑基础信息'
    if (activeDrawer === 'node-link') return '关联 Node'
    if (activeDrawer === 'experience') return '经验记录'
    if (activeDrawer === 'service') return '新增服务'
    if (activeDrawer === 'domain') return '新增域名'
    if (activeDrawer === 'node-evidence') return 'Node 观测证据'
    if (activeDrawer === 'services-detail') return '服务资产详情'
    if (activeDrawer === 'domains-detail') return '域名资产详情'
    if (activeDrawer === 'timeline-detail') return '资产历史详情'
    if (activeDrawer === 'facts-detail') return '基础资料详情'
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
          onCancel={closeDrawer}
          onDraftChange={handleDecisionDraftChange}
          onFeedbackClear={clearDecisionFeedback}
          onSubmit={(event) => void handleDecisionSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'cancellation') {
      if (!state.cancellationPreview) {
        return (
          <div className="asset-cancel-workbench asset-cancel-workbench--loading">
            {state.cancellationPreviewError ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                {state.cancellationPreviewError}
              </p>
            ) : (
              <p className="asset-cancel-workbench__empty">正在加载取消/退役影响范围…</p>
            )}
            <div className="page-form-actions">
              <Button variant="secondary" onClick={closeDrawer}>关闭</Button>
              <Button variant="primary" onClick={() => void refreshCancellationPreview(detail.vps_id)}>
                重新加载
              </Button>
            </div>
          </div>
        )
      }
      return (
        <VPSCancellationWorkbench
          preview={state.cancellationPreview}
          submitting={cancellationSubmitting}
          error={cancellationError ?? state.cancellationPreviewError}
          result={state.cancellationResult}
          onCancel={closeDrawer}
          onSubmit={(input) => handleCancellationSubmit(input)}
        />
      )
    }
    if (activeDrawer === 'facts') {
      return factDraft ? (
        <VPSFactsEditForm
          draft={factDraft}
          providers={selectors.providers}
          providersLoading={selectors.providersLoading}
          providersError={selectors.providersError}
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
          nodes={selectors.nodes}
          nodesLoading={selectors.nodesLoading}
          nodesError={selectors.nodesError}
          controlsDisabled={linkControlsDisabled}
          submitting={linkSubmitting}
          error={linkError}
          notice={linkNotice}
          onCancel={closeDrawer}
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
          onCancel={closeDrawer}
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
          targets={selectors.targets}
          targetsLoading={selectors.targetsLoading}
          targetsError={selectors.targetsError}
          submitting={serviceSubmitting}
          error={serviceError}
          notice={serviceNotice}
          onCancel={closeDrawer}
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
          services={state.services}
          targets={selectors.targets}
          targetsLoading={selectors.targetsLoading}
          targetsError={selectors.targetsError}
          submitting={domainSubmitting}
          error={domainError}
          notice={domainNotice}
          onCancel={closeDrawer}
          onDraftChange={handleDomainDraftChange}
          onFeedbackClear={clearDomainFeedback}
          onSubmit={(event) => void handleDomainSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'node-evidence') {
      return (
        <VPSNodeLinksSection
          nodes={detail.node_links ?? []}
          unlinkingNodeId={unlinkingNodeId}
          linkFeedback={linkFeedback}
          linkFeedbackIsError={linkFeedbackIsError}
          onOpenLink={() => openDrawer('node-link')}
          onUnlinkNode={(node) => void handleUnlinkNode(node)}
        />
      )
    }
    if (activeDrawer === 'services-detail') {
      return (
        <VPSServicesSection
          services={state.services}
          error={serviceError}
          notice={serviceNotice}
          onCreate={() => openDrawer('service')}
        />
      )
    }
    if (activeDrawer === 'domains-detail') {
      return (
        <VPSDomainsSection
          domains={state.domains}
          error={domainError}
          notice={domainNotice}
          onCreate={() => openDrawer('domain')}
        />
      )
    }
    if (activeDrawer === 'timeline-detail') {
      return <VPSTimelinePanel timeline={timeline} />
    }
    if (activeDrawer === 'facts-detail') {
      return (
        <VPSFactsSection
          detail={detail}
          error={factError}
          notice={factNotice}
          onEdit={() => openFactEdit(detail)}
        />
      )
    }
    return null
  }

  return (
    <div className="page-stack asset-page vps-detail-page">
      <VPSDetailHero
        detail={detail}
        isArchived={isArchived}
        lifecycleSubmitting={lifecycleSubmitting}
        onDecisionEdit={() => openDrawer('decision')}
        onCancellationOpen={() => openDrawer('cancellation')}
        onFactEdit={() => openFactEdit(detail)}
        onExperienceLog={() => openDrawer('experience')}
        onNodeLink={() => openDrawer('node-link')}
        onServiceCreate={() => openDrawer('service')}
        onDomainCreate={() => openDrawer('domain')}
        onArchiveStart={() => openLifecycleConfirmation('archive')}
        onRestoreStart={() => openLifecycleConfirmation('restore')}
      />

      {state.subscriptionsError ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {state.subscriptionsError}
        </p>
      ) : null}
      {decisionNotice ? (
        <p className="asset-operation-feedback" role="status">
          {decisionNotice}
          {decisionAction ? (
            <>
              {' '}
              <Link className="text-link" to={decisionAction.to}>{decisionAction.label}</Link>
            </>
          ) : null}
        </p>
      ) : null}

      <VPSDecisionBoard
        detail={detail}
        timeline={timeline}
        primarySubscription={primarySubscription}
        subscriptionLoadFailed={subscriptionLoadFailed}
        subscriptionError={state.subscriptionsError}
        services={state.services}
        domains={state.domains}
        factNotice={factNotice}
        factError={activeDrawer === 'facts' ? null : factError}
        linkFeedback={activeDrawer === 'node-link' || activeDrawer === 'node-evidence' ? null : linkFeedback}
        linkFeedbackIsError={linkFeedbackIsError}
        serviceNotice={serviceNotice}
        serviceError={activeDrawer === 'service' ? null : serviceError}
        domainNotice={domainNotice}
        domainError={activeDrawer === 'domain' ? null : domainError}
        experienceNotice={experienceNotice}
        lifecycleNotice={lifecycleNotice}
        lifecycleError={lifecycleConfirmingAction ? null : lifecycleError}
        cancellationPreview={state.cancellationPreview}
        cancellationPreviewError={state.cancellationPreviewError}
        onDecisionEdit={() => openDrawer('decision')}
        onCancellationOpen={() => openDrawer('cancellation')}
        onFactEdit={() => openFactEdit(detail)}
        onExperienceLog={() => openDrawer('experience')}
        onNodeLink={() => openDrawer('node-link')}
        onOpenFacts={() => openDrawer('facts-detail')}
        onOpenNodeEvidence={() => openDrawer('node-evidence')}
        onOpenServices={() => openDrawer('services-detail')}
        onOpenDomains={() => openDrawer('domains-detail')}
        onOpenTimeline={() => openDrawer('timeline-detail')}
      />

      {lifecycleConfirmingAction ? (
        <VPSLifecycleCard
          detail={detail}
          action={lifecycleConfirmingAction}
          submitting={lifecycleSubmitting}
          error={lifecycleError}
          notice={lifecycleNotice}
          onCancel={closeLifecycleConfirmation}
          onArchive={() => void handleArchiveVPS()}
          onRestore={() => void handleRestoreVPS()}
        />
      ) : null}

      <Modal
        open={activeDrawer !== null}
        onClose={closeDrawer}
        title={drawerTitle()}
        ariaLabel={drawerTitle()}
        persistent={activeDrawer != null && !activeDrawer.endsWith('-detail') && activeDrawer !== 'node-evidence'}
        size={activeDrawer != null && (activeDrawer.endsWith('-detail') || activeDrawer === 'node-evidence' || activeDrawer === 'facts' || activeDrawer === 'cancellation') ? 'lg' : undefined}
        contentClassName={activeDrawer === 'cancellation' ? 'modal-content--asset-cancel' : undefined}
      >
        <div className="vps-detail-drawer">
          {renderDrawerContent()}
        </div>
      </Modal>
    </div>
  )
}
