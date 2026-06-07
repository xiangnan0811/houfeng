import type { ReactNode, RefObject } from 'react'

import { DetailSection } from '../../components/DetailSection'
import { Button } from '../../components/atoms/Button'
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
}: TargetProbeListSectionProps) {
  const enabledProbeCount = probeItems.filter((item) => item.enabled).length
  const latestObservationCount = Array.from(observationsByProbe.values()).reduce(
    (total, observations) => total + observations.length,
    0,
  )
  const defaultAside = (
    <div className="detail-section__aside-actions">
      <span className="detail-section__aside-meta">
        启用 <MonoDigits>{enabledProbeCount}</MonoDigits> / <MonoDigits>{probeItems.length}</MonoDigits> · 最新观测{' '}
        <MonoDigits>{latestObservationCount}</MonoDigits>
      </span>
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
    <DetailSection
      eyebrow="ProbeItem 工作区"
      title="ProbeItem 列表"
      ribbon="accent"
      aside={aside ?? defaultAside}
    >
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
