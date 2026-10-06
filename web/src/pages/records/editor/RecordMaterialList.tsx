import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { attachmentSummary } from '../attachments/attachmentFiles'
import type { RecordMaterialItem } from './RecordMaterialDrawer'

type RecordMaterialListProps = {
  items: readonly RecordMaterialItem[]
  /** 每一行末尾的附加命令（例如插入 / 移除）。 */
  renderActions?: (item: RecordMaterialItem) => ReactNode
}

// 默认标签就是"证据 <id>"/"附件 <id>"，与类型 + ID 重复时不再单独显示。
function customLabel(item: RecordMaterialItem): string {
  return item.label.includes(item.id) ? '' : item.label
}

function MaterialName({ item }: { item: RecordMaterialItem }) {
  if (item.attachment) {
    return (
      <>
        <span className="record-material__label">{item.attachment.display_name}</span>
        <span className="record-material__meta">{attachmentSummary(item.attachment)}</span>
      </>
    )
  }
  // 附件的原始 ID 不给用户看：元数据未到或读不到时只显示类别标签。
  if (item.kind === 'attachment') return <span className="record-material__label">{item.label}</span>
  const label = customLabel(item)
  return (
    <>
      {label ? <span className="record-material__label">{label}</span> : null}
      <code className="record-material__id">{item.id}</code>
    </>
  )
}

export function RecordMaterialList({ items, renderActions }: RecordMaterialListProps) {
  const { state } = useLocation()
  return (
    <ul className="record-materials" aria-label="材料清单">
      {items.map((item) => (
        <li key={`${item.kind}:${item.id}`} className={item.available ? 'record-material' : 'record-material record-material--stale'}>
          <span className="record-material__kind">{item.kind === 'evidence' ? '系统证据' : '用户附件'}</span>
          <span className="record-material__name">
            <MaterialName item={item} />
            {item.pending ? <span className="record-material__meta">读取中</span> : null}
            {!item.available && !item.pending ? <span className="record-material__stale">引用已失效</span> : null}
          </span>
          <span className="record-material__actions">
            {item.kind === 'evidence' ? (
              <Link className="text-link" to={`/evidence/${encodeURIComponent(item.id)}`} state={state}>
                查看证据
              </Link>
            ) : null}
            {renderActions?.(item)}
          </span>
        </li>
      ))}
    </ul>
  )
}
