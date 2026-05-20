import type { ReactNode, RefObject } from 'react'

import { DetailSection } from '../../components/DetailSection'
import { MonoDigits } from '../../components/atoms/Mono'
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
  aside,
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
  const enabledProbeCount = probeItems.filter((item) => item.enabled).length
  const latestObservationCount = Array.from(observationsByProbe.values()).reduce(
    (total, observations) => total + observations.length,
    0,
  )
  const defaultAside = (
    <span className="detail-section__aside-meta">
      启用 <MonoDigits>{enabledProbeCount}</MonoDigits> / <MonoDigits>{probeItems.length}</MonoDigits> · 最新观测{' '}
      <MonoDigits>{latestObservationCount}</MonoDigits>
    </span>
  )

  return (
    <DetailSection
      eyebrow="ProbeItem 工作区"
      title="ProbeItem 列表"
      ribbon="accent"
      aside={aside ?? defaultAside}
    >
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
    </DetailSection>
  )
}
