import type { FormEvent, RefObject } from 'react'

import {
  TargetLatencyTrends,
  TargetWatchtowerHeader,
  type PendingProbeConfirmation,
  type ProbeCreateFormState,
  type ProbeFormMode,
  type TargetRuntimeAction,
} from '../../components/target-detail'

import { Button } from '../../components/atoms/Button'
import { Modal } from '../../components/atoms/Modal'
import { MonoDigits } from '../../components/atoms/Mono'
import type {
  ActiveIncidentRecord,
  AssetContextForTarget,
  ProbeItemRecord,
  ProbeKind,
  ProbeObservation,
  StateChangeEventRecord,
  TargetRecord,
} from '../../lib/types'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { TargetDetailNotices } from './TargetDetailNotices'
import { TargetDetailRecentEvents } from './TargetDetailRecentEvents'
import { TargetDetailStatusBand } from './TargetDetailStatusBand'
import '../monitoring-detail/MonitoringDetailWorkspace.css'
import './TargetDetailWorkspace.css'
import { TargetHistoryDrawer } from './TargetHistoryDrawer'
import { TargetLifecycleSection } from './TargetLifecycleSection'
import { TargetMetadataSection } from './TargetMetadataSection'
import { TargetProbeFormDrawer } from './TargetProbeFormDrawer'
import { TargetProbeListSection } from './TargetProbeListSection'
import { TargetRuntimePauseConfirmation } from './TargetRuntimePauseConfirmation'
import { TargetTimeWindowTabs } from './TargetTimeWindowTabs'
import type { HistoryTab, MetadataFormState, PendingRuntimeConfirmation, TimeWindow } from './types'

function latestObservationAt(observations: ProbeObservation[]) {
  const [firstObservation, ...remainingObservations] = observations
  if (!firstObservation) return null
  return remainingObservations.reduce((latest, observation) =>
    new Date(observation.observed_at).getTime() > new Date(latest).getTime()
      ? observation.observed_at
      : latest,
  firstObservation.observed_at)
}

type TargetDetailPageBodyProps = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  activityLoaded: boolean
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
  incidentsRetrying: boolean
  eventsRetrying: boolean
  onRetryIncidents: () => void
  onRetryEvents: () => void
  recentObservations: ProbeObservation[]
  observationsByProbe: Map<string, ProbeObservation[]>
  runtimeSubmitting: boolean
  runtimeError: string | null
  pendingRuntimeConfirmation: PendingRuntimeConfirmation | null
  runtimeConfirmationActive: boolean
  probeConfirmationActive: boolean
  assetContext: AssetContextForTarget | null
  assetContextError: string | null
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
  onOpenProbeCreate: () => void
  onCloseProbeForm: () => void
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
  maintenanceOpen: boolean
  onOpenMaintenance: () => void
  onCloseMaintenance: () => void
}

export function TargetDetailPageBody({
  target,
  probeItems,
  activityLoaded,
  incidents,
  incidentsError,
  events,
  eventsError,
  incidentsRetrying,
  eventsRetrying,
  onRetryIncidents,
  onRetryEvents,
  recentObservations,
  observationsByProbe,
  runtimeSubmitting,
  runtimeError,
  pendingRuntimeConfirmation,
  runtimeConfirmationActive,
  probeConfirmationActive,
  assetContext,
  assetContextError,
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
  onOpenProbeCreate,
  onCloseProbeForm,
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
  maintenanceOpen,
  onOpenMaintenance,
  onCloseMaintenance,
}: TargetDetailPageBodyProps) {
  const probeRowMutationBusy = probeMutationBusyId !== null
  const probeActionsDisabled =
    probeCreateSubmitting || probeRowMutationBusy || runtimeConfirmationActive || probeConfirmationActive
  const readOnly = READ_ONLY_PREVIEW
  const isArchived = target.run_status === '已归档'
  const archiveRuntimeError =
    pendingRuntimeConfirmation?.action === 'archive' ? runtimeError : null
  const latestRuntimeObservationAt = latestObservationAt([
    ...recentObservations,
    ...Array.from(observationsByProbe.values()).flat(),
  ])
  const observationWorkspaceAside = (
    <div className="target-activity-actions">
      <TargetTimeWindowTabs value={timeWindow} onChange={onTimeWindowChange} />
    </div>
  )
  const eventAside = (
    <div className="target-activity-actions">
      <span className="target-detail-latency__meta">
        事件 <MonoDigits>{events.length}</MonoDigits>
      </span>
      <Button variant="ghost" size="sm" onClick={() => onOpenHistory('events')}>
        查看历史
      </Button>
    </div>
  )

  return (
    <div className="page target-detail-route">
      <div className="target-detail-masthead">
        <TargetWatchtowerHeader
          target={target}
          runtimeSubmitting={runtimeSubmitting}
          disabled={probeConfirmationActive}
          readOnly={readOnly}
          onRuntimeAction={(action) => onRuntimeAction(action)}
          registerActionRef={registerActionRef}
          onOpenHistory={() => onOpenHistory('events')}
          onOpenMaintenance={onOpenMaintenance}
        />
        <TargetDetailStatusBand
          probeItems={probeItems}
          latestObservationAt={latestRuntimeObservationAt}
          assetContext={assetContext}
          assetContextError={assetContextError}
        />
      </div>

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <TargetRuntimePauseConfirmation
          target={target}
          disabled={runtimeSubmitting}
          onConfirm={() => onRuntimeAction('pause', true)}
          onCancel={onCancelPauseConfirmation}
        />
      ) : null}
      <TargetDetailNotices
        target={target}
        incidents={incidents}
        incidentsError={incidentsError}
        incidentsRetrying={incidentsRetrying}
        onRetryIncidents={onRetryIncidents}
        runtimeError={runtimeError && pendingRuntimeConfirmation?.action !== 'archive' ? runtimeError : null}
        onOpenEvents={() => onOpenHistory('incidents')}
      />

      <section className="monitoring-detail-section" aria-label="近期延迟">
        <header className="monitoring-detail-section__head">
          <h2>近期延迟</h2>
          {observationWorkspaceAside}
        </header>
        <TargetLatencyTrends
          probeItems={probeItems}
          recentObservations={recentObservations}
          timeWindow={timeWindow}
          isMaintenance={target.run_status === '维护中'}
        />
      </section>

      <TargetProbeListSection
        probeItems={probeItems}
        observationsByProbe={observationsByProbe}
        actionsDisabled={probeActionsDisabled}
        pendingProbeConfirmation={pendingProbeConfirmation}
        confirmationCardDisabled={probeCreateSubmitting || probeRowMutationBusy}
        registerDeleteButtonRef={registerDeleteButtonRef}
        onAddProbe={onAddProbe}
        onEdit={onEditProbe}
        onToggle={onToggleProbe}
        onDelete={onDeleteProbe}
        onConfirmDelete={onConfirmDeleteProbe}
        onCancelDeleteConfirmation={onCancelDeleteConfirmation}
        addProbeButtonRef={addProbeButtonRef}
        probeFormOpen={probeCreateOpen}
        probeMutationError={probeMutationError}
        addDisabled={probeCreateSubmitting || runtimeConfirmationActive || probeConfirmationActive}
        onOpenCreate={onOpenProbeCreate}
        readOnly={readOnly}
      />

      <TargetDetailRecentEvents
        loaded={activityLoaded}
        events={events}
        error={eventsError}
        retrying={eventsRetrying}
        onRetry={onRetryEvents}
        aside={eventAside}
      />

      <Modal
        open={maintenanceOpen}
        onClose={onCloseMaintenance}
        title="标签、备注与生命周期"
        ariaLabel="标签、备注与生命周期"
        size="md"
        contentClassName="watchtower-form-modal"
      >
        <div className="watchtower-property-list target-maintenance-list">
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
      </Modal>

      <TargetProbeFormDrawer
        target={target}
        open={probeCreateOpen}
        mode={probeFormMode}
        form={probeCreateForm}
        submitting={probeCreateSubmitting}
        error={probeCreateError}
        onClose={onCloseProbeForm}
        onSubmit={onProbeSubmit}
        onProbeKindChange={onProbeKindChange}
        onFieldChange={onProbeFieldChange}
      />

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
        onRetryEvents={onRetryEvents}
        onRetryHistoryIncidents={onRetryHistoryIncidents}
      />
    </div>
  )
}
