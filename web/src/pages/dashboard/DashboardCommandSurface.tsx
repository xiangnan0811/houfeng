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
    (summary.cancellation_attention_vps_count ?? 0) +
    (summary.running_cancelled_asset_count ?? 0) +
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
  if (pressureTotal > 0) return '先处理资产组合决策'
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
  if (isFreshInstall) return '创建 VPS → 补账单事实 → 接入 agent。'

  const summary = overview.asset_summary
  const pressureTotal = assetPressureCount(summary)
  const assetCopy = `资产 ${pressureTotal}`
  const observationCopy =
    abnormalTotal > 0
      ? `异常 ${abnormalTotal} · 严重 ${severeTotal}`
      : maintenanceTotal > 0
        ? `维护 ${maintenanceTotal}`
        : '异常 0'
  const renewalCopy = `续费 ${summary.renewal_due_30d_vps_count}`
  return `${assetCopy} / ${observationCopy} / ${renewalCopy}`
}

function assetFocusDetail(summary: DashboardAssetSummary, pressureTotal: number) {
  if (pressureTotal === 0) return '稳定'
  const lifecycleReviewCount = summary.to_cancel_vps_count + summary.to_migrate_vps_count
  return `续费 ${summary.renewal_due_30d_vps_count} · 决策 ${
    summary.unreviewed_vps_count + lifecycleReviewCount
  } · 取消联动 ${summary.cancellation_attention_vps_count ?? 0}`
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
      detail: `异常 ${abnormalTotal} · 事件证据`,
      to: DASHBOARD_LINKS.eventsSevere,
      tone: 'critical',
      emphasis: true,
    }
  }

  if (abnormalTotal > 0) {
    return {
      label: '观测异常',
      value: abnormalTotal,
      detail: `监控实例 ${overview.abnormal_monitoring_instance_count} · 目标 ${overview.abnormal_target_count}`,
      to: overview.abnormal_monitoring_instance_count > 0 ? DASHBOARD_LINKS.monitoringAbnormal : DASHBOARD_LINKS.targetsAbnormal,
      tone: 'alert',
      emphasis: true,
    }
  }

  if (maintenanceTotal > 0) {
    return {
      label: '维护观察',
      value: maintenanceTotal,
      detail: `监控实例 ${overview.maintenance_monitoring_instance_count} · 目标 ${overview.maintenance_target_count}`,
      to: DASHBOARD_LINKS.eventsMaintenance,
      tone: 'maintenance',
      emphasis: true,
    }
  }

  return {
    label: '观测稳定',
    value: 0,
    detail: '无活跃异常',
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
      to: pressureTotal > 0 ? DASHBOARD_LINKS.assetDecisionsNeedsDecision : DASHBOARD_LINKS.vps,
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
      label: '取消联动',
      value: summary.cancellation_attention_vps_count ?? 0,
      detail: `已取消 ${summary.cancelled_vps_count ?? 0} · 仍运行 ${summary.running_cancelled_asset_count ?? 0}`,
      to: DASHBOARD_LINKS.assetDecisionsNeedsDecision,
      tone: (summary.cancellation_attention_vps_count ?? 0) > 0 ? 'alert' : 'normal',
    },
    {
      label: '30 天续费',
      value: summary.renewal_due_30d_vps_count,
      detail: `订阅 ${summary.renewal_due_30d_subscription_count}`,
      to: DASHBOARD_LINKS.assetDecisionsRenewal,
      tone: summary.renewal_due_30d_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '待决策',
      value: summary.unreviewed_vps_count,
      detail: '未评估',
      to: DASHBOARD_LINKS.assetDecisionsNeedsDecision,
      tone: summary.unreviewed_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '取消 / 迁移',
      value: lifecycleReviewCount,
      detail: `待取消 ${summary.to_cancel_vps_count} · 迁移 ${summary.to_migrate_vps_count}`,
      to: DASHBOARD_LINKS.assetDecisionsNeedsDecision,
      tone: lifecycleReviewCount > 0 ? 'alert' : 'normal',
    },
    {
      label: '未关联监控实例',
      value: summary.unlinked_vps_count,
      detail: '人工核对',
      to: DASHBOARD_LINKS.assetDecisionsEvidence,
      tone: summary.unlinked_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '关联异常',
      value: summary.abnormal_linked_vps_count,
      detail: '异常监控实例',
      to: DASHBOARD_LINKS.monitoringAbnormal,
      tone: summary.abnormal_linked_vps_count > 0 ? 'alert' : 'normal',
    },
    {
      label: '成本',
      value: summary.cost_by_currency.length,
      detail: formatAssetCost(summary),
      to: DASHBOARD_LINKS.assetDecisionsCost,
      tone: summary.cost_by_currency.length > 0 ? 'neutral' : 'normal',
    },
  ]
}

function observabilityRows(metrics: DashboardMetric[], overview: DashboardOverview): CommandRow[] {
  if (metrics.length === 0) {
    return [
      {
        label: 'VPS 主体',
        value: overview.total_monitoring_instance_count,
        detail: '从 VPS 详情页接入观测',
        to: DASHBOARD_LINKS.vps,
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
        label: '创建第一台 VPS',
        detail: '主体 · 入口',
        to: DASHBOARD_LINKS.vps,
        tone: 'notice',
        primary: true,
      },
      {
        label: '从 VPS 接入 agent',
        detail: '详情页 · 一键命令',
        to: DASHBOARD_LINKS.vps,
        tone: 'neutral',
      },
      {
        label: '创建第一个目标',
        detail: '配置服务入口',
        to: DASHBOARD_LINKS.targets,
        tone: 'neutral',
      },
    ]
  }

  const summary = overview.asset_summary
  const actions: NextAction[] = []
  const pressureTotal = assetPressureCount(summary)

  if (pressureTotal > 0) {
    const cancellationAttentionCount = summary.cancellation_attention_vps_count ?? 0
    const actionTarget = cancellationAttentionCount > 0
      ? DASHBOARD_LINKS.assetDecisionsMigrationRetirement
      : DASHBOARD_LINKS.assetDecisionsRenewal
    actions.push({
      label: cancellationAttentionCount > 0 ? '处理取消联动' : '进入组合决策',
      detail: `取消联动 ${cancellationAttentionCount} · 决策 ${summary.unreviewed_vps_count} · 续费 ${summary.renewal_due_30d_vps_count}`,
      to: actionTarget,
      tone: cancellationAttentionCount > 0 ? 'alert' : summary.renewal_due_30d_vps_count > 0 ? 'notice' : 'neutral',
      primary: true,
    })
  }

  if (severeTotal > 0) {
    actions.push({
      label: '处理严重事件',
      detail: `严重 ${severeTotal}`,
      to: DASHBOARD_LINKS.eventsSevere,
      tone: 'critical',
      primary: pressureTotal === 0,
    })
  }

  if (abnormalTotal > 0) {
    actions.push({
      label: '处理观测异常',
      detail: `监控实例 ${overview.abnormal_monitoring_instance_count} · 目标 ${overview.abnormal_target_count}`,
      to: overview.abnormal_monitoring_instance_count > 0 ? DASHBOARD_LINKS.monitoringAbnormal : DASHBOARD_LINKS.targetsAbnormal,
      tone: 'alert',
      primary: pressureTotal === 0 && severeTotal === 0,
    })
  }

  if (summary.unlinked_vps_count > 0) {
    actions.push({
      label: '核对未关联 VPS',
      detail: 'VPS ↔ 监控实例',
      to: DASHBOARD_LINKS.assetDecisionsEvidence,
      tone: 'notice',
    })
  }

  if (maintenanceTotal > 0) {
    actions.push({
      label: '查看维护事件',
      detail: `维护 ${maintenanceTotal}`,
      to: DASHBOARD_LINKS.eventsMaintenance,
      tone: 'maintenance',
      primary: actions.length === 0,
    })
  }

  if (actions.length === 0) {
    return [
      {
        label: '核对 VPS 库存',
        detail: 'provider / 续费 / 监控实例',
        to: DASHBOARD_LINKS.vps,
        tone: 'normal',
        primary: true,
      },
      {
        label: '查看 24h 事件流',
        detail: '异常 / 恢复',
        to: DASHBOARD_LINKS.events24h,
        tone: 'neutral',
      },
      {
        label: '进入资产决策',
        detail: '续费 / 决策',
        to: DASHBOARD_LINKS.assetDecisionsNeedsDecision,
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
            <article className={`dashboard-command-primary dashboard-command-primary--${primaryAction.tone}`}>
              <span className="dashboard-command-primary__eyebrow">今日第一步</span>
              <p className="dashboard-command-primary__detail">{primaryAction.detail}</p>
              <Link className="btn md primary dashboard-command-primary__action" to={primaryAction.to}>
                {primaryAction.label}
              </Link>
            </article>
          ) : null}
          <div className="dashboard-command-surface__secondary-controls">
            {onRefresh ? (
              <button
                type="button"
                className="btn sm ghost dashboard-command-surface__quiet-action"
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
        </div>
      </header>

      <div className="dashboard-command-grid">
        <section className={assetLaneClass} aria-label="资产组合决策">
          <div className="dashboard-command-lane__header">
            <div>
              <p className="dashboard-command-lane__eyebrow">资产组合决策</p>
              <h2>续费 / 决策 / 关联</h2>
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
              <h2>事件 / 监控实例 / 目标</h2>
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
                  aria-label={`处理${item.kind === 'monitoring_instance' ? '监控实例' : '目标'} ${item.name}`}
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

        <section className="dashboard-command-lane dashboard-command-lane--actions" aria-label="次级动作">
          <div className="dashboard-command-lane__header">
            <div>
              <p className="dashboard-command-lane__eyebrow">次级</p>
              <h2>后续</h2>
            </div>
          </div>
          <div className="dashboard-action-list">
            {actions.filter((action) => !action.primary).map((action, index) => (
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
                <span className="dashboard-action-row__cta">打开</span>
              </Link>
            ))}
          </div>
          <Link className="text-link dashboard-command-lane__footer-link" to={DASHBOARD_LINKS.events24h}>
            24h：{trendBalanceLabel(overview)}
          </Link>
        </section>
      </div>
    </section>
  )
}
