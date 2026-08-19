import { beforeEach, describe, expect, it, vi } from 'vitest'

import { searchRecordsForGlobalSearch } from './globalRecordSearch'
import type { RecordDetail } from '../../lib/types'

vi.mock('../../lib/recordsApi', () => ({
  searchRecords: vi.fn(),
}))

const api = await import('../../lib/recordsApi')

function hit(overrides: Partial<RecordDetail['current']> = {}, recordId = 'rec_001'): RecordDetail {
  return {
    record_id: recordId,
    lifecycle: 'active',
    current_revision_id: 'rev_001',
    lock_version: 1,
    authorization_epoch: 1,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-09T08:30:00Z',
    capabilities: {
      read: true, update: false, archive: false, restore: false, draft: false, permanent_delete: false,
    },
    current: {
      record_id: recordId,
      revision_id: 'rev_001',
      revision_no: 1,
      title: '东京节点磁盘 IO 抖动',
      body_markdown: '',
      markdown_dialect_version: 1,
      record_type: 'troubleshooting',
      business_status: 'investigating',
      impact_level: 'medium',
      visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
      subjects: [{
        registry_version: 1,
        kind: 'vps',
        source_id: 'vps_alpha',
        role: 'affected',
        primary: true,
        identity: { display_name: 'Tokyo Edge' },
      }],
      tags: [],
      attachment_ids: [],
      participants: [],
      author_id: 'usr_000000000000000000000001',
      save_reason: '',
      created_at: '2026-08-09T08:30:00Z',
      ...overrides,
    },
  }
}

describe('searchRecordsForGlobalSearch', () => {
  beforeEach(() => {
    vi.mocked(api.searchRecords).mockReset()
  })

  it('asks the server for a bounded page of the raw query', async () => {
    vi.mocked(api.searchRecords).mockResolvedValue({ items: [], generation: 3 })

    await searchRecordsForGlobalSearch('  磁盘 IO  ', 4)

    expect(api.searchRecords).toHaveBeenCalledWith({ q: '磁盘 IO', limit: 4 })
  })

  it('describes a hit by title, type, and primary subject', async () => {
    vi.mocked(api.searchRecords).mockResolvedValue({ items: [hit()], generation: 3 })

    expect(await searchRecordsForGlobalSearch('磁盘', 4)).toEqual([{
      id: 'rec_001',
      label: '东京节点磁盘 IO 抖动',
      hint: '排障 · 排查中 · Tokyo Edge',
      to: '/records/rec_001',
    }])
  })

  it('falls back to the record id when a revision carries no title', async () => {
    const untitled = hit({ title: '   ', subjects: [] })
    delete untitled.current.business_status
    vi.mocked(api.searchRecords).mockResolvedValue({ items: [untitled], generation: 3 })

    expect(await searchRecordsForGlobalSearch('磁盘', 4)).toEqual([{
      id: 'rec_001',
      label: 'rec_001',
      hint: '排障',
      to: '/records/rec_001',
    }])
  })

  it('reports nothing rather than failing when the index is unavailable', async () => {
    // The palette also searches assets. A records index that has not finished
    // building, or a center with the records platform switched off, must not
    // blank out the rest of the results.
    vi.mocked(api.searchRecords).mockRejectedValue(new Error('search index unavailable'))

    expect(await searchRecordsForGlobalSearch('磁盘', 4)).toEqual([])
  })

  it('skips the request entirely for a blank query', async () => {
    expect(await searchRecordsForGlobalSearch('   ', 4)).toEqual([])
    expect(api.searchRecords).not.toHaveBeenCalled()
  })

  it('keeps at most the requested number of hits', async () => {
    vi.mocked(api.searchRecords).mockResolvedValue({
      items: [hit({}, 'rec_001'), hit({}, 'rec_002'), hit({}, 'rec_003')],
      generation: 3,
    })

    expect(await searchRecordsForGlobalSearch('磁盘', 2)).toHaveLength(2)
  })
})
