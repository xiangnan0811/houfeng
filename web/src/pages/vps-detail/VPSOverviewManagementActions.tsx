import { useEffect, useId, useRef, useState, useSyncExternalStore, type FormEvent, type RefObject } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Modal } from '../../components/atoms'
import { VPSCancellationWorkbench } from '../../components/VPSCancellationWorkbench'
import {
  applyVPSCancellation,
  archiveVPS,
  buildVPSDomainCreateBody,
  buildVPSServiceCreateBody,
  createVPSDomain,
  createVPSService,
  createVPSSubscription,
  extendVPSValidity,
  getVPSAsset,
  getVPSArchiveReview,
  getVPSCancellationPreview,
  linkVPSMonitoringInstance,
  listMonitoringInstances,
  listProviders,
  listSubscriptions,
  listTargets,
  listVPSServices,
  unlinkVPSMonitoringInstance,
  updateVPSAsset,
} from '../../lib/api'
import type {
  ApplyCancellationInput,
  ArchiveReview,
  AssetServiceRecord,
  CancellationPreview,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  ExtendVPSValidityInput,
  LifecycleActionResult,
  MonitoringInstanceRecord,
  ProviderRecord,
  SubscriptionRecord,
  TargetRecord,
  VPSAssetDetail,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { useOptionalVPSWriteRegistry } from '../../lib/vpsWriteRegistry-context'
import { VPSDetailDialog, VPSDialogActions } from './VPSDetailDialog'
import { VPSDomainsForm } from './VPSDomainsForm'
import { VPSFactsEditForm } from './VPSFactsEditForm'
import type { VPSManagementController } from './hooks/useVPSManagementController'
import { VPSMonitoringInstanceLinkForm } from './VPSMonitoringInstanceLinkForm'
import { VPSRenewalDecisionForm } from './VPSRenewalDecisionForm'
import { VPSOverviewMonitoringOnboarding } from './VPSOverviewMonitoringOnboarding'
import { VPSOverviewRelationPanels } from './VPSOverviewRelationPanels'
import { VPSServicesForm } from './VPSServicesForm'
import { VPSSubscriptionForm } from './VPSSubscriptionForm'
import { VPSValidityExtensionForm } from './VPSValidityExtensionForm'
import type {
  DecisionDraftState,
  DomainDraftState,
  FactEditFormState,
  LinkDraftState,
  ServiceDraftState,
  SubscriptionDraftState,
  ValidityExtensionDraftState,
} from './types'
import {
  buildDomainInput,
  buildFactEditInput,
  buildServiceInput,
  buildSubscriptionInput,
  buildValidityExtensionInput,
  compareDecisionDraft,
  compareFactDraftAgainstLatest,
  decisionDraftAlreadySatisfied,
  detailToFactEditForm,
  INITIAL_DOMAIN_DRAFT,
  INITIAL_SERVICE_DRAFT,
  INITIAL_SUBSCRIPTION_DRAFT,
  INITIAL_VALIDITY_EXTENSION_DRAFT,
  mergeFactDraftWithLatest,
} from './vpsDetailHelpers'
import { VPSVersionConflictBanner } from './VPSVersionConflictBanner'
import { vpsLifecycleConfirmationCopy } from './vpsLifecycleConfirmationCopy'
import {
  describeManagementError,
  isCancellationPreviewStale,
  isIdempotencyKeyReused,
  isTerminalVPSLifecycle,
  isVPSAssetReadonly,
  isVPSVersionConflict,
  subscriptionLinkageAction,
  subscriptionLinkageNotice,
  type ManagementFeedbackAction,
  type VPSVersionConflictState,
} from './vpsManagementHelpers'
import {
  createVPSWriteOwnerStore,
  type VPSCreateSettleOutcome,
  type VPSPreparedCreateOwner,
  type VPSWriteOperation,
  type VPSWriteOwner,
  type VPSWriteOwnerStore,
} from './vpsWriteOwnerStore'

type Props = {
  vpsId: string
  displayName: string
  management: VPSManagementController
  managementTriggerRef: RefObject<HTMLButtonElement | null>
  onOverviewRefresh: () => Promise<boolean>
  writeOwnerStore?: VPSWriteOwnerStore
  viewToken?: string
}

type PageFeedback = {
  tone: 'success' | 'warning'
  message: string
  action?: ManagementFeedbackAction | null
}

const INITIAL_LINK_DRAFT: LinkDraftState = {
  monitoringInstanceId: '',
  note: '',
}

export function VPSOverviewManagementActions({
  vpsId,
  displayName,
  management,
  managementTriggerRef,
  onOverviewRefresh,
  writeOwnerStore: providedWriteOwnerStore,
  viewToken: providedViewToken,
}: Props) {
  const formId = useId()
  const location = useLocation()
  const navigate = useNavigate()
  const closeManagementPanel = management.closePanel
  const [detail, setDetail] = useState<VPSAssetDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [factDraft, setFactDraft] = useState<FactEditFormState | null>(null)
  const [factDraftBase, setFactDraftBase] = useState<FactEditFormState | null>(null)
  const [decisionDraft, setDecisionDraft] = useState<DecisionDraftState | null>(null)
  const [subscriptionDraft, setSubscriptionDraft] = useState<SubscriptionDraftState>(INITIAL_SUBSCRIPTION_DRAFT)
  const [serviceDraft, setServiceDraft] = useState<ServiceDraftState>(INITIAL_SERVICE_DRAFT)
  const [domainDraft, setDomainDraft] = useState<DomainDraftState>(INITIAL_DOMAIN_DRAFT)
  const [validityExtensionDraft, setValidityExtensionDraft] = useState<ValidityExtensionDraftState>(INITIAL_VALIDITY_EXTENSION_DRAFT)
  const [linkDraft, setLinkDraft] = useState<LinkDraftState>(INITIAL_LINK_DRAFT)

  const [providers, setProviders] = useState<ProviderRecord[]>([])
  const [providersLoading, setProvidersLoading] = useState(false)
  const [providersError, setProvidersError] = useState<string | null>(null)

  const [targets, setTargets] = useState<TargetRecord[]>([])
  const [targetsLoading, setTargetsLoading] = useState(false)
  const [targetsError, setTargetsError] = useState<string | null>(null)

  const [domainServices, setDomainServices] = useState<AssetServiceRecord[]>([])
  const [domainServicesLoading, setDomainServicesLoading] = useState(false)
  const [domainServicesError, setDomainServicesError] = useState<string | null>(null)

  const [subscriptions, setSubscriptions] = useState<SubscriptionRecord[]>([])
  const [subscriptionsLoading, setSubscriptionsLoading] = useState(false)
  const [subscriptionsError, setSubscriptionsError] = useState<string | null>(null)

  const [monitoringInstances, setMonitoringInstances] = useState<MonitoringInstanceRecord[]>([])
  const [monitoringInstancesLoading, setMonitoringInstancesLoading] = useState(false)
  const [monitoringInstancesError, setMonitoringInstancesError] = useState<string | null>(null)

  const [pendingUnlinkMonitoringInstance, setPendingUnlinkMonitoringInstance] = useState<VPSMonitoringInstanceSummary | null>(null)
  const [linkFeedback, setLinkFeedback] = useState<string | null>(null)
  const [linkFeedbackIsError, setLinkFeedbackIsError] = useState(false)
  const [relationRevision, setRelationRevision] = useState(0)

  const [cancellationPreview, setCancellationPreview] = useState<CancellationPreview | null>(null)
  const [cancellationResult, setCancellationResult] = useState<LifecycleActionResult | null>(null)
  const [cancellationLoading, setCancellationLoading] = useState(false)
  const [cancellationError, setCancellationError] = useState<string | null>(null)
  const [archiveReview, setArchiveReview] = useState<ArchiveReview | null>(null)
  const [archiveReviewLoading, setArchiveReviewLoading] = useState(false)
  const [archiveError, setArchiveError] = useState<string | null>(null)
  const [archiveConfirmationName, setArchiveConfirmationName] = useState('')
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [mutationConflict, setMutationConflict] = useState<VPSVersionConflictState | null>(null)
  const [readonlyBlocked, setReadonlyBlocked] = useState(false)
  const [pageFeedback, setPageFeedback] = useState<PageFeedback | null>(null)
  const [loadRevision, setLoadRevision] = useState(0)
  const requestIdRef = useRef(0)
  const mutationGenerationRef = useRef(0)
  const contextWriteOwnerStore = useOptionalVPSWriteRegistry()
  const [localWriteOwnerStore] = useState(createVPSWriteOwnerStore)
  const writeOwnerStore = providedWriteOwnerStore ?? contextWriteOwnerStore ?? localWriteOwnerStore
  const [localViewToken] = useState(() => crypto.randomUUID())
  const viewToken = providedViewToken ?? localViewToken
  const writeOwners = useSyncExternalStore(
    writeOwnerStore.subscribe,
    writeOwnerStore.getSnapshot,
    writeOwnerStore.getSnapshot,
  )
  const currentWriteOwner = writeOwners.get(vpsId)
  const submitting = Boolean(currentWriteOwner)
  const factDraftRef = useRef<FactEditFormState | null>(null)
  const mutationConflictRef = useRef<VPSVersionConflictState | null>(null)
  const decisionDraftRef = useRef<DecisionDraftState | null>(null)

  useEffect(() => {
    mutationConflictRef.current = mutationConflict
    decisionDraftRef.current = decisionDraft
  }, [mutationConflict, decisionDraft])

  function replaceFactDraft(next: FactEditFormState | null) {
    factDraftRef.current = next
    setFactDraft(next)
  }

  const panel = management.panel
  const factsOpen = panel === 'facts'
  const decisionOpen = panel === 'decision'
  const subscriptionOpen = panel === 'subscription'
  const serviceOpen = panel === 'service'
  const domainOpen = panel === 'domain'
  const validityExtensionOpen = panel === 'validity-extension'
  const monitoringLinkOpen = panel === 'monitoring-instance-link'
  const cancellationOpen = panel === 'cancellation'
  const archiveOpen = panel === 'archive'
  const relationPanelOpen = panel === 'monitoring-instance-evidence'
    || panel === 'services-detail'
    || panel === 'domains-detail'
  const detailPanelOpen = factsOpen
    || decisionOpen
    || subscriptionOpen
    || serviceOpen
    || domainOpen
    || validityExtensionOpen
    || monitoringLinkOpen

  const activeSubscriptions = subscriptions.filter((subscription) => subscription.status === 'active')
  const activeSubscription: SubscriptionRecord | null = activeSubscriptions.length === 1 ? (activeSubscriptions[0] ?? null) : null
  const unlinkingMonitoringInstanceId = currentWriteOwner?.operation === 'monitoring-unlink'
    ? currentWriteOwner.monitoringInstanceId ?? null
    : null

  const authorityPanelOpen = detailPanelOpen || relationPanelOpen

  useEffect(() => {
    mutationGenerationRef.current += 1
    // eslint-disable-next-line react-hooks/set-state-in-effect -- route identity invalidates prior conflict UI before the new route can mutate
    setMutationConflict(null)
    setReadonlyBlocked(true)
    return () => {
      mutationGenerationRef.current += 1
    }
  }, [vpsId])

  useEffect(() => {
    if (!detailPanelOpen) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- opening a write panel must drop the prior panel draft before its async authority load starts
    replaceFactDraft(null)
    setFactDraftBase(null)
    setDecisionDraft(null)
    setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
    setServiceDraft(INITIAL_SERVICE_DRAFT)
    setDomainDraft(INITIAL_DOMAIN_DRAFT)
    setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
    setLinkDraft(INITIAL_LINK_DRAFT)
  }, [decisionOpen, detailPanelOpen, domainOpen, factsOpen, monitoringLinkOpen, serviceOpen, subscriptionOpen, validityExtensionOpen, vpsId])

  useEffect(() => {
    if (!authorityPanelOpen) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- a newly selected panel must synchronously invalidate the prior panel's detail before its async read starts
    setDetail(null)
    setDetailLoading(true)
    setDetailError(null)
    setProviders([])
    setProvidersLoading(factsOpen)
    setProvidersError(null)
    setTargets([])
    setTargetsLoading(serviceOpen || domainOpen)
    setTargetsError(null)
    setDomainServices([])
    setDomainServicesLoading(domainOpen)
    setDomainServicesError(null)
    setSubscriptions([])
    setSubscriptionsLoading(validityExtensionOpen)
    setSubscriptionsError(null)
    setMonitoringInstances([])
    setMonitoringInstancesLoading(monitoringLinkOpen)
    setMonitoringInstancesError(null)
    setMutationError(null)
    setMutationConflict(null)

    void getVPSAsset(vpsId)
      .then((nextDetail) => {
        if (requestId !== requestIdRef.current) return
        setDetail(nextDetail)
        if (isTerminalVPSLifecycle(nextDetail.lifecycle_status)) {
          setReadonlyBlocked(true)
          if (detailPanelOpen) {
            navigate(`/archive/${encodeURIComponent(nextDetail.vps_id)}`, { replace: true, state: location.state })
            closeManagementPanel()
          }
          return
        }
        setReadonlyBlocked(false)
        if (factsOpen) {
          const form = detailToFactEditForm(nextDetail)
          replaceFactDraft(form)
          setFactDraftBase(form)
        }
        if (decisionOpen) {
          setDecisionDraft({ renewalDecision: nextDetail.renewal_decision, reason: '' })
        }
      })
      .catch((error: unknown) => {
        if (requestId !== requestIdRef.current) return
        setReadonlyBlocked(true)
        setDetailError(describeManagementError(error, '加载 VPS 事实失败'))
      })
      .finally(() => {
        if (requestId === requestIdRef.current) setDetailLoading(false)
      })

    if (factsOpen) {
      void listProviders()
        .then((nextProviders) => {
          if (requestId !== requestIdRef.current) return
          setProviders(nextProviders)
        })
        .catch((error: unknown) => {
          if (requestId !== requestIdRef.current) return
          setProvidersError(describeManagementError(error, '加载服务商失败'))
        })
        .finally(() => {
          if (requestId === requestIdRef.current) setProvidersLoading(false)
        })
    }

    if (serviceOpen || domainOpen) {
      void listTargets()
        .then((nextTargets) => {
          if (requestId !== requestIdRef.current) return
          setTargets(nextTargets)
        })
        .catch((error: unknown) => {
          if (requestId !== requestIdRef.current) return
          setTargetsError(describeManagementError(error, '加载入口探测列表失败'))
        })
        .finally(() => {
          if (requestId === requestIdRef.current) setTargetsLoading(false)
        })
    }

    if (domainOpen) {
      void listVPSServices(vpsId)
        .then((nextServices) => {
          if (requestId !== requestIdRef.current) return
          setDomainServices(nextServices)
        })
        .catch((error: unknown) => {
          if (requestId !== requestIdRef.current) return
          setDomainServicesError(describeManagementError(error, '加载服务列表失败'))
        })
        .finally(() => {
          if (requestId === requestIdRef.current) setDomainServicesLoading(false)
        })
    }

    if (validityExtensionOpen) {
      void listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' })
        .then((nextSubscriptions) => {
          if (requestId !== requestIdRef.current) return
          setSubscriptions(nextSubscriptions)
        })
        .catch((error: unknown) => {
          if (requestId !== requestIdRef.current) return
          setSubscriptionsError(describeManagementError(error, '加载 VPS 订阅失败'))
        })
        .finally(() => {
          if (requestId === requestIdRef.current) setSubscriptionsLoading(false)
        })
    }

    if (monitoringLinkOpen) {
      void listMonitoringInstances()
        .then((nextMonitoringInstances) => {
          if (requestId !== requestIdRef.current) return
          setMonitoringInstances(nextMonitoringInstances)
        })
        .catch((error: unknown) => {
          if (requestId !== requestIdRef.current) return
          setMonitoringInstancesError(describeManagementError(error, '加载监控实例列表失败'))
        })
        .finally(() => {
          if (requestId === requestIdRef.current) setMonitoringInstancesLoading(false)
        })
    }

    return () => {
      requestIdRef.current += 1
    }
  }, [
    authorityPanelOpen,
    decisionOpen,
    detailPanelOpen,
    domainOpen,
    factsOpen,
    loadRevision,
    location.state,
    closeManagementPanel,
    monitoringLinkOpen,
    navigate,
    serviceOpen,
    subscriptionOpen,
    validityExtensionOpen,
    vpsId,
  ])

  useEffect(() => {
    if (!cancellationOpen) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- opening the workbench must clear any prior authoritative preview before the next server preview is requested
    setCancellationPreview(null)
    setCancellationResult(null)
    setCancellationLoading(true)
    setCancellationError(null)
    setMutationError(null)

    void getVPSCancellationPreview(vpsId)
      .then((preview) => {
        if (requestId !== requestIdRef.current) return
        setCancellationPreview(preview)
      })
      .catch((error: unknown) => {
        if (requestId !== requestIdRef.current) return
        setCancellationError(describeManagementError(error, '加载取消/退役影响预览失败'))
      })
      .finally(() => {
        if (requestId === requestIdRef.current) setCancellationLoading(false)
      })

    return () => {
      requestIdRef.current += 1
    }
  }, [cancellationOpen, loadRevision, vpsId])

  useEffect(() => {
    if (!archiveOpen) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- every archive opening starts from an empty review so stale eligibility can never enable confirmation
    setArchiveReview(null)
    setArchiveReviewLoading(true)
    setArchiveError(null)
    setArchiveConfirmationName('')

    void getVPSArchiveReview(vpsId)
      .then((review) => {
        if (requestId !== requestIdRef.current) return
        setArchiveReview(review)
      })
      .catch((error: unknown) => {
        if (requestId !== requestIdRef.current) return
        setArchiveError(describeManagementError(error, '加载归档资格失败'))
      })
      .finally(() => {
        if (requestId === requestIdRef.current) setArchiveReviewLoading(false)
      })

    return () => {
      requestIdRef.current += 1
    }
  }, [archiveOpen, loadRevision, vpsId])

  function retryLoad() {
    setLoadRevision((current) => current + 1)
  }

  function beginSubmission(operation: VPSWriteOperation): VPSWriteOwner | null {
    const owner = writeOwnerStore.begin({
      vpsId,
      viewToken,
      generation: mutationGenerationRef.current + 1,
      operation,
    })
    if (!owner) return null
    mutationGenerationRef.current = owner.generation
    return owner
  }

  function submissionIsCurrent(generation: number): boolean {
    return mutationGenerationRef.current === generation
  }

  function finishSubmission(owner: VPSWriteOwner) {
    writeOwnerStore.finish(owner)
  }

  async function prepareCreate(owner: VPSWriteOwner, wireBody: unknown): Promise<VPSPreparedCreateOwner | null> {
    const preparedOwner = await writeOwnerStore.prepareCreate(owner, wireBody)
    if (!preparedOwner) return null
    if (submissionIsCurrent(owner.generation)) return preparedOwner
    writeOwnerStore.finishCreate(preparedOwner, 'not_sent')
    return null
  }

  function finishCreate(owner: VPSPreparedCreateOwner, outcome: VPSCreateSettleOutcome) {
    writeOwnerStore.finishCreate(owner, outcome)
  }

  function closePanel() {
    if (submitting) return
    requestIdRef.current += 1
    setMutationConflict(null)
    setMutationError(null)
    setReadonlyBlocked(false)
    setServiceDraft(INITIAL_SERVICE_DRAFT)
    setDomainDraft(INITIAL_DOMAIN_DRAFT)
    setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
    setLinkDraft(INITIAL_LINK_DRAFT)
    setPendingUnlinkMonitoringInstance(null)
    management.closePanel()
    queueMicrotask(() => managementTriggerRef.current?.focus())
  }
  async function routeIfTerminalVPS(generation: number): Promise<void> {
    try {
      const latest = await getVPSAsset(vpsId)
      if (!submissionIsCurrent(generation)) return
      if (isTerminalVPSLifecycle(latest.lifecycle_status)) {
        setReadonlyBlocked(true)
        navigate(`/archive/${encodeURIComponent(vpsId)}`, { replace: true, state: location.state })
        return
      }
      setDetail(latest)
    } catch {
      if (!submissionIsCurrent(generation)) return
    }
    setReadonlyBlocked(true)
  }

  async function preflightWritable(generation: number): Promise<VPSAssetDetail | null> {
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return null
    }
    try {
      const latest = await getVPSAsset(vpsId)
      if (!submissionIsCurrent(generation)) return null
      if (isTerminalVPSLifecycle(latest.lifecycle_status)) {
        setReadonlyBlocked(true)
        navigate(`/archive/${encodeURIComponent(latest.vps_id)}`, { replace: true, state: location.state })
        return null
      }
      setDetail(latest)
      return latest
    } catch {
      if (!submissionIsCurrent(generation)) return null
      setReadonlyBlocked(true)
      setMutationError('当前状态不允许修改')
      return null
    }
  }

  async function submitFacts(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail || !factDraft) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }
    if (mutationConflict?.draftKind === 'facts' && !mutationConflict.loaded) {
      setMutationError('请先加载最新版本后再保存')
      return
    }
    let input
    try {
      input = buildFactEditInput(factDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, 'VPS 基础信息输入无效'))
      return
    }

    const owner = beginSubmission('facts')
    if (!owner) return
    const { generation } = owner
    try {
      await updateVPSAsset(detail.vps_id, input, { expectedUpdatedAt: detail.updated_at })
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setMutationConflict(null)
      setPageFeedback(refreshed
        ? { tone: 'success', message: '基础信息已更新，概览已刷新。' }
        : { tone: 'warning', message: '基础信息已更新，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
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
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '更新基础信息失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  async function submitDecision(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail || !decisionDraft) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }
    if (mutationConflict?.draftKind === 'decision' && !mutationConflict.loaded) {
      setMutationError('请先加载最新版本后再保存')
      return
    }
    if (decisionDraft.renewalDecision === detail.renewal_decision) {
      setMutationError('请选择一个不同的续费决策')
      return
    }

    const owner = beginSubmission('decision')
    if (!owner) return
    const { generation } = owner
    try {
      const reason = decisionDraft.reason.trim()
      const updated = await updateVPSAsset(detail.vps_id, {
        renewal_decision: decisionDraft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      }, { expectedUpdatedAt: detail.updated_at })
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setMutationConflict(null)
      const nextAction = subscriptionLinkageAction(
        updated.renewal_subscription_linkage,
        detail.vps_id,
        updated.renewal_decision,
      )
      setPageFeedback(refreshed
        ? {
            tone: 'success',
            message: subscriptionLinkageNotice(
              updated.renewal_subscription_linkage,
              '续费决策已更新，概览已刷新',
            ),
            action: nextAction,
          }
        : {
            tone: 'warning',
            message: '续费决策已更新，但概览刷新失败，请稍后手动重试。',
            action: nextAction,
          })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
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
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '更新续费决策失败'))
    } finally {
      finishSubmission(owner)
    }
  }
  async function loadLatestVersion() {
    const owner = beginSubmission(mutationConflictRef.current?.draftKind === 'decision' ? 'decision' : 'facts')
    if (!owner) return
    const { generation } = owner
    try {
      const latest = await getVPSAsset(vpsId)
      if (!submissionIsCurrent(generation)) return
      if (isTerminalVPSLifecycle(latest.lifecycle_status)) {
        navigate(`/archive/${encodeURIComponent(vpsId)}`, { replace: true, state: location.state })
        return
      }
      const currentDecisionDraft = decisionDraftRef.current
      if (
        mutationConflictRef.current?.draftKind === 'decision'
        && currentDecisionDraft
        && decisionDraftAlreadySatisfied(currentDecisionDraft, latest)
      ) {
        setMutationConflict(null)
        setDetail(latest)
        const refreshed = await onOverviewRefresh()
        if (!submissionIsCurrent(generation)) return
        finishSubmission(owner)
        setPageFeedback(refreshed
          ? { tone: 'success', message: '该决策已由其他操作完成' }
          : { tone: 'warning', message: '该决策已由其他操作完成，但概览刷新失败，请稍后手动重试。' })
        management.closePanel()
        queueMicrotask(() => managementTriggerRef.current?.focus())
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
      setDetail(latest)
      setMutationConflict((current) => {
        if (!current) return current
        return {
          ...current,
          loaded: true,
          compare: current.draftKind === 'facts'
            ? factsCompare
            : current.draftKind === 'decision' && decisionDraft
              ? compareDecisionDraft(decisionDraft, latest)
              : [],
        }
      })
      setMutationError(null)
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '加载最新版本失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  async function submitSubscription(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail) return

    setMutationError(null)
    let input
    try {
      input = buildSubscriptionInput(subscriptionDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '订阅输入无效'))
      return
    }

    const owner = beginSubmission('subscription')
    if (!owner) return
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      preparedOwner = await prepareCreate(owner, input)
      if (!preparedOwner) return
      await createVPSSubscription(detail.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPageFeedback(refreshed
        ? { tone: 'success', message: '订阅账单事实已创建，概览已刷新。' }
        : { tone: 'warning', message: '订阅账单事实已创建，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '创建订阅失败'))
    } finally {
      if (preparedOwner) finishCreate(preparedOwner, settleOutcome)
      else finishSubmission(owner)
    }
  }

  async function submitService(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }

    let input: CreateAssetServiceInput
    try {
      input = buildServiceInput(serviceDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '服务输入无效'))
      return
    }

    const owner = beginSubmission('service')
    if (!owner) return
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      const latest = await preflightWritable(generation)
      if (!latest) return
      const wireBody = buildVPSServiceCreateBody(input)
      preparedOwner = await prepareCreate(owner, wireBody)
      if (!preparedOwner) return
      await createVPSService(latest.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!submissionIsCurrent(generation)) return
      setRelationRevision((current) => current + 1)
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setServiceDraft(INITIAL_SERVICE_DRAFT)
      setPageFeedback(refreshed
        ? { tone: 'success', message: '服务记录已创建，概览已刷新。' }
        : { tone: 'warning', message: '服务记录已创建，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!submissionIsCurrent(generation)) return
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '创建服务记录失败'))
    } finally {
      if (preparedOwner) finishCreate(preparedOwner, settleOutcome)
      else finishSubmission(owner)
    }
  }

  async function submitDomain(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }
    if (domainServicesError) {
      setMutationError('服务列表暂不可用，请重试后再关联。')
      return
    }

    let input: CreateAssetDomainInput
    try {
      input = buildDomainInput(domainDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '域名输入无效'))
      return
    }

    const owner = beginSubmission('domain')
    if (!owner) return
    const { generation } = owner
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      const latest = await preflightWritable(generation)
      if (!latest) return
      const wireBody = buildVPSDomainCreateBody(input)
      preparedOwner = await prepareCreate(owner, wireBody)
      if (!preparedOwner) return
      await createVPSDomain(latest.vps_id, input, preparedOwner.idempotencyKey)
      settleOutcome = 'confirmed'
      if (!submissionIsCurrent(generation)) return
      setRelationRevision((current) => current + 1)
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setDomainDraft(INITIAL_DOMAIN_DRAFT)
      setPageFeedback(refreshed
        ? { tone: 'success', message: '域名记录已创建，概览已刷新。' }
        : { tone: 'warning', message: '域名记录已创建，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) settleOutcome = 'idempotency_key_reused'
      if (!submissionIsCurrent(generation)) return
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '创建域名记录失败'))
    } finally {
      if (preparedOwner) finishCreate(preparedOwner, settleOutcome)
      else finishSubmission(owner)
    }
  }

  async function submitValidityExtension(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }
    if (subscriptionsError) {
      setMutationError('订阅证据暂不可用，请重试后再延长有效期。')
      return
    }

    if (activeSubscriptions.length === 0) {
      setMutationError('当前 VPS 没有生效中订阅，无法延长有效期。')
      return
    }
    if (activeSubscriptions.length > 1) {
      setMutationError('当前 VPS 存在多个生效中订阅，无法直接延长有效期。')
      return
    }
    if (!activeSubscription) {
      setMutationError('当前 VPS 没有唯一的生效中订阅，无法延长有效期。')
      return
    }

    let input: ExtendVPSValidityInput
    try {
      input = buildValidityExtensionInput(validityExtensionDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '有效期延长输入无效'))
      return
    }

    if (activeSubscription.renew_at && input.extend_to < activeSubscription.renew_at) {
      setMutationError('延长至日期不能早于当前生效中订阅续费日。')
      return
    }

    const owner = beginSubmission('validity-extension')
    if (!owner) return
    const { generation } = owner
    try {
      const latest = await preflightWritable(generation)
      if (!latest) return
      const result = await extendVPSValidity(latest.vps_id, input)
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setValidityExtensionDraft(INITIAL_VALIDITY_EXTENSION_DRAFT)
      setPageFeedback(refreshed
        ? { tone: 'success', message: `有效期已延长，写入 ${result.steps.length} 个审计步骤，概览已刷新。` }
        : { tone: 'warning', message: `有效期已延长，写入 ${result.steps.length} 个审计步骤，但概览刷新失败，请稍后手动重试。` })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '延长有效期失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  async function submitLink(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail) return

    setMutationError(null)
    if (readonlyBlocked) {
      setMutationError('当前状态不允许修改')
      return
    }

    const monitoringInstanceId = linkDraft.monitoringInstanceId.trim()
    if (!monitoringInstanceId) {
      setMutationError('请选择要关联的监控实例。')
      return
    }

    const owner = beginSubmission('link')
    if (!owner) return
    const { generation } = owner
    try {
      const latest = await preflightWritable(generation)
      if (!latest) return
      await linkVPSMonitoringInstance(latest.vps_id, {
        monitoring_instance_id: monitoringInstanceId,
        note: linkDraft.note.trim(),
      })
      if (!submissionIsCurrent(generation)) return
      setRelationRevision((current) => current + 1)
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setLinkDraft(INITIAL_LINK_DRAFT)
      setPageFeedback(refreshed
        ? { tone: 'success', message: '监控实例关联已更新，概览已刷新。' }
        : { tone: 'warning', message: '监控实例关联已更新，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '关联监控实例失败'))
    } finally {
      finishSubmission(owner)
    }
  }


  async function confirmUnlinkMonitoringInstance(monitoringInstance: VPSMonitoringInstanceSummary) {
    const owner = writeOwnerStore.begin({
      vpsId,
      viewToken,
      generation: mutationGenerationRef.current + 1,
      operation: 'monitoring-unlink',
      monitoringInstanceId: monitoringInstance.monitoring_instance_id,
    })
    if (!owner) return
    mutationGenerationRef.current = owner.generation
    const { generation } = owner
    setLinkFeedback(null)
    setLinkFeedbackIsError(false)

    try {
      const latest = await preflightWritable(generation)
      if (!latest) return
      await unlinkVPSMonitoringInstance(latest.vps_id, {
        monitoring_instance_id: monitoringInstance.monitoring_instance_id,
        note: monitoringInstance.note,
      })
      if (!submissionIsCurrent(generation)) return
      setRelationRevision((current) => current + 1)
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPendingUnlinkMonitoringInstance(null)
      setLinkFeedback(refreshed
        ? '监控实例关联已解除'
        : '监控实例关联已解除，但概览刷新失败，请稍后手动重试。')
      setLinkFeedbackIsError(!refreshed)
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      if (isVPSAssetReadonly(error)) {
        await routeIfTerminalVPS(generation)
      }
      if (!submissionIsCurrent(generation)) return
      setLinkFeedback(describeManagementError(error, '解除监控实例关联失败'))
      setLinkFeedbackIsError(true)
    } finally {
      writeOwnerStore.finish(owner)
    }
  }

  function handleAgentUpgrade(monitoringInstance: VPSMonitoringInstanceSummary) {
    if (READ_ONLY_PREVIEW) return
    navigate(`/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(vpsId)}`, {
      state: location.state,
    })
  }

  async function submitCancellation(input: ApplyCancellationInput) {
    if (!cancellationPreview) return
    const owner = beginSubmission('cancellation')
    if (!owner) return
    const { generation } = owner
    setMutationError(null)
    setCancellationError(null)
    try {
      const result = await applyVPSCancellation(vpsId, input)
      if (!submissionIsCurrent(generation)) return
      setCancellationResult(result)

      const [overviewResult, previewResult] = await Promise.allSettled([
        onOverviewRefresh(),
        getVPSCancellationPreview(vpsId),
      ])
      if (!submissionIsCurrent(generation)) return
      const overviewRefreshed = overviewResult.status === 'fulfilled' && overviewResult.value
      const previewRefreshed = previewResult.status === 'fulfilled'
      if (previewResult.status === 'fulfilled') {
        setCancellationPreview(previewResult.value)
      } else {
        setMutationError('取消/退役动作已执行，但影响预览刷新失败，请关闭后重新打开复核。')
      }
      setPageFeedback(overviewRefreshed && previewRefreshed
        ? { tone: 'success', message: `取消/退役动作已完成，写入 ${result.steps.length} 个审计步骤，概览与影响预览已刷新。` }
        : { tone: 'warning', message: `取消/退役动作已完成，写入 ${result.steps.length} 个审计步骤，但部分刷新失败，请重新复核。` })
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      if (isCancellationPreviewStale(error)) {
        try {
          const preview = await getVPSCancellationPreview(vpsId)
          if (!submissionIsCurrent(generation)) return
          setCancellationPreview(preview)
        } catch {
          if (!submissionIsCurrent(generation)) return
        }
        if (!submissionIsCurrent(generation)) return
        setCancellationError('影响范围已变化，请重新加载预览后再确认')
        return
      }
      setMutationError(describeManagementError(error, '执行取消/退役失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  async function submitArchive() {
    if (archiveReviewLoading || !archiveReview) {
      setArchiveError('归档资格尚未加载完成')
      return
    }
    if (!archiveReview.eligible || archiveReview.blockers.length > 0) {
      setArchiveError('仍有归档阻止项，不能归档')
      return
    }
    const confirmationName = archiveConfirmationName.trim()
    if (confirmationName !== archiveReview.vps.display_name.trim()) {
      setArchiveError('请输入完整 VPS 展示名后再确认归档')
      return
    }

    const owner = beginSubmission('lifecycle')
    if (!owner) return
    const { generation } = owner
    setArchiveError(null)
    try {
      await archiveVPS(vpsId, { confirmation_name: confirmationName })
      if (!submissionIsCurrent(generation)) return
      navigate(`/archive/${encodeURIComponent(vpsId)}`, { replace: true })
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setArchiveError(describeManagementError(error, '归档 VPS 失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  const archiveCopy = vpsLifecycleConfirmationCopy(
    archiveReview?.vps ?? { display_name: displayName },
    'archive',
  )
  const archiveBlocked = !archiveReview?.eligible || (archiveReview?.blockers.length ?? 0) > 0
  const archiveNameMatches = Boolean(
    archiveReview
      && archiveConfirmationName.trim() === archiveReview.vps.display_name.trim(),
  )

  return (
    <>
      {currentWriteOwner && currentWriteOwner.viewToken !== viewToken ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          操作处理中，请等待当前写入完成。
        </p>
      ) : null}
      {pageFeedback && panel === null ? (
        <p
          className={pageFeedback.tone === 'warning'
            ? 'asset-operation-feedback asset-operation-feedback--notice'
            : 'asset-operation-feedback'}
          role="status"
        >
          {pageFeedback.message}
          {pageFeedback.action ? (
            <>
              {' '}
              <Link
                className="text-link"
                to={pageFeedback.action.to}
                onClick={(event) => {
                  if (!pageFeedback.action?.panel) return
                  event.preventDefault()
                  management.openPanel(pageFeedback.action.panel)
                  setPageFeedback(null)
                }}
              >
                {pageFeedback.action.label}
              </Link>
            </>
          ) : null}
        </p>
      ) : null}

      {READ_ONLY_PREVIEW ? null : (
        <VPSOverviewMonitoringOnboarding
          vpsId={vpsId}
          management={management}
          managementTriggerRef={managementTriggerRef}
          onOverviewRefresh={onOverviewRefresh}
          writeOwnerStore={writeOwnerStore}
          viewToken={viewToken}
        />
      )}

      {relationPanelOpen ? (
        <VPSOverviewRelationPanels
          key={`${vpsId}:${panel}:${relationRevision}`}
          vpsId={vpsId}
          management={management}
          readOnly={READ_ONLY_PREVIEW || readonlyBlocked || detailLoading || !detail || isTerminalVPSLifecycle(detail.lifecycle_status)}
          writeBlocked={submitting}
          unlinkingMonitoringInstanceId={unlinkingMonitoringInstanceId}
          pendingUnlinkMonitoringInstance={pendingUnlinkMonitoringInstance}
          linkFeedback={linkFeedback}
          linkFeedbackIsError={linkFeedbackIsError}
          onCreateMonitoringInstance={() => management.openPanel('monitoring-instance-create')}
          onOpenLink={() => management.openPanel('monitoring-instance-link')}
          onUpgradeMonitoringInstance={handleAgentUpgrade}
          onRequestUnlinkMonitoringInstance={(mi) => {
            setLinkFeedback(null)
            setPendingUnlinkMonitoringInstance(mi)
          }}
          onCancelUnlinkMonitoringInstance={() => {
            setPendingUnlinkMonitoringInstance(null)
          }}
          onConfirmUnlinkMonitoringInstance={(mi) => void confirmUnlinkMonitoringInstance(mi)}
          onOpenServiceCreate={() => management.openPanel('service')}
          onOpenDomainCreate={() => management.openPanel('domain')}
        />
      ) : null}

      {READ_ONLY_PREVIEW ? null : (
      <>
      <VPSDetailDialog
        open={factsOpen}
        onClose={closePanel}
        title="编辑 VPS 事实"
        ariaLabel="编辑 VPS 事实"
        template="form"
        contentClassName="vps-dialog--facts"
        persistent={submitting}
        footer={detail && factDraft ? (
          <VPSDialogActions formId={formId} onCancel={closePanel} submitting={submitting} error={mutationError} submitLabel="保存基础信息" cancelLabel="取消编辑" />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载 VPS 事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {detailError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {mutationConflict?.draftKind === 'facts' ? (
            <VPSVersionConflictBanner
              conflict={mutationConflict}
              loading={submitting}
              onLoadLatest={() => void loadLatestVersion()}
            />
          ) : null}
          {detail && factDraft ? (
            <VPSFactsEditForm
              key={detail.updated_at}
              draft={factDraft}
              providers={providers}
              providersLoading={providersLoading}
              providersError={providersError}
              submitting={submitting}
              formId={formId}
              onDraftChange={(nextDraft) => {
                replaceFactDraft(nextDraft)
                setMutationError(null)
              }}
              onSubmit={(event) => void submitFacts(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={decisionOpen}
        onClose={closePanel}
        title="续费决策"
        ariaLabel="续费决策"
        template="decision"
        persistent={submitting}
        footer={detail && decisionDraft ? (
          <VPSDialogActions formId={formId} onCancel={closePanel} submitting={submitting} error={mutationError} disabled={decisionDraft.renewalDecision === detail.renewal_decision} submitLabel="保存续费决策" />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载续费决策…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {detailError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {mutationConflict?.draftKind === 'decision' ? (
            <VPSVersionConflictBanner
              conflict={mutationConflict}
              loading={submitting}
              onLoadLatest={() => void loadLatestVersion()}
            />
          ) : null}
          {detail && decisionDraft ? (
            <VPSRenewalDecisionForm
              detail={detail}
              draft={decisionDraft}
              submitting={submitting}
              formId={formId}
              onDraftChange={setDecisionDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitDecision(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={subscriptionOpen}
        onClose={closePanel}
        title="新增订阅事实"
        ariaLabel="新增订阅事实"
        template="form"
        size="lg"
        persistent={submitting}
        footer={detail ? (
          <VPSDialogActions formId={formId} onCancel={closePanel} submitting={submitting} error={mutationError} submitLabel="新增订阅" />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载订阅事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {detailError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail ? (
            <VPSSubscriptionForm
              detail={detail}
              draft={subscriptionDraft}
              submitting={submitting}
              formId={formId}
              onDraftChange={setSubscriptionDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitSubscription(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={serviceOpen}
        onClose={closePanel}
        title="新增服务"
        ariaLabel="新增服务"
        template="form"
        size="lg"
        persistent={submitting}
        footer={detail ? (
          <VPSDialogActions
            formId={formId}
            onCancel={closePanel}
            submitting={submitting}
            error={mutationError}
            submitLabel="创建服务记录"
          />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载 VPS 事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {targetsError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{targetsError}</p> : null}
          {detailError || targetsError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail ? (
            <VPSServicesForm
              formId={formId}
              draft={serviceDraft}
              targets={targetsError ? [] : targets}
              targetsLoading={targetsLoading}
              targetsError={targetsError}
              submitting={submitting}
              onDraftChange={setServiceDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitService(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={domainOpen}
        onClose={closePanel}
        title="新增域名"
        ariaLabel="新增域名"
        template="form"
        size="lg"
        persistent={submitting}
        footer={detail ? (
          <VPSDialogActions
            formId={formId}
            onCancel={closePanel}
            submitting={submitting}
            error={mutationError}
            disabled={Boolean(domainServicesError)}
            submitLabel="创建域名记录"
          />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading || domainServicesLoading ? <p role="status">正在加载服务与域名事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {domainServicesError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{domainServicesError}</p> : null}
          {targetsError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{targetsError}</p> : null}
          {detailError || domainServicesError || targetsError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail && !domainServicesLoading && !domainServicesError ? (
            <VPSDomainsForm
              formId={formId}
              draft={domainDraft}
              services={domainServices}
              targets={targetsError ? [] : targets}
              targetsLoading={targetsLoading}
              targetsError={targetsError}
              submitting={submitting}
              onDraftChange={setDomainDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitDomain(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={validityExtensionOpen}
        onClose={closePanel}
        title="延长有效期"
        ariaLabel="延长有效期"
        template="form"
        size="lg"
        persistent={submitting}
        footer={detail ? (
          <VPSDialogActions
            formId={formId}
            onCancel={closePanel}
            submitting={submitting}
            error={mutationError}
            disabled={Boolean(subscriptionsError) || subscriptionsLoading || activeSubscriptions.length !== 1}
            submitLabel="保存延长记录"
          />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading || subscriptionsLoading ? <p role="status">正在加载订阅事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {subscriptionsError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{subscriptionsError}</p> : null}
          {detailError || subscriptionsError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail && !subscriptionsLoading && !subscriptionsError ? (
            <>
              {activeSubscriptions.length > 1 ? (
                <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                  当前 VPS 存在多个生效中订阅，无法直接延长有效期。
                </p>
              ) : null}
              {activeSubscriptions.length === 0 ? (
                <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
                  当前 VPS 没有生效中订阅，无法直接延长有效期。
                </p>
              ) : null}
              <VPSValidityExtensionForm
                formId={formId}
                detail={detail}
                activeSubscription={activeSubscription}
                draft={validityExtensionDraft}
                submitting={submitting}
                onDraftChange={setValidityExtensionDraft}
                onFeedbackClear={() => setMutationError(null)}
                onSubmit={(event) => void submitValidityExtension(event)}
              />
            </>
          ) : null}
        </div>
      </VPSDetailDialog>

      <VPSDetailDialog
        open={monitoringLinkOpen}
        onClose={closePanel}
        title="关联监控实例"
        ariaLabel="关联监控实例"
        template="form"
        size="lg"
        persistent={submitting}
        footer={detail ? (
          <VPSDialogActions
            formId={formId}
            onCancel={closePanel}
            submitting={submitting}
            error={mutationError}
            disabled={submitting || unlinkingMonitoringInstanceId !== null}
            submitLabel="关联监控实例"
          />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载 VPS 事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {monitoringInstancesError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{monitoringInstancesError}</p> : null}
          {detailError || monitoringInstancesError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail ? (
            <VPSMonitoringInstanceLinkForm
              formId={formId}
              detail={detail}
              draft={linkDraft}
              monitoring={monitoringInstancesError ? [] : monitoringInstances}
              monitoringInstancesLoading={monitoringInstancesLoading}
              monitoringInstancesError={monitoringInstancesError}
              controlsDisabled={submitting || unlinkingMonitoringInstanceId !== null || Boolean(monitoringInstancesError)}
              submitting={submitting}
              onDraftChange={setLinkDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitLink(event)}
            />
          ) : null}
        </div>
      </VPSDetailDialog>

      <Modal
        open={cancellationOpen}
        onClose={closePanel}
        title="取消 / 退役"
        ariaLabel="取消 / 退役"
        size="xl"
        contentClassName="modal-content--asset-cancel"
        persistent={submitting}
      >
        <div className="vps-detail-modal">
          {cancellationLoading ? <p role="status">正在加载取消/退役影响预览…</p> : null}
          {cancellationError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{cancellationError}</p> : null}
          {cancellationError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {cancellationPreview ? (
            <VPSCancellationWorkbench
              key={`${vpsId}:${cancellationPreview.preview_digest}`}
              preview={cancellationPreview}
              submitting={submitting}
              error={mutationError}
              result={cancellationResult}
              onSubmit={(input) => void submitCancellation(input)}
              onCancel={closePanel}
            />
          ) : null}
        </div>
      </Modal>

      <ActionConfirmationModal
        open={archiveOpen}
        title={archiveCopy.title}
        current={archiveCopy.current}
        result={archiveCopy.result}
        impact={archiveCopy.impact}
        unchanged={archiveCopy.unchanged}
        confirmLabel={submitting ? '归档中…' : archiveCopy.confirmLabel}
        disabled={submitting || archiveReviewLoading || archiveBlocked || !archiveNameMatches}
        cancelDisabled={submitting}
        error={archiveError}
        onCancel={closePanel}
        onConfirm={() => void submitArchive()}
      >
        <div className="asset-lifecycle-confirm">
          <p className="asset-lifecycle-confirm__eyebrow">归档审查</p>
          {archiveReviewLoading ? (
            <p className="asset-lifecycle-confirm__callouts" role="status">正在检查归档资格…</p>
          ) : archiveReview?.blockers.length ? (
            <>
              <h4>归档前仍有需要处理的事项。</h4>
              <ul className="asset-lifecycle-confirm__blockers">
                {archiveReview.blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}
              </ul>
            </>
          ) : archiveReview && !archiveReview.eligible ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              服务端判定当前不具备归档资格，请关闭后处理关联状态再重新检查。
            </p>
          ) : archiveReview ? (
            <>
              {archiveReview.warnings.map((warning) => (
                <p key={warning} className="asset-operation-feedback asset-operation-feedback--notice" role="status">{warning}</p>
              ))}
              <h4>输入 VPS 展示名后才能归档，服务端会再次校验资格。</h4>
              <label className="input-field">
                <span className="input-field__label">输入 VPS 名称确认归档</span>
                <input
                  className="input"
                  aria-label="输入 VPS 名称确认归档"
                  value={archiveConfirmationName}
                  onChange={(event) => {
                    setArchiveConfirmationName(event.target.value)
                    setArchiveError(null)
                  }}
                  placeholder={archiveReview.vps.display_name}
                  disabled={submitting}
                />
                <span className="input-field__hint">需要完整匹配：{archiveReview.vps.display_name}</span>
              </label>
            </>
          ) : (
            <>
              <p className="asset-lifecycle-confirm__callouts">归档资格暂未加载成功，请重试或关闭。</p>
              <Button onClick={retryLoad}>重试加载</Button>
            </>
          )}
        </div>
      </ActionConfirmationModal>
      </>
      )}
    </>
  )
}
