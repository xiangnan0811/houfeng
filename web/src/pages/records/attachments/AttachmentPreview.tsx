import { useEffect, useState } from 'react'

import { Modal } from '../../../components/atoms'
import { getAttachmentContent } from '../../../lib/recordsApi'
import type { AttachmentMetadata } from '../../../lib/types'
import { attachmentPreviewKind, attachmentPreviewURL, attachmentSummary } from './attachmentFiles'

// 文本预览最多显示的字符数；后端单份文本预览上限 5 MiB，全部塞进 DOM 会拖慢页面。
const TEXT_PREVIEW_LIMIT = 256 * 1024

type TextPreviewState =
  | { status: 'loading' }
  | { status: 'ready'; text: string; truncated: boolean }
  | { status: 'error' }

type TextLoader = (attachmentId: string, signal: AbortSignal) => Promise<Blob>

function TextPreview({ attachmentId, loadContent }: { attachmentId: string; loadContent: TextLoader }) {
  const [state, setState] = useState<TextPreviewState>({ status: 'loading' })
  useEffect(() => {
    const controller = new AbortController()
    Promise.resolve()
      .then(() => loadContent(attachmentId, controller.signal))
      .then((blob) => blob.text())
      .then(
        (text) => {
          if (controller.signal.aborted) return
          setState({ status: 'ready', text: text.slice(0, TEXT_PREVIEW_LIMIT), truncated: text.length > TEXT_PREVIEW_LIMIT })
        },
        () => {
          if (!controller.signal.aborted) setState({ status: 'error' })
        },
      )
    return () => controller.abort()
  }, [attachmentId, loadContent])

  if (state.status === 'loading') return <p className="record-muted" role="status">正在读取预览</p>
  if (state.status === 'error') return <p className="record-material__stale" role="alert">预览不可用或授权已撤销</p>
  return (
    <>
      <pre className="record-attachment-preview__text">{state.text}</pre>
      {state.truncated ? <p className="record-muted">预览只显示开头部分，完整内容请下载原文件</p> : null}
    </>
  )
}

function ImagePreview({ attachment }: { attachment: AttachmentMetadata }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <p className="record-material__stale" role="alert">预览不可用或授权已撤销</p>
  return (
    <img
      className="record-attachment-preview__image"
      src={attachmentPreviewURL(attachment.attachment_id)}
      alt={`${attachment.display_name} 的安全预览`}
      onError={() => setFailed(true)}
    />
  )
}

const defaultTextLoader: TextLoader = (attachmentId, signal) => getAttachmentContent(attachmentId, 'preview', signal)

export function AttachmentPreviewDialog({
  attachment,
  onClose,
  loadContent = defaultTextLoader,
}: {
  attachment: AttachmentMetadata | null
  onClose: () => void
  loadContent?: TextLoader
}) {
  const kind = attachment ? attachmentPreviewKind(attachment) : null
  return (
    <Modal open={Boolean(attachment && kind)} onClose={onClose} title={attachment?.display_name ?? '附件预览'} size="lg">
      {attachment && kind ? (
        <div className="record-attachment-preview">
          <p className="record-muted">
            {attachmentSummary(attachment)}
            {attachment.media_type === 'application/pdf' ? ' · 只预览第一页' : ''}
          </p>
          {kind === 'image'
            ? <ImagePreview key={attachment.attachment_id} attachment={attachment} />
            : <TextPreview key={attachment.attachment_id} attachmentId={attachment.attachment_id} loadContent={loadContent} />}
        </div>
      ) : null}
    </Modal>
  )
}
