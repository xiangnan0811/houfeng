import type { ReactNode, RefObject } from 'react'

import { Button } from '../../components/atoms/Button'
import {
  TargetProbeList,
  type PendingProbeConfirmation,
} from '../../components/target-detail'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

type TargetProbeListSectionProps = {
  probeItems: ProbeItemRecord[]
  observationsByProbe: Map<string, ProbeObservation[]>
  aside?: ReactNode
  actionsDisabled: boolean
  pendingProbeConfirmation: PendingProbeConfirmation | null
  confirmationCardDisabled: boolean
  registerDeleteButtonRef: (probeItemId: string, element: HTMLButtonElement | null) => void
  onAddProbe: () => void
  onEdit: (probeItem: ProbeItemRecord) => void
  onToggle: (probeItem: ProbeItemRecord) => void
  onDelete: (probeItem: ProbeItemRecord) => void
  onConfirmDelete: (probeItem: ProbeItemRecord) => void
  onCancelDeleteConfirmation: (probeItem: ProbeItemRecord) => void
  addProbeButtonRef: RefObject<HTMLButtonElement | null>
  probeFormOpen: boolean
  probeMutationError: string | null
  addDisabled: boolean
  onOpenCreate: () => void
  readOnly?: boolean
}

export function TargetProbeListSection({
  probeItems,
  observationsByProbe,
  aside,
  actionsDisabled,
  pendingProbeConfirmation,
  confirmationCardDisabled,
  registerDeleteButtonRef,
  onAddProbe,
  onEdit,
  onToggle,
  onDelete,
  onConfirmDelete,
  onCancelDeleteConfirmation,
  addProbeButtonRef,
  probeFormOpen,
  probeMutationError,
  addDisabled,
  onOpenCreate,
  readOnly = false,
}: TargetProbeListSectionProps) {
  const defaultAside = readOnly ? null : (
    <div className="target-probe-section__tools">
      <Button
        ref={addProbeButtonRef}
        variant="secondary"
        size="sm"
        disabled={addDisabled || probeFormOpen}
        onClick={onOpenCreate}
      >
        添加 ProbeItem
      </Button>
    </div>
  )

  return (
    <section className="monitoring-detail-section target-probe-section" aria-label="探测方式">
      <header className="monitoring-detail-section__head">
        <h2>探测方式</h2>
        {aside ?? defaultAside}
      </header>
      {probeMutationError ? (
        <p className="watchtower-runtime-error" role="alert">
          {probeMutationError}
        </p>
      ) : null}
      <TargetProbeList
        probeItems={probeItems}
        observationsByProbe={observationsByProbe}
        actionsDisabled={actionsDisabled}
        pendingProbeConfirmation={pendingProbeConfirmation}
        confirmationCardDisabled={confirmationCardDisabled}
        registerDeleteButtonRef={registerDeleteButtonRef}
        hideActions={readOnly}
        onAddProbe={onAddProbe}
        onEdit={onEdit}
        onToggle={onToggle}
        onDelete={onDelete}
        onConfirmDelete={onConfirmDelete}
        onCancelDeleteConfirmation={onCancelDeleteConfirmation}
      />
    </section>
  )
}
