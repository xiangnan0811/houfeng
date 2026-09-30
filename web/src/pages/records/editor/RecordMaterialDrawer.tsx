import { Button, Modal } from '../../../components/atoms'
import type { DocumentReference } from '../../../lib/documentMarkdown'
import { RecordMaterialList } from './RecordMaterialList'

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
      {items.length === 0 ? <p className="record-muted">当前修订没有可引用材料</p> : (
        <RecordMaterialList
          items={items}
          renderActions={(item) => (
            <>
              <Button size="sm" variant="secondary" disabled={readOnly || !item.available} onClick={() => onInsert(item)}
                aria-label={`插入${item.label}`}>
                插入引用
              </Button>
              <Button size="sm" variant="ghost" disabled={readOnly} onClick={() => onRemove(item)} aria-label={`移除${item.label}`}>
                移除
              </Button>
            </>
          )}
        />
      )}
    </Modal>
  )
}
