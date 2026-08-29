import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
  type RefObject,
} from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { Button, Modal } from '../../components/atoms'
import { createVPSMonitoringInstance, getVPSAsset } from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type { VPSAssetDetail, VPSMonitoringInstanceSummary } from '../../lib/types'
import type { VPSManagementController } from './hooks/useVPSManagementController'
import type { MonitoringInstanceCreateDraftState } from './types'
import {
  buildMonitoringInstanceCreateInput,
  monitoringInstanceCreateDraftFromDetail,
} from './vpsDetailHelpers'
import { VPSMonitoringInstanceCreateForm } from './VPSMonitoringInstanceCreateForm'
import { describeManagementError, isIdempotencyKeyReused } from './vpsManagementHelpers'
import { isStableMonitoringInstanceID } from './vpsOverviewDestination'
import type {
  VPSCreateSettleOutcome,
  VPSPreparedCreateOwner,
  VPSWriteOwnerStore,
} from './vpsWriteOwnerStore'

type VPSOverviewMonitoringOnboardingProps = {
  vpsId: string
  management: VPSManagementController
  managementTriggerRef: RefObject<HTMLButtonElement | null>
  onOverviewRefresh: () => Promise<boolean>
  writeOwnerStore: VPSWriteOwnerStore
  viewToken: string
}

type LoadState = {
  vpsId: string
  viewToken: string
  authorityGeneration: number
  loading: boolean
  detail: VPSAssetDetail | null
  draft: MonitoringInstanceCreateDraftState | null
  error: string | null
}

type PanelAuthority = {
  vpsId: string
  viewToken: string
  panelOpen: boolean
  generation: number
}

type OnboardingFeedback = {
  vpsId: string
  message: string
  to?: string
  actionLabel?: string
}

type OwnCreateAuthority = {
  vpsId: string
  viewToken: string
  authorityGeneration: number
  ownerToken: string
}

type ExternalOwnerObservation = {
  vpsId: string
  viewToken: string
  authorityGeneration: number
  ownerToken: string
  revalidationStarted: boolean
}

const MULTIPLE_LINKS_WARNING = '检测到多个 active 监控关联，请先人工复核。'
const INVALID_MONITORING_AUTHORITY_ERROR = '监控关联证据无效，请重试加载。'

function onboardingPath(vpsId: string, monitoringInstanceId: unknown): string | null {
  if (!isStableMonitoringInstanceID(monitoringInstanceId)) return null
  return `/monitoring/${encodeURIComponent(monitoringInstanceId)}?onboarding=1&return_vps=${encodeURIComponent(vpsId)}`
}

function authoritativeActiveLinks(detail: VPSAssetDetail): VPSMonitoringInstanceSummary[] | null {
  const links: unknown = detail.monitoring_instance_links
  const declaredCount: unknown = detail.active_monitoring_instance_link_count
  if (
    !Array.isArray(links)
    || typeof declaredCount !== 'number'
    || !Number.isFinite(declaredCount)
    || !Number.isInteger(declaredCount)
    || declaredCount < 0
    || links.length !== declaredCount
  ) return null
  if (!links.every((link: unknown) => (
    typeof link === 'object'
    && link !== null
    && isStableMonitoringInstanceID((link as { monitoring_instance_id?: unknown }).monitoring_instance_id)
  ))) return null
  return links as VPSMonitoringInstanceSummary[]
}

function observationMatchesAuthority(
  observation: ExternalOwnerObservation | null,
  vpsId: string,
  viewToken: string,
  authorityGeneration: number,
): observation is ExternalOwnerObservation {
  return observation !== null
    && observation.vpsId === vpsId
    && observation.viewToken === viewToken
    && observation.authorityGeneration === authorityGeneration
}

export function VPSOverviewMonitoringOnboarding({
  vpsId,
  management,
  managementTriggerRef,
  onOverviewRefresh,
  writeOwnerStore,
  viewToken,
}: VPSOverviewMonitoringOnboardingProps) {
  const navigate = useNavigate()
  const panelOpen = management.panel === 'monitoring-instance-create'
  const closeManagementPanel = management.closePanel
  const openManagementPanel = management.openPanel
  const [panelAuthority, setPanelAuthority] = useState<PanelAuthority>(() => ({
    vpsId,
    viewToken,
    panelOpen,
    generation: 0,
  }))
  if (
    panelAuthority.vpsId !== vpsId
    || panelAuthority.viewToken !== viewToken
    || panelAuthority.panelOpen !== panelOpen
  ) {
    setPanelAuthority({
      vpsId,
      viewToken,
      panelOpen,
      generation: panelAuthority.generation + 1,
    })
  }
  const writeOwners = useSyncExternalStore(
    writeOwnerStore.subscribe,
    writeOwnerStore.getSnapshot,
    writeOwnerStore.getSnapshot,
  )
  const currentWriteOwner = writeOwners.get(vpsId)
  const [ownCreateAuthority, setOwnCreateAuthority] = useState<OwnCreateAuthority | null>(null)
  const ownCreateSubmitting = ownCreateAuthority !== null
    && currentWriteOwner?.token === ownCreateAuthority.ownerToken
    && ownCreateAuthority.vpsId === vpsId
    && ownCreateAuthority.viewToken === viewToken
    && ownCreateAuthority.authorityGeneration === panelAuthority.generation
  const [externalOwnerObservation, setExternalOwnerObservation] = useState<ExternalOwnerObservation | null>(null)
  if (panelOpen && currentWriteOwner && !ownCreateSubmitting && (
    !observationMatchesAuthority(
      externalOwnerObservation,
      vpsId,
      viewToken,
      panelAuthority.generation,
    )
    || externalOwnerObservation.ownerToken !== currentWriteOwner.token
  )) {
    setExternalOwnerObservation({
      vpsId,
      viewToken,
      authorityGeneration: panelAuthority.generation,
      ownerToken: currentWriteOwner.token,
      revalidationStarted: false,
    })
  }
  const postOwnerRevalidationRequired = panelOpen
    && currentWriteOwner === undefined
    && observationMatchesAuthority(
      externalOwnerObservation,
      vpsId,
      viewToken,
      panelAuthority.generation,
    )
  const writeBlocked = currentWriteOwner !== undefined || postOwnerRevalidationRequired
  const [loadRevision, setLoadRevision] = useState(0)
  const [loadState, setLoadState] = useState<LoadState>({
    vpsId,
    viewToken,
    authorityGeneration: panelAuthority.generation,
    loading: false,
    detail: null,
    draft: null,
    error: null,
  })
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<OnboardingFeedback | null>(null)
  const generationRef = useRef(0)
  const mountedRef = useRef(false)
  const externalOwnerObservationRef = useRef<ExternalOwnerObservation | null>(externalOwnerObservation)
  const postOwnerRevalidationRequestRef = useRef<ExternalOwnerObservation | null>(null)
  const currentIdentityRef = useRef({
    vpsId,
    viewToken,
    panelOpen,
    authorityGeneration: panelAuthority.generation,
  })

  useLayoutEffect(() => {
    currentIdentityRef.current = {
      vpsId,
      viewToken,
      panelOpen,
      authorityGeneration: panelAuthority.generation,
    }
  }, [panelAuthority.generation, panelOpen, viewToken, vpsId])

  useLayoutEffect(() => {
    externalOwnerObservationRef.current = externalOwnerObservation
  }, [externalOwnerObservation])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      generationRef.current += 1
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- route-local completion feedback must be discarded when the VPS route identity changes
    setFeedback(null)
  }, [vpsId])

  useEffect(() => {
    if (
      !postOwnerRevalidationRequired
      || !externalOwnerObservation
      || externalOwnerObservation.revalidationStarted
    ) return

    const startedObservation = {
      ...externalOwnerObservation,
      revalidationStarted: true,
    }
    externalOwnerObservationRef.current = startedObservation
    postOwnerRevalidationRequestRef.current = startedObservation
    // eslint-disable-next-line react-hooks/set-state-in-effect -- owner disappearance requires one authority read before a stale zero-link form may re-enable
    setExternalOwnerObservation(startedObservation)
    setLoadRevision((current) => current + 1)
  }, [externalOwnerObservation, postOwnerRevalidationRequired])

  const generationIsCurrent = useCallback((
    generation: number,
    targetVPSID: string,
    targetViewToken: string,
    targetAuthorityGeneration: number,
  ): boolean => {
    const current = currentIdentityRef.current
    return mountedRef.current
      && generationRef.current === generation
      && current.vpsId === targetVPSID
      && current.viewToken === targetViewToken
      && current.panelOpen
      && current.authorityGeneration === targetAuthorityGeneration
  }, [])

  const resetLocalForm = useCallback((authority = panelAuthority) => {
    setLoadState({
      vpsId: authority.vpsId,
      viewToken: authority.viewToken,
      authorityGeneration: authority.generation,
      loading: false,
      detail: null,
      draft: null,
      error: null,
    })
    setMutationError(null)
  }, [panelAuthority])

  const showMultipleLinks = useCallback((authority: PanelAuthority) => {
    resetLocalForm(authority)
    setFeedback({ vpsId: authority.vpsId, message: MULTIPLE_LINKS_WARNING })
    openManagementPanel('monitoring-instance-evidence')
  }, [openManagementPanel, resetLocalForm])

  useEffect(() => {
    if (!panelOpen) return

    const generation = ++generationRef.current
    const targetVPSID = vpsId
    const targetViewToken = viewToken
    const loadAuthority = panelAuthority
    const requestedPostOwnerRevalidation = postOwnerRevalidationRequestRef.current
    const postOwnerRevalidation = requestedPostOwnerRevalidation?.revalidationStarted
      && observationMatchesAuthority(
        requestedPostOwnerRevalidation,
        targetVPSID,
        targetViewToken,
        loadAuthority.generation,
      )
      ? requestedPostOwnerRevalidation
      : null
    const postOwnerRevalidationIsCurrent = () => {
      if (!postOwnerRevalidation) return true
      const currentObservation = externalOwnerObservationRef.current
      return observationMatchesAuthority(
        currentObservation,
        targetVPSID,
        targetViewToken,
        loadAuthority.generation,
      )
        && currentObservation.ownerToken === postOwnerRevalidation.ownerToken
        && currentObservation.revalidationStarted
        && writeOwnerStore.getSnapshot().get(targetVPSID) === undefined
    }
    const completePostOwnerRevalidation = () => {
      if (!postOwnerRevalidation) return true
      if (!postOwnerRevalidationIsCurrent()) return false

      externalOwnerObservationRef.current = null
      postOwnerRevalidationRequestRef.current = null
      setExternalOwnerObservation((current) => (
        observationMatchesAuthority(
          current,
          targetVPSID,
          targetViewToken,
          loadAuthority.generation,
        )
        && current.ownerToken === postOwnerRevalidation.ownerToken
          ? null
          : current
      ))
      return true
    }
    setLoadState({
      vpsId: targetVPSID,
      viewToken: targetViewToken,
      authorityGeneration: loadAuthority.generation,
      loading: true,
      detail: null,
      draft: null,
      error: null,
    })
    setMutationError(null)
    setFeedback((current) => current?.vpsId === targetVPSID ? null : current)

    void getVPSAsset(targetVPSID).then((latest) => {
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, loadAuthority.generation)) return
      if (!postOwnerRevalidationIsCurrent()) return
      const activeLinks = authoritativeActiveLinks(latest)
      if (!activeLinks) {
        setLoadState({
          vpsId: targetVPSID,
          viewToken: targetViewToken,
          authorityGeneration: loadAuthority.generation,
          loading: false,
          detail: null,
          draft: null,
          error: INVALID_MONITORING_AUTHORITY_ERROR,
        })
        return
      }
      if (!completePostOwnerRevalidation()) return
      const linkCount = activeLinks.length
      if (linkCount === 1) {
        const monitoringInstanceId = activeLinks[0]?.monitoring_instance_id
        const to = onboardingPath(targetVPSID, monitoringInstanceId)
        if (!to) {
          setLoadState({
            vpsId: targetVPSID,
            viewToken: targetViewToken,
            authorityGeneration: loadAuthority.generation,
            loading: false,
            detail: null,
            draft: null,
            error: INVALID_MONITORING_AUTHORITY_ERROR,
          })
          return
        }
        resetLocalForm(loadAuthority)
        closeManagementPanel()
        navigate(to)
        return
      }
      if (linkCount > 1) {
        showMultipleLinks(loadAuthority)
        return
      }
      setLoadState({
        vpsId: targetVPSID,
        viewToken: targetViewToken,
        authorityGeneration: loadAuthority.generation,
        loading: false,
        detail: latest,
        draft: monitoringInstanceCreateDraftFromDetail(latest),
        error: null,
      })
    }).catch((error: unknown) => {
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, loadAuthority.generation)) return
      if (!postOwnerRevalidationIsCurrent()) return
      setLoadState({
        vpsId: targetVPSID,
        viewToken: targetViewToken,
        authorityGeneration: loadAuthority.generation,
        loading: false,
        detail: null,
        draft: null,
        error: describeManagementError(error, '加载 VPS 监控关联失败'),
      })
    })

    return () => {
      if (generationRef.current === generation) generationRef.current += 1
    }
  }, [
    loadRevision,
    generationIsCurrent,
    closeManagementPanel,
    navigate,
    panelAuthority,
    panelOpen,
    resetLocalForm,
    showMultipleLinks,
    viewToken,
    vpsId,
    writeOwnerStore,
  ])

  function closePanel() {
    generationRef.current += 1
    resetLocalForm()
    closeManagementPanel()
    queueMicrotask(() => managementTriggerRef.current?.focus())
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (loadState.vpsId !== vpsId || !loadState.detail || !loadState.draft) return
    if (
      loadState.viewToken !== viewToken
      || loadState.authorityGeneration !== panelAuthority.generation
      || !panelAuthority.panelOpen
    ) return
    if (writeBlocked) {
      setMutationError('上一次保存仍在进行，请稍后再试')
      return
    }

    let input
    try {
      input = buildMonitoringInstanceCreateInput(loadState.draft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, '监控实例输入无效'))
      return
    }

    const generation = ++generationRef.current
    const targetVPSID = vpsId
    const targetViewToken = viewToken
    const targetAuthorityGeneration = panelAuthority.generation
    const owner = writeOwnerStore.begin({
      vpsId: targetVPSID,
      viewToken: targetViewToken,
      generation,
      operation: 'monitoring-create',
    })
    if (!owner) {
      setMutationError('上一次保存仍在进行，请稍后再试')
      return
    }

    setOwnCreateAuthority({
      vpsId: targetVPSID,
      viewToken: targetViewToken,
      authorityGeneration: targetAuthorityGeneration,
      ownerToken: owner.token,
    })

    setMutationError(null)
    let preparedOwner: VPSPreparedCreateOwner | null = null
    let settleOutcome: VPSCreateSettleOutcome = 'unknown'
    try {
      preparedOwner = await writeOwnerStore.prepareCreate(owner, input)
      if (!preparedOwner) return
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) {
        settleOutcome = 'not_sent'
        return
      }

      const created = await createVPSMonitoringInstance(
        targetVPSID,
        input,
        preparedOwner.idempotencyKey,
      )
      settleOutcome = 'confirmed'
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) return

      const to = onboardingPath(targetVPSID, created.monitoring_instance_id)
      let refreshed = false
      try {
        refreshed = await onOverviewRefresh()
      } catch {
        refreshed = false
      }
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) return

      resetLocalForm(panelAuthority)
      closeManagementPanel()
      if (to && refreshed) {
        navigate(to)
        return
      }
      if (!to) {
        setFeedback({
          vpsId: targetVPSID,
          message: refreshed
            ? '监控实例已创建并关联，但返回的监控实例标识无效，请从监控列表继续接入。'
            : '监控实例已创建并关联，但返回的监控实例标识无效且概览刷新失败，请从监控列表核对。',
          to: '/monitoring',
          actionLabel: '前往监控列表',
        })
        queueMicrotask(() => managementTriggerRef.current?.focus())
        return
      }
      setFeedback({
        vpsId: targetVPSID,
        message: '监控实例已创建并关联，但概览刷新失败',
        to,
      })
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (isIdempotencyKeyReused(error)) {
        settleOutcome = 'idempotency_key_reused'
        if (generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) {
          setMutationError('同一幂等键已用于不同的监控实例内容，请重试')
        }
        return
      }
      if (!generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) return

      if (error instanceof ApiError && error.status === 409) {
        try {
          const latest = await getVPSAsset(targetVPSID)
          if (!generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) return
          const activeLinks = authoritativeActiveLinks(latest)
          if (activeLinks?.length === 1) {
            const monitoringInstanceId = activeLinks[0]?.monitoring_instance_id
            const to = onboardingPath(targetVPSID, monitoringInstanceId)
            if (to) {
              resetLocalForm(panelAuthority)
              closeManagementPanel()
              navigate(to)
              return
            }
          }
          if (activeLinks && activeLinks.length > 1) {
            showMultipleLinks(panelAuthority)
            return
          }
        } catch {
          // The original create conflict is the truthful error when convergence cannot be established.
        }
      }
      if (generationIsCurrent(generation, targetVPSID, targetViewToken, targetAuthorityGeneration)) {
        setMutationError(describeManagementError(error, '创建监控实例失败'))
      }
    } finally {
      if (preparedOwner) writeOwnerStore.finishCreate(preparedOwner, settleOutcome)
      else writeOwnerStore.finish(owner)
      if (mountedRef.current) {
        setOwnCreateAuthority((current) => current?.ownerToken === owner.token ? null : current)
      }
    }
  }

  const currentLoad = panelOpen
    && panelAuthority.panelOpen
    && loadState.vpsId === panelAuthority.vpsId
    && loadState.viewToken === panelAuthority.viewToken
    && loadState.authorityGeneration === panelAuthority.generation
    ? loadState
    : null
  const visibleFeedback = feedback?.vpsId === vpsId && !panelOpen ? feedback : null

  return (
    <>
      {visibleFeedback ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          {visibleFeedback.message}
          {visibleFeedback.to ? (
            <>
              {' '}
              <Link className="text-link" to={visibleFeedback.to}>
                {visibleFeedback.actionLabel ?? '继续接入 agent'}
              </Link>
            </>
          ) : null}
        </p>
      ) : null}

      <Modal
        open={panelOpen}
        onClose={closePanel}
        title="接入/升级 agent"
        ariaLabel="接入/升级 agent"
        size="xl"
        persistent={ownCreateSubmitting}
      >
        <div className="vps-detail-modal">
          {currentLoad?.loading ? <p role="status">正在检查监控关联…</p> : null}
          {currentLoad?.error ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              {currentLoad.error}
            </p>
          ) : null}
          {currentLoad?.error ? (
            <Button onClick={() => setLoadRevision((current) => current + 1)}>重试加载</Button>
          ) : null}
          {currentLoad?.detail && currentLoad.draft ? (
            <VPSMonitoringInstanceCreateForm
              detail={currentLoad.detail}
              draft={currentLoad.draft}
              submitting={ownCreateSubmitting}
              submitDisabled={writeBlocked}
              error={mutationError}
              notice={null}
              onCancel={closePanel}
              onDraftChange={(draft) => {
                setLoadState((current) => (
                  current.vpsId === panelAuthority.vpsId
                  && current.viewToken === panelAuthority.viewToken
                  && current.authorityGeneration === panelAuthority.generation
                    ? { ...current, draft }
                    : current
                ))
              }}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submit(event)}
            />
          ) : null}
        </div>
      </Modal>
    </>
  )
}
