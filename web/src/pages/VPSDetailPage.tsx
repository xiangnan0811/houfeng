import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { Button, Modal } from '../components/atoms'
import { ActionConfirmationModal } from '../components/ActionConfirmationModal'
import { VPSCancellationWorkbench } from '../components/VPSCancellationWorkbench'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  applyVPSCancellation,
  archiveVPS,
  createVPSDomain,
  createVPSMonitoringInstance,
  createVPSService,
  createVPSExperienceLog,
  createVPSSubscription,
  extendVPSValidity,
  getVPSArchiveReview,
  getVPSAsset,
  getVPSCancellationPreview,
  getVPSIPQuality,
  getVPSTimeline,
  linkVPSMonitoringInstance,
  listMonitoringInstances,
  listProviders,
  listSubscriptions,
  listTargets,
  listVPSDomains,
  listVPSServices,
  unlinkVPSMonitoringInstance,
  updateVPSAsset,
} from '../lib/api'
import type {
  ArchiveReview,
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
  VPSIPQualityReport,
  VPSMonitoringInstanceSummary,
} from '../lib/types'
import { VPSContextActionPanel } from './vps-detail/VPSContextActionPanel'
import { VPSDetailErrorPanel } from './vps-detail/VPSDetailErrorPanel'
import { VPSDetailLoading } from './vps-detail/VPSDetailLoading'
import { VPSDetailMissingID } from './vps-detail/VPSDetailMissingID'
import { VPSDetailOverviewPanel } from './vps-detail/VPSDetailOverviewPanel'
import { VPSDomainsForm } from './vps-detail/VPSDomainsForm'
import { VPSDomainsSection } from './vps-detail/VPSDomainsSection'
import { VPSExperienceLogForm } from './vps-detail/VPSExperienceLogForm'
import { VPSFactsEditForm } from './vps-detail/VPSFactsEditForm'
import { VPSFactsSection } from './vps-detail/VPSFactsSection'
import { VPSIPQualitySection } from './vps-detail/VPSIPQualitySection'
import { VPSMonitoringInstanceCreateForm } from './vps-detail/VPSMonitoringInstanceCreateForm'
import { VPSMonitoringInstanceLinkForm } from './vps-detail/VPSMonitoringInstanceLinkForm'
import { VPSMonitoringInstanceLinksSection } from './vps-detail/VPSMonitoringInstanceLinksSection'
import { VPSRenewalDecisionForm } from './vps-detail/VPSRenewalDecisionForm'
import { VPSRelatedOverview } from './vps-detail/VPSRelatedOverview'
import { VPSSingleMachineLedger } from './vps-detail/VPSSingleMachineLedger'
import { VPSSubscriptionForm } from './vps-detail/VPSSubscriptionForm'
import { VPSValidityExtensionForm } from './vps-detail/VPSValidityExtensionForm'
import { VPSServicesForm } from './vps-detail/VPSServicesForm'
import { VPSServicesSection } from './vps-detail/VPSServicesSection'
import { vpsLifecycleConfirmationCopy } from './vps-detail/vpsLifecycleConfirmationCopy'
import { buildVPSDetailOverviewModel } from './vps-detail/vpsDetailOverviewModel'
import type {
  DecisionDraftState,
  DomainDraftState,
  ExperienceDraftState,
  FactEditFormState,
  LinkDraftState,
  MonitoringInstanceCreateDraftState,
  ServiceDraftState,
  SubscriptionDraftState,
  ValidityExtensionDraftState,
  VPSDetailDrawerMode,
} from './vps-detail/types'
import {
  buildMonitoringInstanceCreateInput,
  buildSubscriptionInput,
  buildValidityExtensionInput,
  buildDomainInput,
  buildExperienceLogInput,
  buildFactEditInput,
  buildServiceInput,
  detailToFactEditForm,
  INITIAL_DOMAIN_DRAFT,
  INITIAL_EXPERIENCE_DRAFT,
  INITIAL_SELECTOR_STATE,
  INITIAL_SERVICE_DRAFT,
  INITIAL_SUBSCRIPTION_DRAFT,
  INITIAL_VALIDITY_EXTENSION_DRAFT,
  INITIAL_STATE,
  monitoringInstanceCreateDraftFromDetail,
} from './vps-detail/vpsDetailHelpers'

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

type PageFeedbackItem = {
  key: string
  message: string
  error?: boolean
  action?: { to: string; label: string } | null
}

function isPageFeedbackItem(item: PageFeedbackItem | null): item is PageFeedbackItem {
  return item !== null
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

async function loadIPQuality(targetVPSId: string, detail: VPSAssetDetail): Promise<{
  ipQuality: VPSIPQualityReport | null
  ipQualityError: string | null
}> {
  if (!detail.ip_quality_summary) {
    return {
      ipQuality: {
        summary: null,
        latest_report: null,
        provider_results: [],
        service_unlocks: [],
        history: [],
      },
      ipQualityError: null,
    }
  }
  try {
    const ipQuality = await getVPSIPQuality(targetVPSId)
    return { ipQuality, ipQualityError: null }
  } catch (error: unknown) {
    return {
      ipQuality: {
        summary: detail.ip_quality_summary,
        latest_report: null,
        provider_results: [],
        service_unlocks: [],
        history: [],
      },
      ipQualityError: describeError(error, '加载 IP 质量报告失败'),
    }
  }
}

function selectPrimarySubscription(subscriptions: SubscriptionRecord[]): SubscriptionRecord | null {
  return subscriptions[0] ?? null
}

function selectActiveSubscription(subscriptions: SubscriptionRecord[]): SubscriptionRecord | null {
  return subscriptions.filter((subscription) => subscription.status === 'active')[0] ?? null
}

function normalizeVPSDetail(detail: VPSAssetDetail): VPSAssetDetail {
  return {
    ...detail,
    monitoring_instance_links: detail.monitoring_instance_links ?? [],
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
      return { to: `/vps/${encodeURIComponent(vpsID)}?workbench=cancellation`, label: '打开取消/退役' }
    }
    return { to: `/vps/${encodeURIComponent(vpsID)}?workbench=subscription`, label: '创建/更新订阅' }
  }
  if (linkage.status === 'multiple_active_subscriptions') {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsID)}`, label: '去订阅页选择处理' }
  }
  if (linkage.subscription_id) {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsID)}`, label: '查看关联订阅' }
  }
  return null
}

function drawerModeFromWorkbenchQuery(value: string | null): VPSDetailDrawerMode {
  if (value === 'cancellation') return 'cancellation'
  if (value === 'subscription') return 'subscription'
  if (value === 'monitoring' || value === 'monitoring-instance-create') return 'monitoring-instance-create'
  return null
}

function shouldExposeCancellationWorkbench(detail: VPSAssetDetail, preview: CancellationPreview | null): boolean {
  return detail.renewal_decision === 'migrate' ||
    detail.renewal_decision === 'cancel' ||
    detail.renewal_decision === 'auto_renew_cancelled' ||
    detail.lifecycle_status === 'to_migrate' ||
    detail.lifecycle_status === 'to_cancel' ||
    detail.lifecycle_status === 'cancelled' ||
    detail.lifecycle_status === 'archived' ||
    Boolean(preview && ((preview.warnings ?? []).length > 0 || (preview.blockers ?? []).length > 0))
}

export function VPSDetailPage() {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const initialDrawerFromQuery = drawerModeFromWorkbenchQuery(searchParams.get('workbench'))
  const openCancellationFromQuery = initialDrawerFromQuery === 'cancellation'
  const skipNextQueryDrivenReload = useRef(false)
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
  const [linkDraft, setLinkDraft] = useState<LinkDraftState>({ monitoringInstanceId: '', note: '' })
  const [linkSubmitting, setLinkSubmitting] = useState(false)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkNotice, setLinkNotice] = useState<string | null>(null)
  const [monitoringCreateSubmitting, setMonitoringCreateSubmitting] = useState(false)
  const [monitoringCreateError, setMonitoringCreateError] = useState<string | null>(null)
  const [monitoringCreateNotice, setMonitoringCreateNotice] = useState<string | null>(null)
  const [monitoringCreateDraft, setMonitoringCreateDraft] = useState<MonitoringInstanceCreateDraftState | null>(null)
  const [subscriptionDraft, setSubscriptionDraft] = useState<SubscriptionDraftState>(INITIAL_SUBSCRIPTION_DRAFT)
  const [subscriptionSubmitting, setSubscriptionSubmitting] = useState(false)
  const [subscriptionError, setSubscriptionError] = useState<string | null>(null)
  const [subscriptionNotice, setSubscriptionNotice] = useState<string | null>(null)
  const [validityExtensionDraft, setValidityExtensionDraft] = useState<ValidityExtensionDraftState>(INITIAL_VALIDITY_EXTENSION_DRAFT)
  const [validityExtensionSubmitting, setValidityExtensionSubmitting] = useState(false)
  const [validityExtensionError, setValidityExtensionError] = useState<string | null>(null)
  const [validityExtensionNotice, setValidityExtensionNotice] = useState<string | null>(null)
  const [unlinkingMonitoringInstanceId, setUnlinkingMonitoringInstanceId] = useState<string | null>(null)
  const [unlinkError, setUnlinkError] = useState<string | null>(null)
  const [pendingUnlinkMonitoringInstance, setPendingUnlinkMonitoringInstance] = useState<VPSMonitoringInstanceSummary | null>(null)
  const [lifecycleConfirmingAction, setLifecycleConfirmingAction] = useState<'archive' | 'restore' | null>(null)
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState(false)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [lifecycleNotice, setLifecycleNotice] = useState<string | null>(null)
  const [archiveReview, setArchiveReview] = useState<ArchiveReview | null>(null)
  const [archiveReviewLoading, setArchiveReviewLoading] = useState(false)
  const [archiveConfirmationName, setArchiveConfirmationName] = useState('')
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

  function clearWorkbenchQueryParam() {
    if (!searchParams.has('workbench')) return
    const next = new URLSearchParams(searchParams)
    next.delete('workbench')
    skipNextQueryDrivenReload.current = true
    setSearchParams(next, { replace: true })
  }

  function collapseDrawer() {
    setActiveDrawer(null)
    clearWorkbenchQueryParam()
  }

  useEffect(() => {
    if (!vpsId) {
      return
    }
    if (skipNextQueryDrivenReload.current) {
      skipNextQueryDrivenReload.current = false
      return
    }

    let cancelled = false

    getVPSAsset(vpsId)
      .then(async (detail) => {
        const normalizedDetail = normalizeVPSDetail(detail)
        const [timeline, services, domains, subscriptionState, ipQualityState, cancellationState] = await Promise.all([
          getVPSTimeline(vpsId),
          listVPSServices(vpsId),
          listVPSDomains(vpsId),
          loadSubscriptions(vpsId),
          loadIPQuality(vpsId, normalizedDetail),
          openCancellationFromQuery ? loadCancellationPreview(vpsId) : Promise.resolve({ cancellationPreview: null, cancellationPreviewError: null }),
        ])
        return { normalizedDetail, timeline, services, domains, subscriptionState, ipQualityState, cancellationState }
      })
      .then(({ normalizedDetail, timeline, services, domains, subscriptionState, ipQualityState, cancellationState }) => {
        if (cancelled) return
        if (normalizedDetail.lifecycle_status === 'archived' || normalizedDetail.lifecycle_status === 'cancelled') {
          navigate(`/archive/${encodeURIComponent(normalizedDetail.vps_id)}`, { replace: true })
          return
        }
        setState({
          vpsId,
          error: null,
          detail: normalizedDetail,
          timeline,
          services,
          domains,
          subscriptions: subscriptionState.subscriptions,
          subscriptionsError: subscriptionState.subscriptionsError,
          ipQuality: ipQualityState.ipQuality,
          ipQualityError: ipQualityState.ipQualityError,
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
        setLinkDraft({ monitoringInstanceId: '', note: '' })
        setLinkError(null)
        setLinkNotice(null)
        setMonitoringCreateError(null)
        setMonitoringCreateNotice(null)
        setMonitoringCreateDraft(monitoringInstanceCreateDraftFromDetail(normalizedDetail))
        setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
        setSubscriptionError(null)
        setSubscriptionNotice(null)
        setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
        setValidityExtensionError(null)
        setValidityExtensionNotice(null)
        setUnlinkError(null)
        setPendingUnlinkMonitoringInstance(null)
        setLifecycleConfirmingAction(null)
        setLifecycleError(null)
        setLifecycleNotice(null)
        setArchiveReview(null)
        setArchiveReviewLoading(false)
        setArchiveConfirmationName('')
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
        const initialMonitoringLinks = normalizedDetail.monitoring_instance_links ?? []
        if (initialDrawerFromQuery === 'monitoring-instance-create' && initialMonitoringLinks.length === 1) {
          navigate(`/monitoring/${encodeURIComponent(initialMonitoringLinks[0].monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(normalizedDetail.vps_id)}`)
          return
        }
        if (initialDrawerFromQuery === 'monitoring-instance-create' && initialMonitoringLinks.length > 1) {
          setActiveDrawer('monitoring-instance-evidence')
          return
        }
        setActiveDrawer(initialDrawerFromQuery)
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
          ipQuality: null,
          ipQualityError: null,
          cancellationPreview: null,
          cancellationPreviewError: null,
          cancellationResult: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [initialDrawerFromQuery, navigate, openCancellationFromQuery, vpsId])

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
    const detailResult = normalizeVPSDetail(await getVPSAsset(targetVPSId))
    const [timeline, services, domains, subscriptionState, ipQualityState] = await Promise.all([
      getVPSTimeline(targetVPSId),
      listVPSServices(targetVPSId),
      listVPSDomains(targetVPSId),
      loadSubscriptions(targetVPSId),
      loadIPQuality(targetVPSId, detailResult),
    ])
    setState((current) => {
      const keepCancellationResult = current.vpsId === targetVPSId ? current.cancellationResult : null
      const keepCancellationPreview = current.vpsId === targetVPSId ? current.cancellationPreview : null
      const keepCancellationPreviewError = current.vpsId === targetVPSId ? current.cancellationPreviewError : null
      return {
        vpsId: targetVPSId,
        error: null,
        detail: detailResult,
        timeline,
        services,
        domains,
        subscriptions: subscriptionState.subscriptions,
        subscriptionsError: subscriptionState.subscriptionsError,
        ipQuality: ipQualityState.ipQuality,
        ipQualityError: ipQualityState.ipQualityError,
        cancellationPreview: keepCancellationPreview,
        cancellationPreviewError: keepCancellationPreviewError,
        cancellationResult: keepCancellationResult,
      }
    })
    return detailResult
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

  function clearMonitoringCreateFeedback() {
    setMonitoringCreateError(null)
    setMonitoringCreateNotice(null)
  }

  function handleMonitoringCreateDraftChange(draft: MonitoringInstanceCreateDraftState) {
    setMonitoringCreateDraft(draft)
  }

  function clearSubscriptionFeedback() {
    setSubscriptionError(null)
    setSubscriptionNotice(null)
  }

  function handleSubscriptionDraftChange(draft: SubscriptionDraftState) {
    setSubscriptionDraft(draft)
  }

  function clearValidityExtensionFeedback() {
    setValidityExtensionError(null)
    setValidityExtensionNotice(null)
  }

  function handleValidityExtensionDraftChange(draft: ValidityExtensionDraftState) {
    setValidityExtensionDraft(draft)
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
    setArchiveReview(null)
    setArchiveReviewLoading(false)
    setArchiveConfirmationName('')
  }

  function ensureMonitoringInstancesLoaded() {
    if (selectors.monitoringInstancesLoading || selectors.monitoring.length > 0) return
    setSelectors((current) => ({ ...current, monitoringInstancesLoading: true, monitoringInstancesError: null }))
    listMonitoringInstances()
      .then((monitoring) => setSelectors((current) => ({ ...current, monitoringInstancesLoading: false, monitoringInstancesError: null, monitoring })))
      .catch((error: unknown) => setSelectors((current) => ({
        ...current,
        monitoringInstancesLoading: false,
        monitoringInstancesError: describeError(error, '加载监控实例列表失败'),
        monitoring: [],
      })))
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
    if (mode === 'monitoring-instance-link') {
      clearLinkFormFeedback()
      setUnlinkError(null)
      setPendingUnlinkMonitoringInstance(null)
      ensureMonitoringInstancesLoaded()
    }
    if (mode === 'monitoring-instance-create') {
      clearMonitoringCreateFeedback()
      setUnlinkError(null)
      setPendingUnlinkMonitoringInstance(null)
      if (state.detail) {
        setMonitoringCreateDraft(monitoringInstanceCreateDraftFromDetail(state.detail))
      }
    }
    if (mode === 'subscription') {
      clearSubscriptionFeedback()
    }
    if (mode === 'validity-extension') {
      clearValidityExtensionFeedback()
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

  function openMonitoringAgentWorkbench() {
    const detail = state.detail
    if (!detail) return
    const activeLinks = detail.monitoring_instance_links ?? []
    if (activeLinks.length === 0) {
      openDrawer('monitoring-instance-create')
      return
    }
    if (activeLinks.length === 1) {
      navigate(`/monitoring/${encodeURIComponent(activeLinks[0].monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(detail.vps_id)}`)
      return
    }
    openDrawer('monitoring-instance-evidence')
  }

  function openMonitoringAgentWorkbenchFor(monitoringInstance: VPSMonitoringInstanceSummary) {
    const detail = state.detail
    if (!detail) return
    navigate(`/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(detail.vps_id)}`)
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
    if (activeDrawer === 'monitoring-instance-link') {
      setLinkDraft({ monitoringInstanceId: '', note: '' })
      clearLinkFormFeedback()
    }
    if (activeDrawer === 'monitoring-instance-create') {
      clearMonitoringCreateFeedback()
      if (state.detail) {
        setMonitoringCreateDraft(monitoringInstanceCreateDraftFromDetail(state.detail))
      }
    }
    if (activeDrawer === 'subscription') {
      setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
      clearSubscriptionFeedback()
    }
    if (activeDrawer === 'validity-extension') {
      setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
      clearValidityExtensionFeedback()
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
    if (activeDrawer === 'monitoring-instance-evidence') {
      setUnlinkError(null)
      setPendingUnlinkMonitoringInstance(null)
    }
    if (activeDrawer === 'cancellation') {
      setCancellationError(null)
    }
    collapseDrawer()
  }

  function openLifecycleConfirmation(action: 'archive' | 'restore') {
    setLifecycleConfirmingAction(action)
    setLifecycleError(null)
    setLifecycleNotice(null)
    setArchiveConfirmationName('')
    if (action !== 'archive') {
      setArchiveReview(null)
      setArchiveReviewLoading(false)
      return
    }
    const detail = state.detail
    if (!detail) return
    setArchiveReview(null)
    setArchiveReviewLoading(true)
    getVPSArchiveReview(detail.vps_id)
      .then((review) => {
        setArchiveReview(review)
        setLifecycleError(null)
      })
      .catch((error: unknown) => {
        setArchiveReview(null)
        setLifecycleError(describeError(error, '加载归档资格失败'))
      })
      .finally(() => setArchiveReviewLoading(false))
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
      collapseDrawer()
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
      collapseDrawer()
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

    const monitoringInstanceId = linkDraft.monitoringInstanceId.trim()
    if (!monitoringInstanceId) {
      setLinkError('请选择要关联的监控实例。')
      setLinkNotice(null)
      return
    }

    setLinkSubmitting(true)
    setLinkError(null)
    setLinkNotice(null)
    setUnlinkError(null)

    try {
      await linkVPSMonitoringInstance(detail.vps_id, {
        monitoring_instance_id: monitoringInstanceId,
        note: linkDraft.note.trim(),
      })
      await refreshDetail(detail.vps_id)
      setLinkDraft({ monitoringInstanceId: '', note: '' })
      setLinkNotice('监控实例关联已更新')
      collapseDrawer()
    } catch (error: unknown) {
      setLinkError(describeError(error, '关联监控实例失败'))
    } finally {
      setLinkSubmitting(false)
    }
  }

  async function handleMonitoringInstanceCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    setMonitoringCreateSubmitting(true)
    setMonitoringCreateError(null)
    setMonitoringCreateNotice(null)
    setUnlinkError(null)

    try {
      const input = buildMonitoringInstanceCreateInput(monitoringCreateDraft ?? monitoringInstanceCreateDraftFromDetail(detail))
      const created = await createVPSMonitoringInstance(detail.vps_id, input)
      await refreshDetail(detail.vps_id)
      setMonitoringCreateNotice('监控实例已创建并关联，正在进入接入流程')
      collapseDrawer()
      navigate(`/monitoring/${created.monitoring_instance_id}?onboarding=1&return_vps=${encodeURIComponent(detail.vps_id)}`)
    } catch (error: unknown) {
      setMonitoringCreateError(describeError(error, '创建监控实例失败'))
    } finally {
      setMonitoringCreateSubmitting(false)
    }
  }

  async function handleSubscriptionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearSubscriptionFeedback()

    let input
    try {
      input = buildSubscriptionInput(subscriptionDraft)
    } catch (error: unknown) {
      setSubscriptionError(describeError(error, '订阅输入无效'))
      return
    }

    setSubscriptionSubmitting(true)
    try {
      const subscription = await createVPSSubscription(detail.vps_id, input)
      setState((current) => {
        if (current.vpsId !== detail.vps_id) return current
        return {
          ...current,
          subscriptions: [
            subscription,
            ...current.subscriptions.filter((item) => item.subscription_id !== subscription.subscription_id),
          ],
          subscriptionsError: null,
        }
      })
      setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
      setSubscriptionNotice('订阅账单事实已创建')
      collapseDrawer()
    } catch (error: unknown) {
      setSubscriptionError(describeError(error, '创建订阅失败'))
    } finally {
      setSubscriptionSubmitting(false)
    }
  }

  async function handleValidityExtensionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearValidityExtensionFeedback()

    let input
    try {
      input = buildValidityExtensionInput(validityExtensionDraft)
    } catch (error: unknown) {
      setValidityExtensionError(describeError(error, '有效期延长输入无效'))
      return
    }
    if (activeSubscription?.renew_at && input.extend_to < activeSubscription.renew_at) {
      setValidityExtensionError('延长至日期不能早于当前 active 订阅续费日。')
      return
    }

    setValidityExtensionSubmitting(true)
    try {
      const result = await extendVPSValidity(detail.vps_id, input)
      await refreshDetailAndTimeline(detail.vps_id)
      setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
      setValidityExtensionNotice(`有效期已延长，写入 ${result.steps.length} 个审计步骤`)
      collapseDrawer()
    } catch (error: unknown) {
      setValidityExtensionError(describeError(error, '延长有效期失败'))
    } finally {
      setValidityExtensionSubmitting(false)
    }
  }

  async function handleUnlinkMonitoringInstance(monitoringInstance: VPSMonitoringInstanceSummary) {
    const detail = state.detail
    if (!detail) return

    setUnlinkingMonitoringInstanceId(monitoringInstance.monitoring_instance_id)
    setUnlinkError(null)
    setLinkError(null)
    setLinkNotice(null)

    try {
      await unlinkVPSMonitoringInstance(detail.vps_id, {
        monitoring_instance_id: monitoringInstance.monitoring_instance_id,
        note: monitoringInstance.note,
      })
      await refreshDetail(detail.vps_id)
      setLinkNotice('监控实例关联已解除')
      setPendingUnlinkMonitoringInstance(null)
    } catch (error: unknown) {
      setUnlinkError(describeError(error, '解除监控实例关联失败'))
    } finally {
      setUnlinkingMonitoringInstanceId(null)
    }
  }

  function requestUnlinkMonitoringInstance(monitoringInstance: VPSMonitoringInstanceSummary) {
    setUnlinkError(null)
    setLinkError(null)
    setLinkNotice(null)
    setPendingUnlinkMonitoringInstance(monitoringInstance)
  }

  function cancelUnlinkMonitoringInstance() {
    setPendingUnlinkMonitoringInstance(null)
    setUnlinkError(null)
  }

  async function handleArchiveVPS() {
    const detail = state.detail
    if (!detail) return
    if (archiveReviewLoading) return
    if (!archiveReview) {
      setLifecycleError('归档资格尚未加载完成')
      return
    }
    if (!archiveReview.eligible || archiveReview.blockers.length > 0) {
      setLifecycleError('仍有归档阻止项，不能归档')
      return
    }
    const confirmationName = archiveConfirmationName.trim()
    if (confirmationName !== detail.display_name.trim()) {
      setLifecycleError('请输入完整 VPS 展示名后再确认归档')
      return
    }

    setLifecycleSubmitting(true)
    setLifecycleError(null)
    setLifecycleNotice(null)

    try {
      await archiveVPS(detail.vps_id, { confirmation_name: confirmationName })
      navigate(`/archive/${encodeURIComponent(detail.vps_id)}`, { replace: true })
    } catch (error: unknown) {
      setLifecycleError(describeError(error, '归档 VPS 失败'))
    } finally {
      setLifecycleSubmitting(false)
    }
  }

  async function handleRestoreVPS() {
    const detail = state.detail
    if (!detail) return
    if (detail.lifecycle_status === 'archived') {
      navigate(`/archive/${encodeURIComponent(detail.vps_id)}`, { replace: true })
      return
    }
    setLifecycleError('归档恢复请在归档详情页执行')
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
      collapseDrawer()
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
      collapseDrawer()
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
      collapseDrawer()
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
  const linkControlsDisabled = linkSubmitting || unlinkingMonitoringInstanceId !== null
  const isArchived = detail.lifecycle_status === 'archived'
  const linkFeedback = linkError ?? unlinkError ?? linkNotice
  const linkFeedbackIsError = linkError !== null || unlinkError !== null
  const primarySubscription = selectPrimarySubscription(state.subscriptions)
  const activeSubscription = selectActiveSubscription(state.subscriptions)
  const subscriptionLoadFailed = state.subscriptionsError !== null
  const showCancellationWorkbench = shouldExposeCancellationWorkbench(detail, state.cancellationPreview)
  const overviewModel = buildVPSDetailOverviewModel({
    detail,
    timeline,
    primarySubscription,
    activeSubscription,
    subscriptionLoadFailed,
    subscriptionError: state.subscriptionsError,
    services: state.services,
    domains: state.domains,
    ipQuality: state.ipQuality,
    ipQualityError: state.ipQualityError,
    cancellationAttention: showCancellationWorkbench,
  })
  const archiveBlockers = archiveReview?.blockers ?? []
  const archiveCanConfirm = lifecycleConfirmingAction === 'archive' &&
    !archiveReviewLoading &&
    Boolean(archiveReview?.eligible) &&
    archiveBlockers.length === 0 &&
    archiveConfirmationName.trim() === detail.display_name.trim()
  const lifecycleConfirmDisabled = lifecycleSubmitting ||
    (lifecycleConfirmingAction === 'archive' ? !archiveCanConfirm : false)

  function drawerTitle(): string {
    if (activeDrawer === 'decision') return '调整决策'
    if (activeDrawer === 'cancellation') return '取消/退役'
    if (activeDrawer === 'facts') return '编辑基础资料'
    if (activeDrawer === 'subscription') return '创建/更新订阅'
    if (activeDrawer === 'validity-extension') return '延长有效期'
    if (activeDrawer === 'monitoring-instance-create') return '接入/升级 agent'
    if (activeDrawer === 'monitoring-instance-link') return '关联已有监控实例'
    if (activeDrawer === 'experience') return '记录经验'
    if (activeDrawer === 'service') return '新增服务'
    if (activeDrawer === 'domain') return '新增域名'
    if (activeDrawer === 'monitoring-instance-evidence') return '监控观测'
    if (activeDrawer === 'services-detail') return '服务详情'
    if (activeDrawer === 'domains-detail') return '域名详情'
    if (activeDrawer === 'timeline-detail') return '资产历史'
    if (activeDrawer === 'facts-detail') return '基础资料'
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
    if (activeDrawer === 'monitoring-instance-link') {
      return (
        <VPSMonitoringInstanceLinkForm
          detail={detail}
          draft={linkDraft}
          monitoring={selectors.monitoring}
          monitoringInstancesLoading={selectors.monitoringInstancesLoading}
          monitoringInstancesError={selectors.monitoringInstancesError}
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
    if (activeDrawer === 'monitoring-instance-create') {
      return monitoringCreateDraft ? (
        <VPSMonitoringInstanceCreateForm
          detail={detail}
          draft={monitoringCreateDraft}
          submitting={monitoringCreateSubmitting}
          error={monitoringCreateError}
          notice={monitoringCreateNotice}
          onCancel={closeDrawer}
          onDraftChange={handleMonitoringCreateDraftChange}
          onFeedbackClear={clearMonitoringCreateFeedback}
          onSubmit={(event) => void handleMonitoringInstanceCreateSubmit(event)}
        />
      ) : null
    }
    if (activeDrawer === 'subscription') {
      return (
        <VPSSubscriptionForm
          detail={detail}
          draft={subscriptionDraft}
          submitting={subscriptionSubmitting}
          error={subscriptionError}
          notice={subscriptionNotice}
          onCancel={closeDrawer}
          onDraftChange={handleSubscriptionDraftChange}
          onFeedbackClear={clearSubscriptionFeedback}
          onSubmit={(event) => void handleSubscriptionSubmit(event)}
        />
      )
    }
    if (activeDrawer === 'validity-extension') {
      return (
        <VPSValidityExtensionForm
          detail={detail}
          activeSubscription={activeSubscription}
          draft={validityExtensionDraft}
          submitting={validityExtensionSubmitting}
          error={validityExtensionError}
          notice={validityExtensionNotice}
          onCancel={closeDrawer}
          onDraftChange={handleValidityExtensionDraftChange}
          onFeedbackClear={clearValidityExtensionFeedback}
          onSubmit={(event) => void handleValidityExtensionSubmit(event)}
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
    if (activeDrawer === 'monitoring-instance-evidence') {
      return (
        <VPSMonitoringInstanceLinksSection
          monitoring={detail.monitoring_instance_links ?? []}
          unlinkingMonitoringInstanceId={unlinkingMonitoringInstanceId}
          pendingUnlinkMonitoringInstance={pendingUnlinkMonitoringInstance}
          linkFeedback={linkFeedback}
          linkFeedbackIsError={linkFeedbackIsError}
          onCreateMonitoringInstance={openMonitoringAgentWorkbench}
          onOpenLink={() => openDrawer('monitoring-instance-link')}
          onUpgradeMonitoringInstance={openMonitoringAgentWorkbenchFor}
          onRequestUnlinkMonitoringInstance={requestUnlinkMonitoringInstance}
          onCancelUnlinkMonitoringInstance={cancelUnlinkMonitoringInstance}
          onConfirmUnlinkMonitoringInstance={(monitoringInstance) => void handleUnlinkMonitoringInstance(monitoringInstance)}
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

  const pageFeedbackCandidates: Array<PageFeedbackItem | null> = [
    state.subscriptionsError ? { key: 'subscriptions-error', message: state.subscriptionsError, error: true } : null,
    decisionNotice ? { key: 'decision', message: decisionNotice, action: decisionAction } : null,
    activeDrawer === 'facts' || !factError ? null : { key: 'fact-error', message: factError, error: true },
    factNotice ? { key: 'fact-notice', message: factNotice } : null,
    activeDrawer === 'monitoring-instance-link' || activeDrawer === 'monitoring-instance-evidence' || !linkFeedback
      ? null
      : { key: 'link-feedback', message: linkFeedback, error: linkFeedbackIsError },
    activeDrawer === 'service' || !serviceError ? null : { key: 'service-error', message: serviceError, error: true },
    serviceNotice ? { key: 'service-notice', message: serviceNotice } : null,
    activeDrawer === 'domain' || !domainError ? null : { key: 'domain-error', message: domainError, error: true },
    domainNotice ? { key: 'domain-notice', message: domainNotice } : null,
    experienceNotice ? { key: 'experience-notice', message: experienceNotice } : null,
    activeDrawer === 'subscription' || !subscriptionError ? null : { key: 'subscription-error', message: subscriptionError, error: true },
    subscriptionNotice ? { key: 'subscription-notice', message: subscriptionNotice } : null,
    activeDrawer === 'validity-extension' || !validityExtensionError ? null : { key: 'validity-error', message: validityExtensionError, error: true },
    validityExtensionNotice ? { key: 'validity-notice', message: validityExtensionNotice } : null,
    activeDrawer === 'monitoring-instance-create' || !monitoringCreateError ? null : { key: 'monitoring-create-error', message: monitoringCreateError, error: true },
    monitoringCreateNotice ? { key: 'monitoring-create-notice', message: monitoringCreateNotice } : null,
    lifecycleConfirmingAction || !lifecycleError ? null : { key: 'lifecycle-error', message: lifecycleError, error: true },
    lifecycleNotice ? { key: 'lifecycle-notice', message: lifecycleNotice } : null,
  ]
  const pageFeedbackItems = pageFeedbackCandidates.filter(isPageFeedbackItem)

  return (
    <div className="page-stack asset-page vps-detail-page">
      <VPSDetailOverviewPanel
        model={overviewModel}
        vpsId={detail.vps_id}
        isArchived={isArchived}
        lifecycleSubmitting={lifecycleSubmitting}
        onDecisionEdit={() => openDrawer('decision')}
        onCancellationOpen={() => openDrawer('cancellation')}
        onFactEdit={() => openFactEdit(detail)}
        onFactsOpen={() => openDrawer('facts-detail')}
        onExperienceLog={() => openDrawer('experience')}
        onMonitoringEvidence={() => openDrawer('monitoring-instance-evidence')}
        onMonitoringAgent={openMonitoringAgentWorkbench}
        onMonitoringLink={() => openDrawer('monitoring-instance-link')}
        onSubscriptionOpen={() => openDrawer('subscription')}
        onValidityExtend={() => openDrawer('validity-extension')}
        onServiceCreate={() => openDrawer('service')}
        onDomainCreate={() => openDrawer('domain')}
        onArchiveStart={() => openLifecycleConfirmation('archive')}
        onRestoreStart={() => openLifecycleConfirmation('restore')}
      />

      {pageFeedbackItems.length > 0 ? (
        <div className="vps-detail-feedback-stack" aria-label="VPS 操作反馈">
          {pageFeedbackItems.map((item) => (
            <p
              key={item.key}
              className={[
                'asset-operation-feedback',
                item.error && 'asset-operation-feedback--error',
              ].filter(Boolean).join(' ')}
              role={item.error ? 'alert' : 'status'}
            >
              {item.message}
              {item.action ? (
                <>
                  {' '}
                  <Link className="text-link" to={item.action.to}>{item.action.label}</Link>
                </>
              ) : null}
            </p>
          ))}
        </div>
      ) : null}

      {overviewModel.contextAction ? (
        <VPSContextActionPanel
          action={overviewModel.contextAction}
          onOpenModal={openDrawer}
          onMonitoringAgent={openMonitoringAgentWorkbench}
        />
      ) : null}

      <VPSRelatedOverview
        items={overviewModel.relatedItems}
        onOpenModal={openDrawer}
        onMonitoringAgent={openMonitoringAgentWorkbench}
      />

      <VPSSingleMachineLedger ledger={overviewModel.ledger} onOpenModal={openDrawer} />

      <VPSIPQualitySection vpsId={detail.vps_id} report={state.ipQuality} error={state.ipQualityError} />

      {lifecycleConfirmingAction ? (
        <ActionConfirmationModal
          open
          title={vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).title}
          current={vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).current}
          result={vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).result}
          impact={vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).impact}
          unchanged={vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).unchanged}
          confirmLabel={
            lifecycleSubmitting
              ? lifecycleConfirmingAction === 'restore'
                ? '恢复中…'
                : '归档中…'
              : vpsLifecycleConfirmationCopy(detail, lifecycleConfirmingAction).confirmLabel
          }
          disabled={lifecycleConfirmDisabled}
          cancelDisabled={lifecycleSubmitting}
          error={lifecycleError}
          onCancel={closeLifecycleConfirmation}
          onConfirm={() => {
            if (lifecycleConfirmingAction === 'restore') {
              void handleRestoreVPS()
            } else {
              void handleArchiveVPS()
            }
          }}
        >
          {lifecycleConfirmingAction === 'archive' ? (
            <div className="asset-lifecycle-confirm">
              <p className="asset-lifecycle-confirm__eyebrow">ARCHIVE REVIEW</p>
              {archiveReviewLoading ? (
                <p className="asset-lifecycle-confirm__callouts">正在检查归档资格…</p>
              ) : archiveBlockers.length > 0 ? (
                <>
                  <h4>归档前仍有需要处理的事项。</h4>
                  <ul className="asset-lifecycle-confirm__blockers">
                    {archiveBlockers.map((blocker) => (
                      <li key={blocker}>{blocker}</li>
                    ))}
                  </ul>
                </>
              ) : archiveReview ? (
                <>
                  <h4>输入 VPS 展示名后才能归档，服务端会再次校验资格。</h4>
                  <label className="input-field">
                    <span className="input-field__label">输入 VPS 名称确认归档</span>
                    <input
                      className="input"
                      aria-label="输入 VPS 名称确认归档"
                      value={archiveConfirmationName}
                      onChange={(event) => {
                        setArchiveConfirmationName(event.target.value)
                        setLifecycleError(null)
                      }}
                      placeholder={detail.display_name}
                      disabled={lifecycleSubmitting}
                    />
                    <span className="input-field__hint">需要完整匹配：{detail.display_name}</span>
                  </label>
                </>
              ) : (
                <p className="asset-lifecycle-confirm__callouts">归档资格暂未加载成功，请关闭后重试。</p>
              )}
            </div>
          ) : null}
        </ActionConfirmationModal>
      ) : null}

      <Modal
        open={activeDrawer !== null}
        onClose={closeDrawer}
        title={drawerTitle()}
        ariaLabel={drawerTitle()}
        persistent={activeDrawer != null && !activeDrawer.endsWith('-detail') && activeDrawer !== 'monitoring-instance-evidence'}
        size={activeDrawer != null && (activeDrawer.endsWith('-detail') || activeDrawer === 'monitoring-instance-evidence' || activeDrawer === 'facts' || activeDrawer === 'cancellation' || activeDrawer === 'subscription' || activeDrawer === 'validity-extension' || activeDrawer === 'monitoring-instance-create') ? 'lg' : undefined}
        contentClassName={activeDrawer === 'cancellation' ? 'modal-content--asset-cancel' : undefined}
      >
        <div className="vps-detail-modal">
          {renderDrawerContent()}
        </div>
      </Modal>
    </div>
  )
}
