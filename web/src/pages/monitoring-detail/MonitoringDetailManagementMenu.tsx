import { useEffect, useId, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Input, MonoDigits, Timestamp, type ButtonSize, type ButtonVariant } from '../../components/atoms'
import type { MonitoringInstanceManagementReview, MonitoringInstanceRecord } from '../../lib/types'
import { requiresSharedImpactConfirmation } from '../../lib/assetLifecycle'
import type { MonitoringInstanceRuntimeAction } from '../../components/monitoring-detail'
import { MONITORING_MANAGEMENT_REVIEW_STALE_MESSAGE } from './monitoringDetailConstants'
import type { FrozenDestructiveSubject, ManagementActionOutcome } from './types'


type ManagementConfirmation = { preview_digest: string; confirm_shared_impact: boolean; idempotency_key?: string }
type ManagementActionResult = Promise<ManagementActionOutcome | void> | ManagementActionOutcome | void

type ManagementDialogAction = 'retire'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  runtimeActions: Array<{ action: MonitoringInstanceRuntimeAction; label: string }>
  runtimeSubmitting: boolean
  onRuntimeAction: (action: MonitoringInstanceRuntimeAction) => void
  registerActionRef: (action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) => void
  onOpenOnboarding: () => void
  onboardingActionLabel: string
  onOpenCommands: () => void
  onOpenMetadata: () => void
  triggerVariant?: ButtonVariant
  triggerSize?: ButtonSize
  review: MonitoringInstanceManagementReview | null
  loading: boolean
  error: string | null
  submittingAction: ManagementDialogAction | null
  actionError: string | null
  confirmationResetKey?: number
  onLoadReview: (force?: boolean) => void
  onRetire: (reason: string, confirmation: ManagementConfirmation) => ManagementActionResult

}

const COUNT_ITEMS: Array<{ key: keyof MonitoringInstanceManagementReview['counts']; label: string }> = [
  { key: 'heartbeat_count', label: '心跳' },
  { key: 'host_sample_count', label: '主机样本' },
  { key: 'probe_observation_count', label: '探测观测' },
  { key: 'host_sample_daily_aggregate_count', label: '日聚合' },
  { key: 'ip_quality_report_count', label: 'IP 质量' },
  { key: 'active_incident_count', label: '活跃异常' },
  { key: 'state_change_event_count', label: '事件' },
  { key: 'notification_record_count', label: '通知' },
  { key: 'asset_lifecycle_action_step_count', label: '生命周期动作' },
  { key: 'command_action_audit_count', label: '命令审计' },
  { key: 'active_vps_link_count', label: 'VPS 关联' },
]

function dialogCopy() {
  return {
    title: '退役监控实例',
    current: '当前：实例仍可接入或采集。',
    result: '之后：生命周期变为已退役，停止采集、告警和命令。',
    impact: '已有会话仅保留最小在线证据权限；待执行命令会被清理并保留审计。',
    unchanged: '历史观测与接入阶段永久归属于当前 VPS。重新接入需要签发新会话。',
    confirmLabel: '确认退役',
  }
}

function freezeSubject(monitoringInstance: MonitoringInstanceRecord): FrozenDestructiveSubject {
  return {
    monitoringInstanceId: monitoringInstance.monitoring_instance_id,
    displayName: monitoringInstance.display_name,
    updatedAt: monitoringInstance.updated_at,
  }
}

function menuItems(root: HTMLElement | null): HTMLButtonElement[] {
  return Array.from(root?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
    .filter((element) => !element.hasAttribute('disabled'))
}

export function MonitoringDetailManagementMenu({
  monitoringInstance,
  runtimeActions,
  runtimeSubmitting,
  onRuntimeAction,
  registerActionRef,
  onOpenOnboarding,
  onboardingActionLabel,
  onOpenCommands,
  onOpenMetadata,
  triggerVariant = 'primary',
  triggerSize = 'md',
  review,
  loading,
  error,
  submittingAction,
  actionError,
  confirmationResetKey = 0,
  onLoadReview,
  onRetire,
}: Props) {
  const location = useLocation()
  const generatedId = useId()
  const menuId = `monitoring-detail-management-${generatedId}`
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const mountedRef = useRef(true)
  const retireKeyRef = useRef('')
  const submissionRef = useRef(false)
  const [open, setOpen] = useState(false)
  const [dialogAction, setDialogAction] = useState<ManagementDialogAction | null>(null)
  const [frozenSubject, setFrozenSubject] = useState<FrozenDestructiveSubject | null>(null)
  const [reason, setReason] = useState('')
  const [confirmedKey, setConfirmedKey] = useState<string | null>(null)
  const [versionError, setVersionError] = useState<string | null>(null)
  const [staleNotice, setStaleNotice] = useState<string | null>(null)


  const archived = Boolean(monitoringInstance.archived_at) || monitoringInstance.vps_lifecycle_status === 'archived'
  const retired = archived || monitoringInstance.lifecycle_status === '已退役'
  const copy = dialogAction ? dialogCopy() : null
  const reasonRequired = Boolean(dialogAction)
  const currentDigest = review?.preview_digest ?? ''
  const activeKey = dialogAction && currentDigest
    ? `${monitoringInstance.monitoring_instance_id}:${dialogAction}:${currentDigest}:${confirmationResetKey}`
    : null
  const sharedConfirmed = Boolean(activeKey && confirmedKey === activeKey)
  const sharedRequired = Boolean(review && requiresSharedImpactConfirmation(review.dependency_impacts ?? [], 'monitoring_instance', monitoringInstance.monitoring_instance_id))
  const reviewReady = Boolean(review?.preview_digest) && !error && !loading
  const confirmDisabled =
    !reviewReady ||
    retired ||
    review?.action_reviews.retire.allowed !== true ||
    submittingAction !== null ||
    (reasonRequired && !reason.trim()) ||
    (sharedRequired && !sharedConfirmed)


  const closeMenu = (restoreFocus = false) => {
    setOpen(false)
    if (restoreFocus) {
      queueMicrotask(() => triggerRef.current?.focus())
    }
  }

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    if (!open) return
    menuItems(rootRef.current)[0]?.focus()

    const onPointer = (event: MouseEvent) => {
      if (
        !rootRef.current?.contains(event.target as Node) &&
        !triggerRef.current?.contains(event.target as Node)
      ) {
        closeMenu()
      }
    }
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const buttons = menuItems(rootRef.current)
      if (event.key === 'Tab') {
        closeMenu()
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        closeMenu(true)
        return
      }
      if (buttons.length === 0) return
      const currentIndex = Math.max(0, buttons.findIndex((button) => button === document.activeElement))
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        buttons[(currentIndex + 1) % buttons.length]?.focus()
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        buttons[(currentIndex - 1 + buttons.length) % buttons.length]?.focus()
        return
      }
      if (event.key === 'Home') {
        event.preventDefault()
        buttons[0]?.focus()
        return
      }
      if (event.key === 'End') {
        event.preventDefault()
        buttons[buttons.length - 1]?.focus()
      }
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  function clearDialog() {
    setDialogAction(null)
    setFrozenSubject(null)
    setReason('')
    setConfirmedKey(null)
    setVersionError(null)
    setStaleNotice(null)
  }

  function openDialog(action: ManagementDialogAction) {
    retireKeyRef.current = crypto.randomUUID()
    setFrozenSubject(freezeSubject(monitoringInstance))
    setDialogAction(action)
    setReason('')
    setConfirmedKey(null)
    setVersionError(null)
    setStaleNotice(null)
    onLoadReview(true)
  }

  function closeDialog() {
    if (submittingAction !== null) return
    clearDialog()
  }

  async function confirmDialog() {
    if (submissionRef.current || !dialogAction || !frozenSubject || confirmDisabled || !review?.preview_digest) return
    if (
      monitoringInstance.monitoring_instance_id !== frozenSubject.monitoringInstanceId ||
      monitoringInstance.updated_at !== frozenSubject.updatedAt
    ) {
      setVersionError('实例已更新，请关闭后重新确认。')
      onLoadReview(true)
      return
    }
    const trimmedReason = reason.trim()
    const confirmation = {
      preview_digest: review.preview_digest,
      confirm_shared_impact: sharedRequired ? sharedConfirmed : false,
      idempotency_key: retireKeyRef.current,
    }
    const actionMonitoringInstanceId = frozenSubject.monitoringInstanceId
    setStaleNotice(null)
    submissionRef.current = true
    let outcome: ManagementActionOutcome | void
    try {
      outcome = await onRetire(trimmedReason, confirmation)
    } finally {
      submissionRef.current = false
    }
    if (
      !mountedRef.current ||
      monitoringInstance.monitoring_instance_id !== actionMonitoringInstanceId
    ) return
    if (outcome === 'stale') {
      setConfirmedKey(null)
      setStaleNotice(MONITORING_MANAGEMENT_REVIEW_STALE_MESSAGE)
      return
    }
    if (outcome === 'failed') return
    clearDialog()
  }


  return (
    <div className="monitoring-detail-management" ref={rootRef}>
      <Button
        ref={triggerRef}
        variant={triggerVariant}
        size={triggerSize}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => {
          const next = !open
          setOpen(next)
          if (next) onLoadReview()
        }}
      >
        管理
      </Button>
      {open ? (
        <ul id={menuId} className="monitoring-detail-management__menu" role="menu" aria-label="管理" aria-orientation="vertical">
          {retired ? null : (
            <li role="none" className="monitoring-detail-management__group">
              <p className="monitoring-detail-management__group-label">运行控制</p>
              {runtimeActions.map(({ action, label }) => (
                <button
                  key={action}
                  ref={(element) => registerActionRef(action, element)}
                  type="button"
                  role="menuitem"
                  className="btn lg ghost monitoring-detail-management__item"
                  disabled={runtimeSubmitting}
                  onClick={() => onRuntimeAction(action)}
                >
                  {label}
                </button>
              ))}
              <button
                type="button"
                role="menuitem"
                className="btn lg ghost monitoring-detail-management__item"
                onClick={() => {
                  closeMenu()
                  onOpenOnboarding()
                }}
              >
                {onboardingActionLabel}
              </button>
              <button
                type="button"
                role="menuitem"
                className="btn lg ghost monitoring-detail-management__item"
                onClick={() => {
                  closeMenu()
                  onOpenCommands()
                }}
              >
                执行诊断命令…
              </button>
              <Link
                className="btn lg ghost monitoring-detail-management__item"
                role="menuitem"
                to={`/command-audit?monitoring_instance=${encodeURIComponent(monitoringInstance.monitoring_instance_id)}`}
                state={location.state}
                onClick={() => closeMenu()}
              >
                查看命令审计
              </Link>
            </li>
          )}
          {retired && !archived && monitoringInstance.vps_lifecycle_status === 'active' ? (
            <li role="none" className="monitoring-detail-management__group">
              <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                onClick={() => { closeMenu(); onOpenOnboarding() }}>
                重新接入 agent…
              </button>
            </li>
          ) : null}

          <li role="none" className="monitoring-detail-management__group">
            <p className="monitoring-detail-management__group-label">资料</p>
            <button
              type="button"
              role="menuitem"
              className="btn lg ghost monitoring-detail-management__item"
              disabled={retired}
              onClick={() => {
                closeMenu()
                onOpenMetadata()
              }}
            >
              编辑分组、标签与备注
            </button>
            {archived ? (
              <p className="monitoring-detail-management__note">所属 VPS 已归档，监控历史只读</p>
            ) : null}
          </li>

          <li role="none" className="monitoring-detail-management__group">
            <p className="monitoring-detail-management__group-label">生命周期</p>
            <p className="monitoring-detail-management__current">
              当前：{monitoringInstance.lifecycle_status}
            </p>
            {actionError ? <p className="monitoring-detail-management__note" role="alert">{actionError}</p> : null}
            {error ? (
              <p className="monitoring-detail-management__note" role="alert">
                {error}
                <Button variant="ghost" size="sm" onClick={() => onLoadReview(true)}>重试</Button>
              </p>
            ) : loading && !review ? (
              <p className="monitoring-detail-management__note" role="status">正在加载…</p>
            ) : review ? (
              <>
                {!retired && review.action_reviews.retire.allowed ? (
                  <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('retire') }}>
                    退役
                  </button>
                ) : null}
              </>
            ) : null}
          </li>
        </ul>
      ) : null}

      {dialogAction && copy ? (
        <ActionConfirmationModal
          open
          title={copy.title}
          current={copy.current}
          result={copy.result}
          impact={copy.impact}
          unchanged={copy.unchanged}
          confirmLabel={copy.confirmLabel}
          disabled={confirmDisabled}
          cancelDisabled={submittingAction !== null}
          error={versionError || actionError || staleNotice}
          onCancel={closeDialog}
          onConfirm={confirmDialog}
        >
          {review ? (
            <div className="monitoring-detail-management__review">
              <div className="monitoring-detail-management__counts" aria-label="管理审查计数">
                {COUNT_ITEMS.map((item) => (
                  <div key={item.key}>
                    <span>{item.label}</span>
                    <MonoDigits>{review.counts[item.key]}</MonoDigits>
                  </div>
                ))}
              </div>
              {review.active_vps_links.length > 0 ? (
                <p>
                  关联 VPS：
                  {review.active_vps_links.map((link) => link.display_name).join('、')}
                </p>
              ) : null}
              {sharedRequired ? (
                <label className="asset-cancel-workbench__inline-check">
                  <input type="checkbox" checked={sharedConfirmed} onChange={(event) => setConfirmedKey(event.target.checked && activeKey ? activeKey : null)} />
                  <span>确认此监控实例对多台 VPS 的当前或残留影响</span>
                </label>
              ) : null}
              {dialogAction ? (
                <ul>
                  {(review.action_reviews[dialogAction].blockers).map((blocker) => <li key={`blocker-${blocker}`}>{blocker}</li>)}
                  {(review.action_reviews[dialogAction].warnings).map((warning) => <li key={`warning-${warning}`}>{warning}</li>)}
                </ul>
              ) : null}
              {monitoringInstance.archived_at ? (
                <p>
                  归档时间 <Timestamp value={monitoringInstance.archived_at} />
                  {'；原因：'}
                  {monitoringInstance.archived_reason || '未记录'}
                </p>
              ) : null}
            </div>
          ) : null}
          {reasonRequired ? (
            <Input
              label="原因"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="记录这次管理操作的原因"
            />
          ) : null}
        </ActionConfirmationModal>
      ) : null}
    </div>
  )
}
