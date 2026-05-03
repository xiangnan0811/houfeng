import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TargetRecentEvents } from './TargetRecentEvents'
import type { StateChangeEventRecord } from '../../lib/types'

function event(overrides: Partial<StateChangeEventRecord> = {}): StateChangeEventRecord {
  return {
    event_id: 'evt_001',
    incident_id: 'inc_001',
    incident_class: 'target_probe_failure',
    object_type: 'target',
    object_id: 'tg_001',
    event_type: 'incident_started',
    severity: '严重',
    summary: 'HTTPS 探测连续失败',
    created_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

describe('TargetRecentEvents', () => {
  it('renders the loading placeholder when not yet loaded', () => {
    render(<TargetRecentEvents loaded={false} events={[]} error={null} />)

    expect(screen.getByRole('heading', { name: '正在加载相关事件…' })).toBeInTheDocument()
  })

  it('renders the error fallback when loaded with an error', () => {
    render(<TargetRecentEvents loaded={true} events={[]} error="事件流不可用" />)

    expect(screen.getByRole('heading', { name: '相关事件暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('事件流不可用')).toBeInTheDocument()
  })

  it('renders event summaries when loaded', () => {
    render(<TargetRecentEvents loaded={true} events={[event()]} error={null} />)

    expect(screen.getByText('HTTPS 探测连续失败')).toBeInTheDocument()
  })
})
