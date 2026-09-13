import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

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
    source_summary: 'HTTP 探测在多个监控实例上失败',
    ...overrides,
  }
}

describe('TargetActiveIncidents', () => {
  it('renders the loading placeholder when not yet loaded', () => {
    render(<TargetActiveIncidents loaded={false} incidents={[]} error={null} />)

    expect(screen.getByRole('heading', { name: '正在加载活跃异常…' })).toBeInTheDocument()
  })

  it('renders the error fallback when loaded with an error', () => {
    const onRetry = vi.fn()
    render(
      <TargetActiveIncidents loaded={true} incidents={[]} error="活跃异常读模型失败" onRetry={onRetry} />,
    )

    expect(screen.getByRole('heading', { name: '活跃异常暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('活跃异常读模型失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试加载活跃异常' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('renders incident summaries when loaded', () => {
    render(<TargetActiveIncidents loaded={true} incidents={[incident()]} error={null} />)

    expect(screen.getByText('HTTP 探测在多个监控实例上失败')).toBeInTheDocument()
  })
})
