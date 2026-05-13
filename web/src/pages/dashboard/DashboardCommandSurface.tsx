import { Link } from 'react-router-dom'

import {
  Badge,
  Hostname,
  MonoDigits,
  StatusGlyph,
  Timestamp,
  type BadgeTone,
  type HealthState,
} from '../../components/atoms'
import {
  AUTO_REFRESH_OPTIONS,
  type AutoRefreshOption,
} from '../../lib/useAutoRefresh'
import type { DashboardAssetSummary, DashboardOverview } from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import {
  fleetGlyphState,
  fleetSignalLabel,
  formatAssetCost,
  statusGlyph,
  statusTone,
  trendBalanceLabel,
} from './dashboardHelpers'
import type { AttentionItem, DashboardMetric, FleetState, FleetStateTone } from './types'

type DashboardCommandSurfaceProps = {
  overview: DashboardOverview
  fleetState: FleetState
  metrics: DashboardMetric[]
  attentionItems: AttentionItem[]
  abnormalTotal: number
  severeTotal: number
  maintenanceTotal: number
  isFreshInstall: boolean
  refreshing?: boolean
  onRefresh?: () => void
  autoRefresh?: AutoRefreshOption
  onAutoRefreshChange?: (value: AutoRefreshOption) => void
}

type CommandRow = {
  label: string
  value: number | string
  detail: string
  to: string
  tone: BadgeTone
}

type NextAction = {
  label: string
  detail: string
  to: string
  tone: BadgeTone
  primary?: boolean
}

type FocusItem = {
  label: string
  value: number | string
  detail: string
  to: string
  tone: BadgeTone
  emphasis?: boolean
}

function assetPressureCount(summary: DashboardAssetSummary) {
  return (
    summary.renewal_due_30d_vps_count +
    summary.unreviewed_vps_count +
    summary.to_cancel_vps_count +
    summary.to_migrate_vps_count +
    summary.unlinked_vps_count +
    summary.abnormal_linked_vps_count
  )
}

function commandTitle(
  summary: DashboardAssetSummary,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
) {
  const pressureTotal = assetPressureCount(summary)
  if (isFreshInstall) return '建立第一条资产与观测链路'
  if (pressureTotal > 0 && severeTotal > 0) return '先处理资产压力与严重异常'
  if (pressureTotal > 0) return '先处理资产决策队列'
  if (severeTotal > 0) return '先处理严重异常'
  if (abnormalTotal > 0) return '先处理观测异常'
  if (maintenanceTotal > 0) return '维护对象正在观察'
  return '今日没有紧急处理项'
}

function commandDescription(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
) {
  if (isFreshInstall) {
    return '先创建节点、接入 agent，再补齐 VPS 资产与观测目标；工作台会在有数据后显示续费、决策和异常队列。'
  }

  const summary = overview.asset_summary
  const pressureTotal = assetPressureCount(summary)
  const assetCopy = pressureTotal > 0
    ? `资产侧 ${pressureTotal} 项信号`
    : '资产侧暂无待处理信号'
  const observationCopy =
    abnormalTotal > 0
      ? `观测侧 ${abnormalTotal} 个异常对象，其中严重 ${severeTotal}`
      : maintenanceTotal > 0
        ? `观测侧 ${maintenanceTotal} 个对象处于维护观察`
        : '观测侧暂无活跃异常'
  const renewalCopy =
    summary.renewal_due_30d_vps_count > 0
      ? `30 天续费 ${summary.renewal_due_30d_vps_count} 台 VPS`
      : '30 天内暂无续费压力'
  return `${assetCopy}；${observationCopy}；${renewalCopy}。`
}

function assetFocusDetail(summary: DashboardAssetSummary, pressureTotal: number) {
  if (pressureTotal === 0) return '续费、决策与关联均稳定'
  const lifecycleReviewCount = summary.to_cancel_vps_count + summary.to_migrate_vps_count
  return `续费 ${summary.renewal_due_30d_vps_count} · 决策 ${
    summary.unreviewed_vps_count + lifecycleReviewCount
  } · 缺关联 ${summary.unlinked_vps_count}`
}

function observabilityFocus(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
): FocusItem {
  if (severeTotal > 0) {
    return {
      label: '严重异常',
      value: severeTotal,
      detail: `异常对象 ${abnormalTotal} · 先看事件证据`,
      to: DASHBOARD_LINKS.eventsSevere,
      tone: 'critical',
      emphasis: true,
    }
  }

  if (abnormalTotal > 0) {
    return {
      label: '观测异常',
      value: abnormalTotal,
      detail: `节点 ${overview.abnormal_node_count} · 目标 ${overview.abnormal_target_count}`,
      to: overview.abnormal_node_count > 0 ? DASHBOARD_LINKS.nodesAbnormal : DASHBOARD_LINKS.targetsAbnormal,
      tone: 'alert',
      emphasis: true,
    }
  }

  if (maintenanceTotal > 0) {
    return {
      label: '维护观察',
      value: maintenanceTotal,
      detail: `节点 ${overview.maintenance_node_count} · 目标 ${overview.maintenance_target_count}`,
      to: DASHBOARD_LINKS.eventsMaintenance,
      tone: 'maintenance',
      emphasis: true,
    }
  }

  return {
    label: '观测稳定',
    value: 0,
    detail: '当前没有活跃异常对象',
    to: DASHBOARD_LINKS.events24h,
    tone: 'normal',
  }
}

function commandFocusItems(
  overview: DashboardOverview,
  pressureTotal: number,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  primaryAction?: NextAction,
): FocusItem[] {
  const summary = overview.asset_summary
  return [
    {
      label: pressureTotal > 0 ? '资产压力' : '资产主线',
      value: pressureTotal,
      detail: assetFocusDetail(summary, pressureTotal),
      to: pressureTotal > 0 ? DASHBOARD_LINKS.assetDecisions : DASHBOARD_LINKS.vps,
      tone: pressureTotal > 0 ? 'notice' : 'normal',
      emphasis: pressureTotal > 0,
    },
    observabilityFocus(overview, abnormalTotal, severeTotal, maintenanceTotal),
    {
      label: '下一步',
      value: '01',
      detail: primaryAction?.label ?? '等待工作台生成动作',
      to: primaryAction?.to ?? DASHBOARD_LINKS.events24h,
      tone: primaryAction?.tone ?? 'neutral',
      emphasis: true,
    },
  ]
}

function assetRows(summary: DashboardAssetSummary): CommandRow[] {
  const lifecycleReviewCount = summary.to_cancel_vps_count + summary.to_migrate_vps_count
  return [
    {
      label: '30 天续费',
      value: summary.renewal_due_30d_vps_count,
      detail: `订阅 ${summary.renewal_due_30d_subscription_count}`,
      to: DASHBOARD_LINKS.assetDecisions,
      tone: summary.renewal_due_30d_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '待决策',
      value: summary.unreviewed_vps_count,
      detail: '续费状态未评估',
      to: DASHBOARD_LINKS.assetDecisions,
      tone: summary.unreviewed_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '取消 / 迁移',
      value: lifecycleReviewCount,
      detail: `取消 ${summary.to_cancel_vps_count} · 迁移 ${summary.to_migrate_vps_count}`,
      to: DASHBOARD_LINKS.assetDecisions,
      tone: lifecycleReviewCount > 0 ? 'alert' : 'normal',
    },
    {
      label: '未关联 Node',
      value: summary.unlinked_vps_count,
      detail: '需人工核对',
      to: DASHBOARD_LINKS.vps,
      tone: summary.unlinked_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '关联异常',
      value: summary.abnormal_linked_vps_count,
      detail: 'VPS 关联异常 Node',
      to: DASHBOARD_LINKS.nodesAbnormal,
      tone: summary.abnormal_linked_vps_count > 0 ? 'alert' : 'normal',
    },
    {
      label: '成本',
      value: summary.cost_by_currency.length,
      detail: formatAssetCost(summary),
      to: DASHBOARD_LINKS.subscriptionsRenew30d,
      tone: summary.cost_by_currency.length > 0 ? 'neutral' : 'normal',
    },
  ]
}

function observabilityRows(metrics: DashboardMetric[], overview: DashboardOverview): CommandRow[] {
  if (metrics.length === 0) {
    return [
      {
        label: '节点',
        value: overview.total_node_count,
        detail: '尚未接入观测节点',
        to: DASHBOARD_LINKS.nodes,
        tone: 'notice',
      },
      {
        label: '目标',
        value: overview.total_target_count,
        detail: '尚未配置观测目标',
        to: DASHBOARD_LINKS.targets,
        tone: 'notice',
      },
    ]
  }

  return metrics.map((metric) => ({
    label: metric.label,
    value: metric.value,
    detail: metric.detail,
    to: metric.to,
    tone: metric.tone ?? 'neutral',
  }))
}

function nextActions(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
): NextAction[] {
  if (isFreshInstall) {
    return [
      {
        label: '创建第一个节点',
        detail: '先登记服务器并生成 agent 接入指引',
        to: DASHBOARD_LINKS.nodes,
        tone: 'notice',
        primary: true,
      },
      {
        label: '查看节点接入',
        detail: '处理待接入或绑定待确认节点',
        to: DASHBOARD_LINKS.nodesPendingOnboarding,
        tone: 'neutral',
      },
      {
        label: '创建第一个目标',
        detail: '开始记录服务或端口的观测入口',
        to: DASHBOARD_LINKS.targets,
        tone: 'neutral',
      },
    ]
  }

  const summary = overview.asset_summary
  const actions: NextAction[] = []
  const pressureTotal = assetPressureCount(summary)

  if (pressureTotal > 0) {
    actions.push({
      label: '进入资产决策队列',
      detail: `待决策 ${summary.unreviewed_vps_count} · 30 天续费 ${summary.renewal_due_30d_vps_count} · 缺关联 ${summary.unlinked_vps_count}`,
      to: DASHBOARD_LINKS.assetDecisions,
      tone: summary.renewal_due_30d_vps_count > 0 ? 'notice' : 'neutral',
      primary: true,
    })
  }

  if (severeTotal > 0) {
    actions.push({
      label: '处理严重事件',
      detail: `严重对象 ${severeTotal}，先查看事件证据`,
      to: DASHBOARD_LINKS.eventsSevere,
      tone: 'critical',
      primary: pressureTotal === 0,
    })
  }

  if (abnormalTotal > 0) {
    actions.push({
      label: '处理观测异常',
      detail: `异常节点 ${overview.abnormal_node_count} · 异常目标 ${overview.abnormal_target_count}`,
      to: overview.abnormal_node_count > 0 ? DASHBOARD_LINKS.nodesAbnormal : DASHBOARD_LINKS.targetsAbnormal,
      tone: 'alert',
      primary: pressureTotal === 0 && severeTotal === 0,
    })
  }

  if (summary.unlinked_vps_count > 0) {
    actions.push({
      label: '核对未关联 VPS',
      detail: '补齐 VPS 与 Node 的证据链',
      to: DASHBOARD_LINKS.vps,
      tone: 'notice',
    })
  }

  if (maintenanceTotal > 0) {
    actions.push({
      label: '查看维护事件',
      detail: `维护对象 ${maintenanceTotal}，确认观察窗口`,
      to: DASHBOARD_LINKS.eventsMaintenance,
      tone: 'maintenance',
      primary: actions.length === 0,
    })
  }

  if (actions.length === 0) {
    return [
      {
        label: '核对 VPS 库存',
        detail: '检查 provider、region、续费和关联 Node 是否完整',
        to: DASHBOARD_LINKS.vps,
        tone: 'normal',
        primary: true,
      },
      {
        label: '查看 24h 事件流',
        detail: '确认最近异常与恢复记录',
        to: DASHBOARD_LINKS.events24h,
        tone: 'neutral',
      },
      {
        label: '进入资产决策',
        detail: '复核续费决策队列',
        to: DASHBOARD_LINKS.assetDecisions,
        tone: 'neutral',
      },
    ]
  }

  return actions.slice(0, 4)
}

function laneTone(tone: BadgeTone): HealthState {
  if (tone === 'neutral') return 'offline'
  return tone
}

function commandTone(fleetState: FleetState): FleetStateTone {
  return fleetState.tone
}

export function DashboardCommandSurface({
  overview,
  fleetState,
  metrics,
  attentionItems,
  abnormalTotal,
  severeTotal,
  maintenanceTotal,
  isFreshInstall,
  refreshing = false,
  onRefresh,
  autoRefresh,
  onAutoRefreshChange,
}: DashboardCommandSurfaceProps) {
  const summary = overview.asset_summary
  const pressureTotal = assetPressureCount(summary)
  const actions = nextActions(overview, abnormalTotal, severeTotal, maintenanceTotal, isFreshInstall)
  const primaryAction = actions.find((action) => action.primary) ?? actions[0]
  const tone = commandTone(fleetState)
  const visibleAttention = attentionItems.slice(0, 3)
  const focusItems = commandFocusItems(
    overview,
    pressureTotal,
    abnormalTotal,
    severeTotal,
    maintenanceTotal,
    primaryAction,
  )
  const assetLaneClass = [
    'dashboard-command-lane',
    'dashboard-command-lane--asset',
    pressureTotal > 0 && 'dashboard-command-lane--priority',
  ].filter(Boolean).join(' ')
  const observationLaneClass = [
    'dashboard-command-lane',
    'dashboard-command-lane--observability',
    severeTotal > 0 && 'dashboard-command-lane--critical-priority',
    abnormalTotal > 0 && severeTotal === 0 && 'dashboard-command-lane--priority',
  ].filter(Boolean).join(' ')

  const filteredAssetRows = assetRows(summary).filter(
    (item) => item.value !== 0 && item.value !== '0/0' && item.value !== '0'
  )
  const filteredObservabilityRows = observabilityRows(metrics, overview).filter(
    (item) => item.value !== 0 && item.value !== '0/0' && item.value !== '0'
  )

  const renderEmptyState = (label: string) => (
    <div className="dashboard-command-row dashboard-command-row--normal">
      <span className="dashboard-command-row__glyph" aria-hidden="true">
        <StatusGlyph state="normal" size="sm" />
      </span>
      <span className="dashboard-command-row__label">{label}</span>
      <strong className="dashboard-command-row__value">
        <MonoDigits>0</MonoDigits>
      </strong>
      <span className="dashboard-command-row__detail">暂无待处理项</span>
    </div>
  )

  return (
    <section
      className={`dashboard-command-surface dashboard-command-surface--${tone}`}
      aria-label="工作台 command surface"
    >
      <header className="dashboard-command-surface__header">
        <div className="dashboard-command-surface__intro">
          <div className="dashboard-command-surface__meta">
            <span className="dashboard-command-surface__signal">
              <StatusGlyph state={fleetGlyphState(tone)} size="md" />
              <span>{fleetSignalLabel(tone)}</span>
            </span>
            <span className="dashboard-command-surface__generated">
              摘要生成 <Timestamp value={overview.snapshot_generated_at} mode="absolute" />
            </span>
          </div>
          <p className="dashboard-command-surface__eyebrow">工作台</p>
          <h1>{commandTitle(summary, abnormalTotal, severeTotal, maintenanceTotal, isFreshInstall)}</h1>
          <p>{commandDescription(overview, abnormalTotal, severeTotal, maintenanceTotal, isFreshInstall)}</p>
          <div className="dashboard-command-focus" aria-label="今日判断摘要">
            {focusItems.map((item) => (
              <Link
                className={`dashboard-command-focus__item dashboard-command-focus__item--${item.tone}${
                  item.emphasis ? ' dashboard-command-focus__item--emphasis' : ''
                }`}
                to={item.to}
                key={item.label}
                aria-label={`${item.label}：${item.detail}`}
              >
                <span className="dashboard-command-focus__label">{item.label}</span>
                <strong className="dashboard-command-focus__value">
                  <MonoDigits>{item.value}</MonoDigits>
                </strong>
                <span className="dashboard-command-focus__detail">{item.detail}</span>
              </Link>
            ))}
          </div>
        </div>
        <div className="dashboard-command-surface__controls" aria-label="工作台主要动作">
          {primaryAction ? (
            <Link className="btn btn--primary btn--md" to={primaryAction.to}>
              {primaryAction.label}
            </Link>
          ) : null}
          {onRefresh ? (
            <button
              type="button"
              className="btn btn--ghost btn--md"
              disabled={refreshing}
              onClick={onRefresh}
            >
              {refreshing ? '刷新中…' : '刷新'}
            </button>
          ) : null}
          {onAutoRefreshChange ? (
            <label className="dashboard-command-surface__refresh">
              <span>自动刷新</span>
              <select
                className="auto-refresh-select"
                value={autoRefresh == null ? '' : String(autoRefresh)}
                onChange={(event) => {
                  const value = event.target.value
                  onAutoRefreshChange(value === '' ? null : Number(value))
                }}
                aria-label="自动刷新间隔"
              >
                {AUTO_REFRESH_OPTIONS.map((option) => (
                  <option key={option.label} value={option.value == null ? '' : String(option.value)}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
      </header>

      <div className="dashboard-command-grid">
        <section className={assetLaneClass} aria-label="资产决策队列">
          <div className="dashboard-command-lane__header">
            <div>
              <p className="dashboard-command-lane__eyebrow">资产决策队列</p>
              <h2>续费、决策与缺信息</h2>
            </div>
            <div className="dashboard-command-lane__tools">
              {pressureTotal > 0 ? <span className="dashboard-command-lane__signal">优先处理</span> : null}
              <Badge variant="count" tone={pressureTotal > 0 ? 'notice' : 'normal'}>
                <MonoDigits>{pressureTotal}</MonoDigits>
              </Badge>
            </div>
          </div>
          <div className="dashboard-command-list">
            {filteredAssetRows.length > 0
              ? filteredAssetRows.map((item) => (
                  <Link
                    className={`dashboard-command-row dashboard-command-row--${item.tone}`}
                    to={item.to}
                    key={item.label}
                    aria-label={`${item.label}：${item.detail}`}
                  >
                    <span className="dashboard-command-row__glyph" aria-hidden="true">
                      <StatusGlyph state={laneTone(item.tone)} size="sm" />
                    </span>
                    <span className="dashboard-command-row__label">{item.label}</span>
                    <strong className="dashboard-command-row__value">
                      <MonoDigits>{item.value}</MonoDigits>
                    </strong>
                    <span className="dashboard-command-row__detail">{item.detail}</span>
                  </Link>
                ))
              : renderEmptyState('资产状态全绿')}
          </div>
        </section>

        <section className={observationLaneClass} aria-label="观测异常队列">
          <div className="dashboard-command-lane__header">
            <div>
              <p className="dashboard-command-lane__eyebrow">观测异常队列</p>
              <h2>事件、节点与目标证据</h2>
            </div>
            <div className="dashboard-command-lane__tools">
              {severeTotal > 0 ? (
                <span className="dashboard-command-lane__signal dashboard-command-lane__signal--critical">
                  严重优先
                </span>
              ) : null}
              <Badge variant="count" tone={abnormalTotal > 0 ? 'alert' : 'normal'}>
                <MonoDigits>{abnormalTotal}</MonoDigits>
              </Badge>
            </div>
          </div>
          <div className="dashboard-command-list">
            {filteredObservabilityRows.length > 0
              ? filteredObservabilityRows.map((item) => (
                  <Link
                    className={`dashboard-command-row dashboard-command-row--${item.tone}`}
                    to={item.to}
                    key={item.label}
                    aria-label={`${item.label}：${item.detail}`}
                  >
                    <span className="dashboard-command-row__glyph" aria-hidden="true">
                      <StatusGlyph state={laneTone(item.tone)} size="sm" />
                    </span>
                    <span className="dashboard-command-row__label">{item.label}</span>
                    <strong className="dashboard-command-row__value">
                      <MonoDigits>{item.value}</MonoDigits>
                    </strong>
                    <span className="dashboard-command-row__detail">{item.detail}</span>
                  </Link>
                ))
              : renderEmptyState('观测对象全绿')}
          </div>
          {visibleAttention.length > 0 ? (
            <div className="dashboard-command-attention" aria-label="最高优先级异常对象">
              {visibleAttention.map((item, index) => (
                <Link
                  className={`dashboard-command-attention__item dashboard-command-attention__item--${statusTone(item.health)}`}
                  to={item.route}
                  key={`${item.kind}-${item.id}`}
                  aria-label={`处理${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
                >
                  <span className="dashboard-command-attention__rank">
                    P<MonoDigits>{index + 1}</MonoDigits>
                  </span>
                  <StatusGlyph state={statusGlyph(item.health)} size="sm" />
                  <span className="dashboard-command-attention__copy">
                    <strong>{item.name}</strong>
                    <span>
                      <Hostname truncate maxChars={24}>{item.technicalId}</Hostname>
                      {' · '}
                      {item.issueSummary}
                    </span>
                  </span>
                </Link>
              ))}
            </div>
          ) : null}
        </section>

        <section className="dashboard-command-lane dashboard-command-lane--actions" aria-label="下一步动作">
          <div className="dashboard-command-lane__header">
            <div>
              <p className="dashboard-command-lane__eyebrow">下一步动作</p>
              <h2>今天先做什么</h2>
            </div>
            <span className="dashboard-command-lane__signal dashboard-command-lane__signal--muted">
              按序执行
            </span>
          </div>
          <div className="dashboard-action-list">
            {actions.map((action, index) => (
              <Link
                className={`dashboard-action-row dashboard-action-row--${action.tone}${action.primary ? ' dashboard-action-row--primary' : ''}`}
                to={action.to}
                key={action.label}
                aria-label={`${action.label}：${action.detail}`}
              >
                <span className="dashboard-action-row__index">
                  <MonoDigits>{index + 1}</MonoDigits>
                </span>
                <span className="dashboard-action-row__copy">
                  <strong>{action.label}</strong>
                  <span>{action.detail}</span>
                </span>
                <span className="dashboard-action-row__cta">进入</span>
              </Link>
            ))}
          </div>
          <Link className="text-link dashboard-command-lane__footer-link" to={DASHBOARD_LINKS.events24h}>
            查看 24h 事件趋势：{trendBalanceLabel(overview)}
          </Link>
        </section>
      </div>
    </section>
  )
}
