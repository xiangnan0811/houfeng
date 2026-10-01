import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

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
    // 系统事实只有通道标签，没有查看或编辑入口。
    const systemItem = document.querySelector('.unified-timeline__item--system')
    expect(systemItem?.querySelector('a')).toBeNull()
    expect(screen.queryByRole('link', { name: /编辑/ })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute(
      'href',
      '/evidence/evs_001',
    )
    expect(screen.getByText(/覆盖 full/)).toBeInTheDocument()
    expect(screen.getByText(/命令审计：过期（lagging）/)).toBeInTheDocument()
    expect(document.querySelector('.unified-timeline__mark--human')).not.toBeNull()
    expect(document.querySelector('.unified-timeline__mark--system')).not.toBeNull()
    expect(document.querySelector('.unified-timeline__mark--evidence')).not.toBeNull()
  })

  it('labels a human item without revision as 查看记录', () => {
    render(
      <MemoryRouter>
        <UnifiedTimeline
          items={[item({
            activity_id: 'act_record_only',
            event_kind: 'record_created',
            source_kind: 'record_domain',
            presentation: { version: 1, title: '首条记录' },
            record_id: 'rec_001',
          })]}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: '查看记录' })).toHaveAttribute('href', '/records/rec_001')
    expect(screen.queryByRole('link', { name: '查看修订' })).not.toBeInTheDocument()
  })

  it('renders an explicit empty state', () => {
    render(
      <MemoryRouter>
        <UnifiedTimeline items={[]} emptyTitle="主体尚无活动" />
      </MemoryRouter>,
    )
    expect(screen.getByText('主体尚无活动')).toBeInTheDocument()
  })
})

describe('UnifiedTimeline rows', () => {
  const originalTZ = process.env.TZ
  // 固定非 UTC 时区：UTC 日与本地日不同的瞬间才能锁住"按本地日分组"。
  beforeAll(() => { process.env.TZ = 'Asia/Shanghai' })
  afterAll(() => {
    if (originalTZ === undefined) delete process.env.TZ
    else process.env.TZ = originalTZ
  })

  function timelineItem(id: string, eventAt: string, recordedAt = eventAt): SubjectActivityItem {
    return {
      activity_id: id,
      event_kind: 'monitoring_state_changed',
      event_at: eventAt,
      recorded_at: recordedAt,
      source_kind: 'monitoring_event',
      backfilled: true,
      subjects: [],
      presentation: { version: 1, title: `事件 ${id}`, summary: '告警 · 2.4%' },
    }
  }

  it('groups by local calendar day and keeps tags, recorded time and actions on one line', () => {
    render(
      <MemoryRouter>
        <UnifiedTimeline items={[
          // 服务端按时间倒序返回。c（UTC 08-19）与 a（UTC 08-18）跨 UTC 日、本地同为 08-19；
          // a 与 b 同一 UTC 日（08-18），本地分属 08-19 00:30 与 08-18 23:50。
          timelineItem('c', '2026-08-19T00:30:00Z'),
          timelineItem('a', '2026-08-18T16:30:00Z', '2026-08-18T16:45:00Z'),
          timelineItem('b', '2026-08-18T15:50:00Z', '2026-08-19T02:00:00Z'),
        ]} />
      </MemoryRouter>,
    )
    expect(screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent)).toEqual(['2026-08-19', '2026-08-18'])
    expect(screen.getByRole('region', { name: '2026-08-19' })).toHaveTextContent('事件 a')
    expect(screen.getByRole('region', { name: '2026-08-19' })).toHaveTextContent('事件 c')
    expect(screen.getByRole('region', { name: '2026-08-18' })).toHaveTextContent('事件 b')
    expect(screen.getByText('00:30')).toHaveAttribute('datetime', '2026-08-18T16:30:00Z')
    const line = screen.getByRole('heading', { name: '事件 a' }).parentElement
    expect(line).toHaveClass('unified-timeline__line')
    expect(line).toHaveTextContent('系统事实')
    expect(line).toHaveTextContent('回填')
    expect(line).toHaveTextContent('记入 00:45')
    // 跨日记入写完整日期。
    expect(screen.getByRole('heading', { name: '事件 b' }).parentElement).toHaveTextContent('记入 2026/08/19 10:00')
    expect(screen.getAllByText('告警 · 2.4%')[0]).toHaveClass('unified-timeline__summary')
  })
})

describe('UnifiedTimeline link state', () => {
  function StateProbe() {
    const { state } = useLocation()
    return <pre data-testid="state">{JSON.stringify(state ?? null)}</pre>
  }

  const recordItem: SubjectActivityItem = {
    activity_id: 'r1',
    event_kind: 'record_revised',
    event_at: '2026-08-19T12:00:00Z',
    recorded_at: '2026-08-19T12:00:00Z',
    source_kind: 'record_domain',
    backfilled: false,
    subjects: [],
    presentation: { version: 1, title: '记录修订' },
    record_id: 'rec_1',
    revision_id: 'rrv_1',
  }

  it.each([
    [false, '{"from":"list"}'],
    [true, 'null'],
  ])('carries navigation state unless omitLinkState=%s', (omitLinkState, expected) => {
    render(
      <MemoryRouter initialEntries={[{ pathname: '/subject', state: { from: 'list' } }]}>
        <Routes>
          <Route path="/subject" element={<UnifiedTimeline items={[recordItem]} omitLinkState={omitLinkState} />} />
          <Route path="/records/:recordId/revisions/:revisionId" element={<StateProbe />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('link', { name: '查看修订' }))
    expect(screen.getByTestId('state')).toHaveTextContent(expected)
  })
})
