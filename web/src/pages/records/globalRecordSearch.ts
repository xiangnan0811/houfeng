import { searchRecords } from '../../lib/recordsApi'
import type { RecordDetail } from '../../lib/types'
import { RECORD_TYPE_LABELS } from './recordLabels'
import { BUSINESS_STATUS_LABELS } from './recordWorkspaceModel'

/**
 * One record as the global search palette shows it. The palette stays free of
 * record types so the records transport is only ever reached through this
 * module's dynamic import.
 */
export type GlobalRecordSearchHit = {
  id: string
  label: string
  hint: string
  to: string
}

function describe(record: RecordDetail): string {
  const revision = record.current
  const primary = revision.subjects.find((subject) => subject.primary) ?? revision.subjects[0]
  return [
    RECORD_TYPE_LABELS[revision.record_type],
    revision.business_status ? BUSINESS_STATUS_LABELS[revision.business_status] : '',
    primary ? primary.identity.display_name || primary.source_id : '',
  ].filter(Boolean).join(' · ')
}

/**
 * Searches records for the palette. Failures resolve to no hits on purpose: the
 * records index can legitimately be missing on a fresh install or switched off
 * entirely, and neither should take the rest of the palette down with it.
 */
export async function searchRecordsForGlobalSearch(
  query: string,
  limit: number,
): Promise<GlobalRecordSearchHit[]> {
  const q = query.trim()
  if (!q) return []
  try {
    const response = await searchRecords({ q, limit })
    const items = Array.isArray(response.items) ? response.items : []
    return items.slice(0, limit).map((record) => ({
      id: record.record_id,
      label: record.current.title.trim() || record.record_id,
      hint: describe(record),
      to: `/records/${record.record_id}`,
    }))
  } catch {
    return []
  }
}
