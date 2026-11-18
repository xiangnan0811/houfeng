import { validateCleanupEvent, type DraftBufferStore, type DraftCleanupEvent } from '../pages/records/draftBuffer'
import { clearComparisonSaveAttempts } from './comparisonSaveAttempts'
import { broadcastRecordCleanup } from './recordSecurity'

export type RecordCleanupAck = Pick<DraftCleanupEvent, 'cursor' | 'operation_id' | 'record_id' | 'cleanup_epoch'>

/** The caller serializes events and sends this ack only after durable completion. */
export async function applyRecordClientCleanup(
  store: DraftBufferStore,
  userId: string,
  browserId: string,
  input: DraftCleanupEvent,
): Promise<RecordCleanupAck> {
  const event = structuredClone(input)
  validateCleanupEvent(event)
  // No await before closing local work. Other tabs receive the original string
  // epoch, not a lossy numeric authorization epoch.
  broadcastRecordCleanup(userId, event)
  // sessionStorage is tab-local. Its failure must precede, and block, cursor
  // advancement so reconnection replays rather than acknowledging lost cleanup.
  clearComparisonSaveAttempts(userId, event.record_id)
  await store.applyCleanup(userId, browserId, event)
  return {
    cursor: event.cursor,
    operation_id: event.operation_id,
    record_id: event.record_id,
    cleanup_epoch: event.cleanup_epoch,
  }
}
