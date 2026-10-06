import { useRef, useState, type DragEvent } from 'react'

import { Badge, Button, Modal } from '../../../components/atoms'
import type { BadgeTone } from '../../../components/atoms/Badge'
import type { DocumentReference } from '../../../lib/documentMarkdown'
import { formatBytes } from '../../../lib/format'
import type { AttachmentMetadata } from '../../../lib/types'
import { ATTACHMENT_ACCEPT, ATTACHMENT_FORMAT_HINT } from '../attachments/attachmentFiles'
import type { RecordAttachmentQueueStatus } from '../attachments/recordAttachments'
import { isActiveUpload, type RecordAttachmentUploadRow } from '../attachments/useRecordAttachmentUploads'
import { RecordMaterialList } from './RecordMaterialList'

export type RecordMaterialItem = DocumentReference & {
  label: string
  available: boolean
  /** 附件元数据；证据或尚未读到时为空。 */
  attachment?: AttachmentMetadata
  /** 元数据仍在读取。 */
  pending?: boolean
}

export type RecordMaterialUploads = {
  rows: readonly RecordAttachmentUploadRow[]
  notice: string
  /** 发布进行中时不接受新文件：上传会挂到正被发布消费的草稿上。 */
  disabled?: boolean
  onFiles: (files: readonly File[]) => void
  onRetry: (clientId: string) => void
  onCancel: (clientId: string) => void
  onRemove: (clientId: string) => void
}

type RecordMaterialDrawerProps = {
  open: boolean
  onClose: () => void
  items: readonly RecordMaterialItem[]
  readOnly?: boolean
  uploads?: RecordMaterialUploads | undefined
  onInsert: (item: RecordMaterialItem) => void
  onRemove: (item: RecordMaterialItem) => void
}

const uploadLabels: Record<RecordAttachmentQueueStatus, string> = {
  queued: '等待上传',
  hashing: '正在校验',
  creating: '正在上传',
  uploading: '正在上传',
  processing: '安全检查中',
  available: '可以引用',
  rejected: '未通过',
  expired: '已过期',
  failed: '上传失败',
  cancelled: '已取消',
}

const uploadTones: Record<RecordAttachmentQueueStatus, BadgeTone> = {
  queued: 'neutral',
  hashing: 'notice',
  creating: 'notice',
  uploading: 'notice',
  processing: 'maintenance',
  available: 'normal',
  rejected: 'critical',
  expired: 'offline',
  failed: 'critical',
  cancelled: 'offline',
}

function retryable(status: RecordAttachmentQueueStatus): boolean {
  return status === 'failed' || status === 'cancelled' || status === 'expired'
}

function AttachmentUploadArea({ uploads }: { uploads: RecordMaterialUploads }) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    if (uploads.disabled) return
    const files = Array.from(event.dataTransfer.files)
    if (files.length > 0) uploads.onFiles(files)
  }
  return (
    <section className="record-upload" aria-label="上传附件">
      <div
        className={dragging ? 'record-upload__drop record-upload__drop--active' : 'record-upload__drop'}
        onDragOver={(event) => { event.preventDefault(); setDragging(true) }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
      >
        <Button size="sm" variant="secondary" disabled={uploads.disabled} onClick={() => inputRef.current?.click()}>选择文件</Button>
        <span className="record-upload__hint">或拖入此处 · {ATTACHMENT_FORMAT_HINT}</span>
        <input
          ref={inputRef}
          type="file"
          multiple
          hidden
          accept={ATTACHMENT_ACCEPT}
          aria-label="选择附件文件"
          onChange={(event) => {
            const files = event.currentTarget.files ? Array.from(event.currentTarget.files) : []
            if (files.length > 0 && !uploads.disabled) uploads.onFiles(files)
            event.currentTarget.value = ''
          }}
        />
      </div>
      {uploads.notice ? <p className="record-upload__notice" role="alert">{uploads.notice}</p> : null}
      {uploads.rows.length > 0 ? (
        <ul className="record-materials" aria-label="上传队列">
          {uploads.rows.map((row) => (
            <li key={row.client_id} className="record-material">
              <span className="record-material__kind">上传</span>
              <span className="record-material__name">
                <span className="record-material__label">{row.display_name}</span>
                <span className="record-material__meta">{formatBytes(row.size_bytes)}</span>
                {row.error ? <span className="record-material__stale">{row.error}</span> : null}
              </span>
              <span className="record-material__actions">
                <Badge variant="state" tone={uploadTones[row.status]} withDot>{uploadLabels[row.status]}</Badge>
                {isActiveUpload(row.status) ? (
                  <Button size="sm" variant="ghost" onClick={() => uploads.onCancel(row.client_id)} aria-label={`取消上传${row.display_name}`}>
                    取消
                  </Button>
                ) : null}
                {retryable(row.status) ? (
                  <Button size="sm" variant="secondary" disabled={uploads.disabled} onClick={() => uploads.onRetry(row.client_id)} aria-label={`重试上传${row.display_name}`}>
                    重试
                  </Button>
                ) : null}
                {!isActiveUpload(row.status) ? (
                  <Button size="sm" variant="ghost" onClick={() => uploads.onRemove(row.client_id)} aria-label={`移除上传${row.display_name}`}>
                    移除
                  </Button>
                ) : null}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  )
}

export function RecordMaterialDrawer({
  open,
  onClose,
  items,
  readOnly = false,
  uploads,
  onInsert,
  onRemove,
}: RecordMaterialDrawerProps) {
  return (
    <Modal open={open} onClose={onClose} title="材料与引用" size="lg">
      <div className="record-material-drawer">
        {!readOnly && uploads ? <AttachmentUploadArea uploads={uploads} /> : null}
        {items.length === 0 ? <p className="record-muted">当前修订没有可引用材料</p> : (
          <RecordMaterialList
            items={items}
            renderActions={(item) => (
              <>
                <Button size="sm" variant="secondary" disabled={readOnly || !item.available} onClick={() => onInsert(item)}
                  aria-label={`插入${item.attachment?.display_name ?? item.label}`}>
                  插入引用
                </Button>
                <Button size="sm" variant="ghost" disabled={readOnly} onClick={() => onRemove(item)}
                  aria-label={`移除${item.attachment?.display_name ?? item.label}`}>
                  移除
                </Button>
              </>
            )}
          />
        )}
      </div>
    </Modal>
  )
}
