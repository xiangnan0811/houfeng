import type { FormEvent, RefObject } from 'react'

import {
  TargetProbeForm,
  type ProbeCreateFormState,
  type ProbeFormMode,
} from '../../components/target-detail'
import type { ProbeKind } from '../../lib/types'

type TargetProbeManagementSectionProps = {
  addProbeButtonRef: RefObject<HTMLButtonElement | null>
  probeCreateOpen: boolean
  probeFormMode: ProbeFormMode
  probeCreateForm: ProbeCreateFormState
  probeCreateSubmitting: boolean
  probeCreateError: string | null
  probeMutationError: string | null
  addDisabled: boolean
  onToggleCreate: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onProbeKindChange: (probeKind: ProbeKind) => void
  onFieldChange: <K extends keyof ProbeCreateFormState>(
    field: K,
    value: ProbeCreateFormState[K],
  ) => void
}

export function TargetProbeManagementSection({
  addProbeButtonRef,
  probeCreateOpen,
  probeFormMode,
  probeCreateForm,
  probeCreateSubmitting,
  probeCreateError,
  probeMutationError,
  addDisabled,
  onToggleCreate,
  onSubmit,
  onProbeKindChange,
  onFieldChange,
}: TargetProbeManagementSectionProps) {
  return (
    <div>
      <button
        ref={addProbeButtonRef}
        type="button"
        disabled={addDisabled}
        onClick={onToggleCreate}
      >
        添加 ProbeItem
      </button>
      {probeMutationError ? <p>{probeMutationError}</p> : null}
      {probeCreateOpen ? (
        <TargetProbeForm
          mode={probeFormMode}
          form={probeCreateForm}
          submitting={probeCreateSubmitting}
          error={probeCreateError}
          onSubmit={onSubmit}
          onProbeKindChange={onProbeKindChange}
          onFieldChange={onFieldChange}
        />
      ) : null}
    </div>
  )
}
