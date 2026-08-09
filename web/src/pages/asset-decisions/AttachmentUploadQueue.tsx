import { Badge, Button } from '../../components/atoms'
import type { BadgeTone } from '../../components/atoms/Badge'
import { formatBytes } from '../../lib/format'
import type {
  RecordAttachmentQueueItem,
  RecordAttachmentQueueStatus,
} from './recordAttachments'

type AttachmentUploadQueueProps = {
  items: readonly RecordAttachmentQueueItem[]
  disabled?: boolean
  onFilesSelected: (files: readonly File[]) => void
  onRetry: (clientId: string) => void
  onCancel: (clientId: string) => void
  onRemove: (clientId: string) => void
}

const statusLabels: Record<RecordAttachmentQueueStatus, string> = {
  queued: '等待上传',
  hashing: '正在校验',
  creating: '正在建立会话',
  uploading: '正在上传',
  processing: '安全处理中',
  available: '可以引用',
  rejected: '已拒绝',
  expired: '已过期',
  failed: '上传失败',
  cancelled: '已取消',
}

const statusTones: Record<RecordAttachmentQueueStatus, BadgeTone> = {
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

function active(status: RecordAttachmentQueueStatus): boolean {
  return ['queued', 'hashing', 'creating', 'uploading', 'processing'].includes(status)
}

function retryable(status: RecordAttachmentQueueStatus): boolean {
  return ['failed', 'cancelled', 'expired'].includes(status)
}

export function AttachmentUploadQueue({
  items,
  disabled = false,
  onFilesSelected,
  onRetry,
  onCancel,
  onRemove,
}: AttachmentUploadQueueProps) {
  return (
    <section className="asset-decision-save-members" aria-label="附件上传队列">
      <label className="input-field asset-decision-assessment">
        <span>选择附件</span>
        <input
          className="input"
          type="file"
          multiple
          disabled={disabled}
          aria-label="选择附件"
          onChange={(event) => {
            const files = event.currentTarget.files
              ? Array.from(event.currentTarget.files)
              : []
            if (files.length > 0) onFilesSelected(files)
            event.currentTarget.value = ''
          }}
        />
      </label>

      {items.length === 0 ? (
        <p className="empty-state">尚未选择附件</p>
      ) : (
        <ul className="asset-decision-save-members">
          {items.map((item) => (
            <li className="asset-decision-save-member" key={item.client_id}>
              <div className="asset-decision-save-member__summary">
                <div className="asset-decision-assessment">
                  <strong>{item.display_name}</strong>
                  {item.error && (
                    <span className="tone--critical" role="alert">
                      {item.error}
                    </span>
                  )}
                </div>
                <span className="asset-decision-chip-row">
                  <span className="mono">{formatBytes(item.size_bytes)}</span>
                  <Badge variant="state" tone={statusTones[item.status]} withDot>
                    {statusLabels[item.status]}
                  </Badge>
                </span>
                <div className="asset-decision-member-row__actions">
                  {active(item.status) && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => onCancel(item.client_id)}
                      aria-label={`取消 ${item.display_name} 上传`}
                    >
                      取消
                    </Button>
                  )}
                  {retryable(item.status) && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => onRetry(item.client_id)}
                      aria-label={`重试 ${item.display_name}`}
                    >
                      重试
                    </Button>
                  )}
                  {!active(item.status) && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onRemove(item.client_id)}
                      aria-label={`移除 ${item.display_name} 队列项`}
                    >
                      移除
                    </Button>
                  )}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
