import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AttachmentMetadata } from '../../../lib/types'
import { AttachmentPreviewDialog } from './AttachmentPreview'

function metadata(overrides: Partial<AttachmentMetadata>): AttachmentMetadata {
  return {
    attachment_id: 'att_view', state: 'available', display_name: 'trace.txt', media_type: 'text/plain',
    size_bytes: 10, preview_available: true, ...overrides,
  }
}

describe('AttachmentPreviewDialog', () => {
  it('renders image and PDF previews from the same-origin preview URL', () => {
    const loadContent = vi.fn()
    render(<AttachmentPreviewDialog attachment={metadata({ display_name: 'report.pdf', media_type: 'application/pdf' })} onClose={vi.fn()} loadContent={loadContent} />)
    expect(screen.getByRole('img', { name: 'report.pdf 的安全预览' })).toHaveAttribute('src', '/api/attachments/att_view/content?variant=preview')
    expect(screen.getByText(/只预览第一页/)).toBeInTheDocument()
    expect(loadContent).not.toHaveBeenCalled()
  })

  it('shows a failure note when the preview image cannot load', () => {
    render(<AttachmentPreviewDialog attachment={metadata({ media_type: 'image/png' })} onClose={vi.fn()} />)
    fireEvent.error(screen.getByRole('img'))
    expect(screen.getByRole('alert')).toHaveTextContent('预览不可用或授权已撤销')
  })

  it('reads text previews as plain text and truncates very long ones', async () => {
    const long = 'x'.repeat(256 * 1024 + 5)
    const loadContent = vi.fn().mockResolvedValue(new Blob([long], { type: 'text/plain' }))
    render(<AttachmentPreviewDialog attachment={metadata({})} onClose={vi.fn()} loadContent={loadContent} />)
    expect(await screen.findByText('预览只显示开头部分，完整内容请下载原文件')).toBeInTheDocument()
    expect(loadContent).toHaveBeenCalledWith('att_view', expect.any(AbortSignal))
    expect(document.querySelector('pre')?.textContent).toHaveLength(256 * 1024)
  })

  it('stays closed for attachments without a safe preview', () => {
    render(<AttachmentPreviewDialog attachment={metadata({ preview_available: false })} onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
