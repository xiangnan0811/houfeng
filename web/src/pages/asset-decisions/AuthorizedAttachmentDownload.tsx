import { useEffect, useRef, useState } from 'react'

import { Button } from '../../components/atoms'
import { ApiError } from '../../lib/apiRequest'
import { getAttachmentContent } from '../../lib/recordsApi'
import type { AttachmentContentVariant, AttachmentMetadata } from '../../lib/types'

type AttachmentContentLoader = (
  attachmentId: string,
  variant: AttachmentContentVariant,
  signal: AbortSignal,
) => Promise<Blob>

type AuthorizedAttachmentDownloadProps = {
  attachment: AttachmentMetadata
  variant?: AttachmentContentVariant
  loadContent?: AttachmentContentLoader
}

type DownloadState = 'idle' | 'loading' | 'started' | 'error'

function deniedMessage(reason: unknown): string {
  if (reason instanceof ApiError && (reason.status === 403 || reason.status === 404)) {
    return '附件不可访问或授权已撤销'
  }
  return '附件下载失败，请重试'
}

export function AuthorizedAttachmentDownload({
  attachment,
  variant = 'original',
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

  const previewUnavailable = variant === 'preview' && !attachment.preview_available
  const unavailable = attachment.state !== 'available' || previewUnavailable
  const actionLabel = variant === 'preview' ? '下载安全预览' : '下载原文件'
  const buttonLabel = state === 'started' ? `再次${actionLabel}` : actionLabel

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
      const content = await loadContent(attachment.attachment_id, variant, request.signal)
      if (request.signal.aborted || !mountedRef.current) return
      const objectURL = URL.createObjectURL(content)
      objectURLRef.current = objectURL
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = attachment.display_name
      anchor.rel = 'noopener'
      anchor.click()
      if (mountedRef.current) setState('started')
    } catch (reason: unknown) {
      if (request.signal.aborted || !mountedRef.current) return
      setError(deniedMessage(reason))
      setState('error')
    } finally {
      if (requestRef.current === request) requestRef.current = null
    }
  }

  return (
    <div className="asset-decision-chip-row">
      <Button
        size="sm"
        variant="secondary"
        disabled={unavailable || state === 'loading'}
        aria-label={buttonLabel}
        onClick={() => { void startDownload() }}
      >
        {buttonLabel}
      </Button>
      {unavailable && (
        <span role="note">
          {previewUnavailable ? '安全预览尚不可用' : '附件尚不可下载'}
        </span>
      )}
      {state === 'loading' && (
        <span role="status">
          正在申请下载授权…
        </span>
      )}
      {state === 'started' && (
        <span role="status">
          下载已开始
        </span>
      )}
      {state === 'error' && error && (
        <span className="tone--critical" role="alert">
          {error}
        </span>
      )}
    </div>
  )
}
