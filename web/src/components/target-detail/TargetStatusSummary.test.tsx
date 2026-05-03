import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TargetStatusSummary } from './TargetStatusSummary'
import type { TargetRecord } from '../../lib/types'

function targetRecord(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    base_port: 443,
    execution_node_labels: [],
    run_status: '启用',
    labels: [],
    note: '',
    current_health_status: '关注',
    current_active_incident_count: 1,
    current_primary_issue_summary: 'HTTP 探测延迟升高',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

describe('TargetStatusSummary', () => {
  it('renders three summary cards with health, probe count and primary issue', () => {
    render(<TargetStatusSummary target={targetRecord()} probeItemCount={3} />)

    expect(screen.getByText('健康状态')).toBeInTheDocument()
    expect(screen.getByText('关注')).toBeInTheDocument()
    expect(screen.getByText('ProbeItem 数量')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('当前主问题')).toBeInTheDocument()
    expect(screen.getByText('HTTP 探测延迟升高')).toBeInTheDocument()
  })

  it('falls back to "暂无明显异常" when there is no primary issue', () => {
    render(
      <TargetStatusSummary
        target={targetRecord({ current_primary_issue_summary: '' })}
        probeItemCount={0}
      />,
    )

    expect(screen.getByText('暂无明显异常')).toBeInTheDocument()
  })
})
