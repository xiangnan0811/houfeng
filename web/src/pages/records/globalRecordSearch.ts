import { searchRecords } from '../../lib/recordsApi'
import type { RecordDetail } from '../../lib/types'
import { RECORD_TYPE_LABELS } from './recordLabels'
import { recordSearchParamsFromFilters } from './searchFilterModel'
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

/** Visible failure for this source. Not a successful empty page. */
export const RECORD_SEARCH_UNAVAILABLE_MESSAGE = '运维记录搜索暂不可用'

export type GlobalRecordSearchOutcome = {
  matches: GlobalRecordSearchHit[]
  error: string | null
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
 * Searches records for the palette. A missing index, a refused request, or a
 * transport failure stays on this source as an error. It must not look like a
 * successful empty page, and it must not decide what the asset source shows.
 * A 401 still ends the session inside the transport before this rejection is
 * observed.
 */
export async function searchRecordsForGlobalSearch(
  query: string,
  limit: number,
): Promise<GlobalRecordSearchOutcome> {
  const q = query.trim()
  if (!q) return { matches: [], error: null }
  try {
    const response = await searchRecords({ q, limit })
    const items = Array.isArray(response.items) ? response.items : []
    const hits: GlobalRecordSearchHit[] = items.slice(0, limit).map((record) => ({
      id: record.record_id,
      label: record.current.title.trim() || record.record_id,
      hint: describe(record),
      to: `/records/${record.record_id}`,
    }))
    if (hits.length === 0) return { matches: [], error: null }
    // The palette only ever shows a few of the ranked hits, so it has to offer a
    // way through to the full result set. Only once the server has answered:
    // pointing at the search page is misleading while the index cannot serve it.
    return {
      matches: [...hits, {
        id: RECORD_SEARCH_ALL_HIT_ID,
        label: '查看全部匹配记录',
        hint: q,
        to: `/records?${recordSearchParamsFromFilters({ q }).toString()}`,
      }],
      error: null,
    }
  } catch {
    return { matches: [], error: RECORD_SEARCH_UNAVAILABLE_MESSAGE }
  }
}

/** Marks the trailing hit that leads to the full result set rather than a record. */
export const RECORD_SEARCH_ALL_HIT_ID = '__all__'
