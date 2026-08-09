import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RecordAttachmentQueueItem } from './recordAttachments'
import componentSource from './AttachmentUploadQueue.tsx?raw'
import { AttachmentUploadQueue } from './AttachmentUploadQueue'

const assetsCSS = readFileSync(
  resolve(process.cwd(), 'src/styles/partials/legacy-assets.css'),
  'utf8',
)

const file = new File(['safe content'], 'incident-report-with-a-long-name.txt', {
  type: 'text/plain',
})

function item(
  clientId: string,
  status: RecordAttachmentQueueItem['status'],
): RecordAttachmentQueueItem {
  return {
    client_id: clientId,
    file,
    display_name: `${clientId}-${file.name}`,
    media_type: file.type,
    size_bytes: file.size,
    status,
    ...(status === 'failed' ? { error: '网络连接已中断' } : {}),
  }
}

describe('AttachmentUploadQueue', () => {
  it('emits file selection and controlled retry, cancel, and remove commands', () => {
    const onFilesSelected = vi.fn()
    const onRetry = vi.fn()
    const onCancel = vi.fn()
    const onRemove = vi.fn()
    render(
      <AttachmentUploadQueue
        items={[
          item('uploading', 'uploading'),
          item('failed', 'failed'),
          item('available', 'available'),
        ]}
        onFilesSelected={onFilesSelected}
        onRetry={onRetry}
        onCancel={onCancel}
        onRemove={onRemove}
      />,
    )

    fireEvent.change(screen.getByLabelText('选择附件'), { target: { files: [file] } })
    fireEvent.click(screen.getByRole('button', { name: '取消 uploading-incident-report-with-a-long-name.txt 上传' }))
    fireEvent.click(screen.getByRole('button', { name: '重试 failed-incident-report-with-a-long-name.txt' }))
    fireEvent.click(screen.getByRole('button', { name: '移除 failed-incident-report-with-a-long-name.txt 队列项' }))
    fireEvent.click(screen.getByRole('button', { name: '移除 available-incident-report-with-a-long-name.txt 队列项' }))

    expect(onFilesSelected).toHaveBeenCalledWith([file])
    expect(onCancel).toHaveBeenCalledWith('uploading')
    expect(onRetry).toHaveBeenCalledWith('failed')
    expect(onRemove).toHaveBeenNthCalledWith(1, 'failed')
    expect(onRemove).toHaveBeenNthCalledWith(2, 'available')
    expect(screen.getByText('网络连接已中断')).toHaveAttribute('role', 'alert')
  })

  it('keeps the primitive API-only and owns intrinsic 390px-safe wrapping rules', () => {
    expect(componentSource).not.toMatch(/\bfetch\s*\(/)
    expect(componentSource).toContain('asset-decision-save-member__summary')
    expect(componentSource).toContain('asset-decision-assessment')
    expect(assetsCSS).toMatch(/\.asset-decision-save-member__summary\s*\{[^}]*grid-template-columns:\s*minmax\([^)]*\)\s+minmax\([^)]*\)\s+auto[^}]*min-width:\s*0/s)
    expect(assetsCSS).toMatch(/\.asset-decision-assessment\s*\{[^}]*min-width:\s*0[^}]*max-width:\s*100%/s)
    expect(assetsCSS).toMatch(/@media\s*\(max-width:\s*920px\)[\s\S]*\.asset-decision-save-member__summary,[\s\S]*\{grid-template-columns:\s*1fr\}/s)
  })
})
