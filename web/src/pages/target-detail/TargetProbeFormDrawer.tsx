import { type FormEvent, useId } from 'react'

import { Button, Modal } from '../../components/atoms'
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
  const formId = useId()
  const title = mode.kind === 'edit'
    ? `${target.name} · 编辑 ProbeItem`
    : `${target.name} · 创建 ProbeItem`
  const submitLabel = submitting
    ? mode.kind === 'edit'
      ? '正在保存…'
      : '正在创建…'
    : mode.kind === 'edit'
      ? '保存 ProbeItem'
      : '创建 ProbeItem'

  function handleClose() {
    if (submitting) return
    onClose()
  }

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={title}
      ariaLabel="ProbeItem 表单抽屉"
      size="md"
      contentClassName="watchtower-form-modal"
      footer={
        <div className="watchtower-form-footer">
          {error ? <p className="create-form__error" role="alert">{error}</p> : null}
          <Button type="submit" form={formId} disabled={submitting}>
            {submitLabel}
          </Button>
        </div>
      }
    >
      <TargetProbeForm
        formId={formId}
        hideActions
        mode={mode}
        form={form}
        submitting={submitting}
        error={null}
        onSubmit={onSubmit}
        onProbeKindChange={onProbeKindChange}
        onFieldChange={onFieldChange}
      />
    </Modal>
  )
}
