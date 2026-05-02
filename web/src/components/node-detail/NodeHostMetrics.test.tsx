import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { NodeHostMetrics } from './NodeHostMetrics'
import type { HostSample } from '../../lib/types'

function hostSample(overrides: Partial<HostSample> = {}): HostSample {
  return {
    node_id: 'nd_001',
    observed_at: '2026-04-24T09:05:00Z',
    received_at: '2026-04-24T09:05:02Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    cpu_usage_pct: 12.5,
    load_1: 0.3,
    load_5: 0.4,
    load_15: 0.5,
    mem_used_pct: 65,
    mem_available_bytes: 2147483648,
    swap_used_pct: 0,
    disk_used_pct: 52,
    inode_used_pct: 11,
    net_in_bytes_per_sec: 1024,
    net_out_bytes_per_sec: 2048,
    cpu_iowait_pct: 0.4,
    cpu_steal_pct: 0.1,
    disk_read_bytes_per_sec: 3072,
    disk_write_bytes_per_sec: 4096,
    disk_busy_pct: 3,
    uptime_seconds: 7200,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-001',
    ...overrides,
  }
}

describe('NodeHostMetrics', () => {
  it('renders four metric cards when sample is present', () => {
    render(<NodeHostMetrics sample={hostSample()} />)

    expect(screen.getByText('当前主机指标')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'CPU / Load' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '内存 / Swap' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '磁盘 / Inode' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '网络 / 吞吐' })).toBeInTheDocument()
    expect(screen.getByText('12.5%')).toBeInTheDocument()
    expect(screen.getByText(/2.0 GB/i)).toBeInTheDocument()
    expect(screen.getByText('2小时 0分钟')).toBeInTheDocument()
  })

  it('renders empty state when sample is null', () => {
    render(<NodeHostMetrics sample={null} />)

    expect(screen.getByRole('heading', { name: '尚未收到主机样本' })).toBeInTheDocument()
    expect(
      screen.getByText('该节点已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。'),
    ).toBeInTheDocument()
  })
})
