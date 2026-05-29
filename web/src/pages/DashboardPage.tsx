import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { PageState } from '../components/PageState'
import { ApiError, getDashboard, listVPSAssets } from '../lib/api'
import type { DashboardOverview, VPSAssetRecord, StateChangeEventRecord } from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
  vpsAssets: VPSAssetRecord[]
}

// TODO: replace with real API data when available
const POEMS = [
  '山重水复疑无路，柳暗花明又一村',
  '长风破浪会有时，直挂云帆济沧海',
  '千磨万击还坚劲，任尔东西南北风',
  '不畏浮云遮望眼，自缘身在最高层',
]

function getGreeting(): string {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 11) return '早上好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
}

function severityDotClass(severity: string): string {
  switch (severity) {
    case '严重': return 'dot dot-err'
    case '告警': return 'dot dot-err'
    case '关注': return 'dot dot-warn'
    default: return 'dot dot-ok'
  }
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
    vpsAssets: [],
  })
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    Promise.all([getDashboard(), listVPSAssets().catch(() => [] as VPSAssetRecord[])])
      .then(([overview, vpsAssets]) => {
        if (!mountedRef.current) return
        setState({ loading: false, error: null, overview, vpsAssets })
      })
      .catch((error: unknown) => {
        if (!mountedRef.current) return
        const message = error instanceof ApiError ? error.message : '加载工作台失败'
        setState({ loading: false, error: message, overview: null, vpsAssets: [] })
      })
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
      />
    )
  }

  const overview = state.overview
  const poem = POEMS[Math.floor(new Date().getDate() % POEMS.length)]

  // Metric card data
  const abnormalNodeCount = overview.abnormal_node_count + overview.severe_node_count
  const totalNodes = overview.total_node_count
  const onlineNodes = totalNodes - abnormalNodeCount
  const renewal30d = overview.asset_summary.renewal_due_30d_vps_count
  const cancelledVps = overview.asset_summary.to_cancel_vps_count

  // Cost: aggregate from cost_by_currency
  const costEntries = overview.asset_summary.cost_by_currency
  const monthlyCostStr = costEntries.length > 0
    ? costEntries.map(c => `${c.currency === 'USD' ? '$' : c.currency}${Math.round(c.monthly_total)}`).join(' + ')
    : '—'
  const yearlyCostStr = costEntries.length > 0
    ? costEntries.map(c => `${c.currency === 'USD' ? '$' : c.currency}${Math.round(c.yearly_total)}`).join(' + ')
    : '—'

  // Events (recent)
  const recentEvents = overview.recent_events.slice(0, 4)

  // Nodes for column (show abnormal first, then normal)
  const abnormalNodes = overview.abnormal_nodes.slice(0, 4)

  return (
    <div className="page-stack">
      {/* Welcome */}
      <div className="wb-welcome animate-in">
        <div className="wb-welcome-title">{getGreeting()}，管理员</div>
        <div className="wb-welcome-poem">{poem}</div>
      </div>

      {/* 4 Metric Cards */}
      <div className="wb-cards animate-in d1">
        <div className="wb-card">
          <div className="wb-card-primary">
            <span className={`wb-card-num ${abnormalNodeCount === 0 ? 'ok' : 'err'}`}>{abnormalNodeCount}</span>
            <span className="wb-card-label">异常节点</span>
          </div>
          <div className="wb-card-secondary">共 {totalNodes} 个 · 在线 {onlineNodes}</div>
        </div>
        <div className="wb-card">
          <div className="wb-card-primary">
            <span className={`wb-card-num ${renewal30d > 0 ? 'warn' : ''}`}>{renewal30d}</span>
            <span className="wb-card-label">30天内续费</span>
          </div>
          <div className="wb-card-secondary">未评估 {overview.asset_summary.unreviewed_vps_count} 个 · 已取消 {cancelledVps}</div>
        </div>
        <div className="wb-card">
          <div className="wb-card-primary">
            <span className="wb-card-num">{monthlyCostStr}</span>
            <span className="wb-card-label">月均成本</span>
          </div>
          <div className="wb-card-secondary">今年累计 {yearlyCostStr}</div>
        </div>
        <div className="wb-card">
          <div className="wb-card-primary">
            <span className="wb-card-num">{overview.recent_new_incident_count}</span>
            <span className="wb-card-label">近期异常</span>
          </div>
          <div className="wb-card-secondary">恢复 {overview.recent_recovery_count} 条</div>
        </div>
      </div>

      {/* 5-Column Data Area */}
      <div className="wb-columns animate-in d2">
        {/* Column 1: Attention */}
        <div className="wb-col">
          <div className="wb-col-header">
            <span className="wb-col-title">关注</span>
          </div>
          <div className="wb-col-list">
            {overview.abnormal_nodes.length === 0 && overview.abnormal_targets.length === 0 && (
              <div className="wb-att-item"><span className="wb-att-text text-muted text-sm">暂无需关注项</span></div>
            )}
            {overview.abnormal_nodes.slice(0, 2).map(n => (
              <div className="wb-att-item" key={n.node_id}>
                <span className={`alert-dot ${n.current_health_status === '严重' ? 'err' : 'warn'}`}></span>
                <div className="wb-att-body">
                  <span className="wb-att-text">{n.display_name} {n.current_primary_issue_summary}</span>
                  <span className="wb-att-meta">{n.current_health_status}</span>
                </div>
              </div>
            ))}
            {overview.abnormal_targets.slice(0, 2).map(t => (
              <div className="wb-att-item" key={t.target_id}>
                <span className={`alert-dot ${t.current_health_status === '严重' ? 'err' : 'warn'}`}></span>
                <div className="wb-att-body">
                  <span className="wb-att-text">{t.name} {t.current_primary_issue_summary}</span>
                  <span className="wb-att-meta">{t.current_health_status}</span>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Column 2: Nodes */}
        <div className="wb-col">
          <div className="wb-col-header">
            <span className="wb-col-title">节点</span>
            <span className="wb-col-link" onClick={() => navigate('/nodes')}>全部 →</span>
          </div>
          <div className="wb-col-list">
            {abnormalNodes.length === 0 && (
              <div className="wb-row"><span className="dot dot-ok"></span><span className="wb-row-name text-muted">全部正常</span></div>
            )}
            {abnormalNodes.map(n => (
              <div className="wb-row" key={n.node_id} onClick={() => navigate(`/nodes/${n.node_id}`)}>
                <span className={severityDotClass(n.current_health_status)}></span>
                <span className="wb-row-name">{n.display_name}</span>
                <span className="wb-row-val" style={{ color: n.current_health_status !== '正常' ? 'var(--warn)' : undefined }}>{n.current_health_status}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Column 3: Events */}
        <div className="wb-col">
          <div className="wb-col-header">
            <span className="wb-col-title">动态</span>
            <span className="wb-col-link" onClick={() => navigate('/events')}>全部 →</span>
          </div>
          <div className="wb-col-list">
            {recentEvents.length === 0 && (
              <div className="wb-evt"><span className="wb-evt-text text-muted">暂无事件</span></div>
            )}
            {recentEvents.map((evt, i) => {
              const icon = eventIcon(evt)
              return (
                <div className="wb-evt" key={evt.event_id ?? i}>
                  <span className="wb-evt-time">{formatTime(evt.created_at)}</span>
                  <span className={icon.cls + ' event-icon--sm'}>{icon.char}</span>
                  <span className="wb-evt-text">{evt.summary}</span>
                </div>
              )
            })}
          </div>
        </div>

        {/* Column 4: Cost */}
        <div className="wb-col">
          <div className="wb-col-header">
            <span className="wb-col-title">成本</span>
            <span className="wb-col-link" onClick={() => navigate('/subscriptions')}>详情 →</span>
          </div>
          <div className="wb-col-list">
            {costEntries.length === 0 && (
              <div className="wb-kv"><span className="text-muted">暂无订阅数据</span></div>
            )}
            {costEntries.map((c, i) => (
              <div className="wb-kv" key={i}>
                <span>{c.currency}</span>
                <span className="mono">${Math.round(c.monthly_total)}/月</span>
              </div>
            ))}
          </div>
        </div>

        {/* Column 5: Experience / Memo */}
        <div className="wb-col">
          <div className="wb-col-header">
            <span className="wb-col-title">经验记录</span>
            <span className="wb-col-link" onClick={() => navigate('/vps')}>全部 →</span>
          </div>
          <div className="wb-col-list">
            {/* TODO: wire to real experience log API when available */}
            <div className="wb-note"><span className="wb-note-date">—</span><span className="wb-note-text text-muted">暂无经验记录</span></div>
          </div>
        </div>
      </div>

      {/* Asset Table */}
      <div className="animate-in d3">
        <div className="section-title mt-4">
          资产总览 <span className="section-count">{state.vpsAssets.length}</span>
        </div>
        {state.vpsAssets.length > 0 ? (
          <table className="table">
            <thead>
              <tr>
                <th>VPS</th>
                <th>服务商</th>
                <th>IP</th>
                <th>生命周期</th>
                <th>续费决策</th>
                <th>关联节点</th>
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
                  <td>{vps.active_node_link_count > 0 ? `${vps.active_node_link_count} 个` : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="text-sm text-muted" style={{ padding: '12px 0' }}>暂无 VPS 资产数据</div>
        )}
      </div>
    </div>
  )
}
