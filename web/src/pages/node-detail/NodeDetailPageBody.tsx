import type { RefObject } from 'react'

import {
  NodeWatchtowerHeader,
  NodeWatchtowerMetrics,
  type NodeRuntimeAction,
} from '../../components/node-detail'
import type {
  ActiveIncidentRecord,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'
import { NodeAccessCredentialSection } from './NodeAccessCredentialSection'
import { NodeBindingConflictSection } from './NodeBindingConflictSection'
import { NodeCommandDrawer } from './NodeCommandDrawer'
import { NodeContainersSection } from './NodeContainersSection'
import { NodeDangerCard } from './NodeDangerCard'
import { NodeDiagnosisSummary } from './NodeDiagnosisSummary'
import { NodeHistoryDrawer } from './NodeHistoryDrawer'
import { NodeLifecycleSection } from './NodeLifecycleSection'
import { NodeLinkedVPSSection } from './NodeLinkedVPSSection'
import { NodeMetadataSection } from './NodeMetadataSection'
import { NodeRuntimePauseConfirmation } from './NodeRuntimePauseConfirmation'
import { NodeSnapshotMeta } from './NodeSnapshotMeta'
import { NodeTimeWindowTabs } from './NodeTimeWindowTabs'
import {
  COMMAND_LABELS,
  COMMAND_LIST,
  NODE_BINDING_CONFLICT_STATUS,
  NODE_LIFECYCLE_RETIRED,
} from './nodeDetailConstants'
import { nodeRuntimeActions } from './nodeDetailHelpers'
import type {
  BindingConflictAction,
  HistoryTab,
  NodeLifecycleAction,
  PendingRuntimeConfirmation,
  TimeWindow,
} from './types'

type NodeDetailPageBodyProps = {
  node: NodeRecord
  runtimeFacts: NodeRuntimeFacts | null
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  onRuntimeAction: (action: NodeRuntimeAction, confirmed?: boolean) => void
  onCancelRuntimeConfirmation: () => void
  registerActionRef: (action: NodeRuntimeAction, element: HTMLButtonElement | null) => void
  incidents: ActiveIncidentRecord[]
  events: StateChangeEventRecord[]
  eventsError: string | null
  linkedVPSSectionRef: RefObject<HTMLDivElement | null>
  linkedVPS: VPSSummary[]
  linkedVPSLoading: boolean
  linkedVPSLoaded: boolean
  linkedVPSError: string | null
  bindingConflict: NodeOnboardingState | null
  bindingConflictLoading: boolean
  bindingConflictError: string | null
  bindingAction: BindingConflictAction | null
  onBindingConfirm: () => void
  onBindingReject: () => void
  onBindingReset: () => void
  timeWindow: TimeWindow
  onTimeWindowChange: (value: TimeWindow) => void
  metadataEditing: boolean
  metadataGroupDraft: string
  metadataLabelDraft: string
  metadataNoteDraft: string
  metadataSubmitting: boolean
  metadataError: string | null
  onMetadataGroupDraftChange: (value: string) => void
  onMetadataLabelDraftChange: (value: string) => void
  onMetadataNoteDraftChange: (value: string) => void
  onStartMetadataEdit: () => void
  onCancelMetadataEdit: () => void
  onSaveMetadata: () => void
  showRetireConfirmation: boolean
  lifecycleSubmitting: NodeLifecycleAction | null
  lifecycleError: string | null
  onLifecycleRestore: () => void
  onStartRetire: () => void
  onConfirmRetire: () => void
  onCancelRetire: () => void
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
}

export function NodeDetailPageBody({
  node,
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
  metadataEditing,
  metadataGroupDraft,
  metadataLabelDraft,
  metadataNoteDraft,
  metadataSubmitting,
  metadataError,
  onMetadataGroupDraftChange,
  onMetadataLabelDraftChange,
  onMetadataNoteDraftChange,
  onStartMetadataEdit,
  onCancelMetadataEdit,
  onSaveMetadata,
  showRetireConfirmation,
  lifecycleSubmitting,
  lifecycleError,
  onLifecycleRestore,
  onStartRetire,
  onConfirmRetire,
  onCancelRetire,
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
}: NodeDetailPageBodyProps) {
  const sample = runtimeFacts?.latest_host_sample ?? null
  const recentSamples = runtimeFacts?.recent_host_samples ?? []
  const isMaintenance = node.monitoring_status === '维护中'
  const showBindingConflict = node.binding_status === NODE_BINDING_CONFLICT_STATUS
  const isRetiredNode = node.lifecycle_status === NODE_LIFECYCLE_RETIRED
  const bindingActionsDisabled =
    bindingAction !== null || bindingConflictLoading || !bindingConflict
  const showDangerZone = node.current_active_incident_count > 0
  const firstIncident =
    incidents.length > 0
      ? [...incidents].sort(
          (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
        )[0]
      : null

  return (
    <div className="page-stack">
      <NodeWatchtowerHeader
        node={node}
        latestSample={sample}
        runtimeActions={nodeRuntimeActions(node)}
        runtimeSubmitting={runtimeSubmitting}
        onRuntimeAction={(action) => onRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => onOpenHistory('events')}
        onOpenCommands={onOpenCommands}
      />

      <NodeDiagnosisSummary
        node={node}
        sample={sample}
        incidents={incidents}
        eventsError={eventsError}
        onOpenEvents={() => onOpenHistory('events')}
        onOpenIncidents={() => onOpenHistory('incidents')}
      />

      <NodeLinkedVPSSection
        sectionRef={linkedVPSSectionRef}
        records={linkedVPS}
        loading={linkedVPSLoading}
        loaded={linkedVPSLoaded}
        error={linkedVPSError}
      />

      {showBindingConflict ? (
        <NodeBindingConflictSection
          node={node}
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

      {showDangerZone ? (
        <NodeDangerCard
          node={node}
          firstIncident={firstIncident}
          onOpenEvents={() => onOpenHistory('events')}
        />
      ) : null}

      <NodeTimeWindowTabs value={timeWindow} onChange={onTimeWindowChange} />

      <NodeWatchtowerMetrics
        sample={sample}
        samples={recentSamples}
        isMaintenance={isMaintenance}
      />

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <NodeRuntimePauseConfirmation
          node={node}
          disabled={runtimeSubmitting}
          onConfirm={() => onRuntimeAction('pause', true)}
          onCancel={onCancelRuntimeConfirmation}
        />
      ) : null}
      {runtimeError ? <p className="watchtower-runtime-error" role="alert">{runtimeError}</p> : null}

      <NodeMetadataSection
        node={node}
        editing={metadataEditing}
        groupDraft={metadataGroupDraft}
        labelDraft={metadataLabelDraft}
        noteDraft={metadataNoteDraft}
        submitting={metadataSubmitting}
        error={metadataError}
        onGroupDraftChange={onMetadataGroupDraftChange}
        onLabelDraftChange={onMetadataLabelDraftChange}
        onNoteDraftChange={onMetadataNoteDraftChange}
        onStartEdit={onStartMetadataEdit}
        onCancelEdit={onCancelMetadataEdit}
        onSave={onSaveMetadata}
      />

      <NodeLifecycleSection
        isRetiredNode={isRetiredNode}
        showRetireConfirmation={showRetireConfirmation}
        submitting={lifecycleSubmitting}
        error={lifecycleError}
        onRestore={onLifecycleRestore}
        onStartRetire={onStartRetire}
        onConfirmRetire={onConfirmRetire}
        onCancelRetire={onCancelRetire}
      />

      <NodeAccessCredentialSection node={node} />

      <NodeContainersSection sample={sample} />

      <NodeSnapshotMeta sample={sample} />

      <NodeHistoryDrawer
        node={node}
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

      <NodeCommandDrawer
        node={node}
        open={commandOpen}
        commands={COMMAND_LIST}
        commandLabels={COMMAND_LABELS}
        submitting={commandSubmitting}
        error={commandError}
        onClose={onCloseCommand}
        onExecute={onExecuteCommand}
      />
    </div>
  )
}
