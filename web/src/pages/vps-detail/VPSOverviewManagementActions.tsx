import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Modal } from '../../components/atoms'
import { VPSCancellationWorkbench } from '../../components/VPSCancellationWorkbench'
import {
  applyVPSCancellation,
  archiveVPS,
  createVPSSubscription,
  getVPSAsset,
  getVPSArchiveReview,
  getVPSCancellationPreview,
  listProviders,
  updateVPSAsset,
} from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type {
  ApplyCancellationInput,
  ArchiveReview,
  CancellationPreview,
  LifecycleActionResult,
  ProviderRecord,
  VPSAssetDetail,
} from '../../lib/types'
import { VPSFactsEditForm } from './VPSFactsEditForm'
import type { VPSManagementController } from './hooks/useVPSManagementController'
import { VPSRenewalDecisionForm } from './VPSRenewalDecisionForm'
import { VPSOverviewRelationPanels } from './VPSOverviewRelationPanels'
import { VPSSubscriptionForm } from './VPSSubscriptionForm'
import type { DecisionDraftState, FactEditFormState, SubscriptionDraftState } from './types'
import {
  buildFactEditInput,
  buildSubscriptionInput,
  detailToFactEditForm,
  INITIAL_SUBSCRIPTION_DRAFT,
} from './vpsDetailHelpers'
import { vpsLifecycleConfirmationCopy } from './vpsLifecycleConfirmationCopy'
import {
  describeManagementError,
  subscriptionLinkageAction,
  subscriptionLinkageNotice,
  type ManagementFeedbackAction,
} from './vpsManagementHelpers'

type Props = {
  vpsId: string
  displayName: string
  lifecycleStatus: string
  management: VPSManagementController
  managementTriggerRef: RefObject<HTMLButtonElement | null>
  onOverviewRefresh: () => Promise<boolean>
}

type PageFeedback = {
  tone: 'success' | 'warning'
  message: string
  action?: ManagementFeedbackAction | null
}

export function VPSOverviewManagementActions({
  vpsId,
  displayName,
  lifecycleStatus,
  management,
  managementTriggerRef,
  onOverviewRefresh,
}: Props) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [detail, setDetail] = useState<VPSAssetDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [factDraft, setFactDraft] = useState<FactEditFormState | null>(null)
  const [decisionDraft, setDecisionDraft] = useState<DecisionDraftState | null>(null)
  const [subscriptionDraft, setSubscriptionDraft] = useState<SubscriptionDraftState>(INITIAL_SUBSCRIPTION_DRAFT)
  const [providers, setProviders] = useState<ProviderRecord[]>([])
  const [providersLoading, setProvidersLoading] = useState(false)
  const [providersError, setProvidersError] = useState<string | null>(null)
  const [cancellationPreview, setCancellationPreview] = useState<CancellationPreview | null>(null)
  const [cancellationResult, setCancellationResult] = useState<LifecycleActionResult | null>(null)
  const [cancellationLoading, setCancellationLoading] = useState(false)
  const [cancellationError, setCancellationError] = useState<string | null>(null)
  const [archiveReview, setArchiveReview] = useState<ArchiveReview | null>(null)
  const [archiveReviewLoading, setArchiveReviewLoading] = useState(false)
  const [archiveError, setArchiveError] = useState<string | null>(null)
  const [archiveConfirmationName, setArchiveConfirmationName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [pageFeedback, setPageFeedback] = useState<PageFeedback | null>(null)
  const [loadRevision, setLoadRevision] = useState(0)
  const requestIdRef = useRef(0)
  const mutationGenerationRef = useRef(0)
  const submissionLockRef = useRef(false)

  const panel = management.panel
  const factsOpen = panel === 'facts'
  const decisionOpen = panel === 'decision'
  const subscriptionOpen = panel === 'subscription'
  const cancellationOpen = panel === 'cancellation'
  const archiveOpen = panel === 'archive'
  const relationPanelOpen = panel === 'monitoring-instance-evidence'
    || panel === 'services-detail'
    || panel === 'domains-detail'
  const detailPanelOpen = factsOpen || decisionOpen || subscriptionOpen

  useEffect(() => {
    mutationGenerationRef.current += 1
    submissionLockRef.current = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- route identity invalidates any in-flight mutation UI owner
    setSubmitting(false)
    return () => {
      mutationGenerationRef.current += 1
      submissionLockRef.current = false
    }
  }, [vpsId])

  useEffect(() => {
    const workbench = searchParams.get('workbench')
    if (!workbench) return
    if (workbench === 'archive' && lifecycleStatus !== 'to_cancel' && lifecycleStatus !== 'cancelled') {
      const next = new URLSearchParams(searchParams)
      next.delete('workbench')
      setSearchParams(next, { replace: true })
      return
    }
    if (workbench === 'cancellation') management.openPanel('cancellation')
    else if (workbench === 'subscription') management.openPanel('subscription')
    else if (workbench === 'decision') management.openPanel('decision')
    else if (workbench === 'archive') management.openPanel('archive')
    else return
    const next = new URLSearchParams(searchParams)
    next.delete('workbench')
    setSearchParams(next, { replace: true })
    // Deep-link opens once per VPS identity; later search edits must not re-open closed panels.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vpsId, lifecycleStatus])

  useEffect(() => {
    if (!detailPanelOpen) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- a newly selected panel must synchronously invalidate the prior panel's detail before its async read starts
    setDetail(null)
    setFactDraft(null)
    setDecisionDraft(null)
    setSubscriptionDraft(INITIAL_SUBSCRIPTION_DRAFT)
    setDetailLoading(true)
    setDetailError(null)
    setProviders([])
    setProvidersLoading(factsOpen)
    setProvidersError(null)
    setMutationError(null)

    void getVPSAsset(vpsId)
      .then((nextDetail) => {
        if (requestId !== requestIdRef.current) return
        setDetail(nextDetail)
        if (factsOpen) setFactDraft(detailToFactEditForm(nextDetail))
        if (decisionOpen) {
          setDecisionDraft({ renewalDecision: nextDetail.renewal_decision, reason: '' })
        }
      })
      .catch((error: unknown) => {
        if (requestId !== requestIdRef.current) return
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

    return () => {
      requestIdRef.current += 1
    }
  }, [decisionOpen, detailPanelOpen, factsOpen, loadRevision, subscriptionOpen, vpsId])

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

  function beginSubmission(): number | null {
    if (submissionLockRef.current) return null
    submissionLockRef.current = true
    setSubmitting(true)
    return ++mutationGenerationRef.current
  }

  function submissionIsCurrent(generation: number): boolean {
    return mutationGenerationRef.current === generation
  }

  function finishSubmission(generation: number) {
    if (!submissionIsCurrent(generation)) return
    submissionLockRef.current = false
    setSubmitting(false)
  }

  function closePanel() {
    if (submitting) return
    requestIdRef.current += 1
    management.closePanel()
    queueMicrotask(() => managementTriggerRef.current?.focus())
  }

  async function submitFacts(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail || !factDraft) return

    setMutationError(null)
    let input
    try {
      input = buildFactEditInput(factDraft)
    } catch (error: unknown) {
      setMutationError(describeManagementError(error, 'VPS 基础信息输入无效'))
      return
    }

    const generation = beginSubmission()
    if (generation === null) return
    try {
      await updateVPSAsset(detail.vps_id, input, { expectedUpdatedAt: detail.updated_at })
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPageFeedback(refreshed
        ? { tone: 'success', message: '基础信息已更新，概览已刷新。' }
        : { tone: 'warning', message: '基础信息已更新，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '更新基础信息失败'))
    } finally {
      finishSubmission(generation)
    }
  }

  async function submitDecision(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!detail || !decisionDraft) return

    setMutationError(null)
    if (decisionDraft.renewalDecision === detail.renewal_decision) {
      setMutationError('请选择一个不同的续费决策')
      return
    }

    const generation = beginSubmission()
    if (generation === null) return
    try {
      const reason = decisionDraft.reason.trim()
      const updated = await updateVPSAsset(detail.vps_id, {
        renewal_decision: decisionDraft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      }, { expectedUpdatedAt: detail.updated_at })
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPageFeedback(refreshed
        ? {
            tone: 'success',
            message: subscriptionLinkageNotice(
              updated.renewal_subscription_linkage,
              '续费决策已更新，概览已刷新',
            ),
            action: subscriptionLinkageAction(updated.renewal_subscription_linkage, detail.vps_id),
          }
        : { tone: 'warning', message: '续费决策已更新，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '更新续费决策失败'))
    } finally {
      finishSubmission(generation)
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

    const generation = beginSubmission()
    if (generation === null) return
    try {
      await createVPSSubscription(detail.vps_id, input)
      if (!submissionIsCurrent(generation)) return
      const refreshed = await onOverviewRefresh()
      if (!submissionIsCurrent(generation)) return
      setPageFeedback(refreshed
        ? { tone: 'success', message: '订阅账单事实已创建，概览已刷新。' }
        : { tone: 'warning', message: '订阅账单事实已创建，但概览刷新失败，请稍后手动重试。' })
      management.closePanel()
      queueMicrotask(() => managementTriggerRef.current?.focus())
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setMutationError(describeManagementError(error, '创建订阅失败'))
    } finally {
      finishSubmission(generation)
    }
  }

  async function submitCancellation(input: ApplyCancellationInput) {
    if (!cancellationPreview) return
    const generation = beginSubmission()
    if (generation === null) return
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
      if (error instanceof ApiError && error.status === 409 && error.message === 'cancellation preview stale') {
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
      finishSubmission(generation)
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

    const generation = beginSubmission()
    if (generation === null) return
    setArchiveError(null)
    try {
      await archiveVPS(vpsId, { confirmation_name: confirmationName })
      if (!submissionIsCurrent(generation)) return
      navigate(`/archive/${encodeURIComponent(vpsId)}`, { replace: true })
    } catch (error: unknown) {
      if (!submissionIsCurrent(generation)) return
      setArchiveError(describeManagementError(error, '归档 VPS 失败'))
    } finally {
      finishSubmission(generation)
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

      {relationPanelOpen ? (
        <VPSOverviewRelationPanels
          key={`${vpsId}:${panel}`}
          vpsId={vpsId}
          management={management}
        />
      ) : null}

      <Modal
        open={factsOpen}
        onClose={closePanel}
        title="编辑 VPS 事实"
        ariaLabel="编辑 VPS 事实"
        size="xl"
        persistent={submitting}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载 VPS 事实…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {detailError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail && factDraft ? (
            <VPSFactsEditForm
              draft={factDraft}
              providers={providers}
              providersLoading={providersLoading}
              providersError={providersError}
              submitting={submitting}
              error={mutationError}
              notice={null}
              onCancel={closePanel}
              onDraftChange={(nextDraft) => {
                setFactDraft(nextDraft)
                setMutationError(null)
              }}
              onSubmit={(event) => void submitFacts(event)}
            />
          ) : null}
        </div>
      </Modal>

      <Modal
        open={decisionOpen}
        onClose={closePanel}
        title="续费决策"
        ariaLabel="续费决策"
        size="lg"
        persistent={submitting}
      >
        <div className="vps-detail-modal">
          {detailLoading ? <p role="status">正在加载续费决策…</p> : null}
          {detailError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{detailError}</p> : null}
          {detailError ? <Button onClick={retryLoad}>重试加载</Button> : null}
          {detail && decisionDraft ? (
            <VPSRenewalDecisionForm
              detail={detail}
              draft={decisionDraft}
              submitting={submitting}
              error={mutationError}
              notice={null}
              decisionChanged={decisionDraft.renewalDecision !== detail.renewal_decision}
              onCancel={closePanel}
              onDraftChange={setDecisionDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitDecision(event)}
            />
          ) : null}
        </div>
      </Modal>

      <Modal
        open={subscriptionOpen}
        onClose={closePanel}
        title="订阅事实"
        ariaLabel="订阅事实"
        size="xl"
        persistent={submitting}
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
              error={mutationError}
              notice={null}
              onCancel={closePanel}
              onDraftChange={setSubscriptionDraft}
              onFeedbackClear={() => setMutationError(null)}
              onSubmit={(event) => void submitSubscription(event)}
            />
          ) : null}
        </div>
      </Modal>

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
          <p className="asset-lifecycle-confirm__eyebrow">ARCHIVE REVIEW</p>
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
  )
}
