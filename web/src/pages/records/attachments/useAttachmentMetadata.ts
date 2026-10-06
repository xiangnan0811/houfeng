import { useEffect, useRef, useState } from 'react'

import { getAttachmentMetadata } from '../../../lib/recordsApi'
import type { AttachmentMetadata } from '../../../lib/types'

export type AttachmentMetadataState =
  | { status: 'loading' }
  | { status: 'ready'; metadata: AttachmentMetadata }
  | { status: 'unavailable' }

type MetadataLoader = (attachmentId: string, signal: AbortSignal) => Promise<AttachmentMetadata>

// 记录与修订只给出 attachment_ids，没有批量接口：逐个读取元数据并按 ID 缓存。
// 读不到（无权、已删除、服务异常）一律显示为不可用，不区分原因，避免泄露存在性。
export function useAttachmentMetadata(
  attachmentIds: readonly string[],
  loadMetadata: MetadataLoader = getAttachmentMetadata,
): ReadonlyMap<string, AttachmentMetadataState> {
  const [entries, setEntries] = useState<ReadonlyMap<string, AttachmentMetadataState>>(() => new Map())
  // 已发起或已有结果的 ID；中断的读取会移出，交给下一次 effect 重新读取。
  const requestedRef = useRef(new Set<string>())
  const key = [...new Set(attachmentIds)].sort().join('\n')

  useEffect(() => {
    const requested = requestedRef.current
    const missing = (key ? key.split('\n') : []).filter((id) => !requested.has(id))
    if (missing.length === 0) return
    const controller = new AbortController()
    for (const id of missing) requested.add(id)
    setEntries((current) => {
      const next = new Map(current)
      for (const id of missing) next.set(id, { status: 'loading' })
      return next
    })
    const settled = new Set<string>()
    for (const id of missing) {
      // 先进入 Promise 链：加载函数同步抛错或返回非 Promise 时也只记为不可用。
      Promise.resolve().then(() => loadMetadata(id, controller.signal)).then(
        (metadata): AttachmentMetadataState => ({ status: 'ready', metadata }),
        (): AttachmentMetadataState => ({ status: 'unavailable' }),
      ).then((state) => {
        if (controller.signal.aborted) return
        settled.add(id)
        setEntries((current) => new Map(current).set(id, state))
      })
    }
    return () => {
      controller.abort()
      const pending = missing.filter((id) => !settled.has(id))
      for (const id of pending) requested.delete(id)
      if (pending.length > 0) {
        setEntries((current) => {
          const next = new Map(current)
          for (const id of pending) next.delete(id)
          return next
        })
      }
    }
  }, [key, loadMetadata])

  return entries
}
