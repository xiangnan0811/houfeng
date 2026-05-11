import type { RefObject } from 'react'

import {
  TargetProbeList,
  type PendingProbeConfirmation,
} from '../../components/target-detail'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

type TargetProbeListSectionProps = {
  probeItems: ProbeItemRecord[]
  observationsByProbe: Map<string, ProbeObservation[]>
  actionsDisabled: boolean
  pendingProbeConfirmation: PendingProbeConfirmation | null
  confirmationCardDisabled: boolean
  pendingProbeConfirmationCardRef: RefObject<HTMLDivElement | null>
  registerDeleteButtonRef: (probeItemId: string, element: HTMLButtonElement | null) => void
  onAddProbe: () => void
  onEdit: (probeItem: ProbeItemRecord) => void
  onToggle: (probeItem: ProbeItemRecord) => void
  onDelete: (probeItem: ProbeItemRecord) => void
  onConfirmDelete: (probeItem: ProbeItemRecord) => void
  onCancelDeleteConfirmation: (probeItem: ProbeItemRecord) => void
}

export function TargetProbeListSection({
  probeItems,
  observationsByProbe,
  actionsDisabled,
  pendingProbeConfirmation,
  confirmationCardDisabled,
  pendingProbeConfirmationCardRef,
  registerDeleteButtonRef,
  onAddProbe,
  onEdit,
  onToggle,
  onDelete,
  onConfirmDelete,
  onCancelDeleteConfirmation,
}: TargetProbeListSectionProps) {
  return (
    <details className="watchtower-secondary">
      <summary>ProbeItem 列表</summary>
      <div className="watchtower-secondary__body">
        <TargetProbeList
          probeItems={probeItems}
          observationsByProbe={observationsByProbe}
          actionsDisabled={actionsDisabled}
          pendingProbeConfirmation={pendingProbeConfirmation}
          confirmationCardDisabled={confirmationCardDisabled}
          pendingProbeConfirmationCardRef={pendingProbeConfirmationCardRef}
          registerDeleteButtonRef={registerDeleteButtonRef}
          onAddProbe={onAddProbe}
          onEdit={onEdit}
          onToggle={onToggle}
          onDelete={onDelete}
          onConfirmDelete={onConfirmDelete}
          onCancelDeleteConfirmation={onCancelDeleteConfirmation}
        />
      </div>
    </details>
  )
}
