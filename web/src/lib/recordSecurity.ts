import { flushSync } from 'react-dom'

import { validateCleanupEvent, type DraftCleanupEvent } from '../pages/records/draftBuffer'
import { isRecordId } from './recordIdentity'

export type RecordSecurityReason = 'logout' | 'user-switch' | 'revoke' | 'visibility' | 'pageshow' | 'cleanup'

export type ClientContentLease = {
  recordId: string
  userId: string
  epoch: number
  revoked: boolean
}

export type RecordSecurityController = {
  lease: ClientContentLease
  abort: AbortController
  channel: BroadcastChannel | null
  /** Change the live workspace target without announcing an undurable allocation. */
  bindRecordId: (recordId: string) => boolean
  revoke: (reason: RecordSecurityReason) => void
  dispose: () => void
  signal: AbortSignal
}

const CHANNEL_NAME = 'houfeng-record-security'
const localControllers = new Set<RecordSecurityController>()
const announcedRecordIds = new WeakMap<RecordSecurityController, string>()
// A cleanup can arrive while a workspace is still reading the allocation from
// storage. Remember it until that workspace can bind and consult its tombstone.
const observedCleanups = new Map<string, Set<string>>()
function rememberCleanup(userId: string, recordId: string): void {
  let records = observedCleanups.get(userId)
  if (!records) observedCleanups.set(userId, records = new Set())
  records.add(recordId)
}
// Keep the existing cleanup channel listening even between workspaces, so a
// new view cannot miss a broadcast while the shared tombstone is committing.
const cleanupChannel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL_NAME)
cleanupChannel?.addEventListener('message', (event: MessageEvent<unknown>) => {
  const data = event.data as { type?: string; userId?: string; event?: DraftCleanupEvent } | null
  if (!data || data.type !== 'cleanup' || typeof data.userId !== 'string' || !data.userId || !data.event) return
  try { validateCleanupEvent(data.event) } catch { return }
  rememberCleanup(data.userId, data.event.record_id)
})
// Node's test implementation otherwise keeps the process alive; browsers do not expose unref.
if (cleanupChannel && 'unref' in cleanupChannel && typeof cleanupChannel.unref === 'function') cleanupChannel.unref()
const targetObservers = new Set<{ userId: string; receive: (recordIds: readonly string[]) => void }>()
export type RecordClientAllocationReference = Readonly<{ record_id: string; allocation_id: string }>
const allocationObservers = new Set<{ userId: string; receive: (reference: RecordClientAllocationReference) => void }>()

export function createRecordSecurityController(
  recordId: string,
  userId: string,
  epoch: number,
  onRevoke: (reason: RecordSecurityReason) => void,
): RecordSecurityController {
  const abort = new AbortController()
  const lease: ClientContentLease = { recordId, userId, epoch, revoked: false }
  const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL_NAME)
  const controller: RecordSecurityController = {
    lease,
    abort,
    channel,
    signal: abort.signal,
    bindRecordId(nextRecordId) {
      if (lease.revoked) return false
      if (nextRecordId !== 'new' && !isRecordId(nextRecordId)) throw new Error('Invalid record identity')
      lease.recordId = nextRecordId
      if (observedCleanups.get(userId)?.has(nextRecordId)) {
        controller.revoke('cleanup')
        return false
      }
      return true
    },
    revoke(reason) {
      if (lease.revoked) return
      lease.revoked = true
      abort.abort()
      localControllers.delete(controller)
      if (reason !== 'cleanup') channel?.postMessage({ type: 'revoke', recordId: lease.recordId, userId, epoch, reason })
      channel?.close()
      onRevoke(reason)
    },
    dispose() {
      localControllers.delete(controller)
      if (!lease.revoked) {
        lease.revoked = true
        abort.abort()
      }
      channel?.close()
    },
  }
  localControllers.add(controller)
  if (isRecordId(recordId)) announcedRecordIds.set(controller, recordId)
  if (isRecordId(recordId)) {
    for (const observer of targetObservers) if (observer.userId === userId) observer.receive([recordId])
  }
  channel?.addEventListener('message', (event: MessageEvent<unknown>) => {
    const data = event.data
    if (!data || typeof data !== 'object') return
    const cleanup = data as { type?: string; userId?: string; event?: DraftCleanupEvent }
    if (cleanup.type === 'cleanup') {
      if (cleanup.userId !== userId || !cleanup.event) return
      try { validateCleanupEvent(cleanup.event) } catch { return }
      rememberCleanup(userId, cleanup.event.record_id)
      if (cleanup.event.record_id === lease.recordId) flushSync(() => controller.revoke('cleanup'))
      return
    }
    const message = data as { type?: string; recordId?: string; userId?: string; epoch?: number; reason?: RecordSecurityReason }
    if (message.type === 'invalidate') {
      if (message.userId === userId && message.recordId === lease.recordId) controller.revoke('cleanup')
      return
    }
    if (message.type !== 'revoke') return
    if (message.recordId && message.recordId !== '*' && message.recordId !== lease.recordId) return
    if (message.userId && message.userId !== userId) {
      controller.revoke('user-switch')
      return
    }
    // A revoke carrying an older authorization epoch describes access this tab has
    // already moved past, so acting on it would collapse a still-valid view.
    if (typeof message.epoch === 'number' && message.epoch < lease.epoch) return
    controller.revoke(message.reason ?? 'revoke')
  })
  return controller
}

export function broadcastRecordSessionEnd(
  userId: string,
  reason: Extract<RecordSecurityReason, 'logout' | 'user-switch'>,
): void {
  if (!userId) return
  for (const controller of localControllers) {
    if (controller.lease.userId === userId) controller.revoke(reason)
  }
  if (typeof BroadcastChannel === 'undefined') return
  const channel = new BroadcastChannel(CHANNEL_NAME)
  channel.postMessage({ type: 'revoke', recordId: '*', userId, reason })
  channel.close()
}

/** Close same-tab work synchronously; the wire epoch remains a bigint string. */
export function broadcastRecordCleanup(userId: string, event: DraftCleanupEvent): void {
  validateCleanupEvent(event)
  rememberCleanup(userId, event.record_id)
  flushSync(() => {
    for (const controller of localControllers) {
      if (controller.lease.userId === userId && controller.lease.recordId === event.record_id) controller.revoke('cleanup')
    }
  })
  if (typeof BroadcastChannel === 'undefined') return
  const channel = new BroadcastChannel(CHANNEL_NAME)
  channel.postMessage({ type: 'cleanup', userId, event })
  channel.close()
}

/** Authorization reconciliation closes a copy without inventing a purge event. */
export function broadcastRecordInvalidation(userId: string, recordId: string): void {
  if (!isRecordId(recordId)) throw new Error('Invalid record identity')
  for (const controller of localControllers) {
    if (controller.lease.userId === userId && controller.lease.recordId === recordId) controller.revoke('cleanup')
  }
  if (typeof BroadcastChannel === 'undefined') return
  const channel = new BroadcastChannel(CHANNEL_NAME)
  channel.postMessage({ type: 'invalidate', userId, recordId })
  channel.close()
}

export function observeRecordClientTargets(userId: string, receive: (recordIds: readonly string[]) => void): () => void {
  const observer = { userId, receive }
  targetObservers.add(observer)
  receive([...new Set([...localControllers]
    .filter((controller) => controller.lease.userId === userId && announcedRecordIds.get(controller) === controller.lease.recordId)
    .map((controller) => controller.lease.recordId))])
  return () => { targetObservers.delete(observer) }
}

/** Announce a record after an authorized read or completed mutation. */
export function registerRecordClientTarget(userId: string, recordId: string): void {
  if (!userId || !isRecordId(recordId)) throw new Error('Invalid record identity')
  for (const controller of localControllers) {
    if (controller.lease.userId === userId && controller.lease.recordId === recordId) announcedRecordIds.set(controller, recordId)
  }
  for (const observer of targetObservers) if (observer.userId === userId) observer.receive([recordId])
}

/** Announce only a durably stored allocation; it grants no record read authority. */
export function registerRecordClientAllocation(userId: string, allocation: RecordClientAllocationReference): void {
  if (!userId || !isRecordId(allocation.record_id) || !/^rqa_[0-9a-f]{32}(?![\s\S])/.test(allocation.allocation_id)) throw new Error('Invalid record allocation reference')
  for (const observer of allocationObservers) if (observer.userId === userId) observer.receive(allocation)
}

export function observeRecordClientAllocations(userId: string, receive: (reference: RecordClientAllocationReference) => void): () => void {
  const observer = { userId, receive }
  allocationObservers.add(observer)
  return () => { allocationObservers.delete(observer) }
}

