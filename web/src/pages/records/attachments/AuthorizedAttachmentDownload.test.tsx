import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { AttachmentMetadata } from '../../../lib/types'
import componentSource from './AuthorizedAttachmentDownload.tsx?raw'
import { AuthorizedAttachmentDownload } from './AuthorizedAttachmentDownload'

const attachment = {
  attachment_id: 'att_download',
  state: 'available',
  display_name: 'incident.txt',
  media_type: 'text/plain',
  size_bytes: 12,
  preview_available: true,
} satisfies AttachmentMetadata

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AuthorizedAttachmentDownload', () => {
  it('downloads through an object URL and revokes replaced and unmounted URLs', async () => {
    const loadContent = vi.fn()
      .mockResolvedValueOnce(new Blob(['original one'], { type: 'text/plain' }))
      .mockResolvedValueOnce(new Blob(['original two'], { type: 'text/plain' }))
    const createObjectURL = vi.spyOn(URL, 'createObjectURL')
      .mockReturnValueOnce('blob:original-one')
      .mockReturnValueOnce('blob:original-two')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const { unmount } = render(
      <AuthorizedAttachmentDownload attachment={attachment} loadContent={loadContent} />,
    )

    fireEvent.click(screen.getByRole('button', { name: '下载incident.txt' }))
    await waitFor(() => expect(createObjectURL).toHaveBeenCalledTimes(1))
    expect(loadContent).toHaveBeenNthCalledWith(
      1,
      attachment.attachment_id,
      'original',
      expect.any(AbortSignal),
    )
    expect(anchorClick).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).not.toHaveBeenCalled()

    await waitFor(() => expect(screen.getByRole('button', { name: '下载incident.txt' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '下载incident.txt' }))
    await waitFor(() => expect(createObjectURL).toHaveBeenCalledTimes(2))
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:original-one')

    unmount()
    expect(revokeObjectURL).toHaveBeenLastCalledWith('blob:original-two')
  })

  it('shows the opaque revoked or denied result without creating an object URL', async () => {
    const loadContent = vi.fn().mockRejectedValue(new ApiError(404, 'resource not found', {
      code: 'resource_not_found',
    }))
    const createObjectURL = vi.spyOn(URL, 'createObjectURL')
    render(
      <AuthorizedAttachmentDownload attachment={attachment} loadContent={loadContent} />,
    )

    fireEvent.click(screen.getByRole('button', { name: '下载incident.txt' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('附件不可访问或授权已撤销')
    expect(createObjectURL).not.toHaveBeenCalled()
  })

  it('aborts a pending authorized read and suppresses object URL creation after unmount', async () => {
    let resolveContent: ((content: Blob) => void) | undefined
    let requestSignal: AbortSignal | undefined
    const loadContent = vi.fn((
      _attachmentId: string,
      _variant: 'original' | 'preview',
      signal: AbortSignal,
    ) => {
      requestSignal = signal
      return new Promise<Blob>((resolve) => {
        resolveContent = resolve
      })
    })
    const createObjectURL = vi.spyOn(URL, 'createObjectURL')
    const { unmount } = render(
      <AuthorizedAttachmentDownload attachment={attachment} loadContent={loadContent} />,
    )

    fireEvent.click(screen.getByRole('button', { name: '下载incident.txt' }))
    expect(await screen.findByText('下载中…')).toBeInTheDocument()
    unmount()
    expect(requestSignal?.aborted).toBe(true)
    resolveContent?.(new Blob(['late content']))
    await Promise.resolve()
    await Promise.resolve()

    expect(createObjectURL).not.toHaveBeenCalled()
    expect(componentSource).not.toMatch(/\bfetch\s*\(/)
  })
})
