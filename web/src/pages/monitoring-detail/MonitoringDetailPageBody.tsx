import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'

import {
  MonitoringInstanceWatchtowerHeader,
  type MonitoringInstanceRuntimeAction,
} from '../../components/monitoring-detail'
import { Button } from '../../components/atoms/Button'
import { COMMAND_LABELS, COMMAND_LIST } from '../../config/commands'
import type { MetricThresholds } from '../../config/thresholds'
import type { HeartbeatFreshness } from '../monitoring/types'
import { hostSampleToMetricPoint } from './runtimeObservation'
import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import type {
  ActiveIncidentRecord,
  HostSample,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceManagementReview,
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeFacts,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { MonitoringDetailManagementMenu } from './MonitoringDetailManagementMenu'
import { MonitoringDetailMetadataDialog } from './MonitoringDetailMetadataDialog'
import { MonitoringDetailNotices } from './MonitoringDetailNotices'
import { MonitoringDetailObservations } from './MonitoringDetailObservations'
import { MonitoringDetailRecentEvents } from './MonitoringDetailRecentEvents'
import { MonitoringDetailStatusBand } from './MonitoringDetailStatusBand'
import { MonitoringInstanceBindingConflictDialog } from './MonitoringInstanceBindingConflictDialog'
import { MonitoringInstanceCommandDrawer } from './MonitoringInstanceCommandDrawer'
import { MonitoringInstanceHistoryDrawer } from './MonitoringInstanceHistoryDrawer'
import { MonitoringInstanceOnboardingDrawer } from './MonitoringInstanceOnboardingDrawer'
import { MonitoringInstanceRuntimePauseConfirmation } from './MonitoringInstanceRuntimePauseConfirmation'
import { MonitoringInstanceTimeWindowTabs } from './MonitoringInstanceTimeWindowTabs'
import {
  MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
  MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL,
  MONITORING_INSTANCE_BINDING_RESET_LABEL,
} from './monitoringDetailConstants'
import { monitoringInstanceRuntimeActions, validateReturnVPSId } from './monitoringDetailHelpers'
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
  latestSample: HostSample | null
  snapshotReadAt: Date | null
  heartbeatFreshness: HeartbeatFreshness
  runtimeFactsError: string | null
  runtimeFactsLoading: boolean
  onRetryRuntimeFacts: () => void
  onRetrySettings: () => void
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  metadataEditing: boolean
  metadataGroupDraft: string
  metadataLabelDraft: string
  metadataNoteDraft: string
  metadataSubmitting: boolean
  metadataError: string | null
  managementReview: MonitoringInstanceManagementReview | null
  managementLoading: boolean
  managementError: string | null
  managementSubmittingAction: 'retire' | 'restore-lifecycle' | 'archive' | 'restore-archive' | 'permanent-cleanup' | null
  managementActionError: string | null
  onRuntimeAction: (action: MonitoringInstanceRuntimeAction, confirmed?: boolean) => void
  onCancelRuntimeConfirmation: () => void
  registerActionRef: (action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) => void
  onMetadataGroupDraftChange: (value: string) => void
  onMetadataLabelDraftChange: (value: string) => void
  onMetadataNoteDraftChange: (value: string) => void
  onMetadataStartEdit: () => void
  onMetadataCancelEdit: () => void
  onMetadataSubmit: (event: FormEvent<HTMLFormElement>) => void
  onManagementLoadReview: (force?: boolean) => void
  onManagementRetire: (reason: string) => void
  onManagementRestoreLifecycle: (reason: string) => void
  onManagementArchive: (reason: string, confirmationName: string) => void
  onManagementRestoreArchive: () => void
  onManagementPermanentCleanup: (reason: string, confirmationName: string) => void
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
  incidentsLoaded: boolean
  eventsLoaded: boolean
  incidentsRetrying: boolean
  eventsRetrying: boolean
  onRetryIncidents: () => void
  onRetryEvents: () => void
  onRetryBindingConflict: () => void
  onRetryLinkedVPS: () => void
  commandPollError: string | null
  onRetryCommandPoll: () => void
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
  thresholds: MetricThresholds | null
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
  onExecuteCommand: (commandId: string, options?: { confirmedSensitive?: boolean }) => void
  onboardingOpen: boolean
  onboardingReturnVPSId: string | null
  onOpenOnboarding: () => void
  onCloseOnboarding: () => void
  onRefresh: () => void
}

export function MonitoringDetailPageBody({
  monitoringInstance,
  runtimeFacts,
  latestSample,
  snapshotReadAt,
  heartbeatFreshness,
  runtimeFactsError,
  runtimeFactsLoading,
  onRetryRuntimeFacts,
  onRetrySettings,
  runtimeSubmitting,
  runtimeError,
  pendingRuntimeConfirmation,
  metadataEditing,
  metadataGroupDraft,
  metadataLabelDraft,
  metadataNoteDraft,
  metadataSubmitting,
  metadataError,
  managementReview,
  managementLoading,
  managementError,
  managementSubmittingAction,
  managementActionError,
  onRuntimeAction,
  onCancelRuntimeConfirmation,
  registerActionRef,
  onMetadataGroupDraftChange,
  onMetadataLabelDraftChange,
  onMetadataNoteDraftChange,
  onMetadataStartEdit,
  onMetadataCancelEdit,
  onMetadataSubmit,
  onManagementLoadReview,
  onManagementRetire,
  onManagementRestoreLifecycle,
  onManagementArchive,
  onManagementRestoreArchive,
  onManagementPermanentCleanup,
  incidents,
  incidentsError,
  events,
  eventsError,
  incidentsLoaded,
  eventsLoaded,
  incidentsRetrying,
  eventsRetrying,
  onRetryIncidents,
  onRetryEvents,
  onRetryBindingConflict,
  onRetryLinkedVPS,
  commandPollError,
  onRetryCommandPoll,
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
  thresholds,
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
  onRefresh,
}: MonitoringDetailPageBodyProps) {
  const [searchParams] = useSearchParams()
  const [bindingDialogOpen, setBindingDialogOpen] = useState(false)
  const returnVPSId = validateReturnVPSId(searchParams.get('return_vps'))
  const sample = latestSample
  const metricPoints =
    timeWindow === 'realtime'
      ? realtimeSamples.map(hostSampleToMetricPoint)
      : runtimeFacts?.host_metric_points ?? []
  const isMaintenance = monitoringInstance.monitoring_status === '维护中'
  const archived = Boolean(monitoringInstance.archived_at)
  const showBindingConflict = monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
  const bindingActionsDisabled = bindingAction !== null || bindingConflictLoading || !bindingConflict
  const isUpgradeOnboarding =
    monitoringInstance.binding_status !== '未绑定' ||
    Boolean(monitoringInstance.last_heartbeat_at || monitoringInstance.last_sync_at || sample)
  const readOnly = READ_ONLY_PREVIEW
  // An unbound, unarchived instance gets "接入 agent…" as the header primary action.
  const showOnboardingPrimary =
    !readOnly && !archived && monitoringInstance.binding_status === '未绑定'
  const subjectBase = `/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}`
  const bindingDialogVisible =
    bindingDialogOpen && showBindingConflict && !pendingBindingConfirmation

  return (
    <div className="monitoring-detail-route">
      <MonitoringInstanceWatchtowerHeader
        monitoringInstance={monitoringInstance}
        readOnly={readOnly}
        linkedVPS={linkedVPS}
        linkedVPSLoading={linkedVPSLoading}
        linkedVPSLoaded={linkedVPSLoaded}
        linkedVPSError={linkedVPSError}
        onRetryLinkedVPS={onRetryLinkedVPS}
        actions={
          <>
            {onRefresh ? (
              <Button variant="ghost" size="sm" onClick={onRefresh}>
                刷新
              </Button>
            ) : null}
            {readOnly ? null : (
              <MonitoringDetailManagementMenu
                monitoringInstance={monitoringInstance}
                runtimeActions={monitoringInstanceRuntimeActions(monitoringInstance)}
                runtimeSubmitting={runtimeSubmitting}
                onRuntimeAction={(action) => onRuntimeAction(action)}
                registerActionRef={registerActionRef}
                onOpenOnboarding={onOpenOnboarding}
                onboardingActionLabel={isUpgradeOnboarding ? '升级/重新接入 agent…' : '接入 agent…'}
                onOpenCommands={onOpenCommands}
                onOpenMetadata={onMetadataStartEdit}
                triggerVariant={showOnboardingPrimary ? 'ghost' : 'primary'}
                triggerSize={showOnboardingPrimary ? 'sm' : 'md'}
                review={managementReview}
                loading={managementLoading}
                error={managementError}
                submittingAction={managementSubmittingAction}
                actionError={managementActionError}
                onLoadReview={onManagementLoadReview}
                onRetire={onManagementRetire}
                onRestoreLifecycle={onManagementRestoreLifecycle}
                onArchive={onManagementArchive}
                onRestoreArchive={onManagementRestoreArchive}
                onPermanentCleanup={onManagementPermanentCleanup}
              />
            )}
            {showOnboardingPrimary ? (
              <Button variant="primary" onClick={onOpenOnboarding}>
                {isUpgradeOnboarding ? '升级/重新接入 agent…' : '接入 agent…'}
              </Button>
            ) : null}
          </>
        }
      />

      <MonitoringDetailStatusBand
        monitoringInstance={monitoringInstance}
        heartbeatFreshness={heartbeatFreshness}
        snapshotReadAt={snapshotReadAt}
      />

      <MonitoringDetailNotices
        monitoringInstance={monitoringInstance}
        incidents={incidents}
        incidentsError={incidentsError}
        incidentsLoaded={incidentsLoaded}
        incidentsRetrying={incidentsRetrying}
        onRetryIncidents={onRetryIncidents}
        runtimeError={runtimeError}
        runtimeFactsError={runtimeFactsError}
        runtimeFactsLoading={runtimeFactsLoading}
        hasRetainedRuntimeFacts={Boolean(runtimeFacts)}
        onRetryRuntimeFacts={onRetryRuntimeFacts}
        bindingConflictLoading={bindingConflictLoading}
        bindingConflictError={bindingConflictError}
        onRetryBindingConflict={onRetryBindingConflict}
        onOpenBindingConflict={() => setBindingDialogOpen(true)}
        onOpenIncidentHistory={() => onOpenHistory('incidents')}
      />

      <MonitoringDetailObservations
        timeRangeControl={
          <MonitoringInstanceTimeWindowTabs
            value={timeWindow}
            onChange={onTimeWindowChange}
            streamStatus={runtimeStreamStatus}
            streamError={runtimeStreamError}
          />
        }
        sample={sample}
        metricPoints={metricPoints}
        timeWindow={timeWindow}
        {...(runtimeFacts?.window === undefined ? {} : { window: runtimeFacts.window })}
        isMaintenance={isMaintenance}
        thresholds={thresholds}
        loading={runtimeFactsLoading}
        error={runtimeFactsError}
        snapshotReadAt={snapshotReadAt}
        onRetryThresholds={onRetrySettings}
      />

      <MonitoringDetailRecentEvents
        subjectBase={subjectBase}
        returnVPSId={returnVPSId}
        events={events}
        eventsError={eventsError}
        eventsLoaded={eventsLoaded}
        eventsRetrying={eventsRetrying}
        onRetryEvents={onRetryEvents}
        onOpenHistory={() => onOpenHistory('events')}
      />

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <MonitoringInstanceRuntimePauseConfirmation
          monitoringStatus={pendingRuntimeConfirmation.monitoringStatus}
          disabled={runtimeSubmitting}
          onConfirm={() => onRuntimeAction('pause', true)}
          onCancel={onCancelRuntimeConfirmation}
        />
      ) : null}

      <MonitoringDetailMetadataDialog
        open={metadataEditing}
        monitoringInstance={monitoringInstance}
        groupDraft={metadataGroupDraft}
        labelDraft={metadataLabelDraft}
        noteDraft={metadataNoteDraft}
        submitting={metadataSubmitting}
        error={metadataError}
        onGroupDraftChange={onMetadataGroupDraftChange}
        onLabelDraftChange={onMetadataLabelDraftChange}
        onNoteDraftChange={onMetadataNoteDraftChange}
        onSubmit={onMetadataSubmit}
        onClose={onMetadataCancelEdit}
      />

      <MonitoringInstanceBindingConflictDialog
        open={bindingDialogVisible}
        readOnly={readOnly}
        bindingConflict={bindingConflict}
        loading={bindingConflictLoading}
        error={bindingConflictError}
        bindingAction={bindingAction}
        actionsDisabled={bindingActionsDisabled}
        onConfirm={() => {
          setBindingDialogOpen(false)
          onRequestBindingAction('confirm')
        }}
        onReject={() => {
          setBindingDialogOpen(false)
          onRequestBindingAction('reject')
        }}
        onReset={() => {
          setBindingDialogOpen(false)
          onRequestBindingAction('reset')
        }}
        onRetry={onRetryBindingConflict}
        onClose={() => setBindingDialogOpen(false)}
      />

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

      <MonitoringInstanceHistoryDrawer
        monitoringInstance={monitoringInstance}
        open={historyOpen}
        tab={historyTab}
        events={events}
        eventsError={eventsError}
        onRetryEvents={onRetryEvents}
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
        pollError={commandPollError}
        onRetryPoll={onRetryCommandPoll}
        onClose={onCloseCommand}
        onExecute={onExecuteCommand}
      />

      <MonitoringInstanceOnboardingDrawer
        monitoringInstance={monitoringInstance}
        open={onboardingOpen}
        returnVPSId={onboardingReturnVPSId}
        mode={isUpgradeOnboarding ? 'upgrade' : 'connect'}
        onClose={onCloseOnboarding}
      />
    </div>
  )
}
