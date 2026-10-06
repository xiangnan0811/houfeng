import { useCallback, useEffect, useRef, useState } from 'react'

import type { RecordDraft } from '../../../lib/types'
import { checkAttachmentFile } from './attachmentFiles'
import {
  createRecordAttachmentQueueController,
  type RecordAttachmentQueueController,
  type RecordAttachmentQueueItem,
  type RecordAttachmentQueueStatus,
} from './recordAttachments'

export type RecordAttachmentUploadRow = Readonly<{
  client_id: string
  display_name: string
  size_bytes: number
  status: RecordAttachmentQueueStatus
  error?: string
}>

const ACTIVE: ReadonlySet<RecordAttachmentQueueStatus> = new Set(['queued', 'hashing', 'creating', 'uploading', 'processing'])

export function isActiveUpload(status: RecordAttachmentQueueStatus): boolean {
  return ACTIVE.has(status)
}

type Options = Readonly<{
  /** 上传必须挂在草稿下：没有草稿时先保存一次拿到草稿 ID；冲突或发布进行中时返回 undefined。 */
  ensureDraft: () => Promise<RecordDraft | undefined>
  /** 发布进行中返回真：此时不开始任何上传或重试，草稿正被发布消费。 */
  isPublishing: () => boolean
  /** 附件通过安全处理、可以引用时加入草稿的 attachment_ids。 */
  onAvailable: (attachmentId: string) => void
}>

export function useRecordAttachmentUploads({ ensureDraft, isPublishing, onAvailable }: Options) {
  const [queued, setQueued] = useState<readonly RecordAttachmentQueueItem[]>([])
  const [refused, setRefused] = useState<readonly RecordAttachmentUploadRow[]>([])
  const [notice, setNotice] = useState('')
  // 已接受、正在等草稿保存的文件数：这段时间还没入队，但发布必须已经被挡住。
  const [preparing, setPreparing] = useState(0)
  const preparingRef = useRef(0)
  const controllerRef = useRef<{ draftId: string; controller: RecordAttachmentQueueController } | null>(null)
  const onAvailableRef = useRef(onAvailable)
  const refusedSeqRef = useRef(0)
  const mountedRef = useRef(true)

  useEffect(() => {
    onAvailableRef.current = onAvailable
  }, [onAvailable])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      controllerRef.current?.controller.dispose()
      controllerRef.current = null
    }
  }, [])

  const handleChange = useCallback((items: readonly RecordAttachmentQueueItem[]) => {
    const controller = controllerRef.current?.controller
    for (const item of items) {
      if (item.status !== 'available' || !item.attachment_id) continue
      onAvailableRef.current(item.attachment_id)
      // 已进入草稿的附件改由材料清单展示，队列里只留未完成或失败的项。
      queueMicrotask(() => controller?.remove(item.client_id))
    }
    setQueued(items)
  }, [])

  const addFiles = useCallback(async (files: readonly File[]) => {
    setNotice('')
    const accepted: File[] = []
    const rejected: RecordAttachmentUploadRow[] = []
    for (const file of files) {
      const check = checkAttachmentFile(file)
      if (check.ok) {
        accepted.push(file)
      } else {
        refusedSeqRef.current += 1
        rejected.push({
          client_id: `refused_${refusedSeqRef.current}`,
          display_name: file.name,
          size_bytes: file.size,
          status: 'rejected',
          error: check.reason,
        })
      }
    }
    if (rejected.length > 0) setRefused((current) => [...current, ...rejected])
    if (accepted.length === 0) return
    if (isPublishing()) {
      setNotice('正在发布，完成后再上传附件')
      return
    }
    preparingRef.current += accepted.length
    setPreparing(preparingRef.current)
    try {
      const draft = await ensureDraft()
      // 等草稿期间离开了工作区：不再建队列，否则上传会脱离清理继续跑。
      if (!mountedRef.current) return
      if (!draft) {
        setNotice('草稿暂不可保存，附件未上传')
        return
      }
      // 草稿换了（例如上一份已随发布消费）：旧队列作废，在当前草稿下重新建。
      if (controllerRef.current?.draftId !== draft.draft_id) {
        controllerRef.current?.controller.dispose()
        controllerRef.current = {
          draftId: draft.draft_id,
          controller: createRecordAttachmentQueueController({
            draftId: draft.draft_id,
            onChange: handleChange,
            mediaTypeFor: declaredMediaType,
          }),
        }
      }
      controllerRef.current.controller.enqueue(accepted)
    } finally {
      preparingRef.current -= accepted.length
      if (mountedRef.current) setPreparing(preparingRef.current)
    }
  }, [ensureDraft, handleChange, isPublishing])

  // 草稿已被发布或丢弃：它名下的队列作废。发布前上传必须已经结束，这里只会清掉失败、取消或过期的行。
  const reset = useCallback(() => {
    controllerRef.current?.controller.dispose()
    controllerRef.current = null
    setQueued([])
  }, [])

  const rows: RecordAttachmentUploadRow[] = [
    ...queued.filter((item) => item.status !== 'available').map((item) => ({
      client_id: item.client_id,
      display_name: item.display_name,
      size_bytes: item.size_bytes,
      status: item.status,
      ...(item.error ? { error: item.error } : {}),
    })),
    ...refused,
  ]

  return {
    rows,
    notice,
    active: preparing > 0 || queued.some((item) => isActiveUpload(item.status)),
    // 同步读取最新状态，给发布入口做最后一道检查，不依赖可能滞后的渲染结果。
    isBusy: () => preparingRef.current > 0 ||
      Boolean(controllerRef.current?.controller.getSnapshot().some((item) => isActiveUpload(item.status))),
    addFiles,
    reset,
    retry: (clientId: string) => {
      if (isPublishing()) {
        setNotice('正在发布，完成后再上传附件')
        return
      }
      controllerRef.current?.controller.retry(clientId)
    },
    cancel: (clientId: string) => {
      controllerRef.current?.controller.cancel(clientId)
    },
    remove: (clientId: string) => {
      if (clientId.startsWith('refused_')) {
        setRefused((current) => current.filter((row) => row.client_id !== clientId))
        return
      }
      controllerRef.current?.controller.remove(clientId)
    },
  }
}

// 只有通过 checkAttachmentFile 的文件才会入队，这里总能得到后端接受的类型。
function declaredMediaType(file: File): string {
  const check = checkAttachmentFile(file)
  return check.ok ? check.mediaType : 'application/octet-stream'
}
