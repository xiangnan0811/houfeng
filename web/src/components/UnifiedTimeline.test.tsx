import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import type { SubjectActivityItem } from '../lib/types'
import { timelineChannel } from './timelineChannel'
import { UnifiedTimeline } from './UnifiedTimeline'

function item(partial: Partial<SubjectActivityItem> & Pick<SubjectActivityItem, 'activity_id' | 'event_kind' | 'source_kind' | 'presentation'>): SubjectActivityItem {
  return {
    event_at: '2026-08-10T12:00:00Z',
    recorded_at: '2026-08-10T12:00:01Z',
    backfilled: false,
    subjects: [],
    ...partial,
  }
}

describe('UnifiedTimeline', () => {
  it('classifies channels by source and event kind', () => {
    expect(timelineChannel(item({
      activity_id: 'a1',
      event_kind: 'record_revised',
      source_kind: 'record_domain',
      presentation: { version: 1, title: '修订' },
    }))).toBe('human')
    expect(timelineChannel(item({
      activity_id: 'a2',
      event_kind: 'command_executed',
      source_kind: 'command_audit',
      presentation: { version: 1, title: '命令' },
    }))).toBe('system')
    expect(timelineChannel(item({
      activity_id: 'a3',
      event_kind: 'evidence_captured',
      source_kind: 'evidence_snapshot',
      presentation: { version: 1, title: '证据' },
    }))).toBe('evidence')
  })

  it('renders human / system / evidence with text and distinct marks; system has no edit action', () => {
    render(
      <MemoryRouter>
        <UnifiedTimeline
          items={[
            item({
              activity_id: 'act_human',
              event_kind: 'record_revised',
              source_kind: 'record_domain',
              presentation: { version: 1, title: '磁盘迁移修订' },
              record_id: 'rec_001',
              revision_id: 'rrv_001',
            }),
            item({
              activity_id: 'act_system',
              event_kind: 'monitoring_state_changed',
              source_kind: 'monitoring_event',
              presentation: { version: 1, title: '健康变为告警' },
            }),
            item({
              activity_id: 'act_evidence',
              event_kind: 'evidence_captured',
              source_kind: 'evidence_snapshot',
              presentation: { version: 1, title: '探针快照', summary: '覆盖全量' },
              evidence_snapshot_id: 'evs_001',
              subjects: [{
                kind: 'vps',
                source_id: 'vps_001',
                role: 'affected',
                primary: true,
                identity: { coverage: 'full', bucket: '1h', quality: 'ok' },
                tombstoned: false,
              }],
            }),
          ]}
          sourceStatuses={[
            { source_kind: 'command_audit', state: 'stale', reason_code: 'lagging' },
          ]}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('人工记录')).toBeInTheDocument()
    expect(screen.getByText('系统事实')).toBeInTheDocument()
    expect(screen.getByText('不可变证据')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看修订' })).toHaveAttribute(
      'href',
      '/records/rec_001/revisions/rrv_001',
    )
    expect(screen.getByText('系统事实不可编辑')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /编辑/ })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute(
      'href',
      '/evidence/evs_001',
    )
    expect(screen.getByText(/覆盖 full/)).toBeInTheDocument()
    expect(screen.getByText(/command_audit：stale/)).toBeInTheDocument()
    expect(document.querySelector('.unified-timeline__mark--human')).not.toBeNull()
    expect(document.querySelector('.unified-timeline__mark--system')).not.toBeNull()
    expect(document.querySelector('.unified-timeline__mark--evidence')).not.toBeNull()
  })

  it('renders an explicit empty state', () => {
    render(<UnifiedTimeline items={[]} emptyTitle="主体尚无活动" />)
    expect(screen.getByText('主体尚无活动')).toBeInTheDocument()
  })
})
