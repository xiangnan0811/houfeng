import { VPSLifecycleWorkspace } from './VPSLifecycleWorkspace'
import { VPSMaintenancePanel } from './VPSMaintenancePanel'
import { useCallback, useEffect, useId, useRef, useState, useSyncExternalStore, type FormEvent, type RefObject } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { Button, Input, Modal } from '../../components/atoms'
import { DependencyStatusCorrection } from '../../components/DependencyStatusCorrection'
import {
  archiveVPS,
  buildVPSDomainCreateBody,
  buildVPSServiceCreateBody,
  createVPSDomain,
  createVPSService,
  createVPSSubscription,
  extendVPSValidity,
  getVPSAsset,
  getVPSArchiveReview,
  startVPSMigration,
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
  ArchiveBlockerDetail,
  ArchiveReview,
  AssetServiceRecord,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  ExtendVPSValidityInput,
  MonitoringInstanceRecord,
  ProviderRecord,
  SubscriptionRecord,
  TargetRecord,
  VPSAssetDetail,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { archiveReviewFromError, isLifecycleActionBlocked } from '../../lib/assetLifecycle'
import { ApiError } from '../../lib/api'
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
import { focusFactFieldForError } from './factFieldFocus'
import { VPSArchiveConfirmDialog } from './VPSArchiveConfirmDialog'
import { VPSVersionConflictBanner } from './VPSVersionConflictBanner'
import { vpsLifecycleConfirmationCopy } from './vpsLifecycleConfirmationCopy'
import {
  describeManagementError,
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
  onMonitoringAssociationChanged?: () => void
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
  onMonitoringAssociationChanged,
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

  const [archiveReview, setArchiveReview] = useState<ArchiveReview | null>(null)
  const [archiveReviewLoading, setArchiveReviewLoading] = useState(false)
  const [archiveError, setArchiveError] = useState<string | null>(null)
  const [archiveConfirmationName, setArchiveConfirmationName] = useState('')
  const [archiveReason, setArchiveReason] = useState('')
  const [neverConnectedConfirmed, setNeverConnectedConfirmed] = useState(false)
  const archiveAttempt = useRef<{ body: string; key: string } | null>(null)
  const [archiveStatusCorrectionTarget, setArchiveStatusCorrectionTarget] = useState<{
    kind: 'service' | 'domain'
    id: string
    name: string
    status: string
  } | null>(null)
  const [migrationReason, setMigrationReason] = useState('')
  const [decisionFieldErrors, setDecisionFieldErrors] = useState<ApiError['field_errors']>([])
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
  const archiveOpen = panel === 'archive'
  const migrationOpen = panel === 'start-migration'
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
          setDecisionDraft({ renewalDecision: nextDetail.renewal_decision, reason: nextDetail.renewal_reason ?? '', reviewAt: nextDetail.renewal_review_at?.slice(0, 10) ?? '' })
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
    if (!archiveOpen) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- every archive opening starts from an empty review so stale eligibility can never enable confirmation
    setArchiveReview(null)
    setArchiveReviewLoading(true)
    setArchiveError(null)
    setArchiveConfirmationName('')
    setNeverConnectedConfirmed(false)

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

  const refreshArchiveReview = useCallback(async (targetVpsId: string) => {
    try {
      const review = await getVPSArchiveReview(targetVpsId)
      setArchiveReview(review)
    } catch {
      // ignore
    }
  }, [])

  function handleArchiveBlockerInline(
    detail: ArchiveBlockerDetail,
    kind: 'service-status' | 'domain-status' | 'residual' | 'restore',
  ) {
    if (kind === 'service-status') {
      setArchiveStatusCorrectionTarget({
        kind: 'service',
        id: detail.object_id,
        name: detail.display_name || detail.object_id,
        status: detail.current_state,
      })
    } else if (kind === 'domain-status') {
      setArchiveStatusCorrectionTarget({
        kind: 'domain',
        id: detail.object_id,
        name: detail.display_name || detail.object_id,
        status: detail.current_state,
      })
    } else if (kind === 'residual') {
      management.openPanel('subscription')
    } else if (kind === 'restore') {
      navigate(`/archive/${encodeURIComponent(vpsId)}`)
    }
  }

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
      input = buildFactEditInput(factDraft, { ipv4: detail.ipv4, ipv6: detail.ipv6 })
    } catch (error: unknown) {
      const message = describeManagementError(error, 'VPS 基础信息输入无效')
      setMutationError(message)
      focusFactFieldForError(formId, message)
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
    if (decisionDraft.renewalDecision === detail.renewal_decision && decisionDraft.reason === (detail.renewal_reason ?? '') && (decisionDraft.reviewAt ?? '') === (detail.renewal_review_at?.slice(0, 10) ?? '')) {
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
        renewal_reason: reason,
        renewal_review_at: decisionDraft.reviewAt ? `${decisionDraft.reviewAt}T00:00:00Z` : null,
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
      if (error instanceof ApiError && error.field_errors.length > 0) {
        setDecisionFieldErrors(error.field_errors)
      }
      setMutationError(error instanceof ApiError && error.field_errors.length > 0
        ? '组合不合法。请按字段提示调整生命周期、用途或续费决策。'
        : describeManagementError(error, '更新续费决策失败'))
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
        ? { tone: 'success', message: '订阅已添加，概览已刷新。' }
        : { tone: 'warning', message: '订阅已添加，但概览刷新失败，请稍后手动重试。' })
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
    let input: ExtendVPSValidityInput
    try {
      input = buildValidityExtensionInput(validityExtensionDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '有效期延长输入无效'))
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
      onMonitoringAssociationChanged?.()
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
      onMonitoringAssociationChanged?.()
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
    const reason = archiveReason.trim()
    if (!reason) {
      setArchiveError('需要填写归档原因。')
      return
    }
    if (confirmationName !== archiveReview.vps.display_name.trim()) {
      setArchiveError('请输入完整 VPS 展示名后再确认归档')
      return
    }

    const owner = beginSubmission('lifecycle')
    if (!owner) return
    const { generation } = owner
    setArchiveError(null)
    try {
      const body = { confirmation_name: confirmationName, reason, preview_digest: archiveReview.preview_digest ?? '', never_connected_confirmation: neverConnectedConfirmed }
      const signature = JSON.stringify({ vpsId, ...body })
      if (archiveAttempt.current?.body !== signature) archiveAttempt.current = { body: signature, key: crypto.randomUUID() }
      await archiveVPS(vpsId, { ...body, idempotency_key: archiveAttempt.current.key })
      if (!submissionIsCurrent(generation)) return
      navigate(`/archive/${encodeURIComponent(vpsId)}`, { replace: true, state: location.state })
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      const review = archiveReviewFromError(error)
      if (review) {
        setArchiveReview(review)
        setArchiveError('归档被拒绝。请按下列对象处理，不要把缺少资格当成操作失败。')
        return
      }
      if (isLifecycleActionBlocked(error)) {
        try {
          const fresh = await getVPSArchiveReview(vpsId)
          if (!submissionIsCurrent(generation)) return
          setArchiveReview(fresh)
          setArchiveError('归档被拒绝。已重新加载审查，请按对象处理后再试。')
        } catch {
          if (!submissionIsCurrent(generation)) return
          setArchiveError('归档被拒绝，但无法确认当前阻止项。不要假定可以继续。')
        }
        return
      }
      setArchiveError(describeManagementError(error, '归档 VPS 失败'))
    } finally {
      finishSubmission(owner)
    }
  }

  async function submitMigration() {
    const reason = migrationReason.trim()
    if (!reason) return
    const owner = beginSubmission('lifecycle')
    if (!owner) return
    const { generation } = owner
    setMutationError(null)
    try {
      const result = await startVPSMigration(vpsId, { reason })
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPageFeedback({
        tone: refreshed ? 'success' : 'warning',
        message: refreshed
          ? `已记录开始迁移，动作 ${result.action.action_id}。已建立跟进事项，请记录来源、目标及结果。`
          : '已记录开始迁移，但概览刷新失败，请稍后手动重试。',
      })
      setMigrationReason('')
      management.closePanel()
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '开始迁移失败'))
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
        // 反馈挂在详情页末尾；悬浮在视口底部，关闭对话框后不必滚到页底才能看到结果。
        <p
          className={pageFeedback.tone === 'warning'
            ? 'asset-operation-feedback asset-operation-feedback--notice asset-operation-feedback--floating'
            : 'asset-operation-feedback asset-operation-feedback--floating'}
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
          onMonitoringAssociationChanged={onMonitoringAssociationChanged}
          writeOwnerStore={writeOwnerStore}
          viewToken={viewToken}
        />
      )}

      <VPSDetailDialog open={!READ_ONLY_PREVIEW && (panel === 'followups' || panel === 'maintenance')} onClose={closePanel} title={panel === 'maintenance' ? 'VPS 维护' : '跟进事项'} ariaLabel={panel === 'maintenance' ? 'VPS 维护' : '跟进事项'} template="form">
        {panel === 'followups' ? <VPSLifecycleWorkspace vpsId={vpsId} kind="followups" onChanged={() => void onOverviewRefresh()} /> : null}
        {panel === 'maintenance' ? <VPSMaintenancePanel vpsId={vpsId} onChanged={() => void onOverviewRefresh()} /> : null}
      </VPSDetailDialog>
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
          <VPSDialogActions formId={formId} onCancel={closePanel} submitting={submitting} error={mutationError}  submitLabel="保存续费决策" />
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
              fieldErrors={decisionFieldErrors}
              onEditFacts={() => management.openPanel('facts')}
              onDraftChange={(next) => { setDecisionDraft(next); setDecisionFieldErrors([]) }}
              onFeedbackClear={() => { setMutationError(null); setDecisionFieldErrors([]) }}
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
            disabled={detailLoading}
            submitLabel="保存延长记录"
          />
        ) : undefined}
      >
        <div className="vps-detail-modal">
          {detailLoading || subscriptionsLoading ? <p role="status">正在加载订阅事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {subscriptionsError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{subscriptionsError}</p> : null}
          {detailError || subscriptionsError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail ? (
            <>
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

      <VPSArchiveConfirmDialog
        open={archiveOpen}
        title={archiveCopy.title}
        confirmLabel={submitting ? '归档中…' : archiveCopy.confirmLabel}
        submitting={submitting}
        displayName={displayName}
        review={archiveReview}
        loading={archiveReviewLoading}
        error={archiveError}
        reason={archiveReason}
        confirmationName={archiveConfirmationName}
        neverConnectedConfirmed={neverConnectedConfirmed}
        confirmDisabled={submitting || archiveReviewLoading || archiveBlocked || !archiveNameMatches || archiveReason.trim() === '' || Boolean(archiveReview?.online_evidence?.manual_confirmation_required && !neverConnectedConfirmed)}
        onReasonChange={(value) => { setArchiveReason(value); setArchiveError(null) }}
        onConfirmationNameChange={(value) => { setArchiveConfirmationName(value); setArchiveError(null) }}
        onNeverConnectedChange={setNeverConnectedConfirmed}
        onConfirm={() => void submitArchive()}
        onCancel={closePanel}
        onRetry={retryLoad}
        vpsId={vpsId}
        onInlineBlocker={handleArchiveBlockerInline}
      />
      <Modal
        open={migrationOpen}
        onClose={closePanel}
        title="开始迁移"
        ariaLabel="开始迁移"
        size="md"
        persistent={submitting}
      >
        <div className="asset-lifecycle-confirm">
          <p>记录人工迁移计划及后续结果。资源、服务关联和续费意向需分别处理。</p>
          <p>迁移完成后可结束原关联；结束使用并归档是独立操作。</p>
          <Input label="原因" value={migrationReason} disabled={submitting} onChange={(event) => setMigrationReason(event.target.value)} />
          {mutationError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{mutationError}</p> : null}
          <div className="page-form-actions">
            <Button variant="secondary" onClick={closePanel} disabled={submitting}>取消</Button>
            <Button onClick={() => void submitMigration()} disabled={submitting || migrationReason.trim() === ''}>确认开始迁移</Button>
          </div>
        </div>
      </Modal>
      {archiveStatusCorrectionTarget ? (
        <DependencyStatusCorrection
          open
          kind={archiveStatusCorrectionTarget.kind}
          objectId={archiveStatusCorrectionTarget.id}
          displayName={archiveStatusCorrectionTarget.name}
          currentStatus={archiveStatusCorrectionTarget.status}
          parentLifecycle={archiveReview?.vps.lifecycle_status ?? detail?.lifecycle_status ?? 'cancelled'}
          onClose={() => setArchiveStatusCorrectionTarget(null)}
          onCompleted={() => {
            setArchiveStatusCorrectionTarget(null)
            void refreshArchiveReview(vpsId)
            void onOverviewRefresh?.()
          }}
        />
      ) : null}
      </>
      )}
    </>
  )
}
