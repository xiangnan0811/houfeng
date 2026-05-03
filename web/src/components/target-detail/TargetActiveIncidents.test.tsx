import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TargetActiveIncidents } from './TargetActiveIncidents'
import type { ActiveIncidentRecord } from '../../lib/types'

function incident(overrides: Partial<ActiveIncidentRecord> = {}): ActiveIncidentRecord {
  return {
    incident_id: 'inc_001',
    incident_class: 'target_probe_failure',
    object_type: 'target',
    object_id: 'tg_001',
    severity: '严重',
    started_at: '2026-04-24T08:58:00Z',
    last_evaluated_at: '2026-04-24T09:05:00Z',
    source_summary: 'HTTP 探测在多个节点上失败',
    ...overrides,
  }
}

describe('TargetActiveIncidents', () => {
  it('renders the loading placeholder when not yet loaded', () => {
    render(<TargetActiveIncidents loaded={false} incidents={[]} error={null} />)

    expect(screen.getByRole('heading', { name: '正在加载活跃异常…' })).toBeInTheDocument()
  })

  it('renders the error fallback when loaded with an error', () => {
    render(
      <TargetActiveIncidents loaded={true} incidents={[]} error="活跃异常读模型失败" />,
    )

    expect(screen.getByRole('heading', { name: '活跃异常暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('活跃异常读模型失败')).toBeInTheDocument()
  })

  it('renders incident summaries when loaded', () => {
    render(<TargetActiveIncidents loaded={true} incidents={[incident()]} error={null} />)

    expect(screen.getByText('HTTP 探测在多个节点上失败')).toBeInTheDocument()
  })
})
