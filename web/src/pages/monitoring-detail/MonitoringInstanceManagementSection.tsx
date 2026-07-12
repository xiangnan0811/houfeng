import { useState } from 'react'
import { Link } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Input, MonoDigits, Timestamp } from '../../components/atoms'
import type { MonitoringInstanceManagementReview, MonitoringInstanceRecord } from '../../lib/types'

type ManagementDialogAction =
  | 'retire'
  | 'restore-lifecycle'
  | 'archive'
  | 'restore-archive'
  | 'permanent-cleanup'

type MonitoringInstanceManagementSectionProps = {
  monitoringInstance: MonitoringInstanceRecord
  review: MonitoringInstanceManagementReview | null
  loading: boolean
  error: string | null
  submittingAction: ManagementDialogAction | null
  actionError: string | null
  onLoadReview: () => void
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

export function MonitoringInstanceManagementSection({
  monitoringInstance,
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
}: MonitoringInstanceManagementSectionProps) {
  const [dialogAction, setDialogAction] = useState<ManagementDialogAction | null>(null)
  const [reason, setReason] = useState('')
  const [confirmationName, setConfirmationName] = useState('')
  const displayName = monitoringInstance.display_name
  const archived = Boolean(monitoringInstance.archived_at)
  const copy = dialogAction ? dialogCopy(dialogAction, displayName) : null
  const reasonRequired = needsReason(dialogAction)
  const confirmationRequired = needsConfirmation(dialogAction)
  const confirmDisabled =
    submittingAction !== null ||
    (reasonRequired && !reason.trim()) ||
    (confirmationRequired && confirmationName.trim() !== displayName)

  function openDialog(action: ManagementDialogAction) {
    setDialogAction(action)
    setReason('')
    setConfirmationName('')
  }

  function closeDialog() {
    if (submittingAction !== null) return
    setDialogAction(null)
    setReason('')
    setConfirmationName('')
  }

  function confirmDialog() {
    if (!dialogAction || confirmDisabled) return
    const trimmedReason = reason.trim()
    const trimmedConfirmationName = confirmationName.trim()
    if (dialogAction === 'retire') onRetire(trimmedReason)
    if (dialogAction === 'restore-lifecycle') onRestoreLifecycle(trimmedReason)
    if (dialogAction === 'archive') onArchive(trimmedReason, trimmedConfirmationName)
    if (dialogAction === 'restore-archive') onRestoreArchive()
    if (dialogAction === 'permanent-cleanup') onPermanentCleanup(trimmedReason, trimmedConfirmationName)
    setDialogAction(null)
    setReason('')
    setConfirmationName('')
  }

  return (
    <>
      <details
        className="watchtower-secondary monitoring-management"
        onToggle={(event) => {
          if (event.currentTarget.open) onLoadReview()
        }}
      >
        <summary>管理实例</summary>
        <div className="watchtower-secondary__body monitoring-management__body">
          <div className="monitoring-management__status">
            <div>
              <span>生命周期</span>
              <strong>{monitoringInstance.lifecycle_status}</strong>
            </div>
            <div>
              <span>运行状态</span>
              <strong>{monitoringInstance.monitoring_status}</strong>
            </div>
            <div>
              <span>绑定</span>
              <strong>{monitoringInstance.binding_status}</strong>
            </div>
            <div>
              <span>归档</span>
              <strong>{archived ? '已归档' : '未归档'}</strong>
            </div>
          </div>
          {archived && monitoringInstance.archived_at ? (
            <p className="asset-operation-feedback asset-operation-feedback--notice">
              归档时间 <Timestamp value={monitoringInstance.archived_at} />；原因：{monitoringInstance.archived_reason || '未记录'}
            </p>
          ) : null}

          {loading && !review ? <p className="watchtower-property-item__desc">正在加载管理审查…</p> : null}
          {error ? <p className="watchtower-property-item__error" role="alert">{error}</p> : null}
          {actionError ? <p className="watchtower-property-item__error" role="alert">{actionError}</p> : null}

          {review ? (
            <>
              <div className="monitoring-management__counts" aria-label="管理审查计数">
                {COUNT_ITEMS.map((item) => (
                  <div key={item.key}>
                    <span>{item.label}</span>
                    <MonoDigits>{review.counts[item.key]}</MonoDigits>
                  </div>
                ))}
              </div>

              <div className="monitoring-management__grid">
                <section>
                  <h3>活跃 VPS 关联</h3>
                  {review.active_vps_links.length === 0 ? (
                    <p>没有活跃 VPS 关联。</p>
                  ) : (
                    <ul>
                      {review.active_vps_links.map((link) => (
                        <li key={link.link_id}>
                          <Link className="text-link" to={`/vps/${link.vps_id}`}>{link.display_name}</Link>
                          <span>{link.lifecycle_status} · {link.usage_status}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </section>

                <section>
                  <h3>阻塞与警告</h3>
                  {review.blockers.length === 0 && review.warnings.length === 0 ? (
                    <p>当前没有阻塞项或警告。</p>
                  ) : (
                    <ul>
                      {review.blockers.map((blocker) => <li key={`blocker-${blocker}`}>{blocker}</li>)}
                      {review.warnings.map((warning) => <li key={`warning-${warning}`}>{warning}</li>)}
                    </ul>
                  )}
                </section>
              </div>

              <div className="monitoring-management__actions">
                <Button
                  variant="secondary"
                  disabled={!review.actions.can_retire || submittingAction !== null}
                  onClick={() => openDialog('retire')}
                >
                  退役实例
                </Button>
                <Button
                  variant="secondary"
                  disabled={!review.actions.can_restore_lifecycle || submittingAction !== null}
                  onClick={() => openDialog('restore-lifecycle')}
                >
                  恢复生命周期
                </Button>
                <Button
                  variant="secondary"
                  disabled={!review.actions.can_archive || submittingAction !== null}
                  onClick={() => openDialog('archive')}
                >
                  归档实例
                </Button>
                <Button
                  variant="secondary"
                  disabled={!review.actions.can_restore_archive || submittingAction !== null}
                  onClick={() => openDialog('restore-archive')}
                >
                  恢复归档
                </Button>
                <Button
                  variant="danger"
                  disabled={!review.actions.can_permanent_cleanup || submittingAction !== null}
                  onClick={() => openDialog('permanent-cleanup')}
                >
                  永久清理
                </Button>
              </div>
            </>
          ) : null}
        </div>
      </details>

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
          onCancel={closeDialog}
          onConfirm={confirmDialog}
        >
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
    </>
  )
}
