import { useId, type FormEvent } from 'react'

import { Button, Input, Modal } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'

type Props = {
  open: boolean
  monitoringInstance: MonitoringInstanceRecord
  groupDraft: string
  labelDraft: string
  noteDraft: string
  submitting: boolean
  error: string | null
  onGroupDraftChange: (value: string) => void
  onLabelDraftChange: (value: string) => void
  onNoteDraftChange: (value: string) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onClose: () => void
}

export function MonitoringDetailMetadataDialog({
  open,
  monitoringInstance,
  groupDraft,
  labelDraft,
  noteDraft,
  submitting,
  error,
  onGroupDraftChange,
  onLabelDraftChange,
  onNoteDraftChange,
  onSubmit,
  onClose,
}: Props) {
  const formId = useId()
  const noteId = useId()

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="分组、标签与备注"
      size="md"
      footer={
        <>
          <Button variant="secondary" disabled={submitting} onClick={onClose}>取消</Button>
          <Button variant="primary" type="submit" form={formId} disabled={submitting}>
            {submitting ? '正在保存…' : '保存'}
          </Button>
        </>
      }
    >
      <form id={formId} className="page-stack" onSubmit={onSubmit}>
        <p className="monitoring-detail-dialog__subject">{monitoringInstance.display_name}</p>
        <Input
          label="分组"
          name="metadata-group"
          value={groupDraft}
          onChange={(event) => onGroupDraftChange(event.target.value)}
        />
        <Input
          label="标签"
          name="metadata-labels"
          value={labelDraft}
          onChange={(event) => onLabelDraftChange(event.target.value)}
          hint="用逗号分隔多个标签"
        />
        <div className="input-field">
          <label className="input-field__label" htmlFor={noteId}>备注</label>
          <textarea
            id={noteId}
            className="input"
            name="metadata-note"
            rows={3}
            value={noteDraft}
            onChange={(event) => onNoteDraftChange(event.target.value)}
          />
        </div>
        {error ? (
          <p role="alert" aria-live="assertive" className="watchtower-property-item__error">{error}</p>
        ) : null}
      </form>
    </Modal>
  )
}