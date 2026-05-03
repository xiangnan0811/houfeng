import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TargetHero } from './TargetHero'
import type { TargetRecord } from '../../lib/types'

function targetRecord(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    base_port: 443,
    execution_node_labels: ['edge'],
    run_status: '启用',
    labels: ['公开'],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: '2026-04-24T09:00:00Z',
    last_failure_at: '2026-04-24T08:30:00Z',
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

describe('TargetHero', () => {
  it('renders target name, host:port summary line and status badges', () => {
    render(<TargetHero target={targetRecord()} />)

    expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument()
    expect(screen.getByText('目标详情')).toBeInTheDocument()
    expect(screen.getByText('service · blog.example.com:443')).toBeInTheDocument()
    expect(screen.getByText('启用')).toBeInTheDocument()
    expect(screen.getByText('正常')).toBeInTheDocument()
  })

  it('omits the port suffix when base_port is missing', () => {
    render(<TargetHero target={targetRecord({ base_port: undefined })} />)

    expect(screen.getByText('service · blog.example.com')).toBeInTheDocument()
  })
})
