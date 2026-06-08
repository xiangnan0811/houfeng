import type { FormEvent } from 'react'

import {
  MonitoringInstanceWatchtowerHeader,
  MonitoringInstanceWatchtowerMetrics,
  type MonitoringInstanceRuntimeAction,
} from '../../components/monitoring-detail'
import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import type {
  ActiveIncidentRecord,
  HostSample,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeFacts,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'
import { MonitoringInstanceBindingConflictSection } from './MonitoringInstanceBindingConflictSection'
import { MonitoringInstanceCommandDrawer } from './MonitoringInstanceCommandDrawer'
import { MonitoringInstanceDangerCard } from './MonitoringInstanceDangerCard'
import { MonitoringInstanceHistoryDrawer } from './MonitoringInstanceHistoryDrawer'
import { MonitoringInstanceMetadataSection } from './MonitoringInstanceMetadataSection'
import { MonitoringInstanceOnboardingDrawer } from './MonitoringInstanceOnboardingDrawer'
import { MonitoringInstanceRuntimePauseConfirmation } from './MonitoringInstanceRuntimePauseConfirmation'
import { MonitoringInstanceSnapshotMeta } from './MonitoringInstanceSnapshotMeta'
import { MonitoringInstanceTimeWindowTabs } from './MonitoringInstanceTimeWindowTabs'
import {
  COMMAND_LABELS,
  COMMAND_LIST,
  MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
  MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL,
  MONITORING_INSTANCE_BINDING_RESET_LABEL,
} from './monitoringDetailConstants'
import { monitoringInstanceRuntimeActions } from './monitoringDetailHelpers'
import type {
  BindingConflictAction,
  HistoryTab,
  PendingBindingConfirmation,
  PendingRuntimeConfirmation,
  RuntimeStreamStatus,
  TimeWindow,
} from './types'

const bindingConfirmationCopy: Record<
  BindingConflictAction,
  {
    title: string
    current: string
    result: string
    impact: string
    unchanged: string
    confirmLabel: string
    submittingLabel: string
  }
> = {
  confirm: {
    title: MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL,
    current: '当前：监控实例保留原有 agent 指纹，并有一个待确认的新指纹。',
    result: '操作后：新指纹成为当前绑定指纹，待确认状态清空。',
    impact: '会允许这次 agent 指纹变更继续接入，适用于确认重装或合法替换 agent 后。',
    unchanged: '不会删除历史事件、观测记录或监控实例资料。',
    confirmLabel: MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL,
    submittingLabel: '正在确认…',
  },
  reject: {
    title: MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL,
    current: '当前：监控实例存在一个待确认的新指纹。',
    result: '操作后：待确认新指纹会被拒绝，当前绑定保持不变。',
    impact: '会阻止这次未知指纹接管该监控实例，后续合法接入可能需要重新生成一次性命令。',
    unchanged: '不会删除当前已绑定 agent、历史事件或观测记录。',
    confirmLabel: MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL,
    submittingLabel: '正在拒绝…',
  },
  reset: {
    title: MONITORING_INSTANCE_BINDING_RESET_LABEL,
    current: '当前：监控实例已有绑定或待确认绑定状态。',
    result: '操作后：绑定状态重置为未绑定，等待重新接入。',
    impact: '会清空当前绑定和待确认指纹，后续需要重新生成一次性接入命令完成绑定。',
    unchanged: '不会删除监控实例资料、历史事件或观测记录。',
    confirmLabel: MONITORING_INSTANCE_BINDING_RESET_LABEL,
    submittingLabel: '正在重置…',
  },
}

type MonitoringDetailPageBodyProps = {
  monitoringInstance: MonitoringInstanceRecord
  runtimeFacts: MonitoringInstanceRuntimeFacts | null
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  metadataEditing: boolean
  metadataGroupDraft: string
  metadataLabelDraft: string
  metadataNoteDraft: string
  metadataSubmitting: boolean
  metadataError: string | null
  onRuntimeAction: (action: MonitoringInstanceRuntimeAction, confirmed?: boolean) => void
  onCancelRuntimeConfirmation: () => void
  registerActionRef: (action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) => void
  onMetadataGroupDraftChange: (value: string) => void
  onMetadataLabelDraftChange: (value: string) => void
  onMetadataNoteDraftChange: (value: string) => void
  onMetadataStartEdit: () => void
  onMetadataCancelEdit: () => void
  onMetadataSubmit: (event: FormEvent<HTMLFormElement>) => void
  incidents: ActiveIncidentRecord[]
  events: StateChangeEventRecord[]
  eventsError: string | null
  linkedVPS: VPSSummary[]
  linkedVPSLoading: boolean
  linkedVPSLoaded: boolean
  linkedVPSError: string | null
  bindingConflict: MonitoringInstanceOnboardingState | null
  bindingConflictLoading: boolean
  bindingConflictError: string | null
  bindingAction: BindingConflictAction | null
  pendingBindingConfirmation: PendingBindingConfirmation | null
  onBindingConfirm: () => void
  onBindingReject: () => void
  onBindingReset: () => void
  onRequestBindingAction: (action: BindingConflictAction) => void
  onCancelBindingConfirmation: () => void
  timeWindow: TimeWindow
  onTimeWindowChange: (value: TimeWindow) => void
  realtimeSamples: HostSample[]
  runtimeStreamStatus: RuntimeStreamStatus
  runtimeStreamError: string | null
  historyOpen: boolean
  historyTab: HistoryTab
  historyIncidents: ActiveIncidentRecord[] | null
  historyIncidentsLoading: boolean
  historyIncidentsError: string | null
  onOpenHistory: (tab: HistoryTab) => void
  onCloseHistory: () => void
  onHistoryTabChange: (tab: HistoryTab) => void
  onRetryHistoryIncidents: () => void
  commandOpen: boolean
  commandSubmitting: boolean
  commandError: string | null
  onOpenCommands: () => void
  onCloseCommand: () => void
  onExecuteCommand: (commandId: string) => void
  onboardingOpen: boolean
  onboardingReturnVPSId: string | null
  onOpenOnboarding: () => void
  onCloseOnboarding: () => void
}

export function MonitoringDetailPageBody({
  monitoringInstance,
  runtimeFacts,
  runtimeSubmitting,
  runtimeError,
  pendingRuntimeConfirmation,
  metadataEditing,
  metadataGroupDraft,
  metadataLabelDraft,
  metadataNoteDraft,
  metadataSubmitting,
  metadataError,
  onRuntimeAction,
  onCancelRuntimeConfirmation,
  registerActionRef,
  onMetadataGroupDraftChange,
  onMetadataLabelDraftChange,
  onMetadataNoteDraftChange,
  onMetadataStartEdit,
  onMetadataCancelEdit,
  onMetadataSubmit,
  incidents,
  events,
  eventsError,
  linkedVPS,
  linkedVPSLoading,
  linkedVPSLoaded,
  linkedVPSError,
  bindingConflict,
  bindingConflictLoading,
  bindingConflictError,
  bindingAction,
  pendingBindingConfirmation,
  onBindingConfirm,
  onBindingReject,
  onBindingReset,
  onRequestBindingAction,
  onCancelBindingConfirmation,
  timeWindow,
  onTimeWindowChange,
  realtimeSamples,
  runtimeStreamStatus,
  runtimeStreamError,
  historyOpen,
  historyTab,
  historyIncidents,
  historyIncidentsLoading,
  historyIncidentsError,
  onOpenHistory,
  onCloseHistory,
  onHistoryTabChange,
  onRetryHistoryIncidents,
  commandOpen,
  commandSubmitting,
  commandError,
  onOpenCommands,
  onCloseCommand,
  onExecuteCommand,
  onboardingOpen,
  onboardingReturnVPSId,
  onOpenOnboarding,
  onCloseOnboarding,
}: MonitoringDetailPageBodyProps) {
  const realtimeLatestSample = timeWindow === 'realtime'
    ? realtimeSamples[realtimeSamples.length - 1] ?? null
    : null
  const sample = realtimeLatestSample ?? runtimeFacts?.latest_host_sample ?? null
  const metricPoints =
    timeWindow === 'realtime'
      ? realtimeSamples
      : runtimeFacts?.host_metric_points ?? []
  const isMaintenance = monitoringInstance.monitoring_status === '维护中'
  const showBindingConflict = monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
  const bindingActionsDisabled =
    bindingAction !== null || bindingConflictLoading || !bindingConflict
  const showDangerZone = monitoringInstance.current_active_incident_count > 0
  const firstIncident =
    incidents.length > 0
      ? [...incidents].sort(
          (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
        )[0]
      : null

  return (
    <div className="page-stack">
      <MonitoringInstanceWatchtowerHeader
        monitoringInstance={monitoringInstance}
        latestSample={sample}
        runtimeActions={monitoringInstanceRuntimeActions(monitoringInstance)}
        runtimeSubmitting={runtimeSubmitting}
        onRuntimeAction={(action) => onRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => onOpenHistory('events')}
        onOpenCommands={onOpenCommands}
        onOpenOnboarding={onOpenOnboarding}
        linkedVPS={linkedVPS}
        linkedVPSLoading={linkedVPSLoading}
        linkedVPSLoaded={linkedVPSLoaded}
        linkedVPSError={linkedVPSError}
      />

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <MonitoringInstanceRuntimePauseConfirmation
          monitoringInstance={monitoringInstance}
          disabled={runtimeSubmitting}
          onConfirm={() => onRuntimeAction('pause', true)}
          onCancel={onCancelRuntimeConfirmation}
        />
      ) : null}
      {runtimeError ? <p className="watchtower-runtime-error" role="alert">{runtimeError}</p> : null}

      <MonitoringInstanceMetadataSection
        monitoringInstance={monitoringInstance}
        editing={metadataEditing}
        groupDraft={metadataGroupDraft}
        labelDraft={metadataLabelDraft}
        noteDraft={metadataNoteDraft}
        submitting={metadataSubmitting}
        error={metadataError}
        onGroupDraftChange={onMetadataGroupDraftChange}
        onLabelDraftChange={onMetadataLabelDraftChange}
        onNoteDraftChange={onMetadataNoteDraftChange}
        onStartEdit={onMetadataStartEdit}
        onCancelEdit={onMetadataCancelEdit}
        onSubmit={onMetadataSubmit}
      />

      {showDangerZone ? (
        <MonitoringInstanceDangerCard
          monitoringInstance={monitoringInstance}
          firstIncident={firstIncident}
          onOpenEvents={() => onOpenHistory('events')}
        />
      ) : null}

      {showBindingConflict ? (
        <MonitoringInstanceBindingConflictSection
          bindingConflict={bindingConflict}
          loading={bindingConflictLoading}
          error={bindingConflictError}
          bindingAction={bindingAction}
          actionsDisabled={bindingActionsDisabled}
          onConfirm={() => onRequestBindingAction('confirm')}
          onReject={() => onRequestBindingAction('reject')}
          onReset={() => onRequestBindingAction('reset')}
        />
      ) : null}

      {pendingBindingConfirmation && bindingConflict ? (
        <ActionConfirmationModal
          open
          title={bindingConfirmationCopy[pendingBindingConfirmation.action].title}
          current={bindingConfirmationCopy[pendingBindingConfirmation.action].current}
          result={bindingConfirmationCopy[pendingBindingConfirmation.action].result}
          impact={bindingConfirmationCopy[pendingBindingConfirmation.action].impact}
          unchanged={bindingConfirmationCopy[pendingBindingConfirmation.action].unchanged}
          confirmLabel={
            bindingAction === pendingBindingConfirmation.action
              ? bindingConfirmationCopy[pendingBindingConfirmation.action].submittingLabel
              : bindingConfirmationCopy[pendingBindingConfirmation.action].confirmLabel
          }
          disabled={bindingAction !== null}
          error={bindingConflictError}
          onCancel={onCancelBindingConfirmation}
          onConfirm={() => {
            if (pendingBindingConfirmation.action === 'confirm') {
              onBindingConfirm()
            } else if (pendingBindingConfirmation.action === 'reject') {
              onBindingReject()
            } else {
              onBindingReset()
            }
          }}
        />
      ) : null}

      <MonitoringInstanceTimeWindowTabs
        value={timeWindow}
        onChange={onTimeWindowChange}
        streamStatus={runtimeStreamStatus}
        streamError={runtimeStreamError}
      />

      <MonitoringInstanceWatchtowerMetrics
        sample={sample}
        metricPoints={metricPoints}
        timeWindow={timeWindow}
        window={runtimeFacts?.window}
        isMaintenance={isMaintenance}
      />

      <MonitoringInstanceSnapshotMeta sample={sample} />

      <MonitoringInstanceHistoryDrawer
        monitoringInstance={monitoringInstance}
        open={historyOpen}
        tab={historyTab}
        events={events}
        eventsError={eventsError}
        historyIncidents={historyIncidents}
        historyIncidentsLoading={historyIncidentsLoading}
        historyIncidentsError={historyIncidentsError}
        onClose={onCloseHistory}
        onTabChange={onHistoryTabChange}
        onRetryHistoryIncidents={onRetryHistoryIncidents}
      />

      <MonitoringInstanceCommandDrawer
        monitoringInstance={monitoringInstance}
        open={commandOpen}
        commands={COMMAND_LIST}
        commandLabels={COMMAND_LABELS}
        submitting={commandSubmitting}
        error={commandError}
        onClose={onCloseCommand}
        onExecute={onExecuteCommand}
      />

      <MonitoringInstanceOnboardingDrawer
        monitoringInstance={monitoringInstance}
        open={onboardingOpen}
        returnVPSId={onboardingReturnVPSId}
        onClose={onCloseOnboarding}
      />
    </div>
  )
}
