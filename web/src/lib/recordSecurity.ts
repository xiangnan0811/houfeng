export type RecordSecurityReason = 'logout' | 'user-switch' | 'revoke' | 'visibility' | 'pageshow'

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
  revoke: (reason: RecordSecurityReason) => void
  dispose: () => void
  signal: AbortSignal
}

const CHANNEL_NAME = 'houfeng-record-security'

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
    revoke(reason) {
      if (lease.revoked) return
      lease.revoked = true
      abort.abort()
      channel?.postMessage({ type: 'revoke', recordId, userId, epoch, reason })
      channel?.close()
      onRevoke(reason)
    },
    dispose() {
      if (!lease.revoked) {
        lease.revoked = true
        abort.abort()
      }
      channel?.close()
    },
  }
  channel?.addEventListener('message', (event: MessageEvent<unknown>) => {
    const data = event.data
    if (!data || typeof data !== 'object') return
    const message = data as { type?: string; recordId?: string; userId?: string; epoch?: number; reason?: RecordSecurityReason }
    if (message.type !== 'revoke') return
    if (message.recordId && message.recordId !== '*' && message.recordId !== recordId) return
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
  if (!userId || typeof BroadcastChannel === 'undefined') return
  const channel = new BroadcastChannel(CHANNEL_NAME)
  channel.postMessage({ type: 'revoke', recordId: '*', userId, reason })
  channel.close()
}

