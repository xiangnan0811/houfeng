import { formatBytes } from '../../../lib/format'
import type { AttachmentMetadata } from '../../../lib/types'

// 与后端准入（internal/center/attachments/admission.go）的扩展名—类型配对保持一致。
// 浏览器给出的 file.type 对日志、补丁等常为空或不一致，统一按扩展名发送后端接受的类型，
// 避免上传完成时才被拒绝、白占配额。
const MEDIA_TYPES_BY_EXTENSION: Readonly<Record<string, string>> = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.pdf': 'application/pdf',
  '.zip': 'application/zip',
  '.tar': 'application/x-tar',
  '.gz': 'application/gzip',
  '.zst': 'application/zstd',
  '.zstd': 'application/zstd',
  '.txt': 'text/plain',
  '.log': 'text/plain',
  '.ini': 'text/plain',
  '.toml': 'text/plain',
  '.md': 'text/markdown',
  '.markdown': 'text/markdown',
  '.json': 'application/json',
  '.yaml': 'application/yaml',
  '.yml': 'application/yaml',
  '.csv': 'text/csv',
  '.tsv': 'text/tab-separated-values',
  '.patch': 'text/x-patch',
  '.diff': 'text/x-diff',
}

export const ATTACHMENT_MAX_BYTES = 50 * 1024 * 1024

export const ATTACHMENT_ACCEPT = Object.keys(MEDIA_TYPES_BY_EXTENSION).join(',')

export const ATTACHMENT_FORMAT_HINT = '图片、PDF、文本与日志、压缩包，单个不超过 50 MiB'

export type AttachmentFileCheck =
  | { ok: true; mediaType: string }
  | { ok: false; reason: string }

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot).toLowerCase() : ''
}

export function checkAttachmentFile(file: Pick<File, 'name' | 'size'>): AttachmentFileCheck {
  const mediaType = MEDIA_TYPES_BY_EXTENSION[extensionOf(file.name)]
  if (!mediaType) return { ok: false, reason: '不支持的文件类型' }
  if (file.size <= 0) return { ok: false, reason: '文件为空' }
  if (file.size > ATTACHMENT_MAX_BYTES) return { ok: false, reason: '超过 50 MiB 上限' }
  return { ok: true, mediaType }
}

export function attachmentKindLabel(mediaType: string): string {
  if (mediaType.startsWith('image/')) return '图片'
  if (mediaType === 'application/pdf') return 'PDF'
  if (['application/zip', 'application/x-tar', 'application/gzip', 'application/zstd'].includes(mediaType)) return '压缩包'
  if (mediaType.startsWith('text/') || mediaType === 'application/json' || mediaType === 'application/yaml') return '文本'
  return '文件'
}

export function attachmentSummary(file: { media_type: string; size_bytes: number }): string {
  return `${attachmentKindLabel(file.media_type)} · ${formatBytes(file.size_bytes)}`
}

export type AttachmentPreviewKind = 'image' | 'text'

// 图片与 PDF 的安全预览都是后端重新渲染的 PNG，文本类是纯文本；压缩包没有预览。
export function attachmentPreviewKind(attachment: AttachmentMetadata): AttachmentPreviewKind | null {
  if (attachment.state !== 'available' || !attachment.preview_available) return null
  if (attachment.media_type.startsWith('image/') || attachment.media_type === 'application/pdf') return 'image'
  return 'text'
}

// 站点 CSP 只允许同源图片（img-src 'self'，不含 blob:），图片预览直接引用同源内容地址。
export function attachmentPreviewURL(attachmentId: string): string {
  return `/api/attachments/${encodeURIComponent(attachmentId)}/content?variant=preview`
}

// 与后端 sanitizeContentFilename（internal/center/attachments/download.go）同一套规则：
// 最多 180 个字符，控制字符与 / \ " ; : 换成 _，去掉首尾空格和点，空名回退为 attachment。
export function safeDownloadFilename(displayName: string): string {
  let name = ''
  let count = 0
  for (const character of displayName) {
    if (count >= 180) break
    count += 1
    name += /\p{Cc}/u.test(character) || '/\\";:'.includes(character) ? '_' : character
  }
  name = name.replace(/^[ .]+|[ .]+$/gu, '')
  return name === '' || name === '.' || name === '..' ? 'attachment' : name
}
