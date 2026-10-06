import { getAttachmentMetadata } from '../../../lib/recordsApi'
import type { AttachmentMetadata } from '../../../lib/types'
import { useIdLookup, type IdLookupLoader, type IdLookupState } from '../hooks/useIdLookup'

export type AttachmentMetadataState = IdLookupState<AttachmentMetadata>

/** 逐个读取附件元数据（文件名、类型、大小、状态）并按 ID 缓存。 */
export function useAttachmentMetadata(
  attachmentIds: readonly string[],
  loadMetadata: IdLookupLoader<AttachmentMetadata> = getAttachmentMetadata,
): ReadonlyMap<string, AttachmentMetadataState> {
  return useIdLookup(attachmentIds, loadMetadata)
}
