import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { NodeHero } from './NodeHero'
import type { NodeRecord } from '../../lib/types'

function nodeRecord(overrides: Partial<NodeRecord> = {}): NodeRecord {
  return {
    node_id: 'nd_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: ['edge', 'core'],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-24T09:00:00Z',
    last_sync_at: '2026-04-24T09:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

describe('NodeHero', () => {
  it('renders display name, region/city/provider line and status badges', () => {
    render(<NodeHero node={nodeRecord()} />)

    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByText('节点详情')).toBeInTheDocument()
    expect(screen.getByText('ap-northeast-1 · Tokyo · Vultr')).toBeInTheDocument()
    expect(screen.getByText('在用')).toBeInTheDocument()
    expect(screen.getByText('启用')).toBeInTheDocument()
    expect(screen.getByText('已绑定')).toBeInTheDocument()
  })

  it('falls back to "暂无明显异常" when there is no primary issue summary', () => {
    render(<NodeHero node={nodeRecord()} />)
    expect(screen.getByText('暂无明显异常')).toBeInTheDocument()
  })
})
