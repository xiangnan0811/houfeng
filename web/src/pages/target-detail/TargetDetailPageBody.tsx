import type { FormEvent, RefObject } from 'react'

import {
  TargetLatencyTrends,
  TargetWatchtowerHeader,
  type PendingProbeConfirmation,
  type ProbeCreateFormState,
  type ProbeFormMode,
  type TargetRuntimeAction,
} from '../../components/target-detail'
import type {
  ActiveIncidentRecord,
  ProbeItemRecord,
  ProbeKind,
  ProbeObservation,
  StateChangeEventRecord,
  TargetRecord,
} from '../../lib/types'
import { TargetDangerCard } from './TargetDangerCard'
import { TargetHistoryDrawer } from './TargetHistoryDrawer'
import { TargetLifecycleSection } from './TargetLifecycleSection'
import { TargetMetadataSection } from './TargetMetadataSection'
import { TargetProbeListSection } from './TargetProbeListSection'
import { TargetProbeManagementSection } from './TargetProbeManagementSection'
import { TargetRuntimePauseConfirmation } from './TargetRuntimePauseConfirmation'
import { TargetSnapshotMeta } from './TargetSnapshotMeta'
import { TargetTimeWindowTabs } from './TargetTimeWindowTabs'
import type { HistoryTab, MetadataFormState, PendingRuntimeConfirmation, TimeWindow } from './types'

type TargetDetailPageBodyProps = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  incidents: ActiveIncidentRecord[]
  events: StateChangeEventRecord[]
  eventsError: string | null
  recentObservations: ProbeObservation[]
  observationsByProbe: Map<string, ProbeObservation[]>
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  runtimeConfirmationActive: boolean
  probeConfirmationActive: boolean
  onRuntimeAction: (action: TargetRuntimeAction, confirmed?: boolean) => void
  onCancelPauseConfirmation: () => void
  onCancelArchiveConfirmation: () => void
  registerActionRef: (
    action: TargetRuntimeAction,
    element: HTMLButtonElement | null,
  ) => void
  timeWindow: TimeWindow
  onTimeWindowChange: (value: TimeWindow) => void
  addProbeButtonRef: RefObject<HTMLButtonElement | null>
  probeCreateOpen: boolean
  probeFormMode: ProbeFormMode
  probeCreateForm: ProbeCreateFormState
  probeCreateSubmitting: boolean
  probeCreateError: string | null
  probeMutationError: string | null
  onToggleCreate: () => void
  onProbeSubmit: (event: FormEvent<HTMLFormElement>) => void
  onProbeKindChange: (probeKind: ProbeKind) => void
  onProbeFieldChange: <K extends keyof ProbeCreateFormState>(
    field: K,
    value: ProbeCreateFormState[K],
  ) => void
  metadataEditing: boolean
  metadataSubmitting: boolean
  metadataError: string | null
  metadataForm: MetadataFormState
  onMetadataGroupChange: (value: string) => void
  onMetadataLabelChange: (value: string) => void
  onMetadataNoteChange: (value: string) => void
  onStartMetadataEdit: () => void
  onCancelMetadataEdit: () => void
  onMetadataSubmit: (event: FormEvent<HTMLFormElement>) => void
  probeMutationBusyId: string | null
  pendingProbeConfirmation: PendingProbeConfirmation | null
  pendingProbeConfirmationCardRef: RefObject<HTMLDivElement | null>
  registerDeleteButtonRef: (probeItemId: string, element: HTMLButtonElement | null) => void
  onAddProbe: () => void
  onEditProbe: (probeItem: ProbeItemRecord) => void
  onToggleProbe: (probeItem: ProbeItemRecord) => void
  onDeleteProbe: (probeItem: ProbeItemRecord) => void
  onConfirmDeleteProbe: (probeItem: ProbeItemRecord) => void
  onCancelDeleteConfirmation: (probeItem: ProbeItemRecord) => void
  historyOpen: boolean
  historyTab: HistoryTab
  historyIncidents: ActiveIncidentRecord[] | null
  historyIncidentsLoading: boolean
  historyIncidentsError: string | null
  onOpenHistory: (tab: HistoryTab) => void
  onCloseHistory: () => void
  onHistoryTabChange: (tab: HistoryTab) => void
  onRetryHistoryIncidents: () => void
}

export function TargetDetailPageBody({
  target,
  probeItems,
  incidents,
  events,
  eventsError,
  recentObservations,
  observationsByProbe,
  runtimeSubmitting,
  runtimeError,
  pendingRuntimeConfirmation,
  runtimeConfirmationActive,
  probeConfirmationActive,
  onRuntimeAction,
  onCancelPauseConfirmation,
  onCancelArchiveConfirmation,
  registerActionRef,
  timeWindow,
  onTimeWindowChange,
  addProbeButtonRef,
  probeCreateOpen,
  probeFormMode,
  probeCreateForm,
  probeCreateSubmitting,
  probeCreateError,
  probeMutationError,
  onToggleCreate,
  onProbeSubmit,
  onProbeKindChange,
  onProbeFieldChange,
  metadataEditing,
  metadataSubmitting,
  metadataError,
  metadataForm,
  onMetadataGroupChange,
  onMetadataLabelChange,
  onMetadataNoteChange,
  onStartMetadataEdit,
  onCancelMetadataEdit,
  onMetadataSubmit,
  probeMutationBusyId,
  pendingProbeConfirmation,
  pendingProbeConfirmationCardRef,
  registerDeleteButtonRef,
  onAddProbe,
  onEditProbe,
  onToggleProbe,
  onDeleteProbe,
  onConfirmDeleteProbe,
  onCancelDeleteConfirmation,
  historyOpen,
  historyTab,
  historyIncidents,
  historyIncidentsLoading,
  historyIncidentsError,
  onOpenHistory,
  onCloseHistory,
  onHistoryTabChange,
  onRetryHistoryIncidents,
}: TargetDetailPageBodyProps) {
  const showDangerZone = target.current_active_incident_count > 0
  const firstIncident =
    incidents.length > 0
      ? [...incidents].sort(
          (a, b) =>
            new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
        )[0]
      : null
  const probeRowMutationBusy = probeMutationBusyId !== null
  const probeActionsDisabled =
    probeCreateSubmitting || probeRowMutationBusy || runtimeConfirmationActive || probeConfirmationActive
  const isArchived = target.run_status === '已归档'
  const archiveRuntimeError =
    pendingRuntimeConfirmation?.action === 'archive' ? runtimeError : null

  return (
    <div className="page-stack">
      <TargetWatchtowerHeader
        target={target}
        runtimeSubmitting={runtimeSubmitting}
        disabled={probeConfirmationActive}
        onRuntimeAction={(action) => onRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => onOpenHistory('events')}
      />

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <TargetRuntimePauseConfirmation
          target={target}
          disabled={runtimeSubmitting}
          onConfirm={() => onRuntimeAction('pause', true)}
          onCancel={onCancelPauseConfirmation}
        />
      ) : null}
      {runtimeError && pendingRuntimeConfirmation?.action !== 'archive' ? (
        <p className="watchtower-runtime-error" role="alert">
          {runtimeError}
        </p>
      ) : null}

      {showDangerZone ? (
        <TargetDangerCard
          target={target}
          firstIncident={firstIncident}
          onOpenEvents={() => onOpenHistory('events')}
        />
      ) : null}

      <TargetTimeWindowTabs value={timeWindow} onChange={onTimeWindowChange} />

      <TargetLatencyTrends
        probeItems={probeItems}
        recentObservations={recentObservations}
        isMaintenance={target.run_status === '维护中'}
        watchtower
      />

      <TargetProbeListSection
        probeItems={probeItems}
        observationsByProbe={observationsByProbe}
        actionsDisabled={probeActionsDisabled}
        pendingProbeConfirmation={pendingProbeConfirmation}
        confirmationCardDisabled={probeCreateSubmitting || probeRowMutationBusy}
        pendingProbeConfirmationCardRef={pendingProbeConfirmationCardRef}
        registerDeleteButtonRef={registerDeleteButtonRef}
        onAddProbe={onAddProbe}
        onEdit={onEditProbe}
        onToggle={onToggleProbe}
        onDelete={onDeleteProbe}
        onConfirmDelete={onConfirmDeleteProbe}
        onCancelDeleteConfirmation={onCancelDeleteConfirmation}
      />

      <div className="watchtower-property-list">
        <TargetProbeManagementSection
          addProbeButtonRef={addProbeButtonRef}
          probeCreateOpen={probeCreateOpen}
          probeFormMode={probeFormMode}
          probeCreateForm={probeCreateForm}
          probeCreateSubmitting={probeCreateSubmitting}
          probeCreateError={probeCreateError}
          probeMutationError={probeMutationError}
          addDisabled={probeCreateSubmitting || runtimeConfirmationActive || probeConfirmationActive}
          onToggleCreate={onToggleCreate}
          onSubmit={onProbeSubmit}
          onProbeKindChange={onProbeKindChange}
          onFieldChange={onProbeFieldChange}
        />

        <TargetMetadataSection
          target={target}
          editing={metadataEditing}
          groupDraft={metadataForm.group}
          labelDraft={metadataForm.labels}
          noteDraft={metadataForm.note}
          submitting={metadataSubmitting}
          error={metadataError}
          onGroupDraftChange={onMetadataGroupChange}
          onLabelDraftChange={onMetadataLabelChange}
          onNoteDraftChange={onMetadataNoteChange}
          onStartEdit={onStartMetadataEdit}
          onCancelEdit={onCancelMetadataEdit}
          onSubmit={onMetadataSubmit}
        />

        <TargetLifecycleSection
          isArchived={isArchived}
          runtimeSubmitting={runtimeSubmitting}
          probeConfirmationActive={probeConfirmationActive}
          showArchiveConfirmation={pendingRuntimeConfirmation?.action === 'archive'}
          error={archiveRuntimeError}
          onRestore={() => onRuntimeAction('restore-to-paused')}
          onStartArchive={() => onRuntimeAction('archive')}
          onConfirmArchive={() => onRuntimeAction('archive', true)}
          onCancelArchive={onCancelArchiveConfirmation}
          registerActionRef={registerActionRef}
        />
      </div>

      <TargetSnapshotMeta />

      <TargetHistoryDrawer
        target={target}
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
    </div>
  )
}
