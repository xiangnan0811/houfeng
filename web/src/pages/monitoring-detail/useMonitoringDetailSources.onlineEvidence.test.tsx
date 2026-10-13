import { act, renderHook } from '@testing-library/react'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getMonitoringInstance, getMonitoringInstanceRuntimeFacts, getSettings } from '../../lib/api'
import type { HostSample, MonitoringInstanceRecord, MonitoringInstanceRuntimeFacts, SettingsRecord } from '../../lib/types'
import { classifyHeartbeatFreshness } from '../monitoring/heartbeatFreshness'
import { useMonitoringDetailSources } from './useMonitoringDetailSources'

vi.mock('../../lib/api', async (original) => ({
  ...await original<typeof import('../../lib/api')>(),
  getMonitoringInstance: vi.fn(), getMonitoringInstanceRuntimeFacts: vi.fn(), getSettings: vi.fn(),
}))
vi.mock('../../lib/observabilityApi', () => ({ listEvents: vi.fn().mockResolvedValue([]), listIncidents: vi.fn().mockResolvedValue([]) }))

class Socket {
  static instances: Socket[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  close = vi.fn()
  readonly url: string
  constructor(url: string) { this.url = url; Socket.instances.push(this) }
  open() { this.onopen?.() }
  send(message: unknown) { this.onmessage?.({ data: JSON.stringify(message) } as MessageEvent<string>) }
  disconnect() { this.onclose?.() }
  fail() { this.onerror?.() }
}

const start = '2026-04-24T10:00:00Z'
const at = (seconds: number) => new Date(Date.parse(start) + seconds * 1000).toISOString()
function evidence(seconds = 0, heartbeatSeconds: number | null = seconds, id = 'mi_001') {
  return { type: 'online_evidence', monitoring_instance_id: id, last_trusted_online_at: heartbeatSeconds === null ? null : at(heartbeatSeconds), read_at: at(seconds) }
}
function sample(seconds = 0, backfilled = false): HostSample {
  return { monitoring_instance_id: 'mi_001', observed_at: at(seconds), received_at: at(seconds + 1), sync_batch_id: `s-${seconds}`, is_backfilled: backfilled, cpu_usage_pct: seconds } as HostSample
}
function facts(window = '24h'): MonitoringInstanceRuntimeFacts {
  return { monitoring_instance_id: 'mi_001', read_at: at(0), latest_host_sample: sample(), recent_host_samples: [sample()], host_metric_points: [], window: { key: window, started_at: at(-86400), ended_at: at(0), sample_count: 1, bucket_count: 1, available_started_at: at(0), available_ended_at: at(0) } }
}
function setup(window = '24h') {
  return renderHook(({ id }) => {
    const [searchParams, setSearchParams] = useSearchParams()
    return useMonitoringDetailSources({ monitoringInstanceId: id, searchParams, setSearchParams, locationState: null })
  }, { initialProps: { id: 'mi_001' }, wrapper: ({ children }) => <MemoryRouter initialEntries={[`/monitoring/mi_001?window=${window}`]}>{children}</MemoryRouter> })
}
async function flush() { await act(async () => {}) }
function currentSocket() { return Socket.instances.at(-1)! }

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(start)
  Socket.instances = []
  vi.stubGlobal('WebSocket', Socket)
  vi.mocked(getMonitoringInstance).mockImplementation(async (id) => ({ monitoring_instance_id: id, last_trusted_online_at: start, note: 'saved', updated_at: start, metadata_updated_at: start } as MonitoringInstanceRecord))
  vi.mocked(getMonitoringInstanceRuntimeFacts).mockImplementation(async (_id, window) => facts(window))
  vi.mocked(getSettings).mockResolvedValue({ incident_defaults: { heartbeat_interval_seconds: 5, stale_threshold_intervals: 12 } } as SettingsRecord)
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.clearAllMocks() })

describe('monitoring online evidence stream', () => {
  it('keeps heartbeat and live samples fresh over four days without changing metadata', async () => {
    const { result } = setup('realtime')
    await flush()
    act(() => { currentSocket().open(); currentSocket().send(evidence()) })
    const original = result.current.state.monitoringInstance
    for (let day = 1; day <= 4; day++) {
      const seconds = day * 86400
      act(() => {
        vi.setSystemTime(at(seconds))
        currentSocket().send(evidence(seconds, seconds - 2))
        currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(seconds) })
      })
      expect(result.current.onlineEvidence?.read_at).toBe(at(seconds))
      expect(classifyHeartbeatFreshness(result.current.onlineEvidence!.last_trusted_online_at!, result.current.heartbeatPolicy, new Date(result.current.onlineEvidence!.read_at)).kind).toBe('fresh')
      expect(result.current.realtimeSamples.at(-1)?.observed_at).toBe(at(seconds))
    }
    expect(result.current.state.monitoringInstance).toBe(original)
    expect(Socket.instances).toHaveLength(1)
  })

  it('seeds delayed initial history without losing a newer streamed sample', async () => {
    let resolve!: (value: MonitoringInstanceRuntimeFacts) => void
    vi.mocked(getMonitoringInstanceRuntimeFacts).mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const { result } = setup('realtime')
    await flush()
    act(() => {
      currentSocket().open()
      currentSocket().send(evidence(20))
      currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(20) })
    })
    expect(result.current.realtimeSamples.map((item) => item.observed_at)).toEqual([at(20)])
    await act(async () => resolve({ ...facts('realtime'), recent_host_samples: [sample(-1800), sample(0)] }))
    expect(result.current.latestSample?.observed_at).toBe(at(20))
    expect(result.current.realtimeSamples.map((item) => item.observed_at)).toEqual([at(-1800), at(0), at(20)])
    vi.mocked(getMonitoringInstanceRuntimeFacts).mockResolvedValueOnce({ ...facts('realtime'), read_at: at(25), latest_host_sample: null, recent_host_samples: [] })
    act(() => result.current.retryRuntime())
    await flush()
    expect(result.current.latestSample).toBeNull()
    expect(result.current.realtimeSamples).toEqual([])
  })

  it('uses evidence read_at for the exact heartbeat threshold and recovery, independently of HTTP and samples', async () => {
    const { result } = setup('realtime')
    await flush()
    act(() => { currentSocket().open(); currentSocket().send(evidence(59, 0)) })
    const classify = () => classifyHeartbeatFreshness(result.current.onlineEvidence!.last_trusted_online_at!, result.current.heartbeatPolicy, new Date(result.current.onlineEvidence!.read_at)).kind
    expect(classify()).toBe('fresh')
    act(() => currentSocket().send(evidence(60, 0)))
    expect(classify()).toBe('stale')
    act(() => currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(100) }))
    expect(classify()).toBe('stale')
    act(() => result.current.retryRuntime())
    await flush()
    expect(result.current.onlineEvidence?.read_at).toBe(at(60))
    expect(classify()).toBe('stale')
    act(() => currentSocket().send(evidence(65, 64)))
    expect(classify()).toBe('fresh')
  })

  it.each(['24h', '7d', '30d'])('updates %s evidence while preserving the historical chart and ignoring samples', async (window) => {
    const { result } = setup(window)
    await flush()
    const snapshot = result.current.state.runtimeFacts
    const latest = result.current.latestSample
    expect(currentSocket().url).toContain('include_samples=false')
    act(() => {
      currentSocket().open()
      currentSocket().send(evidence(10))
      currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(10) })
    })
    expect(result.current.onlineEvidenceStatus).toBe('current')
    expect(result.current.state.runtimeFacts).toBe(snapshot)
    expect(result.current.latestSample).toBe(latest)
    expect(result.current.realtimeSamples).toEqual([])
  })

  it('continues online-only evidence during paused or stopped sampling without inventing a new sample', async () => {
    vi.mocked(getMonitoringInstance).mockResolvedValue({ monitoring_instance_id: 'mi_001', monitoring_status: '暂停', last_trusted_online_at: start } as MonitoringInstanceRecord)
    const { result } = setup('realtime')
    await flush()
    const oldSample = result.current.latestSample
    act(() => { currentSocket().open(); currentSocket().send(evidence(86400)) })
    expect(result.current.onlineEvidenceStatus).toBe('current')
    expect(result.current.onlineEvidence?.last_trusted_online_at).toBe(at(86400))
    expect(result.current.latestSample).toBe(oldSample)
    expect(result.current.state.monitoringInstance?.monitoring_status).toBe('暂停')
  })

  it('rejects mismatched, malformed, and older evidence and accepts a null heartbeat independently of backfill', async () => {
    const { result } = setup('realtime')
    await flush()
    act(() => { currentSocket().open(); currentSocket().send(evidence(10)) })
    const accepted = result.current.onlineEvidence
    act(() => {
      currentSocket().send(evidence(20, 20, 'mi_other'))
      currentSocket().send({ ...evidence(20), read_at: '04/24/2026' })
      currentSocket().send({ ...evidence(20), last_trusted_online_at: 'invalid' })
      currentSocket().send({ ...evidence(20), read_at: '2026-02-30T10:00:00Z' })
      currentSocket().send(evidence(20, 21))
      currentSocket().send(evidence(5))
      currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(20, true) })
    })
    expect(result.current.onlineEvidence).toBe(accepted)
    expect(result.current.latestSample?.observed_at).toBe(at(0))
    act(() => currentSocket().send(evidence(25, null)))
    expect(result.current.onlineEvidence?.last_trusted_online_at).toBeNull()
  })

  it('retains the last result on disconnect and silence, ignores retired sockets, and awaits evidence on reconnect', async () => {
    const { result } = setup()
    await flush()
    const first = currentSocket()
    act(() => { first.open(); first.send(evidence()); first.disconnect() })
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    expect(result.current.onlineEvidence?.read_at).toBe(at(0))
    act(() => vi.advanceTimersByTime(2000))
    const second = currentSocket()
    act(() => { second.open(); first.send(evidence(100)) })
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    expect(result.current.onlineEvidence?.read_at).toBe(at(0))
    act(() => second.send(evidence(2)))
    expect(result.current.onlineEvidenceStatus).toBe('current')
    act(() => vi.advanceTimersByTime(15000))
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    expect(second.close).toHaveBeenCalled()
  })

  it('allows a delayed reconnect handshake after a long established connection', async () => {
    const { result } = setup()
    await flush()
    act(() => { currentSocket().open(); currentSocket().send(evidence()) })
    for (let seconds = 5; seconds <= 30; seconds += 5) {
      act(() => { vi.advanceTimersByTime(5000); currentSocket().send(evidence(seconds)) })
    }
    act(() => currentSocket().disconnect())
    act(() => vi.advanceTimersByTime(2000))
    const retry = currentSocket()
    act(() => vi.advanceTimersByTime(5000))
    expect(Socket.instances).toHaveLength(2)
    expect(retry.close).not.toHaveBeenCalled()
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    act(() => { retry.open(); retry.send(evidence(37)) })
    expect(result.current.onlineEvidenceStatus).toBe('current')
    expect(result.current.onlineEvidence?.read_at).toBe(at(37))
  })

  it('stops automatic reconnect after an opened first connection never provides evidence and allows manual retry', async () => {
    const { result } = setup('realtime')
    await flush()
    act(() => {
      currentSocket().open()
      currentSocket().send({ type: 'host_sample', monitoring_instance_id: 'mi_001', sample: sample(5) })
      vi.advanceTimersByTime(15000)
    })
    expect(result.current.onlineEvidenceStatus).toBe('unavailable')
    act(() => vi.advanceTimersByTime(30000))
    expect(Socket.instances).toHaveLength(1)
    act(() => result.current.retryOnlineEvidence())
    expect(Socket.instances).toHaveLength(2)
    act(() => { currentSocket().open(); currentSocket().send(evidence(45)) })
    expect(result.current.onlineEvidenceStatus).toBe('current')
  })

  it.each(['close', 'error'])('keeps pre-open %s failures manual-only until evidence establishes automatic recovery', async (event) => {
    const { result } = setup()
    await flush()
    act(() => { if (event === 'close') currentSocket().disconnect(); else currentSocket().fail() })
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    act(() => vi.advanceTimersByTime(30000))
    expect(Socket.instances).toHaveLength(1)
    act(() => result.current.retryOnlineEvidence())
    expect(Socket.instances).toHaveLength(2)
    act(() => { currentSocket().open(); currentSocket().send(evidence(30)) })
    expect(result.current.onlineEvidenceStatus).toBe('current')
    act(() => currentSocket().disconnect())
    act(() => vi.advanceTimersByTime(1999))
    expect(Socket.instances).toHaveLength(2)
    act(() => vi.advanceTimersByTime(1))
    expect(Socket.instances).toHaveLength(3)
  })

  it('keeps an opened connection with no evidence manual-only after close and reports unavailable at 15 seconds', async () => {
    const { result } = setup()
    await flush()
    act(() => { currentSocket().open(); currentSocket().disconnect() })
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    act(() => vi.advanceTimersByTime(14999))
    expect(Socket.instances).toHaveLength(1)
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    act(() => vi.advanceTimersByTime(1))
    expect(result.current.onlineEvidenceStatus).toBe('unavailable')
    act(() => vi.advanceTimersByTime(30000))
    expect(Socket.instances).toHaveLength(1)
  })

  it('checks expiry immediately when a throttled hidden tab becomes visible', async () => {
    const { result } = setup()
    await flush()
    act(() => { currentSocket().open(); currentSocket().send(evidence()) })
    act(() => {
      vi.setSystemTime(at(60))
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
    expect(currentSocket().close).toHaveBeenCalled()
  })

  it('closes old window/instance generations and clears evidence on binding reset without altering drafts or tokens', async () => {
    const { result, rerender } = setup('realtime')
    await flush()
    const old = currentSocket()
    act(() => { old.open(); old.send(evidence(10)); result.current.setTimeWindow('7d') })
    await flush()
    expect(old.close).toHaveBeenCalled()
    expect(Socket.instances).toHaveLength(2)
    act(() => old.send(evidence(100)))
    expect(result.current.onlineEvidence?.read_at).toBe(at(10))
    act(() => { currentSocket().open(); currentSocket().send(evidence(20)); result.current.resetObservationEpoch() })
    expect(result.current.onlineEvidence).toBeNull()
    expect(result.current.onlineEvidenceStatus).toBe('connecting')
    const reset = currentSocket()
    act(() => { reset.open(); reset.send(evidence(0)) })
    expect(result.current.onlineEvidence?.read_at).toBe(at(0))
    rerender({ id: 'mi_other' })
    await flush()
    act(() => reset.send(evidence(200)))
    expect(result.current.onlineEvidence).toBeNull()
  })

  it('surfaces disconnect chart reread failure, retains the chart, and recovers through local retry', async () => {
    const { result } = setup('realtime')
    await flush()
    const chart = result.current.state.runtimeFacts
    vi.mocked(getMonitoringInstanceRuntimeFacts).mockRejectedValueOnce(new Error('database down'))
    act(() => { currentSocket().open(); currentSocket().send(evidence()); currentSocket().disconnect() })
    await flush()
    expect(result.current.runtimeFactsError).toBe('database down')
    expect(result.current.state.runtimeFacts).toBe(chart)
    act(() => result.current.retryRuntime())
    await flush()
    expect(result.current.runtimeFactsError).toBeNull()
    expect(result.current.onlineEvidenceStatus).toBe('interrupted')
  })
})
