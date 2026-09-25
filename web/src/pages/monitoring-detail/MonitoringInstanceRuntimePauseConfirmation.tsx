import { useState } from 'react'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { SharedImpactPanel } from '../../components/SharedImpactPanel'
import { requiresSharedImpactConfirmation } from '../../lib/assetLifecycle'
import type { DependencyImpact, GlobalActionConfirmation, MonitoringInstanceRecord } from '../../lib/types'
import {
  MONITORING_PAUSE_REVIEW_FAILED_MESSAGE,
  MONITORING_PAUSE_REVIEW_NOT_READY_MESSAGE,
} from './monitoringDetailConstants'
import { pauseConfirmationCurrent } from './monitoringDetailHelpers'

type MonitoringInstanceRuntimePauseConfirmationProps = {
  monitoringInstanceId: string
  monitoringStatus: MonitoringInstanceRecord['monitoring_status']
  impacts: DependencyImpact[]
  previewDigest: string
  reviewLoaded: boolean
  reviewError: string | null
  resetKey: number
  disabled: boolean
  error?: string | null
  onConfirm: (confirmation: GlobalActionConfirmation) => void
  onCancel: () => void
}

export function MonitoringInstanceRuntimePauseConfirmation({
  monitoringInstanceId,
  monitoringStatus,
  impacts,
  previewDigest,
  reviewLoaded,
  reviewError,
  resetKey,
  disabled,
  error = null,
  onConfirm,
  onCancel,
}: MonitoringInstanceRuntimePauseConfirmationProps) {
  // Consent is scoped to one review digest and reset generation, so a new review clears it.
  const confirmationScope = `${previewDigest}:${resetKey}`
  const [confirmedScope, setConfirmedScope] = useState<string | null>(null)
  const confirmed = confirmedScope === confirmationScope

  const reviewReady = reviewLoaded && previewDigest.trim() !== '' && !reviewError
  const sharedRequired = reviewReady && requiresSharedImpactConfirmation(impacts, 'monitoring_instance', monitoringInstanceId)
  const related = impacts.filter((impact) => impact.object_type === 'monitoring_instance' && impact.object_id === monitoringInstanceId)

  return (
    <ActionConfirmationModal
      open
      title="确认暂停监控实例监控"
      current={pauseConfirmationCurrent({ monitoring_status: monitoringStatus } as MonitoringInstanceRecord)}
      result="操作后：监控运行状态变为暂停。"
      impact="会停止主机指标采集，并停止该监控实例承担的探针执行。趋势图会从此开始出现数据空档。"
      unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
      confirmLabel="确认暂停监控"
      disabled={disabled || !reviewReady || (sharedRequired && !confirmed)}
      cancelDisabled={disabled}

      error={error}
      onConfirm={() => {
        if (!reviewReady) return
        onConfirm({
          preview_digest: previewDigest,
          confirm_shared_impact: sharedRequired ? confirmed : false,
        })
      }}
      onCancel={onCancel}
    >
      {reviewError ? <p role="alert">{MONITORING_PAUSE_REVIEW_FAILED_MESSAGE}</p> : null}
      {!reviewReady && !reviewError ? <p role="status">{MONITORING_PAUSE_REVIEW_NOT_READY_MESSAGE}</p> : null}
      {sharedRequired ? (
        <>
          <p>共享影响摘要 {previewDigest}</p>
          <SharedImpactPanel
            impacts={related}
            confirmations={[{
              key: monitoringInstanceId,
              label: '确认此监控实例对多台 VPS 的当前或残留影响',
              checked: confirmed,
            }]}
            onToggle={(_key, checked) => setConfirmedScope(checked ? confirmationScope : null)}
          />
        </>
      ) : null}
    </ActionConfirmationModal>
  )
}
