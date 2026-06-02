import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { MonitoringInstanceWatchtowerMetrics } from '../components/monitoring-detail'
import { Hostname, MonoDigits, StatusGlyph, Timestamp, type HealthState } from '../components/atoms'
import { PageState } from '../components/PageState'
import { StatusBadge } from '../components/StatusBadge'
import { ApiError, getMonitoringInstance, getMonitoringInstanceRuntimeFacts } from '../lib/api'
import type { MonitoringInstanceRecord, MonitoringInstanceRuntimeFacts } from '../lib/types'

type MonitoringInstanceState = {
  loading: boolean
  error: string | null
  monitoringInstance: MonitoringInstanceRecord | null
  runtimeFacts: MonitoringInstanceRuntimeFacts | null
}

type StoredMonitoringInstanceState = MonitoringInstanceState & {
  monitoringInstanceId: string | null
}

type CompareSide = 'left' | 'right'

function useMonitoringInstanceData(monitoringInstanceId: string | null): MonitoringInstanceState {
  const [state, setState] = useState<StoredMonitoringInstanceState>(() => ({
    monitoringInstanceId,
    loading: !!monitoringInstanceId,
    error: monitoringInstanceId ? null : '缺少监控实例 ID',
    monitoringInstance: null,
    runtimeFacts: null,
  }))

  useEffect(() => {
    if (!monitoringInstanceId) return
    let cancelled = false

    Promise.all([getMonitoringInstance(monitoringInstanceId), getMonitoringInstanceRuntimeFacts(monitoringInstanceId)])
      .then(([monitoringInstance, runtimeFacts]) => {
        if (cancelled) return
        setState({ monitoringInstanceId, loading: false, error: null, monitoringInstance, runtimeFacts })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '监控实例不存在'
            : error instanceof Error
              ? error.message
              : '加载失败'
        setState({ monitoringInstanceId, loading: false, error: message, monitoringInstance: null, runtimeFacts: null })
      })

    return () => { cancelled = true }
  }, [monitoringInstanceId])

  if (!monitoringInstanceId) {
    return { loading: false, error: '缺少监控实例 ID', monitoringInstance: null, runtimeFacts: null }
  }
  if (state.monitoringInstanceId !== monitoringInstanceId) {
    return { loading: true, error: null, monitoringInstance: null, runtimeFacts: null }
  }

  return state
}

export function MonitoringComparePage() {
  const [searchParams] = useSearchParams()
  const ids = searchParams.getAll('id')
  const idA = ids[0] ?? null
  const idB = ids[1] ?? null

  const monitoringInstanceA = useMonitoringInstanceData(idA)
  const monitoringInstanceB = useMonitoringInstanceData(idB)

  if (ids.length < 2) {
    return (
      <PageState
        kind="empty"
        eyebrow="监控实例对比"
        title="需要选择 2 个监控实例"
        description="请先在监控实例列表勾选两个监控实例，再进入 A / B 指标对比。"
        action={<Link className="btn md secondary" to="/monitoring">返回监控实例列表</Link>}
      />
    )
  }

  return (
    <div className="page-stack">
      <CompareCommandPanel stateA={monitoringInstanceA} stateB={monitoringInstanceB} />

      <div className="compare-identity">
        <CompareMonitoringInstanceIdentity state={monitoringInstanceA} side="left" />
        <CompareMonitoringInstanceIdentity state={monitoringInstanceB} side="right" />
      </div>

      <CompareSummaryStrip stateA={monitoringInstanceA} stateB={monitoringInstanceB} />

      <DetailSection
        eyebrow="24h runtime facts"
        title="主机指标对比"
        aside="详细趋势仍使用 MonitoringInstanceWatchtowerMetrics"
      >
        <div className="compare-metrics">
          <div className="compare-metrics__col">
            {!monitoringInstanceA.loading && !monitoringInstanceA.error && monitoringInstanceA.runtimeFacts ? (
              <MonitoringInstanceWatchtowerMetrics
                sample={monitoringInstanceA.runtimeFacts.latest_host_sample ?? null}
                metricPoints={monitoringInstanceA.runtimeFacts.host_metric_points ?? []}
                timeWindow="24h"
                window={monitoringInstanceA.runtimeFacts.window}
              />
            ) : (
              <CompareColumnPlaceholder state={monitoringInstanceA} />
            )}
          </div>
          <div className="compare-metrics__col">
            {!monitoringInstanceB.loading && !monitoringInstanceB.error && monitoringInstanceB.runtimeFacts ? (
              <MonitoringInstanceWatchtowerMetrics
                sample={monitoringInstanceB.runtimeFacts.latest_host_sample ?? null}
                metricPoints={monitoringInstanceB.runtimeFacts.host_metric_points ?? []}
                timeWindow="24h"
                window={monitoringInstanceB.runtimeFacts.window}
              />
            ) : (
              <CompareColumnPlaceholder state={monitoringInstanceB} />
            )}
          </div>
        </div>
      </DetailSection>
    </div>
  )
}

function sideLabel(side: CompareSide): 'A' | 'B' {
  return side === 'left' ? 'A' : 'B'
}

function monitoringInstanceHealthGlyphState(monitoringInstance: MonitoringInstanceRecord): HealthState {
  if (monitoringInstance.monitoring_status === '维护中') return 'maintenance'
  if (monitoringInstance.monitoring_status === '暂停') return 'offline'
  if (monitoringInstance.current_health_status === '正常') return 'normal'
  if (monitoringInstance.current_health_status === '关注') return 'notice'
  if (monitoringInstance.current_health_status === '告警') return 'alert'
  if (monitoringInstance.current_health_status === '严重') return 'critical'
  return 'offline'
}

function monitoringInstanceContext(monitoringInstance: MonitoringInstanceRecord): string {
  const parts = [monitoringInstance.group, monitoringInstance.provider, monitoringInstance.region, monitoringInstance.city].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '位置上下文未标记'
}

function CompareCommandPanel({ stateA, stateB }: { stateA: MonitoringInstanceState; stateB: MonitoringInstanceState }) {
  return (
    <section className="compare-command" aria-labelledby="monitoringInstance-compare-title">
      <div className="compare-command__intro">
        <p className="compare-command__eyebrow">监控实例对比 · 24h runtime facts</p>
        <h1 id="monitoringInstance-compare-title">判断两个监控实例是否需要深入排查</h1>
        <p>
          先对齐 A/B 的身份、健康、运行态、绑定态、位置与样本可用性；只有差异明显时再下钻详细主机指标。
        </p>
      </div>
      <div className="compare-command__aside">
        <div className="compare-command__selection" aria-label="当前对比对象">
          <CompareCommandPeer state={stateA} side="left" />
          <CompareCommandPeer state={stateB} side="right" />
        </div>
        <Link className="btn md ghost" to="/monitoring">返回监控实例列表</Link>
      </div>
    </section>
  )
}

function CompareCommandPeer({ state, side }: { state: MonitoringInstanceState; side: CompareSide }) {
  const label = sideLabel(side)
  if (state.loading) {
    return (
      <article className="compare-command-peer">
        <span className="compare-command-peer__side">{label}</span>
        <div className="compare-command-peer__body">
          <span className="compare-command-peer__label">{label} 监控实例</span>
          <strong>读取中</strong>
          <span>正在读取身份与运行事实</span>
        </div>
      </article>
    )
  }
  if (state.error || !state.monitoringInstance) {
    return (
      <article className="compare-command-peer compare-command-peer--error">
        <span className="compare-command-peer__side">{label}</span>
        <div className="compare-command-peer__body">
          <span className="compare-command-peer__label">{label} 监控实例</span>
          <strong>不可用</strong>
          <span>{state.error ?? '监控实例不可用'}</span>
        </div>
      </article>
    )
  }
  const monitoringInstance = state.monitoringInstance
  return (
    <article className="compare-command-peer">
      <span className="compare-command-peer__side">{label}</span>
      <StatusGlyph state={monitoringInstanceHealthGlyphState(monitoringInstance)} size="sm" ariaLabel={`${label} 监控实例健康状态`} />
      <div className="compare-command-peer__body">
        <span className="compare-command-peer__label">{label} 监控实例</span>
        <strong>{monitoringInstance.display_name}</strong>
        <span>{monitoringInstance.current_health_status} · {monitoringInstance.monitoring_status}</span>
      </div>
    </article>
  )
}

function CompareSummaryStrip({ stateA, stateB }: { stateA: MonitoringInstanceState; stateB: MonitoringInstanceState }) {
  return (
    <section className="compare-summary-strip" aria-labelledby="compare-summary-title">
      <header className="compare-summary-strip__header">
        <div>
          <p className="compare-summary-strip__eyebrow">Compare Summary</p>
          <h2 id="compare-summary-title">A/B 摘要判断</h2>
        </div>
        <p>默认先看状态与样本是否可比；详细图表保留在下方。</p>
      </header>
      <div className="compare-summary-strip__grid">
        <CompareSummaryCard state={stateA} side="left" />
        <CompareSummaryCard state={stateB} side="right" />
      </div>
    </section>
  )
}

function CompareSummaryCard({ state, side }: { state: MonitoringInstanceState; side: CompareSide }) {
  const label = sideLabel(side)
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title={`${label} 摘要读取中`}
        description="正在建立 24h runtime facts 摘要。"
        surface="empty"
        compact
        className="compare-summary-card compare-summary-card--state"
      />
    )
  }
  if (state.error || !state.monitoringInstance) {
    return (
      <PageState
        kind="error"
        title={`${label} 摘要不可用`}
        description="该侧监控实例无法生成摘要，详细指标会保持不可用状态。"
        technicalSummary={state.error ?? '监控实例不可用'}
        surface="empty"
        compact
        className="compare-summary-card compare-summary-card--state"
      />
    )
  }

  const monitoringInstance = state.monitoringInstance
  const sample = state.runtimeFacts?.latest_host_sample ?? null
  const sampleCount = state.runtimeFacts?.window?.sample_count ?? state.runtimeFacts?.host_metric_points?.length ?? 0

  return (
    <article className="compare-summary-card" aria-label={`${label} 侧摘要`}>
      <header className="compare-summary-card__header">
        <span className="compare-summary-card__side">{label}</span>
        <div>
          <p>{label} 侧摘要</p>
          <h3>{monitoringInstance.display_name}</h3>
        </div>
      </header>
      <dl className="compare-summary-card__rows">
        <div className="compare-summary-row">
          <dt>健康状态</dt>
          <dd>
            <StatusGlyph state={monitoringInstanceHealthGlyphState(monitoringInstance)} size="sm" ariaLabel={`${label} 健康状态`} />
            <StatusBadge label={monitoringInstance.current_health_status} />
          </dd>
        </div>
        <div className="compare-summary-row">
          <dt>接入阶段</dt>
          <dd><StatusBadge label={monitoringInstance.lifecycle_status} /></dd>
        </div>
        <div className="compare-summary-row">
          <dt>运行 / 绑定</dt>
          <dd>
            <StatusBadge label={monitoringInstance.monitoring_status} />
            <StatusBadge label={monitoringInstance.binding_status} />
          </dd>
        </div>
        <div className="compare-summary-row compare-summary-row--stacked">
          <dt>位置上下文</dt>
          <dd>{monitoringInstanceContext(monitoringInstance)}</dd>
        </div>
        <div className="compare-summary-row compare-summary-row--stacked">
          <dt>样本可用性</dt>
          <dd>
            <span className="compare-summary-row__sample">
              <StatusGlyph
                state={sample ? (sample.maintenance_context ? 'maintenance' : 'normal') : 'offline'}
                size="sm"
                ariaLabel={`${label} 样本状态`}
              />
              {sample ? '有样本' : '无样本'}
            </span>
            {sample ? (
              <span className="compare-summary-row__detail">
                窗口样本 <MonoDigits>{sampleCount}</MonoDigits> 条 · 最近观测{' '}
                <Timestamp value={sample.observed_at} mode="absolute" />
              </span>
            ) : (
              <span className="compare-summary-row__detail">24h runtime facts 暂无 HostSample</span>
            )}
          </dd>
        </div>
      </dl>
    </article>
  )
}

function CompareMonitoringInstanceIdentity({ state, side }: { state: MonitoringInstanceState; side: CompareSide }) {
  const label = sideLabel(side)
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title={`${label} 监控实例读取中`}
        description="正在读取监控实例身份与最近运行事实，用于建立对比基线。"
        surface="empty"
        compact
        className="compare-identity__state"
      />
    )
  }
  if (state.error || !state.monitoringInstance) {
    return (
      <PageState
        kind="error"
        title={`${label} 监控实例不可用`}
        description="当前监控实例无法参与对比，请返回监控实例列表重新选择。"
        technicalSummary={state.error ?? '监控实例不可用'}
        surface="empty"
        compact
        className="compare-identity__state"
      />
    )
  }
  const monitoringInstance = state.monitoringInstance
  return (
    <div className="compare-identity__card">
      <div className="compare-identity__header">
        <span className="compare-identity__side">{label}</span>
        <StatusGlyph
          state={monitoringInstanceHealthGlyphState(monitoringInstance)}
          size="md"
          ariaLabel={`${label} 监控实例健康状态`}
        />
        <div className="compare-identity__title">
          <span>对比对象 {label}</span>
          <Link className="text-link" to={`/monitoring/${monitoringInstance.monitoring_instance_id}`}>
            {monitoringInstance.display_name}
          </Link>
        </div>
        <Link className="compare-identity__detail" to={`/monitoring/${monitoringInstance.monitoring_instance_id}`}>
          监控实例详情
        </Link>
      </div>
      <p className="compare-identity__meta">
        <Hostname truncate maxChars={24}>{monitoringInstance.monitoring_instance_id}</Hostname>
        <span>{monitoringInstanceContext(monitoringInstance)}</span>
      </p>
      <div className="badge-row">
        <StatusBadge label={monitoringInstance.lifecycle_status} />
        <StatusBadge label={monitoringInstance.monitoring_status} />
        <StatusBadge label={monitoringInstance.binding_status} />
        <StatusBadge label={monitoringInstance.current_health_status} />
      </div>
    </div>
  )
}

function CompareColumnPlaceholder({ state }: { state: MonitoringInstanceState }) {
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title="指标读取中"
        description="正在读取最近主机样本，图表会在运行事实可用后显示。"
        surface="empty"
        compact
      />
    )
  }
  return (
    <PageState
      kind="error"
      title="指标不可用"
      description="当前监控实例没有可用于对比的主机指标。"
      technicalSummary={state.error ?? '指标不可用'}
      surface="empty"
      compact
      action={<Link className="btn sm ghost" to="/monitoring">返回监控实例列表重新选择</Link>}
    />
  )
}
