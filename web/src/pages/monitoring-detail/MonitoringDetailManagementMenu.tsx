import { useEffect, useId, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Input, MonoDigits, Timestamp, type ButtonSize, type ButtonVariant } from '../../components/atoms'
import type { MonitoringInstanceManagementReview, MonitoringInstanceRecord } from '../../lib/types'
import type { MonitoringInstanceRuntimeAction } from '../../components/monitoring-detail'
import type { FrozenDestructiveSubject } from './types'

type ManagementDialogAction =
  | 'retire'
  | 'restore-lifecycle'
  | 'archive'
  | 'restore-archive'
  | 'permanent-cleanup'

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
  onLoadReview: (force?: boolean) => void
  onRetire: (reason: string) => void
  onRestoreLifecycle: (reason: string) => void
  onArchive: (reason: string, confirmationName: string) => void
  onRestoreArchive: () => void
  onPermanentCleanup: (reason: string, confirmationName: string) => void
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

function dialogCopy(action: ManagementDialogAction, displayName: string) {
  switch (action) {
    case 'retire':
      return {
        title: '退役监控实例',
        current: '当前：实例仍在工作集内，可继续接入或采集。',
        result: '之后：生命周期变为已退役，运行状态变为暂停。',
        impact: '会撤销继续控制和接入所需的 token，并让 agent 后续只拿到空计划。',
        unchanged: '不会删除历史心跳、样本、事件或关联审查信息。',
        confirmLabel: '确认退役',
      }
    case 'restore-lifecycle':
      return {
        title: '恢复监控实例生命周期',
        current: '当前：实例处于已退役状态。',
        result: '之后：生命周期回到观察中，运行状态保持暂停。',
        impact: '后续需要用户显式恢复监控或重新接入，不会自动开始采集。',
        unchanged: '不会恢复旧 token 或待执行命令。',
        confirmLabel: '确认恢复生命周期',
      }
    case 'archive':
      return {
        title: '归档监控实例',
        current: `当前：${displayName} 仍在可操作工作集内。`,
        result: '之后：实例退出默认列表，变为只读归档对象。',
        impact: '会撤销 token、待绑定指纹和待执行动作，阻止继续接入、控制或写入观测。',
        unchanged: '不会删除历史观测、事件、通知或审查计数。',
        confirmLabel: '确认归档',
      }
    case 'restore-archive':
      return {
        title: '恢复归档监控实例',
        current: '当前：实例处于归档只读状态。',
        result: '之后：实例回到观察中 + 暂停。',
        impact: '恢复后仍需显式恢复监控或重新接入，不会自动采集。',
        unchanged: '不会恢复旧 token、待绑定指纹或待执行命令。',
        confirmLabel: '确认恢复归档',
      }
    case 'permanent-cleanup':
      return {
        title: '永久清理监控实例',
        current: `当前：${displayName} 将进入不可恢复清理流程。`,
        result: '之后：监控实例和可删除关联记录会被删除。',
        impact: '此操作不可撤销，只适合清理误创建的空实例或已归档且审查允许的实例。',
        unchanged: '命令审计元数据将永久保留，可继续在全局审计页查询，且不会计入已删除关联数量；有阻塞项时后端会拒绝清理。',
        confirmLabel: '确认永久清理',
      }
  }
}

function needsReason(action: ManagementDialogAction | null) {
  return action === 'retire' || action === 'restore-lifecycle' || action === 'archive' || action === 'permanent-cleanup'
}

function needsConfirmation(action: ManagementDialogAction | null) {
  return action === 'archive' || action === 'permanent-cleanup'
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
  onLoadReview,
  onRetire,
  onRestoreLifecycle,
  onArchive,
  onRestoreArchive,
  onPermanentCleanup,
}: Props) {
  const location = useLocation()
  const generatedId = useId()
  const menuId = `monitoring-detail-management-${generatedId}`
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [dialogAction, setDialogAction] = useState<ManagementDialogAction | null>(null)
  const [frozenSubject, setFrozenSubject] = useState<FrozenDestructiveSubject | null>(null)
  const [reason, setReason] = useState('')
  const [confirmationName, setConfirmationName] = useState('')
  const [versionError, setVersionError] = useState<string | null>(null)

  const archived = Boolean(monitoringInstance.archived_at)
  const retired = archived || monitoringInstance.lifecycle_status === '已退役'
  const displayName = frozenSubject?.displayName ?? monitoringInstance.display_name
  const copy = dialogAction ? dialogCopy(dialogAction, displayName) : null
  const reasonRequired = needsReason(dialogAction)
  const confirmationRequired = needsConfirmation(dialogAction)
  const confirmDisabled =
    submittingAction !== null ||
    (reasonRequired && !reason.trim()) ||
    (confirmationRequired && confirmationName.trim() !== displayName)

  const closeMenu = (restoreFocus = false) => {
    setOpen(false)
    if (restoreFocus) {
      queueMicrotask(() => triggerRef.current?.focus())
    }
  }

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

  function openDialog(action: ManagementDialogAction) {
    setFrozenSubject(freezeSubject(monitoringInstance))
    setDialogAction(action)
    setReason('')
    setConfirmationName('')
    setVersionError(null)
  }

  function closeDialog() {
    if (submittingAction !== null) return
    setDialogAction(null)
    setFrozenSubject(null)
    setReason('')
    setConfirmationName('')
    setVersionError(null)
  }

  function confirmDialog() {
    if (!dialogAction || !frozenSubject || confirmDisabled) return
    if (
      monitoringInstance.monitoring_instance_id !== frozenSubject.monitoringInstanceId ||
      monitoringInstance.updated_at !== frozenSubject.updatedAt
    ) {
      setVersionError('实例已更新，请关闭后重新确认。')
      onLoadReview(true)
      return
    }
    const trimmedReason = reason.trim()
    const trimmedConfirmationName = confirmationName.trim()
    if (dialogAction === 'retire') onRetire(trimmedReason)
    if (dialogAction === 'restore-lifecycle') onRestoreLifecycle(trimmedReason)
    if (dialogAction === 'archive') onArchive(trimmedReason, trimmedConfirmationName)
    if (dialogAction === 'restore-archive') onRestoreArchive()
    if (dialogAction === 'permanent-cleanup') onPermanentCleanup(trimmedReason, trimmedConfirmationName)
    setDialogAction(null)
    setFrozenSubject(null)
    setReason('')
    setConfirmationName('')
    setVersionError(null)
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

          <li role="none" className="monitoring-detail-management__group">
            <p className="monitoring-detail-management__group-label">资料</p>
            <button
              type="button"
              role="menuitem"
              className="btn lg ghost monitoring-detail-management__item"
              disabled={archived}
              onClick={() => {
                closeMenu()
                onOpenMetadata()
              }}
            >
              编辑分组、标签与备注
            </button>
            {archived ? (
              <p className="monitoring-detail-management__note">已归档实例资料只读</p>
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
                {review.actions.can_retire ? (
                  <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('retire') }}>
                    退役
                  </button>
                ) : null}
                {review.actions.can_restore_lifecycle ? (
                  <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('restore-lifecycle') }}>
                    恢复生命周期
                  </button>
                ) : null}
                {review.actions.can_archive ? (
                  <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('archive') }}>
                    归档
                  </button>
                ) : null}
                {review.actions.can_restore_archive ? (
                  <button type="button" role="menuitem" className="btn lg ghost monitoring-detail-management__item"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('restore-archive') }}>
                    恢复归档
                  </button>
                ) : null}
                {review.actions.can_permanent_cleanup ? (
                  <button type="button" role="menuitem"
                    className="btn lg ghost monitoring-detail-management__item monitoring-detail-management__item--danger"
                    disabled={submittingAction !== null}
                    onClick={() => { closeMenu(); openDialog('permanent-cleanup') }}>
                    永久清理
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
          error={versionError}
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
              {review.blockers.length > 0 || review.warnings.length > 0 ? (
                <ul>
                  {review.blockers.map((blocker) => <li key={`blocker-${blocker}`}>{blocker}</li>)}
                  {review.warnings.map((warning) => <li key={`warning-${warning}`}>{warning}</li>)}
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
          {confirmationRequired ? (
            <Input
              label="输入实例名称确认"
              value={confirmationName}
              onChange={(event) => setConfirmationName(event.target.value)}
              placeholder={displayName}
              hint={`请输入 ${displayName}`}
            />
          ) : null}
        </ActionConfirmationModal>
      ) : null}
    </div>
  )
}