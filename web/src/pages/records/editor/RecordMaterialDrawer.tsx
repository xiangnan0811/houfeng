import { Button, Modal } from '../../../components/atoms'
import type { DocumentReference } from '../../../lib/documentMarkdown'

export type RecordMaterialItem = DocumentReference & {
  label: string
  available: boolean
}

type RecordMaterialDrawerProps = {
  open: boolean
  onClose: () => void
  items: readonly RecordMaterialItem[]
  readOnly?: boolean
  onInsert: (item: RecordMaterialItem) => void
  onRemove: (item: RecordMaterialItem) => void
}

export function RecordMaterialDrawer({
  open,
  onClose,
  items,
  readOnly = false,
  onInsert,
  onRemove,
}: RecordMaterialDrawerProps) {
  return (
    <Modal open={open} onClose={onClose} title="材料与引用" size="lg">
      {items.length === 0 ? <p className="text-muted">当前修订没有可引用材料</p> : (
        <ul className="action-confirm__callouts" aria-label="材料清单">
          {items.map((item) => (
            <li key={`${item.kind}:${item.id}`} className={item.available ? 'card' : 'card card--dim'}>
              <strong>{item.kind === 'evidence' ? '系统证据' : '用户附件'}</strong>
              <span>{item.label}</span>
              <code>{item.id}</code>
              {!item.available ? <span>引用已失效</span> : null}
              <div className="page-form-actions">
                <Button size="sm" variant="secondary" disabled={readOnly || !item.available} onClick={() => onInsert(item)}
                  aria-label={`插入${item.label}`}>
                  插入引用
                </Button>
                <Button size="sm" variant="ghost" disabled={readOnly} onClick={() => onRemove(item)} aria-label={`移除${item.label}`}>
                  从当前修订移除
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}
