import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  getTarget,
  getTargetRuntimeFacts,
  listTargetProbeItems,
} from '../lib/api'
import {
  formatConfigSummary,
  formatDateTime,
  formatLabelList,
  formatLatency,
} from '../lib/format'
import type {
  ProbeItemRecord,
  ProbeObservation,
  TargetRecord,
  TargetRuntimeFacts,
} from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  target: TargetRecord | null
  probeItems: ProbeItemRecord[]
  runtimeFacts: TargetRuntimeFacts | null
}

export function TargetDetailPage() {
  const { targetId } = useParams()
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    target: null,
    probeItems: [],
    runtimeFacts: null,
  })

  useEffect(() => {
    let cancelled = false
    if (!targetId) {
      setState({
        loading: false,
        error: '目标不存在',
        target: null,
        probeItems: [],
        runtimeFacts: null,
      })
      return
    }

    setState((current) => ({ ...current, loading: true, error: null }))
    Promise.all([
      getTarget(targetId),
      listTargetProbeItems(targetId),
      getTargetRuntimeFacts(targetId),
    ])
      .then(([target, probeItems, runtimeFacts]) => {
        if (cancelled) return
        setState({ loading: false, error: null, target, probeItems, runtimeFacts })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '目标不存在'
            : error instanceof Error
              ? error.message
              : '加载目标详情失败'
        setState({
          loading: false,
          error: message,
          target: null,
          probeItems: [],
          runtimeFacts: null,
        })
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

  if (state.loading) {
    return <section className="page-panel">正在加载目标详情…</section>
  }

  if (state.error || !state.target) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Target Detail</p>
        <h2 className="page-panel__title">目标详情不可用</h2>
        <p className="page-panel__description">{state.error ?? '未找到目标'}</p>
        <Link className="text-link" to="/targets">
          返回目标列表
        </Link>
      </section>
    )
  }

  const target = state.target

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
          <p className="summary-card__value">{state.probeItems.length}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">当前主问题</p>
          <p className="summary-card__value summary-card__value--text">
            {target.current_primary_issue_summary || '暂无明显异常'}
          </p>
        </article>
      </div>

      <DetailSection eyebrow="Probe Items" title="ProbeItem 列表">
        {state.probeItems.length === 0 ? (
          <div className="empty-state">
            <h3>当前还没有 ProbeItem</h3>
            <p>当前还没有 ProbeItem，请为该入口添加至少一种观测方式。</p>
          </div>
        ) : (
          <div className="probe-list">
            {state.probeItems.map((probeItem) => {
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

      <DetailSection eyebrow="Reserved" title="趋势与事件">
        <div className="placeholder-stack">
          <div className="placeholder-card">
            <h3>趋势视图</h3>
            <p>当前切片只接入最新原始观测结果，趋势图将在后续切片补齐。</p>
          </div>
          <div className="placeholder-card">
            <h3>事件流</h3>
            <p>事件与 incident 仍由后续切片接入，这里先保留版位。</p>
          </div>
        </div>
      </DetailSection>
    </div>
  )
}
