import { describe, expect, it } from 'vitest'

import type { AttachmentMetadata } from '../../../lib/types'
import { attachmentPreviewKind, attachmentPreviewURL, attachmentSummary, checkAttachmentFile, safeDownloadFilename } from './attachmentFiles'

function metadata(overrides: Partial<AttachmentMetadata>): AttachmentMetadata {
  return {
    attachment_id: 'att_1', state: 'available', display_name: 'a.txt', media_type: 'text/plain',
    size_bytes: 10, preview_available: true, ...overrides,
  }
}

describe('checkAttachmentFile', () => {
  it.each([
    ['trace.LOG', 'text/plain'],
    ['notes.md', 'text/markdown'],
    ['config.yml', 'application/yaml'],
    ['config.toml', 'text/plain'],
    ['fix.diff', 'text/x-diff'],
    ['shot.JPEG', 'image/jpeg'],
    ['bundle.zst', 'application/zstd'],
    ['report.pdf', 'application/pdf'],
  ])('declares the backend-accepted media type for %s', (name, mediaType) => {
    expect(checkAttachmentFile({ name, size: 1 })).toEqual({ ok: true, mediaType })
  })

  it('refuses files the backend would reject before reserving quota', () => {
    expect(checkAttachmentFile({ name: 'capture.pcap', size: 1 })).toEqual({ ok: false, reason: '不支持的文件类型' })
    expect(checkAttachmentFile({ name: 'noextension', size: 1 })).toEqual({ ok: false, reason: '不支持的文件类型' })
    expect(checkAttachmentFile({ name: 'empty.txt', size: 0 })).toEqual({ ok: false, reason: '文件为空' })
    expect(checkAttachmentFile({ name: 'huge.zip', size: 50 * 1024 * 1024 + 1 })).toEqual({ ok: false, reason: '超过 50 MiB 上限' })
    expect(checkAttachmentFile({ name: 'edge.zip', size: 50 * 1024 * 1024 })).toEqual({ ok: true, mediaType: 'application/zip' })
  })
})

describe('attachment presentation', () => {
  it('summarizes kind and size', () => {
    expect(attachmentSummary({ media_type: 'application/pdf', size_bytes: 2048 })).toMatch(/^PDF · /)
    expect(attachmentSummary({ media_type: 'application/gzip', size_bytes: 1 })).toMatch(/^压缩包 · /)
  })

  it('previews images and PDFs as images, text as text, and nothing without a safe preview', () => {
    expect(attachmentPreviewKind(metadata({ media_type: 'image/png' }))).toBe('image')
    expect(attachmentPreviewKind(metadata({ media_type: 'application/pdf' }))).toBe('image')
    expect(attachmentPreviewKind(metadata({ media_type: 'text/csv' }))).toBe('text')
    expect(attachmentPreviewKind(metadata({ preview_available: false }))).toBeNull()
    expect(attachmentPreviewKind(metadata({ state: 'quarantined' }))).toBeNull()
    expect(attachmentPreviewURL('att_a/b')).toBe('/api/attachments/att_a%2Fb/content?variant=preview')
  })
})

describe('safeDownloadFilename', () => {
  it('applies the same sanitizing rules as the server Content-Disposition', () => {
    expect(safeDownloadFilename('quarterly:report.txt')).toBe('quarterly_report.txt')
    expect(safeDownloadFilename('a/b\\c"d;e.txt')).toBe('a_b_c_d_e.txt')
    expect(safeDownloadFilename('notes.txt. ')).toBe('notes.txt')
    expect(safeDownloadFilename('tab\tname.log')).toBe('tab_name.log')
    expect(safeDownloadFilename(' .. ')).toBe('attachment')
    expect(safeDownloadFilename('报'.repeat(200) + '.txt')).toBe('报'.repeat(180))
  })
})
