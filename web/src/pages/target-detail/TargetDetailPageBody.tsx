import type { FormEvent, RefObject } from 'react'

import {
  TargetActiveIncidents,
  TargetLatencyTrends,
  TargetRecentEvents,
  TargetWatchtowerHeader,
  type PendingProbeConfirmation,
  type ProbeCreateFormState,
  type ProbeFormMode,
  type TargetRuntimeAction,
} from '../../components/target-detail'
import { DetailSection } from '../../components/DetailSection'
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
import { TargetDecisionBoard } from './TargetDecisionBoard'
import { TargetDangerCard } from './TargetDangerCard'
import { TargetHistoryDrawer } from './TargetHistoryDrawer'
import { TargetLifecycleSection } from './TargetLifecycleSection'
import { TargetMetadataSection } from './TargetMetadataSection'
import { TargetProbeFormDrawer } from './TargetProbeFormDrawer'
import { TargetProbeListSection } from './TargetProbeListSection'
import { TargetRuntimePauseConfirmation } from './TargetRuntimePauseConfirmation'
import { TargetSnapshotMeta } from './TargetSnapshotMeta'
import { TargetTimeWindowTabs } from './TargetTimeWindowTabs'
import type { HistoryTab, MetadataFormState, PendingRuntimeConfirmation, TimeWindow } from './types'

function latestObservationAt(observations: ProbeObservation[]) {
  if (observations.length === 0) return null
  return observations.reduce((latest, observation) =>
    new Date(observation.observed_at).getTime() > new Date(latest).getTime()
      ? observation.observed_at
      : latest,
  observations[0].observed_at)
}

function countLatencySamples(observations: ProbeObservation[]) {
  return observations.filter((observation) => observation.latency_ms != null).length
}

function activityAside(label: string, count: number) {
  return (
    <span className="detail-section__aside-meta">
      {label} <MonoDigits>{count}</MonoDigits>
    </span>
  )
}

type TargetDetailPageBodyProps = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  activityLoaded: boolean
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
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
  const enabledProbeCount = probeItems.filter((item) => item.enabled).length
  const latencySampleCount = countLatencySamples(recentObservations)
  const latestRuntimeObservationAt = latestObservationAt([
    ...recentObservations,
    ...Array.from(observationsByProbe.values()).flat(),
  ])
  const observationWorkspaceAside = (
    <span className="detail-section__aside-meta">
      {timeWindow} · latency 样本 <MonoDigits>{latencySampleCount}</MonoDigits> · ProbeItem{' '}
      <MonoDigits>{enabledProbeCount}</MonoDigits>/<MonoDigits>{probeItems.length}</MonoDigits>
    </span>
  )
  const eventAside = (
    <div className="target-activity-actions">
      <span className="detail-section__aside-meta">
        事件 <MonoDigits>{events.length}</MonoDigits>
      </span>
      <Button variant="ghost" size="sm" onClick={() => onOpenHistory('events')}>
        查看历史
      </Button>
    </div>
  )

  return (
    <div className="page-stack">
      <TargetWatchtowerHeader
        target={target}
        runtimeSubmitting={runtimeSubmitting}
        disabled={probeConfirmationActive}
        onRuntimeAction={(action) => onRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => onOpenHistory('events')}
        onOpenMaintenance={onOpenMaintenance}
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

      <TargetDecisionBoard
        target={target}
        probeItems={probeItems}
        recentObservations={recentObservations}
        latestObservationAt={latestRuntimeObservationAt}
        latencySampleCount={latencySampleCount}
        assetContext={assetContext}
        assetContextError={assetContextError}
        onOpenHistory={() => onOpenHistory('events')}
      />

      {showDangerZone ? (
        <TargetDangerCard
          target={target}
          firstIncident={firstIncident}
          onOpenEvents={() => onOpenHistory('events')}
        />
      ) : null}

      <DetailSection
        eyebrow="观测工作区"
        title="运行控制与近期延迟"
        ribbon={target.run_status === '维护中' ? 'maintenance' : 'accent'}
        aside={observationWorkspaceAside}
      >
        <div className="target-observation-workbench">
          <div className="target-observation-workbench__intro">
            <div>
              <p className="target-observation-workbench__eyebrow">Runtime controls</p>
              <h3>运行控制状态：{target.run_status}</h3>
              <p>
                运行控制在右上角操作菜单中执行；时间窗口切换只刷新 runtime facts，不重载目标身份、ProbeItem 或事件证据。
              </p>
            </div>
            <TargetTimeWindowTabs value={timeWindow} onChange={onTimeWindowChange} />
          </div>

          <TargetLatencyTrends
            probeItems={probeItems}
            recentObservations={recentObservations}
            timeWindow={timeWindow}
            isMaintenance={target.run_status === '维护中'}
            watchtower
          />
        </div>
      </DetailSection>

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
      />

      <Modal
        open={maintenanceOpen}
        onClose={onCloseMaintenance}
        title="标签、备注与生命周期"
        ariaLabel="标签、备注与生命周期"
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

      <div className="target-activity-grid" aria-label="当前异常与事件证据">
        <TargetActiveIncidents
          loaded={activityLoaded}
          incidents={incidents}
          error={incidentsError}
          aside={activityAside('活跃', incidents.length)}
        />
        <TargetRecentEvents
          loaded={activityLoaded}
          events={events}
          error={eventsError}
          aside={eventAside}
        />
      </div>

      <TargetSnapshotMeta />

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
        onRetryHistoryIncidents={onRetryHistoryIncidents}
      />
    </div>
  )
}
