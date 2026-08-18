import { useState } from 'react'

import { Button, Modal } from '../../../components/atoms'
import { extractTaskItems } from '../../../lib/documentMarkdown'

type PromoteChecklistActionDialogProps = {
  open: boolean
  source: string
  busy?: boolean
  onClose: () => void
  onConfirm: (values: { title: string; details: string }) => void
}

export function PromoteChecklistActionDialog({
  open,
  source,
  busy = false,
  onClose,
  onConfirm,
}: PromoteChecklistActionDialogProps) {
  const items = extractTaskItems(source)
  const [selected, setSelected] = useState(0)
  const [previewed, setPreviewed] = useState(false)
  const current = items[selected]
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="提升为行动项"
      size="md"
      dialogRole="alertdialog"
      footer={
        <div className="page-form-actions">
          <Button variant="ghost" onClick={onClose}>取消</Button>
          {current && !previewed ? (
            <Button variant="secondary" onClick={() => setPreviewed(true)}>预览行动项</Button>
          ) : null}
          {current && previewed ? (
            <Button
              disabled={busy}
              onClick={() => onConfirm({ title: current.text, details: current.text })}
            >
              确认创建行动项
            </Button>
          ) : null}
        </div>
      }
    >
      {items.length === 0 ? <p>当前正文没有可提升的勾选条目。</p> : (
        <>
          <fieldset className="action-confirm__callouts">
            <legend>选择勾选条目</legend>
            {items.map((item, index) => (
              <label key={`${item.text}-${index}`}>
                <input
                  type="radio"
                  name="promote-item"
                  checked={selected === index}
                  onChange={() => {
                    setSelected(index)
                    setPreviewed(false)
                  }}
                />
                <span>{item.text}</span>
              </label>
            ))}
          </fieldset>
          {previewed && current ? (
            <p className="inline-alert info" role="status">
              将创建标题为“{current.text}”的行动项。确认后不会改写正文，也不会勾选或删除该条目。
            </p>
          ) : (
            <p className="text-muted">提升前必须预览并确认，正文保持不变。</p>
          )}
        </>
      )}
    </Modal>
  )
}
