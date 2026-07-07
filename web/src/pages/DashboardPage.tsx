import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { PageState } from '../components/PageState'
import { Badge, Card, SectionTitle, StatCard } from '../components/atoms'
import { statusTone } from './dashboard/dashboardHelpers'
import { ApiError, getDashboard, getSubscriptionOverview, listVPSAssets } from '../lib/api'
import { formatMoney } from '../lib/format'
import type { DashboardOverview, VPSAssetRecord, StateChangeEventRecord, SubscriptionOverview } from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
  subscriptionOverview: SubscriptionOverview | null
  vpsAssets: VPSAssetRecord[]
}

function getGreeting(): string {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 11) return '早上好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
}

function eventIcon(evt: StateChangeEventRecord): { cls: string; char: string } {
  if (evt.event_type === 'incident_recovered') return { cls: 'event-icon ei-ok', char: '✓' }
  if (evt.event_type === 'incident_escalated') return { cls: 'event-icon ei-err', char: '!' }
  return { cls: 'event-icon ei-warn', char: '!' }
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function lifecycleLabel(status: string): string {
  const map: Record<string, string> = { active: '在用', idle: '闲置', testing: '测试中', to_migrate: '待迁移', to_cancel: '待取消', cancelled: '已取消', archived: '已归档' }
  return map[status] ?? status
}

function lifecycleBadgeClass(status: string): string {
  if (status === 'active') return 'badge-ok'
  if (status === 'to_cancel' || status === 'cancelled' || status === 'archived') return 'badge-muted'
  return 'badge-warn'
}

function renewalLabel(decision: string): string {
  const map: Record<string, string> = { unreviewed: '未评估', keep: '保留', observe: '观察', migrate: '迁移', cancel: '取消', auto_renew_cancelled: '已取消续费', replaced: '已替换' }
  return map[decision] ?? decision
}

function renewalBadgeClass(decision: string): string {
  if (decision === 'keep') return 'badge-ok'
  if (decision === 'unreviewed') return 'badge-warn'
  if (decision === 'cancel' || decision === 'auto_renew_cancelled') return 'badge-muted'
  return 'badge-neutral'
}

export function DashboardPage() {
  const navigate = useNavigate()
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    overview: null,
    subscriptionOverview: null,
    vpsAssets: [],
  })
  const mountedRef = useRef(true)

  function loadData() {
    Promise.all([
      getDashboard(),
      listVPSAssets().catch(() => [] as VPSAssetRecord[]),
      getSubscriptionOverview().catch(() => null),
    ])
      .then(([overview, vpsAssets, subscriptionOverview]) => {
        if (!mountedRef.current) return
        setState({ loading: false, error: null, overview, subscriptionOverview, vpsAssets })
      })
      .catch((error: unknown) => {
        if (!mountedRef.current) return
        const message = error instanceof ApiError ? error.message : '加载工作台失败'
        setState({ loading: false, error: message, overview: null, subscriptionOverview: null, vpsAssets: [] })
      })
  }

  useEffect(() => {
    mountedRef.current = true
    loadData()
    return () => { mountedRef.current = false }
  }, [])

  if (state.loading) {
    return <PageState kind="loading" title="正在加载工作台…" />
  }

  if (state.error || !state.overview) {
    return (
      <PageState
        kind="error"
        eyebrow="工作台"
        title="工作台不可用"
        description={state.error ?? '未获取到概览数据'}
        technicalSummary={state.error}
        action={
          <button
            type="button"
            className="btn primary"
            onClick={() => {
              setState((s) => ({ ...s, loading: true, error: null }))
              loadData()
            }}
          >
            重试
          </button>
        }
      />
    )
  }

  const overview = state.overview

  // Metric card data
  const abnormalMonitoringInstanceCount = overview.abnormal_monitoring_instance_count + overview.severe_monitoring_instance_count
  const totalMonitoringInstances = overview.total_monitoring_instance_count
  const renewal30d = overview.asset_summary.renewal_due_30d_vps_count
  const cancellationAttention = overview.asset_summary.cancellation_attention_vps_count ?? 0
  const runningCancelledAssets = overview.asset_summary.running_cancelled_asset_count ?? 0

  const assetOnboardingNeeded =
    state.vpsAssets.length === 0 &&
    totalMonitoringInstances === 0 &&
    overview.total_target_count === 0

  const subscriptionOverview = state.subscriptionOverview

  const costEntries = overview.asset_summary.cost_by_currency
  const baseCurrency = subscriptionOverview?.base_currency ?? overview.asset_summary.base_currency ?? 'CNY'
  const monthlyCostStr = subscriptionOverview
    ? formatMoney(subscriptionOverview.total_monthly_cost, baseCurrency)
    : costEntries.length > 0
      ? costEntries.map(c => formatMoney(Math.round(c.monthly_total), c.currency)).join(' + ')
      : '—'
  const yearlyCostStr = subscriptionOverview
    ? formatMoney(subscriptionOverview.total_yearly_cost, baseCurrency)
    : costEntries.length > 0
      ? costEntries.map(c => formatMoney(Math.round(c.yearly_total), c.currency)).join(' + ')
      : '—'
  const renewalSummaryCount = subscriptionOverview?.renewal_due_14d_count ?? renewal30d
  const budgetRiskCount = subscriptionOverview?.budget_risk_count ?? overview.asset_summary.budget_risk_count ?? 0
  const exchangeRateStaleCount = subscriptionOverview?.exchange_rate_stale_count ?? overview.asset_summary.exchange_rate_stale_count ?? 0

  // Events (recent)
  const recentEvents = overview.recent_events.slice(0, 4)

  // Monitoring instances for column (show abnormal first, then normal)
  const abnormalMonitoringInstances = overview.abnormal_monitoring_instances.slice(0, 4)

  return (
    <div className="page-stack">
      <div className="dash-header">
        <div>
          <div className="dash-greeting">{getGreeting()}，管理员</div>
          <div className="dash-sub">资产、观测与账单的全局概览</div>
        </div>
      </div>

      {/* 4 Metric Cards */}
      <div className="stat-grid">
        <StatCard
          value={abnormalMonitoringInstanceCount}
          tone={abnormalMonitoringInstanceCount === 0 ? 'normal' : 'err'}
          label="异常监控实例"
          sub="观测列表 · 从 VPS 详情页接入"
        />
        <StatCard
          value={renewalSummaryCount}
          tone={renewalSummaryCount > 0 ? 'warn' : 'normal'}
          label="14天内续费"
          sub={`30 天 ${subscriptionOverview?.renewal_due_30d_count ?? renewal30d} · 联动待处理 ${cancellationAttention}`}
        />
        <StatCard value={monthlyCostStr} label="月均成本" sub={`年化 ${yearlyCostStr}`} />
        <StatCard
          value={budgetRiskCount}
          tone={budgetRiskCount > 0 || exchangeRateStaleCount > 0 ? 'warn' : 'normal'}
          label="预算风险"
          sub={`汇率异常 ${exchangeRateStaleCount} · 近期异常 ${overview.recent_new_incident_count}`}
        />
      </div>

      {/* Overview panels: responsive 6-col grid (3 + 2) */}
      <div className="dash-panels">
        {/* Column 1: Attention */}
          <Card className="dash-panel">
          <SectionTitle title="关注" />
          {assetOnboardingNeeded ? (
            <div className="dash-att dash-att--clickable" onClick={() => navigate('/vps')}>
              <span className="alert-dot warn"></span>
              <div className="dash-att-body">
                <span className="dash-att-text">先创建第一台 VPS</span>
                <span className="dash-att-meta">订阅和 agent 接入都在 VPS 详情页完成</span>
              </div>
            </div>
          ) : null}
          {!assetOnboardingNeeded && overview.abnormal_monitoring_instances.length === 0 && overview.abnormal_targets.length === 0 && cancellationAttention === 0 && (
            <div className="dash-att"><span className="dash-att-text text-muted text-sm">暂无需关注项</span></div>
          )}
          {cancellationAttention > 0 ? (
            <div className="dash-att dash-att--clickable" onClick={() => navigate('/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement')}>
              <span className="alert-dot warn"></span>
              <div className="dash-att-body">
                <span className="dash-att-text">取消/过期资产状态不一致</span>
                <span className="dash-att-meta">VPS {cancellationAttention} · 仍运行 {runningCancelledAssets}</span>
              </div>
            </div>
          ) : null}
          {subscriptionOverview && subscriptionOverview.budget_risk_count > 0 ? (
            <div className="dash-att dash-att--clickable" onClick={() => navigate('/asset-decisions?view=cost&renew_within_days=30&scenario=budget_reduction')}>
              <span className="alert-dot warn"></span>
              <div className="dash-att-body">
                <span className="dash-att-text">订阅预算接近或超过上限</span>
                <span className="dash-att-meta">风险 {subscriptionOverview.budget_risk_count} · 汇率异常 {subscriptionOverview.exchange_rate_stale_count}</span>
              </div>
            </div>
          ) : null}
          {overview.abnormal_monitoring_instances.slice(0, 2).map(n => (
            <div className="dash-att" key={n.monitoring_instance_id}>
              <span className={`alert-dot ${n.current_health_status === '严重' ? 'err' : 'warn'}`}></span>
              <div className="dash-att-body">
                <span className="dash-att-text">{n.display_name} {n.current_primary_issue_summary}</span>
                <span className="dash-att-meta">观测异常 · 回到所属 VPS 判断影响</span>
              </div>
            </div>
          ))}
          {overview.abnormal_targets.slice(0, 2).map(t => (
            <div className="dash-att" key={t.target_id}>
              <span className={`alert-dot ${t.current_health_status === '严重' ? 'err' : 'warn'}`}></span>
              <div className="dash-att-body">
                <span className="dash-att-text">{t.name} {t.current_primary_issue_summary}</span>
                <span className="dash-att-meta">{t.current_health_status}</span>
              </div>
            </div>
          ))}
        </Card>

        {/* Column 2: Monitoring instances */}
          <Card className="dash-panel">
          <SectionTitle title="观测事实" action={<span className="panel-link" onClick={() => navigate('/monitoring')}>列表 →</span>} />
          {abnormalMonitoringInstances.length === 0 && (
            <div className="dash-row"><span className="dot dot-ok"></span><span className="dash-row-name text-muted">暂无异常观测</span></div>
          )}
          {abnormalMonitoringInstances.map(n => (
            <div className="dash-row" key={n.monitoring_instance_id} onClick={() => navigate(`/monitoring/${n.monitoring_instance_id}`)}>
              <span className="dash-row-name">{n.display_name}</span>
              <Badge variant="state" tone={statusTone(n.current_health_status)} withDot>{n.current_health_status}</Badge>
            </div>
          ))}
        </Card>

        {/* Column 3: Events */}
          <Card className="dash-panel">
          <SectionTitle title="动态" action={<span className="panel-link" onClick={() => navigate('/events')}>全部 →</span>} />
          {recentEvents.length === 0 && (
            <div className="dash-evt"><span className="dash-evt-text text-muted">暂无事件</span></div>
          )}
          {recentEvents.map((evt, i) => {
            const icon = eventIcon(evt)
            return (
              <div className="dash-evt" key={evt.event_id ?? i}>
                <span className="dash-evt-time">{formatTime(evt.created_at)}</span>
                <span className={icon.cls + ' event-icon--sm'}>{icon.char}</span>
                <span className="dash-evt-text">{evt.summary}</span>
              </div>
            )
          })}
        </Card>

        {/* Column 4: Cost (wide) */}
          <Card className="dash-panel dash-panel--wide">
          <SectionTitle title="账单事实" action={<span className="panel-link" onClick={() => navigate('/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup')}>缺口 →</span>} />
          {!subscriptionOverview && costEntries.length === 0 && (
            <div className="dash-kv"><span className="text-muted">暂无账单事实</span></div>
          )}
          {subscriptionOverview ? (
            <>
              <div className="dash-kv">
                <span>基准月成本</span>
                <span className="mono">{formatMoney(subscriptionOverview.total_monthly_cost, subscriptionOverview.base_currency)}</span>
              </div>
              <div className="dash-kv">
                <span>未来 30 天续费</span>
                <span className="mono">{subscriptionOverview.renewal_due_30d_count}</span>
              </div>
              <div className="dash-kv">
                <span>预算风险</span>
                <span className="mono">{subscriptionOverview.budget_risk_count}</span>
              </div>
              <div className="dash-kv">
                <span>汇率异常</span>
                <span className="mono">{subscriptionOverview.exchange_rate_stale_count}</span>
              </div>
            </>
          ) : null}
          {costEntries.map((c, i) => (
            <div className="dash-kv" key={i}>
              <span>{c.currency}</span>
              <span className="mono">{formatMoney(Math.round(c.monthly_total), c.currency)}/月</span>
            </div>
          ))}
        </Card>

        {/* Column 5: Experience / Memo (wide) */}
          <Card className="dash-panel dash-panel--wide">
          <SectionTitle title="经验记录" action={<span className="panel-link" onClick={() => navigate('/vps')}>全部 →</span>} />
          <div className="dash-note"><span className="dash-note-date">—</span><span className="dash-note-text text-muted">暂无经验记录</span></div>
        </Card>
      </div>

      {/* Asset Table */}
      <div>
        <SectionTitle className="mt-4" title="资产总览" count={state.vpsAssets.length} />
        {state.vpsAssets.length > 0 ? (
          <table className="table">
            <thead>
              <tr>
                <th>VPS</th>
                <th>服务商</th>
                <th>IP</th>
                <th>生命周期</th>
                <th>续费决策</th>
                <th>关联监控实例</th>
              </tr>
            </thead>
            <tbody>
              {state.vpsAssets.slice(0, 8).map(vps => (
                <tr key={vps.vps_id} onClick={() => navigate(`/vps/${vps.vps_id}`)} className="row-clickable">
                  <td className="name">{vps.display_name}</td>
                  <td>{vps.provider_name}</td>
                  <td className="mono">{vps.ipv4 || '—'}</td>
                  <td><span className={`badge ${lifecycleBadgeClass(vps.lifecycle_status)}`}>{lifecycleLabel(vps.lifecycle_status)}</span></td>
                  <td><span className={`badge ${renewalBadgeClass(vps.renewal_decision)}`}>{renewalLabel(vps.renewal_decision)}</span></td>
                  <td>{vps.active_monitoring_instance_link_count > 0 ? `${vps.active_monitoring_instance_link_count} 个` : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="text-sm text-muted">暂无 VPS 资产数据</div>
        )}
      </div>
    </div>
  )
}
