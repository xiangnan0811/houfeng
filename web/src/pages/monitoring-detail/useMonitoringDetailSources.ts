import { useEffect, useRef, useState } from 'react'
import type { SetURLSearchParams } from 'react-router-dom'

import {
  ApiError,
  getMonitoringInstance,
  getMonitoringInstanceRuntimeFacts,
  getSettings,
  monitoringInstanceRuntimeStreamURL,
} from '../../lib/api'
import { listEvents, listIncidents } from '../../lib/observabilityApi'
import { resolveThresholds, type MetricThresholds } from '../../config/thresholds'
import type { HostSample, HostSampleStreamMessage, OnlineEvidenceStreamMessage } from '../../lib/types'
import { readHeartbeatFreshnessPolicy } from '../monitoring/heartbeatFreshness'
import type { HeartbeatFreshnessPolicy } from '../monitoring/types'
import { INITIAL_MONITORING_DETAIL_STATE, describeError } from './monitoringDetailHelpers'
import {
  applyHttpLatestSample,
  mergeLatestHostSample,
  parseReadAt,
  parseTimeWindow,
  runtimeFactsMatchWindow,
} from './runtimeObservation'
import type { MonitoringDetailPageState, OnlineEvidenceStatus, RuntimeStreamStatus, TimeWindow } from './types'
import { acceptOnlineEvidence } from './onlineEvidence'

const REALTIME_WINDOW_MS = 60 * 60 * 1000
const RUNTIME_STREAM_RECONNECT_MS = 2000
const ONLINE_EVIDENCE_TIMEOUT_MS = 15000

function sampleKey(sample: HostSample): string {
  return `${sample.observed_at}::${sample.sync_batch_id}`
}

function realtimeSeedSamples(runtimeFacts: { recent_host_samples?: HostSample[]; latest_host_sample?: HostSample | null }): HostSample[] {
  if (runtimeFacts.recent_host_samples?.length) return runtimeFacts.recent_host_samples
  return runtimeFacts.latest_host_sample ? [runtimeFacts.latest_host_sample] : []
}

function mergeRealtimeSamples(current: HostSample[], incoming: HostSample[]): HostSample[] {
  if (incoming.length === 0) return current
  const byKey = new Map<string, HostSample>()
  for (const sample of current) byKey.set(sampleKey(sample), sample)
  for (const sample of incoming) byKey.set(sampleKey(sample), sample)
  const sorted = [...byKey.values()].sort((a, b) => {
    const timeDiff = new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime()
    if (timeDiff !== 0) return timeDiff
    return a.sync_batch_id.localeCompare(b.sync_batch_id)
  })
  const newestTime = sorted.reduce((max, sample) => {
    const ms = new Date(sample.observed_at).getTime()
    return Number.isNaN(ms) ? max : Math.max(max, ms)
  }, 0)
  if (newestTime <= 0) return sorted
  const cutoff = newestTime - REALTIME_WINDOW_MS
  return sorted.filter((sample) => {
    const ms = new Date(sample.observed_at).getTime()
    return Number.isNaN(ms) || ms >= cutoff
  })
}

type UseMonitoringDetailSourcesArgs = {
  monitoringInstanceId?: string
  searchParams: URLSearchParams
  setSearchParams: SetURLSearchParams
  locationState: unknown
}

export function useMonitoringDetailSources({
  monitoringInstanceId,
  searchParams,
  setSearchParams,
  locationState,
}: UseMonitoringDetailSourcesArgs) {
  const timeWindow = parseTimeWindow(searchParams.get('window'))
  const [state, setState] = useState<MonitoringDetailPageState>(INITIAL_MONITORING_DETAIL_STATE)
  const [recordReloadKey, setRecordReloadKey] = useState(0)
  const [runtimeReloadKey, setRuntimeReloadKey] = useState(0)
  const [settingsReloadKey, setSettingsReloadKey] = useState(0)
  const [incidentsReloadKey, setIncidentsReloadKey] = useState(0)
  const [eventsReloadKey, setEventsReloadKey] = useState(0)
  const [realtimeSamples, setRealtimeSamples] = useState<HostSample[]>([])
  const [runtimeStreamStatus, setRuntimeStreamStatus] = useState<RuntimeStreamStatus>('idle')
  const [runtimeStreamError, setRuntimeStreamError] = useState<string | null>(null)
  const [streamReloadKey, setStreamReloadKey] = useState(0)
  const [observationEpoch, setObservationEpoch] = useState(0)
  const observationEpochRef = useRef(0)
  const [onlineEvidence, setOnlineEvidence] = useState<OnlineEvidenceStreamMessage | null>(null)
  const [onlineEvidenceStatus, setOnlineEvidenceStatus] = useState<OnlineEvidenceStatus>('connecting')
  const onlineEvidenceRef = useRef<OnlineEvidenceStreamMessage | null>(null)
  const evidenceEstablishedRef = useRef(false)
  const [thresholds, setThresholds] = useState<MetricThresholds | null>(null)
  const [heartbeatPolicy, setHeartbeatPolicy] = useState<HeartbeatFreshnessPolicy | null>(null)
  const [settingsResolved, setSettingsResolved] = useState(false)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [latestSample, setLatestSample] = useState<HostSample | null>(null)
  const [snapshotReadAt, setSnapshotReadAt] = useState<Date | null>(null)
  const [loadedTimeWindow, setLoadedTimeWindow] = useState<TimeWindow | null>(null)
  const [runtimeFactsError, setRuntimeFactsError] = useState<string | null>(null)
  const [runtimeFactsLoading, setRuntimeFactsLoading] = useState(false)
  const [incidentsRetrying, setIncidentsRetrying] = useState(false)
  const [eventsRetrying, setEventsRetrying] = useState(false)
  const runtimeRequestRef = useRef(0)
  const latestSampleRef = useRef<HostSample | null>(null)
  const currentRouteMonitoringInstanceIdRef = useRef<string | null>(monitoringInstanceId ?? null)
  const isMountedRef = useRef(true)

  useEffect(() => {
    currentRouteMonitoringInstanceIdRef.current = monitoringInstanceId ?? null
  }, [monitoringInstanceId])

  useEffect(() => {
    isMountedRef.current = true
    return () => {
      isMountedRef.current = false
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- route identity reset: the previous instance's stream and sample state must be dropped before the new route's requests start
    setRealtimeSamples([])
    setRuntimeStreamStatus('idle')
    setRuntimeStreamError(null)
    setOnlineEvidence(null)
    onlineEvidenceRef.current = null
    evidenceEstablishedRef.current = false
    setOnlineEvidenceStatus('connecting')
    setLatestSample(null)
    latestSampleRef.current = null
    setSnapshotReadAt(null)
    setLoadedTimeWindow(null)
    setRuntimeFactsError(null)
  }, [monitoringInstanceId])

  useEffect(() => {
    let cancelled = false
    getSettings()
      .then((settings) => {
        if (cancelled) return
        const policy = readHeartbeatFreshnessPolicy(settings.incident_defaults)
        setHeartbeatPolicy(policy)
        setThresholds(resolveThresholds(settings.incident_defaults))
        setSettingsError(policy ? null : '新鲜度策略不可用')
        setSettingsResolved(true)
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setHeartbeatPolicy(null)
        setThresholds(null)
        setSettingsError(describeError(error, '新鲜度策略不可用'))
        setSettingsResolved(true)
      })
    return () => {
      cancelled = true
    }
  }, [settingsReloadKey])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) return
    getMonitoringInstance(monitoringInstanceId)
      .then((monitoringInstance) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedMonitoringInstanceId: monitoringInstanceId,
          error: null,
          monitoringInstance,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '监控实例不存在'
            : describeError(error, '加载监控实例详情失败')
        setState((current) => ({
          ...current,
          requestedMonitoringInstanceId: monitoringInstanceId,
          error: message,
          monitoringInstance: null,
        }))
      })
    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, recordReloadKey])

  useEffect(() => {
    if (!monitoringInstanceId) return
    const requestedWindow = timeWindow
    const requestId = ++runtimeRequestRef.current
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- window fetch gate: the loading flag is the network gate and must be set before the request resolves
    setRuntimeFactsLoading(true)
    setRuntimeFactsError(null)
    getMonitoringInstanceRuntimeFacts(monitoringInstanceId, requestedWindow)
      .then((runtimeFacts) => {
        if (cancelled || runtimeRequestRef.current !== requestId) return
        if (runtimeFacts.monitoring_instance_id !== monitoringInstanceId) return
        if (!runtimeFactsMatchWindow(runtimeFacts, requestedWindow)) {
          setRuntimeFactsError('运行指标窗口与请求不一致')
          setRuntimeFactsLoading(false)
          return
        }
        setState((current) => ({ ...current, runtimeFacts }))
        setLoadedTimeWindow(requestedWindow)
        setRuntimeFactsError(null)
        setRuntimeFactsLoading(false)
        const readAt = parseReadAt(runtimeFacts.read_at)
        setSnapshotReadAt(readAt)
        const currentLatest = latestSampleRef.current
        const nextLatest = applyHttpLatestSample(
          currentLatest,
          runtimeFacts.latest_host_sample,
          monitoringInstanceId,
          runtimeFacts.read_at,
        )
        latestSampleRef.current = nextLatest
        setLatestSample(nextLatest)
        if (requestedWindow === 'realtime') {
          if (nextLatest == null) setRealtimeSamples([])
          else if (nextLatest === currentLatest && currentLatest != null) {
            // The stream can arrive before the initial HTTP history. Keep its
            // newer latest sample while still filling the requested hour.
            setRealtimeSamples((current) => mergeRealtimeSamples(realtimeSeedSamples(runtimeFacts), current))
          } else {
            setRealtimeSamples(realtimeSeedSamples(runtimeFacts))
          }
        }
      })
      .catch((error: unknown) => {
        if (cancelled || runtimeRequestRef.current !== requestId) return
        setRuntimeFactsError(describeError(error, '加载运行指标失败'))
        setRuntimeFactsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, timeWindow, runtimeReloadKey])

  useEffect(() => {
    if (!monitoringInstanceId) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- new connection must receive evidence before restoring a current conclusion
    setOnlineEvidenceStatus('connecting')
    if (typeof WebSocket === 'undefined') {
      setRuntimeStreamStatus('disconnected')
      setRuntimeStreamError('当前浏览器不支持 WebSocket')
      setOnlineEvidenceStatus('unavailable')
      return
    }

    let closed = false
    const connectionEpoch = observationEpochRef.current
    let socket: WebSocket | null = null
    let reconnectTimer: number | undefined
    let firstSuccessfulOpenAt: number | null = null
    let connectionOpenedAt: number | null = null
    let lastEvidenceAt: number | null = null
    let unavailable = false

    // Reuse the window request gate so disconnect reads, manual retry and
    // binding resets cannot race or silently swallow a chart error.
    const refreshSnapshot = () => setRuntimeReloadKey((key) => key + 1)

    const interrupt = () => {
      if (closed || unavailable) return
      setOnlineEvidenceStatus('interrupted')
      setRuntimeStreamStatus('disconnected')
      const previous = socket
      socket = null // Retire this generation before close can dispatch another event.
      previous?.close()
      if (timeWindow === 'realtime') refreshSnapshot()
      if (evidenceEstablishedRef.current && reconnectTimer === undefined) {
        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined
          connect(true)
        }, RUNTIME_STREAM_RECONNECT_MS)
      }
    }

    const checkExpiry = () => {
      if (closed || unavailable) return
      const now = Date.now()
      if (!evidenceEstablishedRef.current && firstSuccessfulOpenAt !== null && now - firstSuccessfulOpenAt >= ONLINE_EVIDENCE_TIMEOUT_MS) {
        unavailable = true
        setOnlineEvidenceStatus('unavailable')
        setRuntimeStreamStatus('disconnected')
        if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer)
        const previous = socket
        socket = null
        previous?.close()
      } else if (socket && connectionOpenedAt !== null && now - (lastEvidenceAt ?? connectionOpenedAt) >= ONLINE_EVIDENCE_TIMEOUT_MS) {
        interrupt()
      }
    }

    const connect = (reconnect: boolean) => {
      if (closed || unavailable) return
      setRuntimeStreamStatus(reconnect ? 'reconnecting' : 'connecting')
      setRuntimeStreamError(null)
      connectionOpenedAt = null
      lastEvidenceAt = null
      const connection = new WebSocket(monitoringInstanceRuntimeStreamURL(monitoringInstanceId, globalThis.location.origin, timeWindow === 'realtime'))
      socket = connection
      const isCurrent = () => !closed && socket === connection && observationEpochRef.current === connectionEpoch && currentRouteMonitoringInstanceIdRef.current === monitoringInstanceId
      connection.onopen = () => {
        if (!isCurrent()) return
        connectionOpenedAt = Date.now()
        if (firstSuccessfulOpenAt === null) firstSuccessfulOpenAt = connectionOpenedAt
        setRuntimeStreamStatus('connected')
        setRuntimeStreamError(null)
      }
      connection.onmessage = (event) => {
        if (!isCurrent()) return
        try {
          const message = JSON.parse(String(event.data)) as HostSampleStreamMessage
          if (message.type as string === 'online_evidence') {
            const next = acceptOnlineEvidence(onlineEvidenceRef.current, message, monitoringInstanceId)
            if (next === onlineEvidenceRef.current) return
            onlineEvidenceRef.current = next
            setOnlineEvidence(next)
            evidenceEstablishedRef.current = true
            lastEvidenceAt = Date.now()
            setOnlineEvidenceStatus('current')
            return
          }
          if (timeWindow !== 'realtime') return
          if (message.type !== 'host_sample' || message.monitoring_instance_id !== monitoringInstanceId) return
          const sample = message.sample
          setLatestSample((current) => {
            const next = mergeLatestHostSample(current, sample, monitoringInstanceId, { allowBackfilled: false })
            latestSampleRef.current = next
            return next
          })
          setRealtimeSamples((current) => {
            const next = mergeLatestHostSample(current.at(-1) ?? null, sample, monitoringInstanceId, { allowBackfilled: false })
            if (next !== sample) return current
            return mergeRealtimeSamples(current, [sample])
          })
          setState((current) => {
            if (current.requestedMonitoringInstanceId !== monitoringInstanceId || !current.runtimeFacts) return current
            const latest = mergeLatestHostSample(current.runtimeFacts.latest_host_sample, sample, monitoringInstanceId, { allowBackfilled: false })
            return {
              ...current,
              runtimeFacts: {
                ...current.runtimeFacts,
                latest_host_sample: latest,
                recent_host_samples: latest === sample
                  ? mergeRealtimeSamples(current.runtimeFacts.recent_host_samples ?? [], [sample])
                  : current.runtimeFacts.recent_host_samples ?? [],
              },
            }
          })
        } catch {
          setRuntimeStreamError('实时数据解析失败')
        }
      }
      connection.onerror = () => {
        if (!isCurrent()) return
        setRuntimeStreamError('实时连接异常')
        interrupt()
      }
      connection.onclose = () => {
        if (!isCurrent()) return
        interrupt()
      }
    }

    connect(false)
    const expiryTimer = window.setInterval(checkExpiry, 1000)
    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') checkExpiry()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      closed = true
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer)
      window.clearInterval(expiryTimer)
      document.removeEventListener('visibilitychange', onVisibilityChange)
      socket?.close()
    }
  }, [monitoringInstanceId, observationEpoch, streamReloadKey, timeWindow])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- retry gate: the retrying flag is set before the reload request starts
    if (incidentsReloadKey > 0) setIncidentsRetrying(true)
    listIncidents({ object_type: 'monitoring_instance', object_id: monitoringInstanceId })
      .then((records) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedIncidentsMonitoringInstanceId: monitoringInstanceId,
          incidents: records,
          incidentsError: null,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedIncidentsMonitoringInstanceId: monitoringInstanceId,
          incidents: current.requestedIncidentsMonitoringInstanceId === monitoringInstanceId ? current.incidents : [],
          incidentsError: describeError(error, '加载活跃异常失败'),
        }))
      })
      .finally(() => {
        if (!cancelled) setIncidentsRetrying(false)
      })
    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, incidentsReloadKey])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- retry gate: the retrying flag is set before the reload request starts
    if (eventsReloadKey > 0) setEventsRetrying(true)
    listEvents({ object_type: 'monitoring_instance', object_id: monitoringInstanceId })
      .then((records) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedEventsMonitoringInstanceId: monitoringInstanceId,
          events: records,
          eventsError: null,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedEventsMonitoringInstanceId: monitoringInstanceId,
          events: current.requestedEventsMonitoringInstanceId === monitoringInstanceId ? current.events : [],
          eventsError: describeError(error, '加载相关事件失败'),
        }))
      })
      .finally(() => {
        if (!cancelled) setEventsRetrying(false)
      })
    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, eventsReloadKey])

  function setTimeWindow(next: TimeWindow) {
    const params = new URLSearchParams(searchParams)
    if (next === '24h') params.delete('window')
    else params.set('window', next)
    if (next !== 'realtime') setRealtimeSamples([])
    setSearchParams(params, { replace: true, state: locationState })
  }

  function resetObservationEpoch() {
    observationEpochRef.current += 1
    onlineEvidenceRef.current = null
    evidenceEstablishedRef.current = false
    setOnlineEvidence(null)
    setOnlineEvidenceStatus('connecting')
    setObservationEpoch((key) => key + 1)
    runtimeRequestRef.current += 1
    latestSampleRef.current = null
    setLatestSample(null)
    setRealtimeSamples([])
    setLoadedTimeWindow(null)
    setRuntimeFactsError(null)
    setRuntimeReloadKey((key) => key + 1)
  }

  return {
    timeWindow,
    setTimeWindow,
    state,
    setState,
    latestSample,
    snapshotReadAt,
    loadedTimeWindow,
    runtimeFactsError,
    runtimeFactsLoading,
    realtimeSamples,
    runtimeStreamStatus,
    runtimeStreamError,
    onlineEvidence,
    onlineEvidenceStatus,
    retryOnlineEvidence: () => setStreamReloadKey((key) => key + 1),
    thresholds,
    heartbeatPolicy,
    settingsResolved,
    settingsError,
    incidentsRetrying,
    eventsRetrying,
    retryRecord: () => setRecordReloadKey((key) => key + 1),
    retryRuntime: () => setRuntimeReloadKey((key) => key + 1),
    retrySettings: () => setSettingsReloadKey((key) => key + 1),
    retryIncidents: () => setIncidentsReloadKey((key) => key + 1),
    retryEvents: () => setEventsReloadKey((key) => key + 1),
    resetObservationEpoch,
    isMountedRef,
    currentRouteMonitoringInstanceIdRef,
  }
}
