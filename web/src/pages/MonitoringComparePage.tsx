import { useEffect, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Hostname } from '../components/atoms'
import { PageState } from '../components/PageState'
import { ApiError, getMonitoringInstance, getMonitoringInstanceRuntimeFacts } from '../lib/api'
import type { MonitoringInstanceRecord, MonitoringInstanceRuntimeFacts } from '../lib/types'
import { MonitoringCompareMetrics } from './monitoring-compare/MonitoringCompareMetrics'
import {
  monitoringInstanceAttentionBadges,
  monitoringInstanceHasHeartbeatEvidence,
  monitoringIssueSummary,
} from './monitoring/monitoringHelpers'
import { resolveMonitoringListHref } from './monitoring/monitoringListUrl'
import {
  formatMonitoringInstanceLocation,
  formatMonitoringInstanceProvider,
  withReturnVPSQuery,
} from './monitoring-detail/monitoringDetailHelpers'
import './monitoring-detail/MonitoringDetailWorkspace.css'
import './MonitoringComparePage.css'

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
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const ids = searchParams.getAll('id')
  const idA = ids[0] ?? null
  const idB = ids[1] ?? null
  const listHref = resolveMonitoringListHref(location.state)

  const idsReady = ids.length >= 2
  const monitoringInstanceA = useMonitoringInstanceData(idsReady ? idA : null)
  const monitoringInstanceB = useMonitoringInstanceData(idsReady ? idB : null)
  const nameA = monitoringInstanceA.monitoringInstance?.display_name
  const nameB = monitoringInstanceB.monitoringInstance?.display_name
  const title = nameA && nameB ? `${nameA} · ${nameB}` : '监控实例对比'

  if (ids.length < 2) {
    return (
      <PageState
        kind="empty"
        eyebrow="监控实例对比"
        title="需要选择 2 个监控实例"
        description="请先在监控实例列表勾选两个监控实例，再进入 A / B 指标对比。"
        action={<Link className="btn md secondary" to={listHref} state={location.state}>返回监控实例列表</Link>}
      />
    )
  }

  return (
    <div className="monitoring-compare">
      <header className="monitoring-compare__head">
        <div>
          <h1>{title}</h1>
          <p className="monitoring-compare__sub">近 24h</p>
        </div>
        <Link className="btn sm ghost" to={listHref} state={location.state}>返回监控实例列表</Link>
      </header>
      <div className="monitoring-compare__columns">
        <ComparePane state={monitoringInstanceA} side="left" />
        <ComparePane state={monitoringInstanceB} side="right" />
      </div>
    </div>
  )
}

function sideLabel(side: CompareSide): 'A' | 'B' {
  return side === 'left' ? 'A' : 'B'
}

function ComparePane({ state, side }: { state: MonitoringInstanceState; side: CompareSide }) {
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const label = sideLabel(side)
  const listHref = resolveMonitoringListHref(location.state)

  if (state.loading) {
    return (
      <section className="monitoring-compare-pane" aria-label={`${label} 监控实例`}>
        <PageState
          kind="loading"
          title={`${label} 读取中`}
          description="正在读取身份与近 24h 运行事实。"
          surface="empty"
          compact
        />
      </section>
    )
  }

  if (state.error || !state.monitoringInstance) {
    return (
      <section className="monitoring-compare-pane" aria-label={`${label} 监控实例`}>
        <PageState
          kind="error"
          title={`${label} 监控实例不可用`}
          description="当前监控实例无法参与对比，请返回监控实例列表重新选择。"
          technicalSummary={state.error ?? '监控实例不可用'}
          surface="empty"
          compact
          action={<Link className="btn sm ghost" to={listHref} state={location.state}>返回监控实例列表重新选择</Link>}
        />
      </section>
    )
  }

  const monitoringInstance = state.monitoringInstance
  const facts = state.runtimeFacts
  const detailTo = withReturnVPSQuery(
    `/monitoring/${monitoringInstance.monitoring_instance_id}`,
    searchParams.get('return_vps'),
  )
  const provider = formatMonitoringInstanceProvider(monitoringInstance.provider)
  const locationText = formatMonitoringInstanceLocation(monitoringInstance.region, monitoringInstance.city)
  const notice = compareNotice(monitoringInstance)

  return (
    <section className="monitoring-compare-pane" aria-label={`${label} ${monitoringInstance.display_name}`}>
      <header className="monitoring-compare-pane__identity">
        <div className="monitoring-compare-pane__title">
          <span className="monitoring-compare-pane__side">{label}</span>
          <Link className="text-link monitoring-compare-pane__name" to={detailTo} state={location.state}>
            {monitoringInstance.display_name}
          </Link>
          <Link className="text-link" to={detailTo} state={location.state}>监控实例详情</Link>
        </div>
        <dl className="monitoring-compare-pane__facts">
          {provider ? (
            <div>
              <dt>服务商</dt>
              <dd>{provider}</dd>
            </div>
          ) : null}
          {locationText ? (
            <div>
              <dt>位置</dt>
              <dd>{locationText}</dd>
            </div>
          ) : null}
          {monitoringInstance.group ? (
            <div>
              <dt>分组</dt>
              <dd>{monitoringInstance.group}</dd>
            </div>
          ) : null}
          <div>
            <dt>ID</dt>
            <dd><Hostname>{monitoringInstance.monitoring_instance_id}</Hostname></dd>
          </div>
        </dl>
      </header>
      <div className="monitoring-compare-pane__notice">
        {notice ? (
          <div className={`monitoring-detail-notice monitoring-detail-notice--${notice.tone}`} role={notice.tone === 'maintenance' || notice.tone === 'offline' ? 'status' : 'alert'}>
            <div className="monitoring-detail-notice__copy">
              <span className="monitoring-detail-notice__mark">{notice.mark}</span>
              <span className="monitoring-detail-notice__text">{notice.title}</span>
            </div>
            <div className="monitoring-detail-notice__actions">
              <Link className="btn sm secondary" to={detailTo} state={location.state}>打开详情</Link>
            </div>
          </div>
        ) : null}
      </div>
      {facts ? (
        <MonitoringCompareMetrics
          metricPoints={facts.host_metric_points ?? []}
          {...(facts.window === undefined ? {} : { window: facts.window })}
        />
      ) : (
        <PageState
          kind="error"
          title="指标不可用"
          description="当前监控实例没有可用于对比的主机指标。"
          technicalSummary={state.error ?? '指标不可用'}
          surface="empty"
          compact
        />
      )}
    </section>
  )
}

function compareNotice(monitoringInstance: MonitoringInstanceRecord): {
  tone: 'critical' | 'alert' | 'notice' | 'maintenance' | 'offline'
  mark: string
  title: string
} | null {
  const badges = monitoringInstanceAttentionBadges(
    monitoringInstance,
    monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
  )
  const summary = monitoringIssueSummary(monitoringInstance)
  const health = badges.find((badge) => badge.label === '严重' || badge.label === '告警' || badge.label === '关注')
  if (health) {
    return {
      tone: health.label === '严重' ? 'critical' : health.label === '关注' ? 'notice' : 'alert',
      mark: health.label,
      title: summary && summary !== health.label ? summary : '存在活跃异常',
    }
  }
  const control = badges[0]
  if (!control) return null
  if (control.label === '维护中') return { tone: 'maintenance', mark: '维护中', title: '观测继续，异常通知已抑制' }
  if (control.label === '暂停') return { tone: 'offline', mark: '暂停', title: '监控已暂停' }
  if (control.label === '未绑定') return { tone: 'alert', mark: '未绑定', title: '尚未接入 agent' }
  if (control.label === '等待绑定确认') return { tone: 'alert', mark: '待确认', title: '绑定冲突待确认' }
  if (control.label === '未知') return { tone: 'alert', mark: '未收到心跳', title: '还没有来自这台主机的心跳' }
  return { tone: 'alert', mark: control.label, title: summary || control.label }
}
