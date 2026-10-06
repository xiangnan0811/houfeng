import { useEffect, useRef, useState } from 'react'

import { ApiError } from '../../../lib/apiRequest'
import { getAttachmentContent } from '../../../lib/recordsApi'
import type { AttachmentContentVariant, AttachmentMetadata } from '../../../lib/types'
import { safeDownloadFilename } from './attachmentFiles'

type AttachmentContentLoader = (
  attachmentId: string,
  variant: AttachmentContentVariant,
  signal: AbortSignal,
) => Promise<Blob>

type AuthorizedAttachmentDownloadProps = {
  attachment: AttachmentMetadata
  loadContent?: AttachmentContentLoader
}

type DownloadState = 'idle' | 'loading' | 'error'

function deniedMessage(reason: unknown): string {
  if (reason instanceof ApiError && (reason.status === 403 || reason.status === 404)) {
    return '附件不可访问或授权已撤销'
  }
  return '附件下载失败，请重试'
}

// 材料清单行内的下载链接：经授权读取原文件后以对象 URL 触发保存，不直接暴露内容地址。
export function AuthorizedAttachmentDownload({
  attachment,
  loadContent = getAttachmentContent,
}: AuthorizedAttachmentDownloadProps) {
  const [state, setState] = useState<DownloadState>('idle')
  const [error, setError] = useState<string | null>(null)
  const mountedRef = useRef(true)
  const requestRef = useRef<AbortController | null>(null)
  const objectURLRef = useRef<string | null>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      requestRef.current?.abort()
      requestRef.current = null
      if (objectURLRef.current) URL.revokeObjectURL(objectURLRef.current)
      objectURLRef.current = null
    }
  }, [])

  const unavailable = attachment.state !== 'available'

  async function startDownload(): Promise<void> {
    if (unavailable || state === 'loading') return
    requestRef.current?.abort()
    if (objectURLRef.current) URL.revokeObjectURL(objectURLRef.current)
    objectURLRef.current = null

    const request = new AbortController()
    requestRef.current = request
    setError(null)
    setState('loading')
    try {
      const content = await loadContent(attachment.attachment_id, 'original', request.signal)
      if (request.signal.aborted || !mountedRef.current) return
      const objectURL = URL.createObjectURL(content)
      objectURLRef.current = objectURL
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = safeDownloadFilename(attachment.display_name)
      anchor.rel = 'noopener'
      anchor.click()
      if (mountedRef.current) setState('idle')
    } catch (reason: unknown) {
      if (request.signal.aborted || !mountedRef.current) return
      setError(deniedMessage(reason))
      setState('error')
    } finally {
      if (requestRef.current === request) requestRef.current = null
    }
  }

  return (
    <>
      <button
        type="button"
        className="text-link"
        disabled={unavailable || state === 'loading'}
        aria-label={`下载${attachment.display_name}`}
        onClick={() => { void startDownload() }}
      >
        {state === 'loading' ? '下载中…' : '下载'}
      </button>
      {state === 'error' && error ? <span className="record-material__stale" role="alert">{error}</span> : null}
    </>
  )
}
