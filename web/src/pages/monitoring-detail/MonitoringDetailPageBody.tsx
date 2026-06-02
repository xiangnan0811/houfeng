import type { RefObject } from 'react'

import { DetailSection } from '../../components/DetailSection'
import {
  MonitoringInstanceWatchtowerHeader,
  MonitoringInstanceWatchtowerMetrics,
  type MonitoringInstanceRuntimeAction,
} from '../../components/monitoring-detail'
import type {
  ActiveIncidentRecord,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeFacts,
  HostSample,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'
import { MonitoringInstanceBindingConflictSection } from './MonitoringInstanceBindingConflictSection'
import { MonitoringInstanceCommandDrawer } from './MonitoringInstanceCommandDrawer'
import { MonitoringInstanceContainersSection } from './MonitoringInstanceContainersSection'
import { MonitoringInstanceDangerCard } from './MonitoringInstanceDangerCard'
import { MonitoringInstanceHistoryDrawer } from './MonitoringInstanceHistoryDrawer'
import { MonitoringInstanceLinkedVPSSection } from './MonitoringInstanceLinkedVPSSection'
import { MonitoringInstanceOnboardingDrawer } from './MonitoringInstanceOnboardingDrawer'
import { MonitoringInstanceRuntimePauseConfirmation } from './MonitoringInstanceRuntimePauseConfirmation'
import { MonitoringInstanceSnapshotMeta } from './MonitoringInstanceSnapshotMeta'
import { MonitoringInstanceTimeWindowTabs } from './MonitoringInstanceTimeWindowTabs'
import {
  COMMAND_LABELS,
  COMMAND_LIST,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
} from './monitoringDetailConstants'
import { monitoringInstanceRuntimeActions } from './monitoringDetailHelpers'
import type {
  BindingConflictAction,
  HistoryTab,
  PendingRuntimeConfirmation,
  RuntimeStreamStatus,
  TimeWindow,
} from './types'

type MonitoringDetailPageBodyProps = {
  monitoringInstance: MonitoringInstanceRecord
  runtimeFacts: MonitoringInstanceRuntimeFacts | null
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  onRuntimeAction: (action: MonitoringInstanceRuntimeAction, confirmed?: boolean) => void
  onCancelRuntimeConfirmation: () => void
  registerActionRef: (action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) => void
  incidents: ActiveIncidentRecord[]
  events: StateChangeEventRecord[]
  eventsError: string | null
  linkedVPSSectionRef: RefObject<HTMLDivElement | null>
  linkedVPS: VPSSummary[]
  linkedVPSLoading: boolean
  linkedVPSLoaded: boolean
  linkedVPSError: string | null
  bindingConflict: MonitoringInstanceOnboardingState | null
  bindingConflictLoading: boolean
  bindingConflictError: string | null
  bindingAction: BindingConflictAction | null
  onBindingConfirm: () => void
  onBindingReject: () => void
  onBindingReset: () => void
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
  onRuntimeAction,
  onCancelRuntimeConfirmation,
  registerActionRef,
  incidents,
  events,
  eventsError,
  linkedVPSSectionRef,
  linkedVPS,
  linkedVPSLoading,
  linkedVPSLoaded,
  linkedVPSError,
  bindingConflict,
  bindingConflictLoading,
  bindingConflictError,
  bindingAction,
  onBindingConfirm,
  onBindingReject,
  onBindingReset,
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
          onConfirm={onBindingConfirm}
          onReject={onBindingReject}
          onReset={onBindingReset}
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

      <MonitoringInstanceLinkedVPSSection
        sectionRef={linkedVPSSectionRef}
        records={linkedVPS}
        loading={linkedVPSLoading}
        loaded={linkedVPSLoaded}
        error={linkedVPSError}
      />

      <DetailSection
        eyebrow="RUNTIME FACTS"
        title="容器列表"
        aside={sample?.containers?.length ? `${sample.containers.length} 个` : '暂无数据'}
      >
        <MonitoringInstanceContainersSection sample={sample} />
      </DetailSection>

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
