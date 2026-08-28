import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { Button, Modal } from '../../components/atoms'
import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { VPSCancellationWorkbench } from '../../components/VPSCancellationWorkbench'
import { VPSTimelinePanel } from '../../components/VPSTimelinePanel'
import {
  applyVPSCancellation,
  archiveVPS,
  buildVPSDomainCreateBody,
  buildVPSServiceCreateBody,
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
} from '../../lib/api'
import { useOptionalVPSWriteRegistry } from '../../lib/vpsWriteRegistry-context'
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
  SubscriptionRecord,
  UpdateVPSAssetInput,
  VPSAssetDetail,
  VPSIPQualityReport,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { VPSDetailErrorPanel } from './VPSDetailErrorPanel'
import { VPSDetailLoading } from './VPSDetailLoading'
import { VPSDetailMissingID } from './VPSDetailMissingID'
import { VPSDetailOverviewPanel } from './VPSDetailOverviewPanel'
import { VPSDomainsForm } from './VPSDomainsForm'
import { VPSDomainsSection } from './VPSDomainsSection'
import { VPSExperienceLogForm } from './VPSExperienceLogForm'
import { VPSFactsEditForm } from './VPSFactsEditForm'
import { VPSFactsSection } from './VPSFactsSection'
import { VPSIPQualitySection } from './VPSIPQualitySection'
import { VPSMonitoringInstanceCreateForm } from './VPSMonitoringInstanceCreateForm'
import { VPSMonitoringInstanceLinkForm } from './VPSMonitoringInstanceLinkForm'
import { VPSMonitoringInstanceLinksSection } from './VPSMonitoringInstanceLinksSection'
import { VPSRenewalDecisionForm } from './VPSRenewalDecisionForm'
import { VPSRelatedOverview } from './VPSRelatedOverview'
import { VPSSingleMachineLedger } from './VPSSingleMachineLedger'
import { VPSSubscriptionForm } from './VPSSubscriptionForm'
import { VPSValidityExtensionForm } from './VPSValidityExtensionForm'
import { VPSServicesForm } from './VPSServicesForm'
import { VPSServicesSection } from './VPSServicesSection'
import { vpsLifecycleConfirmationCopy } from './vpsLifecycleConfirmationCopy'

const LARGE_MODAL_SIZE = 'lg'
import { buildVPSDetailOverviewModel } from './vpsDetailOverviewModel'
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
} from './types'
import {
  buildMonitoringInstanceCreateInput,
  buildSubscriptionInput,
  buildValidityExtensionInput,
  buildDomainInput,
  buildExperienceLogInput,
  buildFactEditInput,
  buildServiceInput,
  compareDecisionDraft,
  compareFactDraftAgainstLatest,
  decisionDraftAlreadySatisfied,
  detailToFactEditForm,
  mergeFactDraftWithLatest,
  INITIAL_DOMAIN_DRAFT,
  INITIAL_EXPERIENCE_DRAFT,
  INITIAL_SELECTOR_STATE,
  INITIAL_SERVICE_DRAFT,
  INITIAL_SUBSCRIPTION_DRAFT,
  INITIAL_VALIDITY_EXTENSION_DRAFT,
  INITIAL_STATE,
  monitoringInstanceCreateDraftFromDetail,
} from './vpsDetailHelpers'
import { VPSVersionConflictBanner } from './VPSVersionConflictBanner'
import {
  describeManagementError as describeError,
  isCancellationPreviewStale,
  isIdempotencyKeyReused,
  isTerminalVPSLifecycle,
  isVPSAssetReadonly,
  isVPSVersionConflict,
  subscriptionLinkageAction,
  subscriptionLinkageNotice,
  type VPSVersionConflictState,
} from './vpsManagementHelpers'
import {
  createVPSWriteOwnerStore,
  type VPSWriteOperation,
  type VPSWriteOwner,
  type VPSWriteOwnerStore,
  type VPSPreparedCreateOwner,
} from './vpsWriteOwnerStore'
import type { VPSCreateSettleOutcome } from './vpsWriteOwnerStore'

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

type LegacyVPSDetailProps = {
  writeOwnerStore?: VPSWriteOwnerStore
  viewToken?: string
  onViewAuthorityInvalidatedWriteSettled?: (vpsId: string, viewToken: string) => void
}

export function LegacyVPSDetail({
  writeOwnerStore: providedWriteOwnerStore,
  viewToken: providedViewToken,
  onViewAuthorityInvalidatedWriteSettled,
}: LegacyVPSDetailProps = {}) {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const initialDrawerFromQuery = drawerModeFromWorkbenchQuery(searchParams.get('workbench'))
  const openCancellationFromQuery = initialDrawerFromQuery === 'cancellation'
  const skipNextQueryDrivenReload = useRef(false)
  const cancellationPreviewGenerationRef = useRef(0)
  const mutationGenerationRef = useRef(0)
  const archiveReviewRequestRef = useRef(0)
  const currentVpsIdRef = useRef(vpsId)
  const latestLoadLockRef = useRef(false)
  const contextWriteOwnerStore = useOptionalVPSWriteRegistry()
  const [localWriteOwnerStore] = useState(createVPSWriteOwnerStore)
  const writeOwnerStore = providedWriteOwnerStore ?? contextWriteOwnerStore ?? localWriteOwnerStore
  const [localViewToken] = useState(() => crypto.randomUUID())
  const viewToken = providedViewToken ?? localViewToken
  const factDraftRef = useRef<FactEditFormState | null>(null)
  const [state, setState] = useState(INITIAL_STATE)
  const [selectors, setSelectors] = useState(INITIAL_SELECTOR_STATE)
  const writeOwners = useSyncExternalStore(
    writeOwnerStore.subscribe,
    writeOwnerStore.getSnapshot,
    writeOwnerStore.getSnapshot,
  )
  const [decisionDraft, setDecisionDraft] = useState<DecisionDraftState>({
    renewalDecision: 'unreviewed',
    reason: '',
  })
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [decisionAction, setDecisionAction] = useState<{ to: string; label: string } | null>(null)
  const [activeDrawer, setActiveDrawer] = useState<VPSDetailDrawerMode>(null)
  const [factDraft, setFactDraft] = useState<FactEditFormState | null>(null)
  const [factDraftBase, setFactDraftBase] = useState<FactEditFormState | null>(null)
  const [factError, setFactError] = useState<string | null>(null)
  const [factNotice, setFactNotice] = useState<string | null>(null)
  const [mutationConflict, setMutationConflict] = useState<VPSVersionConflictState | null>(null)
  const [latestLoading, setLatestLoading] = useState(false)
  const [readonlyBlocked, setReadonlyBlocked] = useState(false)
  const [linkDraft, setLinkDraft] = useState<LinkDraftState>({ monitoringInstanceId: '', note: '' })
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkNotice, setLinkNotice] = useState<string | null>(null)
  const [monitoringCreateError, setMonitoringCreateError] = useState<string | null>(null)
  const [monitoringCreateNotice, setMonitoringCreateNotice] = useState<string | null>(null)
  const [monitoringCreateAction, setMonitoringCreateAction] = useState<{ to: string; label: string } | null>(null)
  const [monitoringCreateDraft, setMonitoringCreateDraft] = useState<MonitoringInstanceCreateDraftState | null>(null)
  const [subscriptionDraft, setSubscriptionDraft] = useState<SubscriptionDraftState>(INITIAL_SUBSCRIPTION_DRAFT)
  const [subscriptionError, setSubscriptionError] = useState<string | null>(null)
  const [subscriptionNotice, setSubscriptionNotice] = useState<string | null>(null)
  const [validityExtensionDraft, setValidityExtensionDraft] = useState<ValidityExtensionDraftState>(INITIAL_VALIDITY_EXTENSION_DRAFT)
  const [validityExtensionError, setValidityExtensionError] = useState<string | null>(null)
  const [validityExtensionNotice, setValidityExtensionNotice] = useState<string | null>(null)
  const [unlinkError, setUnlinkError] = useState<string | null>(null)
  const [pendingUnlinkMonitoringInstance, setPendingUnlinkMonitoringInstance] = useState<VPSMonitoringInstanceSummary | null>(null)
  const [lifecycleConfirmingAction, setLifecycleConfirmingAction] = useState<'archive' | 'restore' | null>(null)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [lifecycleNotice, setLifecycleNotice] = useState<string | null>(null)
  const [archiveReview, setArchiveReview] = useState<ArchiveReview | null>(null)
  const [archiveReviewLoading, setArchiveReviewLoading] = useState(false)
  const [archiveConfirmationName, setArchiveConfirmationName] = useState('')
  const [cancellationError, setCancellationError] = useState<string | null>(null)
  const [experienceDraft, setExperienceDraft] = useState<ExperienceDraftState>(INITIAL_EXPERIENCE_DRAFT)
  const [experienceError, setExperienceError] = useState<string | null>(null)
  const [experienceNotice, setExperienceNotice] = useState<string | null>(null)
  const [serviceDraft, setServiceDraft] = useState<ServiceDraftState>(INITIAL_SERVICE_DRAFT)
  const [serviceError, setServiceError] = useState<string | null>(null)
  const [serviceNotice, setServiceNotice] = useState<string | null>(null)
  const [domainDraft, setDomainDraft] = useState<DomainDraftState>(INITIAL_DOMAIN_DRAFT)
  const [domainError, setDomainError] = useState<string | null>(null)
  const [domainNotice, setDomainNotice] = useState<string | null>(null)

  function clearWorkbenchQueryParam() {
    if (!searchParams.has('workbench')) return
    const next = new URLSearchParams(searchParams)
    next.delete('workbench')
    skipNextQueryDrivenReload.current = true
    setSearchParams(next, { replace: true })
  }

  function replaceFactDraft(next: FactEditFormState | null) {
    factDraftRef.current = next
    setFactDraft(next)
  }

  function invalidateMutations() {
    mutationGenerationRef.current += 1
    latestLoadLockRef.current = false
    setLatestLoading(false)
  }

  function mutationIsCurrent(generation: number): boolean {
    return mutationGenerationRef.current === generation
  }

  function beginVpsWrite(
    targetVpsId: string,
    operation: VPSWriteOperation,
    monitoringInstanceId?: string,
  ): VPSWriteOwner | null {
    const owner = writeOwnerStore.begin({
      vpsId: targetVpsId,
      viewToken,
      generation: mutationGenerationRef.current + 1,
      operation,
      ...(monitoringInstanceId ? { monitoringInstanceId } : {}),
    })
    if (!owner) return null
    mutationGenerationRef.current = owner.generation
    return owner
  }

  function finishVpsWrite(owner: VPSWriteOwner) {
    const released = writeOwnerStore.finish(owner)
    if (released && !mutationIsCurrent(owner.generation)) {
      onViewAuthorityInvalidatedWriteSettled?.(owner.vpsId, owner.viewToken)
    }
  }

  async function prepareVpsCreate(owner: VPSWriteOwner, wireBody: unknown): Promise<VPSPreparedCreateOwner | null> {
    const preparedOwner = await writeOwnerStore.prepareCreate(owner, wireBody)
    if (!preparedOwner) return null
    if (mutationIsCurrent(owner.generation)) return preparedOwner
    finishVpsCreate(preparedOwner, 'not_sent')
    return null
  }

  function finishVpsCreate(owner: VPSPreparedCreateOwner, outcome: VPSCreateSettleOutcome) {
    const released = writeOwnerStore.finishCreate(owner, outcome)
    if (released && !mutationIsCurrent(owner.generation)) {
      onViewAuthorityInvalidatedWriteSettled?.(owner.vpsId, owner.viewToken)
    }
  }

  function collapseDrawer() {
    setActiveDrawer(null)
    setMutationConflict(null)
    setReadonlyBlocked(false)
    clearWorkbenchQueryParam()
  }

  useEffect(() => {
    currentVpsIdRef.current = vpsId
    archiveReviewRequestRef.current += 1
    const invalidateRouteAuthority = () => {
      archiveReviewRequestRef.current += 1
      mutationGenerationRef.current += 1
      latestLoadLockRef.current = false
    }
    if (!vpsId) {
      return invalidateRouteAuthority
    }
    if (skipNextQueryDrivenReload.current) {
      skipNextQueryDrivenReload.current = false
      return invalidateRouteAuthority
    }

    let cancelled = false
    const routeGeneration = ++mutationGenerationRef.current
    const routePreviewGeneration = ++cancellationPreviewGenerationRef.current
    latestLoadLockRef.current = false
    const routeIsCurrent = () => (
      !cancelled &&
      currentVpsIdRef.current === vpsId &&
      mutationGenerationRef.current === routeGeneration
    )

    getVPSAsset(vpsId)
      .then(async (detail) => {
        if (!routeIsCurrent()) return null
        const normalizedDetail = normalizeVPSDetail(detail)
        if (normalizedDetail.lifecycle_status === 'archived' || normalizedDetail.lifecycle_status === 'cancelled') {
          if (!routeIsCurrent()) return null
          navigate(`/archive/${encodeURIComponent(normalizedDetail.vps_id)}`, { replace: true })
          return null
        }
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
      .then((payload) => {
        if (payload == null || !routeIsCurrent()) return
        const { normalizedDetail, timeline, services, domains, subscriptionState, ipQualityState, cancellationState } = payload
        const cancellationResetGeneration = initialDrawerFromQuery === 'cancellation'
          ? null
          : ++cancellationPreviewGenerationRef.current
        setState((current) => {
          if (!routeIsCurrent()) return current
          const routePreviewIsCurrent = (
            initialDrawerFromQuery === 'cancellation' &&
            cancellationPreviewGenerationRef.current === routePreviewGeneration
          )
          const cancellationResetIsCurrent = (
            cancellationResetGeneration !== null &&
            cancellationPreviewGenerationRef.current === cancellationResetGeneration
          )
          return {
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
            cancellationPreview: routePreviewIsCurrent
              ? cancellationState.cancellationPreview
              : cancellationResetIsCurrent
                ? null
                : current.cancellationPreview,
            cancellationPreviewError: routePreviewIsCurrent
              ? cancellationState.cancellationPreviewError
              : cancellationResetIsCurrent
                ? null
                : current.cancellationPreviewError,
            cancellationResult: routePreviewIsCurrent || cancellationResetIsCurrent
              ? null
              : current.cancellationResult,
          }
        })
        setDecisionDraft({ renewalDecision: normalizedDetail.renewal_decision, reason: '' })
        setDecisionError(null)
        setDecisionNotice(null)
        setDecisionAction(null)
        replaceFactDraft(detailToFactEditForm(normalizedDetail))
        setFactDraftBase(detailToFactEditForm(normalizedDetail))
        setFactError(null)
        setFactNotice(null)
        setMutationConflict(null)
        setReadonlyBlocked(false)
        setLatestLoading(false)
        setLinkDraft({ monitoringInstanceId: '', note: '' })
        setLinkError(null)
        setLinkNotice(null)
        setMonitoringCreateError(null)
        setMonitoringCreateNotice(null)
        setMonitoringCreateAction(null)
        setMonitoringCreateDraft(monitoringInstanceCreateDraftFromDetail(normalizedDetail))
        setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
        setSubscriptionError(null)
        setSubscriptionNotice(null)
        setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
        setValidityExtensionError(null)
        setValidityExtensionNotice(null)
        setUnlinkError(null)
        setPendingUnlinkMonitoringInstance(null)
        archiveReviewRequestRef.current += 1
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
        const initialMonitoringLink = initialMonitoringLinks[0]
        if (initialDrawerFromQuery === 'monitoring-instance-create' && initialMonitoringLinks.length === 1 && initialMonitoringLink) {
          navigate(`/monitoring/${encodeURIComponent(initialMonitoringLink.monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(normalizedDetail.vps_id)}`)
          return
        }
        if (initialDrawerFromQuery === 'monitoring-instance-create' && initialMonitoringLinks.length > 1) {
          setActiveDrawer('monitoring-instance-evidence')
          return
        }
        setActiveDrawer(initialDrawerFromQuery)
      })
      .catch((error: unknown) => {
        if (!routeIsCurrent()) return
        setState((current) => {
          if (!routeIsCurrent()) return current
          return {
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
          }
        })
      })

    return () => {
      cancelled = true
      invalidateRouteAuthority()
    }
  }, [initialDrawerFromQuery, navigate, openCancellationFromQuery, vpsId])

  const applyCancellationPreview = useCallback(async (
    targetVPSId: string,
    generation: number,
    ownsRefresh: () => boolean = () => true,
  ) => {
    const cancellationState = await loadCancellationPreview(targetVPSId)
    if (generation !== cancellationPreviewGenerationRef.current || !ownsRefresh()) return false
    setState((current) => {
      if (
        current.vpsId !== targetVPSId ||
        generation !== cancellationPreviewGenerationRef.current ||
        !ownsRefresh()
      ) return current
      return {
        ...current,
        cancellationPreview: cancellationState.cancellationPreview,
        cancellationPreviewError: cancellationState.cancellationPreviewError,
      }
    })
    return true
  }, [])

  const refreshCancellationPreview = useCallback(async (targetVPSId: string) => {
    const generation = ++cancellationPreviewGenerationRef.current
    await applyCancellationPreview(targetVPSId, generation)
  }, [applyCancellationPreview])

  async function refreshDetail(
    targetVPSId: string,
    ownsRefresh: () => boolean = () => true,
  ): Promise<VPSAssetDetail> {
    const detail = normalizeVPSDetail(await getVPSAsset(targetVPSId))
    setState((current) => {
      if (current.vpsId !== targetVPSId || !current.timeline || !ownsRefresh()) return current
      return { ...current, error: null, detail }
    })
    return detail
  }

  async function refreshDetailAndTimeline(
    targetVPSId: string,
    ownsRefresh: () => boolean = () => true,
  ): Promise<VPSAssetDetail> {
    const detailResult = normalizeVPSDetail(await getVPSAsset(targetVPSId))
    const [timeline, services, domains, subscriptionState, ipQualityState] = await Promise.all([
      getVPSTimeline(targetVPSId),
      listVPSServices(targetVPSId),
      listVPSDomains(targetVPSId),
      loadSubscriptions(targetVPSId),
      loadIPQuality(targetVPSId, detailResult),
    ])
    setState((current) => {
      if (!ownsRefresh() || current.vpsId !== targetVPSId) return current
      return {
        ...current,
        error: null,
        detail: detailResult,
        timeline,
        services,
        domains,
        subscriptions: subscriptionState.subscriptions,
        subscriptionsError: subscriptionState.subscriptionsError,
        ipQuality: ipQualityState.ipQuality,
        ipQualityError: ipQualityState.ipQualityError,
      }
    })
    return detailResult
  }

  async function refreshServices(
    targetVPSId: string,
    ownsRefresh: () => boolean = () => true,
  ): Promise<AssetServiceRecord[]> {
    const services = await listVPSServices(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId || !ownsRefresh()) return current
      return { ...current, services }
    })
    return services
  }

  async function refreshDomains(
    targetVPSId: string,
    ownsRefresh: () => boolean = () => true,
  ): Promise<AssetDomainRecord[]> {
    const domains = await listVPSDomains(targetVPSId)
    setState((current) => {
      if (current.vpsId !== targetVPSId || !ownsRefresh()) return current
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
    replaceFactDraft(draft)
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
    setMonitoringCreateAction(null)
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
    archiveReviewRequestRef.current += 1
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
      setState((current) => ({
        ...current,
        cancellationPreview: null,
        cancellationPreviewError: null,
        cancellationResult: null,
      }))
      if (state.detail) {
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
    const activeLink = activeLinks[0]
    if (activeLinks.length === 1 && activeLink) {
      navigate(`/monitoring/${encodeURIComponent(activeLink.monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(detail.vps_id)}`)
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
    if (activeDrawer === 'cancellation') {
      cancellationPreviewGenerationRef.current += 1
    }
    invalidateMutations()
    if (activeDrawer === 'decision') {
      if (state.detail) {
        setDecisionDraft({ renewalDecision: state.detail.renewal_decision, reason: '' })
      }
      clearDecisionFeedback()
    }
    if (activeDrawer === 'facts') {
      if (state.detail) {
        const form = detailToFactEditForm(state.detail)
        replaceFactDraft(form)
        setFactDraftBase(form)
      }
      setFactError(null)
      setFactNotice(null)
    }
    setMutationConflict(null)
    setReadonlyBlocked(false)
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
    const requestId = ++archiveReviewRequestRef.current
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
    const targetVpsId = detail.vps_id
    const ownsArchiveReview = () => (
      archiveReviewRequestRef.current === requestId && currentVpsIdRef.current === targetVpsId
    )
    setArchiveReview(null)
    setArchiveReviewLoading(true)
    getVPSArchiveReview(targetVpsId)
      .then((review) => {
        if (!ownsArchiveReview()) return
        setArchiveReview(review)
        setLifecycleError(null)
      })
      .catch((error: unknown) => {
        if (!ownsArchiveReview()) return
        setArchiveReview(null)
        setLifecycleError(describeError(error, '加载归档资格失败'))
      })
      .finally(() => {
        if (!ownsArchiveReview()) return
        setArchiveReviewLoading(false)
      })
  }

  async function routeIfTerminalVPS(vpsID: string, generation: number): Promise<boolean> {
    try {
      const latest = normalizeVPSDetail(await getVPSAsset(vpsID))
      if (!mutationIsCurrent(generation)) return true
      if (isTerminalVPSLifecycle(latest.lifecycle_status)) {
        navigate(`/archive/${encodeURIComponent(latest.vps_id)}`, { replace: true })
        return true
      }
    } catch {
      if (!mutationIsCurrent(generation)) return true
    }
    if (!mutationIsCurrent(generation)) return true
    setReadonlyBlocked(true)
    return false
  }

  async function loadLatestVersion() {
    if (latestLoadLockRef.current) return
    const detail = state.detail
    if (!detail) return
    latestLoadLockRef.current = true
    const generation = ++mutationGenerationRef.current
    const targetVpsId = detail.vps_id
    setLatestLoading(true)
    try {
      const latest = normalizeVPSDetail(await getVPSAsset(targetVpsId))
      if (!mutationIsCurrent(generation)) return
      if (isTerminalVPSLifecycle(latest.lifecycle_status)) {
        navigate(`/archive/${encodeURIComponent(latest.vps_id)}`, { replace: true })
        return
      }
      if (mutationConflict?.draftKind === 'decision' && decisionDraftAlreadySatisfied(decisionDraft, latest)) {
        setState((current) => current.vpsId === targetVpsId ? { ...current, detail: latest } : current)
        setMutationConflict(null)
        setDecisionNotice('该决策已由其他操作完成')
        collapseDrawer()
        await refreshDetailAndTimeline(targetVpsId, () => mutationIsCurrent(generation))
        return
      }
      const currentDraft = factDraftRef.current
      const factsCompare = currentDraft
        ? compareFactDraftAgainstLatest(factDraftBase ?? detailToFactEditForm(latest), currentDraft, latest)
        : []
      if (currentDraft && factDraftBase) {
        replaceFactDraft(mergeFactDraftWithLatest(factDraftBase, currentDraft, latest))
      } else if (currentDraft) {
        replaceFactDraft(mergeFactDraftWithLatest(detailToFactEditForm(latest), currentDraft, latest))
      }
      setFactDraftBase(detailToFactEditForm(latest))
      setState((current) => current.vpsId === targetVpsId ? { ...current, detail: latest } : current)
      setMutationConflict((current) => {
        if (!current) return current
        return {
          ...current,
          loaded: true,
          compare: current.draftKind === 'facts'
            ? factsCompare
            : current.draftKind === 'decision'
              ? compareDecisionDraft(decisionDraft, latest)
              : [],
        }
      })
      setFactError(null)
      setDecisionError(null)
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      const message = describeError(error, '加载最新版本失败')
      if (mutationConflict?.draftKind === 'decision') setDecisionError(message)
      else setFactError(message)
    } finally {
      if (mutationIsCurrent(generation)) {
        latestLoadLockRef.current = false
        setLatestLoading(false)
      }
    }
  }

  async function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    clearDecisionFeedback()
    if (readonlyBlocked) {
      setDecisionError('当前状态不允许修改')
      return
    }
    if (mutationConflict?.draftKind === 'decision' && !mutationConflict.loaded) {
      setDecisionError('请先加载最新版本后再保存')
      return
    }

    if (decisionDraft.renewalDecision === detail.renewal_decision) {
      setDecisionError('请选择一个不同的续费决策')
      return
    }

    const reason = decisionDraft.reason.trim()
    const owner = beginVpsWrite(detail.vps_id, 'decision')
    if (!owner) {
      setDecisionError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    try {
      const updated = await updateVPSAsset(detail.vps_id, {
        renewal_decision: decisionDraft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      }, { expectedUpdatedAt: detail.updated_at })
      if (!mutationIsCurrent(generation)) return
      const refreshed = await refreshDetailAndTimeline(detail.vps_id, () => mutationIsCurrent(generation))
      if (!mutationIsCurrent(generation)) return
      setDecisionDraft({ renewalDecision: refreshed.renewal_decision, reason: '' })
      setMutationConflict(null)
      setDecisionNotice(subscriptionLinkageNotice(updated.renewal_subscription_linkage))
      setDecisionAction(subscriptionLinkageAction(
        updated.renewal_subscription_linkage,
        detail.vps_id,
        updated.renewal_decision,
      ))
      collapseDrawer()
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      if (isVPSVersionConflict(error)) {
        setMutationConflict({
          kind: 'vps_version_conflict',
          draftKind: 'decision',
          loaded: false,
          staleUpdatedAt: detail.updated_at,
          compare: [],
        })
      }
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(detail.vps_id, generation)
      }
      if (!mutationIsCurrent(generation)) return
      setDecisionError(describeError(error, '更新续费决策失败'))
    } finally {
      finishVpsWrite(owner)
    }
  }

  function openFactEdit(detail: VPSAssetDetail) {
    ensureProvidersLoaded()
    const form = detailToFactEditForm(detail)
    replaceFactDraft(form)
    setFactDraftBase(form)
    setFactError(null)
    setFactNotice(null)
    setMutationConflict(null)
    setReadonlyBlocked(false)
    setActiveDrawer('facts')
  }

  async function handleFactSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail || !factDraft) return

    setFactError(null)
    setFactNotice(null)
    if (readonlyBlocked) {
      setFactError('当前状态不允许修改')
      return
    }
    if (mutationConflict?.draftKind === 'facts' && !mutationConflict.loaded) {
      setFactError('请先加载最新版本后再保存')
      return
    }

    let input: UpdateVPSAssetInput
    try {
      input = buildFactEditInput(factDraft)
    } catch (error: unknown) {
      setFactError(describeError(error, 'VPS 基础信息输入无效'))
      return
    }

    const owner = beginVpsWrite(detail.vps_id, 'facts')
    if (!owner) {
      setFactError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    try {
      await updateVPSAsset(detail.vps_id, input, { expectedUpdatedAt: detail.updated_at })
      if (!mutationIsCurrent(generation)) return
      const refreshed = await refreshDetailAndTimeline(detail.vps_id, () => mutationIsCurrent(generation))
      if (!mutationIsCurrent(generation)) return
      const form = detailToFactEditForm(refreshed)
      replaceFactDraft(form)
      setFactDraftBase(form)
      setMutationConflict(null)
      collapseDrawer()
      setFactNotice('基础信息已更新，资产历史已刷新')
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      if (isVPSVersionConflict(error)) {
        setMutationConflict({
          kind: 'vps_version_conflict',
          draftKind: 'facts',
          loaded: false,
          staleUpdatedAt: detail.updated_at,
          compare: [],
        })
      }
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(detail.vps_id, generation)
      }
      if (!mutationIsCurrent(generation)) return
      setFactError(describeError(error, '更新基础信息失败'))
    } finally {
      finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'link')
    if (!owner) {
      setLinkError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    setLinkError(null)
    setLinkNotice(null)
    setUnlinkError(null)

    try {
      await linkVPSMonitoringInstance(detail.vps_id, {
        monitoring_instance_id: monitoringInstanceId,
        note: linkDraft.note.trim(),
      })
      if (!mutationIsCurrent(generation)) return
      await refreshDetail(detail.vps_id, () => mutationIsCurrent(generation))
      if (!mutationIsCurrent(generation)) return
      setLinkDraft({ monitoringInstanceId: '', note: '' })
      setLinkNotice('监控实例关联已更新')
      collapseDrawer()
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      setLinkError(describeError(error, '关联监控实例失败'))
    } finally {
      finishVpsWrite(owner)
    }
  }

  async function handleMonitoringInstanceCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const detail = state.detail
    if (!detail) return

    const owner = beginVpsWrite(detail.vps_id, 'monitoring-create')
    if (!owner) {
      setMonitoringCreateError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    setMonitoringCreateError(null)
    setMonitoringCreateNotice(null)
    setMonitoringCreateAction(null)
    setUnlinkError(null)

    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      const input = buildMonitoringInstanceCreateInput(monitoringCreateDraft ?? monitoringInstanceCreateDraftFromDetail(detail))
      preparedOwner = await prepareVpsCreate(owner, input)
      if (!preparedOwner) return
      const created = await createVPSMonitoringInstance(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!mutationIsCurrent(generation)) return
      const onboardingPath = `/monitoring/${created.monitoring_instance_id}?onboarding=1&return_vps=${encodeURIComponent(detail.vps_id)}`
      try {
        await refreshDetail(detail.vps_id, () => mutationIsCurrent(generation))
        if (!mutationIsCurrent(generation)) return
        setMonitoringCreateNotice('监控实例已创建并关联，正在进入接入流程')
        collapseDrawer()
        navigate(onboardingPath)
      } catch (refreshError: unknown) {
        if (!mutationIsCurrent(generation)) return
        setMonitoringCreateNotice(`监控实例已创建并关联，但权威状态刷新失败：${describeError(refreshError, '权威状态刷新失败')}`)
        setMonitoringCreateAction({ to: onboardingPath, label: '继续接入 agent' })
        collapseDrawer()
      }
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!mutationIsCurrent(generation)) return
      setMonitoringCreateError(describeError(error, '创建监控实例失败'))
    } finally {
      if (preparedOwner) finishVpsCreate(preparedOwner, settleOutcome)
      else finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'subscription')
    if (!owner) {
      setSubscriptionError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      preparedOwner = await prepareVpsCreate(owner, input)
      if (!preparedOwner) return
      const subscription = await createVPSSubscription(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!mutationIsCurrent(generation)) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !mutationIsCurrent(generation)) return current
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
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!mutationIsCurrent(generation)) return
      setSubscriptionError(describeError(error, '创建订阅失败'))
    } finally {
      if (preparedOwner) finishVpsCreate(preparedOwner, settleOutcome)
      else finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'validity-extension')
    if (!owner) {
      setValidityExtensionError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    try {
      const result = await extendVPSValidity(detail.vps_id, input)
      if (!mutationIsCurrent(generation)) return
      await refreshDetailAndTimeline(detail.vps_id, () => mutationIsCurrent(generation))
      if (!mutationIsCurrent(generation)) return
      setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
      setValidityExtensionNotice(`有效期已延长，写入 ${result.steps.length} 个审计步骤`)
      collapseDrawer()
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      setValidityExtensionError(describeError(error, '延长有效期失败'))
    } finally {
      finishVpsWrite(owner)
    }
  }

  async function handleUnlinkMonitoringInstance(monitoringInstance: VPSMonitoringInstanceSummary) {
    const detail = state.detail
    if (!detail) return

    const owner = beginVpsWrite(
      detail.vps_id,
      'monitoring-unlink',
      monitoringInstance.monitoring_instance_id,
    )
    if (!owner) {
      setUnlinkError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    setUnlinkError(null)
    setLinkError(null)
    setLinkNotice(null)

    try {
      await unlinkVPSMonitoringInstance(detail.vps_id, {
        monitoring_instance_id: monitoringInstance.monitoring_instance_id,
        note: monitoringInstance.note,
      })
      if (!mutationIsCurrent(generation)) return
      await refreshDetail(detail.vps_id, () => mutationIsCurrent(generation))
      if (!mutationIsCurrent(generation)) return
      setLinkNotice('监控实例关联已解除')
      setPendingUnlinkMonitoringInstance(null)
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      setUnlinkError(describeError(error, '解除监控实例关联失败'))
    } finally {
      finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'lifecycle')
    if (!owner) {
      setLifecycleError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    setLifecycleError(null)
    setLifecycleNotice(null)

    try {
      await archiveVPS(detail.vps_id, { confirmation_name: confirmationName })
      if (!mutationIsCurrent(generation)) return
      navigate(`/archive/${encodeURIComponent(detail.vps_id)}`, { replace: true })
    } catch (error: unknown) {
      if (!mutationIsCurrent(generation)) return
      setLifecycleError(describeError(error, '归档 VPS 失败'))
    } finally {
      finishVpsWrite(owner)
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
    const owner = beginVpsWrite(detail.vps_id, 'cancellation')
    if (!owner) {
      setCancellationError('上一次保存仍在进行，请稍后再试')
      return
    }
    const mutationGeneration = owner.generation
    const generation = cancellationPreviewGenerationRef.current
    const stillCurrent = () => (
      generation === cancellationPreviewGenerationRef.current && mutationIsCurrent(mutationGeneration)
    )

    setCancellationError(null)
    setLifecycleNotice(null)
    setLifecycleError(null)

    try {
      const result: LifecycleActionResult = await applyVPSCancellation(detail.vps_id, input)
      if (!stillCurrent()) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !stillCurrent()) return current
        return { ...current, cancellationResult: result }
      })
      const refreshed = await refreshDetailAndTimeline(detail.vps_id, stillCurrent)
      if (!stillCurrent()) return
      if (refreshed.lifecycle_status === 'cancelled' || refreshed.lifecycle_status === 'archived') {
        navigate(`/archive/${encodeURIComponent(refreshed.vps_id)}`, { replace: true })
        return
      }
      const applied = await applyCancellationPreview(detail.vps_id, generation, stillCurrent)
      if (!applied || !stillCurrent()) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !stillCurrent()) return current
        return { ...current, cancellationResult: result }
      })
      setLifecycleNotice(`取消/退役动作已完成，写入 ${result.steps.length} 个审计步骤`)
    } catch (error: unknown) {
      if (isCancellationPreviewStale(error)) {
        if (!stillCurrent()) return
        const applied = await applyCancellationPreview(detail.vps_id, generation, stillCurrent)
        if (!applied || !stillCurrent()) return
        setCancellationError('影响范围已变化，请重新加载预览后再确认')
        return
      }
      if (!stillCurrent()) return
      setCancellationError(describeError(error, '执行取消/退役失败'))
    } finally {
      finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'experience')
    if (!owner) {
      setExperienceError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      preparedOwner = await prepareVpsCreate(owner, input)
      if (!preparedOwner) return
      const created = await createVPSExperienceLog(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!mutationIsCurrent(generation)) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !current.timeline || !mutationIsCurrent(generation)) return current
        return {
          ...current,
          timeline: {
            ...current.timeline,
            experience_logs: [
              created,
              ...current.timeline.experience_logs.filter((item) => item.experience_log_id !== created.experience_log_id),
            ],
          },
        }
      })
      setExperienceDraft(INITIAL_EXPERIENCE_DRAFT)
      try {
        await refreshDetailAndTimeline(detail.vps_id, () => mutationIsCurrent(generation))
        if (!mutationIsCurrent(generation)) return
        setExperienceNotice('经验记录已写入资产历史')
        collapseDrawer()
      } catch (refreshError: unknown) {
        if (!mutationIsCurrent(generation)) return
        setExperienceNotice(`经验记录已创建，但权威状态刷新失败：${describeError(refreshError, '权威状态刷新失败')}`)
        collapseDrawer()
      }
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!mutationIsCurrent(generation)) return
      setExperienceError(describeError(error, '创建经验记录失败'))
    } finally {
      if (preparedOwner) finishVpsCreate(preparedOwner, settleOutcome)
      else finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'service')
    if (!owner) {
      setServiceError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      const wireBody = buildVPSServiceCreateBody(input)
      preparedOwner = await prepareVpsCreate(owner, wireBody)
      if (!preparedOwner) return
      const created = await createVPSService(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!mutationIsCurrent(generation)) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !mutationIsCurrent(generation)) return current
        return {
          ...current,
          services: [created, ...current.services.filter((item) => item.service_id !== created.service_id)],
        }
      })
      setServiceDraft(INITIAL_SERVICE_DRAFT)
      try {
        await refreshServices(detail.vps_id, () => mutationIsCurrent(generation))
        if (!mutationIsCurrent(generation)) return
        setServiceNotice('服务记录已创建')
        collapseDrawer()
      } catch (refreshError: unknown) {
        if (!mutationIsCurrent(generation)) return
        setServiceNotice(`服务记录已创建，但权威状态刷新失败：${describeError(refreshError, '权威状态刷新失败')}`)
        collapseDrawer()
      }
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!mutationIsCurrent(generation)) return
      setServiceError(describeError(error, '创建服务记录失败'))
    } finally {
      if (preparedOwner) finishVpsCreate(preparedOwner, settleOutcome)
      else finishVpsWrite(owner)
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

    const owner = beginVpsWrite(detail.vps_id, 'domain')
    if (!owner) {
      setDomainError('上一次保存仍在进行，请稍后再试')
      return
    }
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      const wireBody = buildVPSDomainCreateBody(input)
      preparedOwner = await prepareVpsCreate(owner, wireBody)
      if (!preparedOwner) return
      const created = await createVPSDomain(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!mutationIsCurrent(generation)) return
      setState((current) => {
        if (current.vpsId !== detail.vps_id || !mutationIsCurrent(generation)) return current
        return {
          ...current,
          domains: [created, ...current.domains.filter((item) => item.domain_id !== created.domain_id)],
        }
      })
      setDomainDraft(INITIAL_DOMAIN_DRAFT)
      try {
        await refreshDomains(detail.vps_id, () => mutationIsCurrent(generation))
        if (!mutationIsCurrent(generation)) return
        setDomainNotice('域名记录已创建')
        collapseDrawer()
      } catch (refreshError: unknown) {
        if (!mutationIsCurrent(generation)) return
        setDomainNotice(`域名记录已创建，但权威状态刷新失败：${describeError(refreshError, '权威状态刷新失败')}`)
        collapseDrawer()
      }
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!mutationIsCurrent(generation)) return
      setDomainError(describeError(error, '创建域名记录失败'))
    } finally {
      if (preparedOwner) finishVpsCreate(preparedOwner, settleOutcome)
      else finishVpsWrite(owner)
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
  const currentWriteOwner = writeOwners.get(detail.vps_id) ?? null
  const writeBlocked = currentWriteOwner !== null
  const lifecycleSubmitting = currentWriteOwner?.operation === 'lifecycle'
  const unlinkingMonitoringInstanceId = currentWriteOwner?.operation === 'monitoring-unlink'
    ? currentWriteOwner.monitoringInstanceId ?? null
    : null
  const decisionChanged = decisionDraft.renewalDecision !== detail.renewal_decision
  const linkControlsDisabled = writeBlocked || unlinkingMonitoringInstanceId !== null
  const isArchived = detail.lifecycle_status === 'archived' || detail.lifecycle_status === 'cancelled'
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
  const lifecycleConfirmDisabled = writeBlocked ||
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
        <>
          {mutationConflict?.draftKind === 'decision' ? (
            <VPSVersionConflictBanner
              conflict={mutationConflict}
              loading={latestLoading}
              onLoadLatest={() => void loadLatestVersion()}
            />
          ) : null}
          <VPSRenewalDecisionForm
            detail={detail}
            draft={decisionDraft}
            submitting={writeBlocked || latestLoading}
            error={decisionError}
            notice={decisionNotice}
            decisionChanged={decisionChanged}
            onCancel={closeDrawer}
            onDraftChange={handleDecisionDraftChange}
            onFeedbackClear={clearDecisionFeedback}
            onSubmit={(event) => void handleDecisionSubmit(event)}
          />
        </>
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
          key={`${detail.vps_id}:${state.cancellationPreview.preview_digest}`}
          preview={state.cancellationPreview}
          submitting={writeBlocked}
          error={cancellationError ?? state.cancellationPreviewError}
          result={state.cancellationResult}
          onCancel={closeDrawer}
          onSubmit={(input) => handleCancellationSubmit(input)}
        />
      )
    }
    if (activeDrawer === 'facts') {
      return factDraft ? (
        <>
          {mutationConflict?.draftKind === 'facts' ? (
            <VPSVersionConflictBanner
              conflict={mutationConflict}
              loading={latestLoading}
              onLoadLatest={() => void loadLatestVersion()}
            />
          ) : null}
          <VPSFactsEditForm
            key={detail.updated_at}
            draft={factDraft}
            providers={selectors.providers}
            providersLoading={selectors.providersLoading}
            providersError={selectors.providersError}
            submitting={writeBlocked || latestLoading}
            error={factError}
            notice={factNotice}
            onCancel={closeDrawer}
            onDraftChange={handleFactDraftChange}
            onSubmit={(event) => void handleFactSubmit(event)}
          />
        </>
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          submitting={writeBlocked}
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
          writeBlocked={writeBlocked}
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
    monitoringCreateNotice ? { key: 'monitoring-create-notice', message: monitoringCreateNotice, action: monitoringCreateAction } : null,
    lifecycleConfirmingAction || !lifecycleError ? null : { key: 'lifecycle-error', message: lifecycleError, error: true },
    lifecycleNotice ? { key: 'lifecycle-notice', message: lifecycleNotice } : null,
  ]
  const pageFeedbackItems = pageFeedbackCandidates.filter(isPageFeedbackItem)

  return (
    <div className="page-stack asset-page vps-detail-page">
      {currentWriteOwner && currentWriteOwner.viewToken !== viewToken ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          操作处理中，请等待当前写入完成。
        </p>
      ) : null}
      <VPSDetailOverviewPanel
        model={overviewModel}
        vpsId={detail.vps_id}
        isArchived={isArchived}
        lifecycleSubmitting={lifecycleSubmitting}
        writeBlocked={writeBlocked}
        onDecisionEdit={() => openDrawer('decision')}
        onTimelineOpen={() => openDrawer('timeline-detail')}
        onServicesOpen={() => openDrawer('services-detail')}
        onDomainsOpen={() => openDrawer('domains-detail')}
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
        {...(activeDrawer != null && (activeDrawer.endsWith('-detail') || activeDrawer === 'monitoring-instance-evidence' || activeDrawer === 'facts' || activeDrawer === 'cancellation' || activeDrawer === 'subscription' || activeDrawer === 'validity-extension' || activeDrawer === 'monitoring-instance-create') ? { size: LARGE_MODAL_SIZE } : {})}
        {...(activeDrawer === 'cancellation' ? { contentClassName: 'modal-content--asset-cancel' } : {})}
      >
        <div className="vps-detail-modal">
          {renderDrawerContent()}
        </div>
      </Modal>
    </div>
  )
}
