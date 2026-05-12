import type { FormEvent, RefObject } from 'react'
import { Button } from '../../components/atoms/Button'

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
    <div className="watchtower-property-item" style={{ flexDirection: probeCreateOpen ? 'column' : 'row', alignItems: probeCreateOpen ? 'stretch' : 'center' }}>
      <div className="watchtower-property-item__main">
        <span className="watchtower-property-item__title">探测项目配置</span>
        <span className="watchtower-property-item__desc">配置目标的主机存活与应用探测规则。</span>
        {probeMutationError ? <span role="alert" style={{ color: 'var(--color-state-critical)' }}>{probeMutationError}</span> : null}
      </div>

      <div className="watchtower-property-item__actions">
        <Button
          ref={addProbeButtonRef}
          variant="secondary"
          disabled={addDisabled || probeCreateOpen}
          onClick={onToggleCreate}
        >
          添加 ProbeItem
        </Button>
      </div>

      {probeCreateOpen ? (
        <div style={{ marginTop: 'var(--space-4)', width: '100%' }}>
          <TargetProbeForm
            mode={probeFormMode}
            form={probeCreateForm}
            submitting={probeCreateSubmitting}
            error={probeCreateError}
            onSubmit={onSubmit}
            onProbeKindChange={onProbeKindChange}
            onFieldChange={onFieldChange}
          />
        </div>
      ) : null}
    </div>
  )
}
