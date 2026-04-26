import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  archiveTarget,
  enterTargetMaintenance,
  exitTargetMaintenance,
  getTarget,
  getTargetRuntimeFacts,
  listEvents,
  listIncidents,
  listTargetProbeItems,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
} from '../lib/api'
import {
  formatConfigSummary,
  formatDateTime,
  formatLabelList,
  formatLatency,
} from '../lib/format'
import type {
  ActiveIncidentRecord,
  ProbeItemRecord,
  ProbeObservation,
  StateChangeEventRecord,
  TargetRecord,
  TargetRuntimeFacts,
} from '../lib/types'

type State = {
  requestedTargetId: string | null
  error: string | null
  target: TargetRecord | null
  probeItems: ProbeItemRecord[]
  runtimeFacts: TargetRuntimeFacts | null
  requestedActivityTargetId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

const TARGET_PAUSE_CONFIRM_MESSAGE = '暂停会停止采集并产生数据空档，确定继续吗？'
const TARGET_ARCHIVE_CONFIRM_MESSAGE = '归档会让目标退出当前工作集，但会保留历史记录，确定继续吗？'

type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  if (target.run_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '暂停') {
    return [
      { action: 'resume', label: '恢复' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '已归档') {
    return [{ action: 'restore-to-paused', label: '恢复到暂停' }]
  }

  return []
}

export function TargetDetailPage() {
  const { targetId } = useParams()
  return <TargetDetailPageContent key={targetId ?? 'missing-target'} targetId={targetId} />
}

function TargetDetailPageContent({ targetId }: { targetId?: string }) {
  const [state, setState] = useState<State>({
    requestedTargetId: null,
    error: null,
    target: null,
    probeItems: [],
    runtimeFacts: null,
    requestedActivityTargetId: null,
    incidents: [],
    incidentsError: null,
    events: [],
    eventsError: null,
  })
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const currentRouteTargetIdRef = useRef<string | null>(targetId ?? null)
  const currentRequestedTargetIdRef = useRef<string | null>(null)
  const isMountedRef = useRef(true)

  useEffect(() => {
    currentRequestedTargetIdRef.current = state.requestedTargetId
  }, [state.requestedTargetId])

  useEffect(
    () => () => {
      isMountedRef.current = false
    },
    [],
  )

  useEffect(() => {
    let cancelled = false
    if (!targetId) return

    Promise.all([
      getTarget(targetId),
      listTargetProbeItems(targetId),
      getTargetRuntimeFacts(targetId),
    ])
      .then(([target, probeItems, runtimeFacts]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedTargetId: targetId,
          error: null,
          target,
          probeItems,
          runtimeFacts,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '目标不存在'
            : describeError(error, '加载目标详情失败')
        setState((current) => ({
          ...current,
          requestedTargetId: targetId,
          error: message,
          target: null,
          probeItems: [],
          runtimeFacts: null,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [targetId])

  useEffect(() => {
    let cancelled = false
    if (!targetId) return

    Promise.allSettled([
      listIncidents({ object_type: 'target', object_id: targetId }),
      listEvents({ object_type: 'target', object_id: targetId }),
    ]).then(([incidentsResult, eventsResult]) => {
      if (cancelled) return
      setState((current) => ({
        ...current,
        requestedActivityTargetId: targetId,
        incidents:
          incidentsResult.status === 'fulfilled' ? incidentsResult.value : [],
        incidentsError:
          incidentsResult.status === 'fulfilled'
            ? null
            : describeError(incidentsResult.reason, '加载活跃异常失败'),
        events: eventsResult.status === 'fulfilled' ? eventsResult.value : [],
        eventsError:
          eventsResult.status === 'fulfilled'
            ? null
            : describeError(eventsResult.reason, '加载相关事件失败'),
      }))
    })

    return () => {
      cancelled = true
    }
  }, [targetId])

  const observationsByProbe = useMemo(() => {
    const map = new Map<string, ProbeObservation[]>()
    for (const observation of state.runtimeFacts?.latest_probe_observations ?? []) {
      const existing = map.get(observation.probe_item_id) ?? []
      existing.push(observation)
      map.set(observation.probe_item_id, existing)
    }
    return map
  }, [state.runtimeFacts])

  const missingTargetId = !targetId
  const isCurrentTarget = state.requestedTargetId === targetId
  const hasCurrentActivity = state.requestedActivityTargetId === targetId
  const error = isCurrentTarget ? state.error : null
  const target = isCurrentTarget ? state.target : null
  const probeItems = isCurrentTarget ? state.probeItems : []
  const incidents = hasCurrentActivity ? state.incidents : []
  const incidentsError = hasCurrentActivity ? state.incidentsError : null
  const events = hasCurrentActivity ? state.events : []
  const eventsError = hasCurrentActivity ? state.eventsError : null

  async function handleRuntimeAction(action: TargetRuntimeAction) {
    if (!target) return
    if (action === 'pause' && !window.confirm(TARGET_PAUSE_CONFIRM_MESSAGE)) {
      return
    }
    if (action === 'archive' && !window.confirm(TARGET_ARCHIVE_CONFIRM_MESSAGE)) {
      return
    }

    const actionTargetId = target.target_id
    setRuntimeSubmitting(true)
    setRuntimeError(null)

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterTargetMaintenance(actionTargetId)
          : action === 'exit-maintenance'
            ? await exitTargetMaintenance(actionTargetId)
            : action === 'pause'
              ? await pauseTarget(actionTargetId)
              : action === 'resume'
                ? await resumeTarget(actionTargetId)
                : action === 'archive'
                ? await archiveTarget(actionTargetId)
                  : await restoreTargetToPaused(actionTargetId)
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        target: updated,
      }))
    } catch (error) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId
      ) {
        return
      }
      setRuntimeError(describeError(error, '目标运行控制操作失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId
      ) {
        setRuntimeSubmitting(false)
      }
    }
  }

  if (!missingTargetId && !isCurrentTarget) {
    return <section className="page-panel">正在加载目标详情…</section>
  }

  if (missingTargetId || error || !target) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Target Detail</p>
        <h2 className="page-panel__title">目标详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到目标'}</p>
        <Link className="text-link" to="/targets">
          返回目标列表
        </Link>
      </section>
    )
  }

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">Target Detail</p>
          <h2 className="hero-panel__title">{target.name}</h2>
          <p className="hero-panel__description">
            {target.target_type} · {target.host}
            {target.base_port ? `:${target.base_port}` : ''}
          </p>
          <div className="badge-row">
            <StatusBadge label={target.run_status} />
            <StatusBadge label={target.current_health_status} />
            <StatusBadge label={target.target_type} />
          </div>
        </div>
        <div className="hero-panel__meta">
          <div className="hero-meta-card">
            <span>标签</span>
            <strong>{formatLabelList(target.labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>执行节点标签</span>
            <strong>{formatLabelList(target.execution_node_labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近成功</span>
            <strong>{formatDateTime(target.last_success_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近失败</span>
            <strong>{formatDateTime(target.last_failure_at)}</strong>
          </div>
        </div>
      </section>

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">健康状态</p>
          <p className="summary-card__value">{target.current_health_status}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">ProbeItem 数量</p>
          <p className="summary-card__value">{probeItems.length}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">当前主问题</p>
          <p className="summary-card__value summary-card__value--text">
            {target.current_primary_issue_summary || '暂无明显异常'}
          </p>
        </article>
      </div>

      <DetailSection eyebrow="Runtime Control" title="运行控制">
        <div className="page-stack">
          <p>维护会继续采集，但不解释结果。暂停会停止采集并产生数据空档。归档会退出当前工作集并保留历史。</p>
          <div className="badge-row badge-row--wrap">
            {targetRuntimeActions(target).map(({ action, label }) => (
              <button
                key={action}
                type="button"
                disabled={runtimeSubmitting}
                onClick={() => void handleRuntimeAction(action)}
              >
                {label}
              </button>
            ))}
          </div>
          {runtimeError ? <p>{runtimeError}</p> : null}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Probe Items" title="ProbeItem 列表">
        {probeItems.length === 0 ? (
          <div className="empty-state">
            <h3>当前还没有 ProbeItem</h3>
            <p>当前还没有 ProbeItem，请为该入口添加至少一种观测方式。</p>
          </div>
        ) : (
          <div className="probe-list">
            {probeItems.map((probeItem) => {
              const observations = observationsByProbe.get(probeItem.probe_item_id) ?? []
              return (
                <article key={probeItem.probe_item_id} className="probe-card">
                  <header className="probe-card__header">
                    <div>
                      <h3>{probeItem.probe_kind.toUpperCase()}</h3>
                      <p>{formatConfigSummary(probeItem.config)}</p>
                    </div>
                    <div className="badge-row">
                      <StatusBadge label={probeItem.enabled ? '启用' : '停用'} />
                      <StatusBadge label={probeItem.frequency_tier} tone="cyan" />
                    </div>
                  </header>

                  <dl className="probe-card__meta">
                    <div>
                      <dt>超时</dt>
                      <dd>{probeItem.timeout_seconds}s</dd>
                    </div>
                    <div>
                      <dt>最近观测</dt>
                      <dd>
                        {observations.length > 0
                          ? formatDateTime(observations[0].observed_at)
                          : '尚无观测结果'}
                      </dd>
                    </div>
                  </dl>

                  {observations.length > 0 ? (
                    <div className="observation-list">
                      {observations.map((observation) => (
                        <div
                          key={`${observation.probe_item_id}-${observation.node_id}`}
                          className="observation-row"
                        >
                          <div>
                            <strong>{observation.node_id}</strong>
                            <p>{formatDateTime(observation.observed_at)}</p>
                          </div>
                          <div>
                            <StatusBadge
                              label={
                                observation.result_kind === 'success' ? '成功' : '失败'
                              }
                              tone={
                                observation.result_kind === 'success' ? 'green' : 'red'
                              }
                            />
                          </div>
                          <div>
                            <span>Latency</span>
                            <strong>{formatLatency(observation.latency_ms)}</strong>
                          </div>
                          <div>
                            <span>HTTP / TLS</span>
                            <strong>
                              {observation.http_status ?? observation.tls_expiry_days ?? '—'}
                            </strong>
                          </div>
                          <div>
                            <span>错误摘要</span>
                            <strong>
                              {observation.error_summary || observation.error_code || '—'}
                            </strong>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="empty-inline">尚无观测结果</div>
                  )}
                </article>
              )
            })}
          </div>
        )}
      </DetailSection>

      <DetailSection eyebrow="Incidents" title="当前活跃异常">
        {!hasCurrentActivity ? (
          <div className="empty-state">
            <h3>正在加载活跃异常…</h3>
            <p>等待目标相关的 incident 读模型返回最新结果。</p>
          </div>
        ) : incidentsError ? (
          <div className="empty-state">
            <h3>活跃异常暂不可用</h3>
            <p>{incidentsError}</p>
          </div>
        ) : (
          <IncidentList incidents={incidents} />
        )}
      </DetailSection>

      <DetailSection eyebrow="Events" title="最近相关事件">
        {!hasCurrentActivity ? (
          <div className="empty-state">
            <h3>正在加载相关事件…</h3>
            <p>等待目标相关的事件流返回最新记录。</p>
          </div>
        ) : eventsError ? (
          <div className="empty-state">
            <h3>相关事件暂不可用</h3>
            <p>{eventsError}</p>
          </div>
        ) : (
          <EventList events={events} />
        )}
      </DetailSection>
    </div>
  )
}
