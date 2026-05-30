import type { FormEvent } from 'react'

import { Modal } from '../../components/atoms'
import {
  TargetProbeForm,
  type ProbeCreateFormState,
  type ProbeFormMode,
} from '../../components/target-detail'
import type { ProbeKind, TargetRecord } from '../../lib/types'

type TargetProbeFormDrawerProps = {
  target: TargetRecord
  open: boolean
  mode: ProbeFormMode
  form: ProbeCreateFormState
  submitting: boolean
  error: string | null
  onClose: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onProbeKindChange: (probeKind: ProbeKind) => void
  onFieldChange: <K extends keyof ProbeCreateFormState>(
    field: K,
    value: ProbeCreateFormState[K],
  ) => void
}

export function TargetProbeFormDrawer({
  target,
  open,
  mode,
  form,
  submitting,
  error,
  onClose,
  onSubmit,
  onProbeKindChange,
  onFieldChange,
}: TargetProbeFormDrawerProps) {
  const title = mode.kind === 'edit'
    ? `${target.name} · 编辑 ProbeItem`
    : `${target.name} · 创建 ProbeItem`

  function handleClose() {
    if (submitting) return
    onClose()
  }

  return (
    <Modal open={open} onClose={handleClose} title={title} ariaLabel="ProbeItem 表单抽屉">
      <div className="target-probe-drawer">
        <div className="target-probe-drawer__intro">
          <div>
            <p className="target-probe-drawer__eyebrow">ProbeItem 工作面</p>
            <p className="target-probe-drawer__description">
              创建和编辑探测规则在抽屉内完成，主页面保留 ProbeItem 证据扫描路径。
            </p>
          </div>
        </div>

        <TargetProbeForm
          mode={mode}
          form={form}
          submitting={submitting}
          error={error}
          onSubmit={onSubmit}
          onProbeKindChange={onProbeKindChange}
          onFieldChange={onFieldChange}
        />
      </div>
    </Modal>
  )
}
