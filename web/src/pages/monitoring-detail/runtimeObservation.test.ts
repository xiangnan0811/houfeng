import { describe, expect, it } from 'vitest'

import type { HostSample } from '../../lib/types'
import {
  compareHostSampleRecency,
  hostNetworkRate,
  hostSampleToMetricPoint,
  isEligibleHostSample,
  mergeLatestHostSample,
  applyHttpLatestSample,
  parseTimeWindow,
  runtimeFactsMatchWindow,
} from './runtimeObservation'

function sample(overrides: Partial<HostSample> = {}): HostSample {
  return {
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-04-24T10:00:00Z',
    received_at: '2026-04-24T10:00:01Z',
    agent_version: 'dev',
    fingerprint: 'fp-mi',
    cpu_usage_pct: 10,
    load_1: 0.1,
    load_5: 0.2,
    load_15: 0.3,
    mem_used_pct: 40,
    mem_available_bytes: 1,
    mem_total_bytes: 2,
    swap_used_pct: 0,
    disk_used_pct: 30,
    disk_total_bytes: 4,
    inode_used_pct: 5,
    net_in_bytes_per_sec: 0,
    net_out_bytes_per_sec: 0,
    network_rates_valid: true,
    cpu_iowait_pct: 1,
    cpu_steal_pct: 0,
    disk_read_bytes_per_sec: 0,
    disk_write_bytes_per_sec: 0,
    disk_busy_pct: 0,
    uptime_seconds: 60,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-a',
    ...overrides,
  }
}

describe('runtimeObservation', () => {
  it('defaults unknown windows to 24h and keeps explicit realtime', () => {
    expect(parseTimeWindow(null)).toBe('24h')
    expect(parseTimeWindow('nope')).toBe('24h')
    expect(parseTimeWindow('realtime')).toBe('realtime')
    expect(parseTimeWindow('7d')).toBe('7d')
  })

  it('treats genuine network zero as a rate and unknown/legacy marker as missing', () => {
    expect(hostNetworkRate(sample({ net_in_bytes_per_sec: 0, network_rates_valid: true }), 'in')).toBe(0)
    expect(hostNetworkRate(sample({ net_out_bytes_per_sec: 0, network_rates_valid: true }), 'out')).toBe(0)
    expect(hostNetworkRate(sample({ net_in_bytes_per_sec: 0, network_rates_valid: false }), 'in')).toBeNull()
    expect(hostNetworkRate(sample({ net_in_bytes_per_sec: 4096, network_rates_valid: null }), 'in')).toBeNull()
    const legacy = sample({ net_in_bytes_per_sec: 4096 })
    delete legacy.network_rates_valid
    expect(hostNetworkRate(legacy, 'in')).toBeNull()
  })

  it('rejects inner wrong id, invalid time, and backfilled current claims', () => {
    expect(isEligibleHostSample(sample({ monitoring_instance_id: 'mi_other' }), 'mi_001', { allowBackfilled: false })).toBe(false)
    expect(isEligibleHostSample(sample({ observed_at: 'not-a-time' }), 'mi_001', { allowBackfilled: false })).toBe(false)
    expect(isEligibleHostSample(sample({ is_backfilled: true }), 'mi_001', { allowBackfilled: false })).toBe(false)
    expect(isEligibleHostSample(sample({ is_backfilled: true }), 'mi_001', { allowBackfilled: true })).toBe(true)
  })

  it('ranks later observed samples over earlier ones and prefers live over backfill at the same observed time', () => {
    const older = sample({ observed_at: '2026-04-24T10:00:00Z', received_at: '2026-04-24T10:00:05Z', sync_batch_id: 'z' })
    const newer = sample({ observed_at: '2026-04-24T10:00:10Z', received_at: '2026-04-24T10:00:11Z', sync_batch_id: 'a' })
    expect(compareHostSampleRecency(newer, older)).toBeGreaterThan(0)

    const live = sample({ observed_at: '2026-04-24T10:00:00Z', is_backfilled: false, received_at: '2026-04-24T10:00:01Z' })
    const backfill = sample({ observed_at: '2026-04-24T10:00:00Z', is_backfilled: true, received_at: '2026-04-24T10:00:09Z' })
    expect(compareHostSampleRecency(live, backfill)).toBeGreaterThan(0)
  })

  it('keeps stream merge monotonic and rejects backfilled live claims', () => {
    const current = sample({ observed_at: '2026-04-24T10:00:10Z', cpu_usage_pct: 42, sync_batch_id: 'live' })
    const older = sample({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 9, sync_batch_id: 'old' })
    expect(mergeLatestHostSample(current, older, 'mi_001', { allowBackfilled: true })?.sync_batch_id).toBe('live')
    expect(mergeLatestHostSample(current, sample({ is_backfilled: true, observed_at: '2026-04-24T10:00:20Z' }), 'mi_001', { allowBackfilled: false })).toBe(current)
  })

  it('lets a newer empty HTTP snapshot clear prior-epoch latest', () => {
    const current = sample({ observed_at: '2026-04-24T10:00:10Z', received_at: '2026-04-24T10:00:11Z', sync_batch_id: 'epoch-a' })
    expect(applyHttpLatestSample(current, null, 'mi_001', '2026-04-24T10:01:00Z')).toBeNull()
  })

  it('does not let a late older HTTP snapshot overwrite a live sample received after read_at', () => {
    const live = sample({
      observed_at: '2026-04-24T10:00:20Z',
      received_at: '2026-04-24T10:00:21Z',
      cpu_usage_pct: 41,
      sync_batch_id: 'live',
    })
    const olderHttp = sample({
      observed_at: '2026-04-24T10:00:00Z',
      received_at: '2026-04-24T10:00:01Z',
      cpu_usage_pct: 9,
      sync_batch_id: 'http-old',
    })
    expect(applyHttpLatestSample(live, olderHttp, 'mi_001', '2026-04-24T10:00:10Z')?.sync_batch_id).toBe('live')
    expect(applyHttpLatestSample(live, null, 'mi_001', '2026-04-24T10:00:10Z')?.sync_batch_id).toBe('live')
  })

  it('lets current-binding HTTP replace an older observed sample when the live sample is not after read_at', () => {
    const previousEpoch = sample({ observed_at: '2026-04-24T10:00:10Z', received_at: '2026-04-24T10:00:11Z', sync_batch_id: 'epoch-a' })
    const rebound = sample({ observed_at: '2026-04-24T09:50:00Z', received_at: '2026-04-24T10:01:00Z', sync_batch_id: 'epoch-b' })
    expect(applyHttpLatestSample(previousEpoch, rebound, 'mi_001', '2026-04-24T10:01:05Z')?.sync_batch_id).toBe('epoch-b')
  })

  it('maps live network rates into series points and leaves unknown rates as null', () => {
    const point = hostSampleToMetricPoint(sample({ net_in_bytes_per_sec: 0, network_rates_valid: true, swap_used_pct: 4, load_1: 0.9, disk_busy_pct: 6 }))
    expect(point.net_in_bytes_per_sec).toBe(0)
    expect(point.swap_used_pct).toBe(4)
    expect(point.load_1).toBe(0.9)
    expect(point.disk_busy_pct).toBe(6)
    expect(hostSampleToMetricPoint(sample({ net_in_bytes_per_sec: 8, network_rates_valid: false })).net_in_bytes_per_sec).toBeNull()
  })

  it('accepts facts without a window for legacy payloads and requires matching keys otherwise', () => {
    expect(runtimeFactsMatchWindow({ monitoring_instance_id: 'mi_001', latest_host_sample: null }, '24h')).toBe(true)
    expect(runtimeFactsMatchWindow({
      monitoring_instance_id: 'mi_001',
      latest_host_sample: null,
      window: {
        key: '7d',
        started_at: '2026-04-17T10:00:00Z',
        ended_at: '2026-04-24T10:00:00Z',
        bucket_count: 1,
        available_started_at: null,
        available_ended_at: null,
        sample_count: 0,
      },
    }, '24h')).toBe(false)
  })
})
