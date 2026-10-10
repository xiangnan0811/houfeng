import { Link } from 'react-router-dom'

import { MonoDigits, Sparkline, StatusGlyph, Timestamp } from '../../components/atoms'
import type {
  DashboardJudgement,
  DashboardObservabilityModel,
  DashboardReadyModel,
  DashboardTone,
} from './dashboardModel'
import type { DashboardMonitoringCoverage } from './dashboardCoverage'
import { DASHBOARD_LINKS } from './dashboardLinks'
import { DashboardActivityPanel, DashboardRenewalsPanel } from './DashboardPanels'
import { trendTotal, type DashboardActivityItem, type DashboardRenewalPanel } from './dashboardPanels'

type DashboardCommandSurfaceProps = {
  model: DashboardReadyModel
  /** 24h 新增异常逐小时计数；后端未提供时为 null，不绘制趋势。 */
  incidentTrend: number[] | null
  activity: DashboardActivityItem[]
  renewals: DashboardRenewalPanel
  supportingLoading: boolean
  onRetrySupporting?: () => void
  overviewRefreshFailed?: boolean
  /** 有观测对象但没有可用通知渠道，失联与告警不会推送。 */
  notificationGap?: boolean
}

function glyphState(tone: DashboardTone) {
  if (tone === 'neutral') return 'offline' as const
  return tone
}

function signalLabel(model: DashboardReadyModel): string {
  if (model.mode === 'stable' && model.degradations.length > 0) return '局部数据不可用'
  if (model.mode === 'onboarding') return '待接入'
  if (model.mode === 'critical') return '严重'
  if (model.mode === 'abnormal') return '异常'
  if (model.mode === 'maintenance') return '维护'
  if (model.title === '尚有目标无观测') return '尚有目标无观测'
  if (model.title === '观测已过期') return '观测已过期'
  if (model.mode === 'stable' && model.tone === 'normal') return '运行异常 0'
  if (model.mode === 'stable') return '待核对'
  return model.title
}

function JudgementItem({ item, trend }: { item: DashboardJudgement; trend: number[] | null }) {
  // 趋势图对辅助技术隐藏，但其信息（24h 新增合计）进入链接的可访问名称。
  const trendSummary = trend ? `24 小时新增异常 ${trendTotal(trend)} 次` : null
  return (
    <Link
      className={`dashboard-judgement dashboard-judgement--${item.tone}`}
      to={item.to}
      aria-label={`${item.label}：${item.value}；${item.detail}${trendSummary ? `；${trendSummary}` : ''}`}
    >
      <span className="dashboard-judgement__label">
        <span className="dashboard-judgement__glyph" aria-hidden="true">
          <StatusGlyph state={glyphState(item.tone)} size="sm" />
        </span>
        {item.label}
      </span>
      <strong className="dashboard-judgement__value">{item.value}</strong>
      <span className="dashboard-judgement__detail">{item.detail}</span>
      {trend ? (
        <span className="dashboard-judgement__trend" aria-hidden="true">
          <Sparkline values={trend} tone={item.tone === 'critical' || item.tone === 'alert' ? item.tone : 'default'} width={96} height={28} />
          <small>24h 新增 {trendTotal(trend)}</small>
        </span>
      ) : null}
    </Link>
  )
}

function StaleObservationFacts({
  observation,
  ownsGlobalCount,
}: {
  observation: DashboardObservabilityModel
  ownsGlobalCount: boolean
}) {
  const showGlobal = observation.staleTargetCount > 0 && !ownsGlobalCount
  if (!showGlobal && observation.staleGroups.length === 0) return null
  return (
    <div className="dashboard-stale-facts">
      {showGlobal ? (
        <Link to={DASHBOARD_LINKS.targetsStale} aria-label={`观测过期 ${observation.staleTargetCount}`}>
          观测过期 <MonoDigits>{observation.staleTargetCount}</MonoDigits>
        </Link>
      ) : null}
      {observation.staleGroups.length > 0 ? (
        <ul className="dashboard-stale-groups" aria-label="分组观测过期">
          {observation.staleGroups.map((group) => (
            <li key={group.group}>
              <Link to={group.to} aria-label={`${group.group} 观测过期 ${group.staleTargetCount}`}>
                <span>{group.group}</span>
                <MonoDigits>{group.staleTargetCount}</MonoDigits>
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

export function DashboardCommandSurface({
  model,
  incidentTrend,
  activity,
  renewals,
  supportingLoading,
  onRetrySupporting,
  overviewRefreshFailed = false,
  notificationGap = false,
}: DashboardCommandSurfaceProps) {
  const observation = model.observability
  const billingUnavailable = model.billingEvidence.status === 'unavailable'

  return (
    <section
      className={`dashboard-decision-surface dashboard-decision-surface--${model.tone}`}
      aria-label="工作台决策面"
    >
      <header className="dashboard-decision-surface__header">
        <div className="dashboard-decision-surface__intro">
          <div className="dashboard-decision-surface__meta">
            <span className="dashboard-decision-surface__signal">
              <StatusGlyph state={model.tone} size="sm" />
              {signalLabel(model)}
            </span>
            <span className="dashboard-decision-surface__generated">
              摘要生成 <Timestamp value={model.snapshotGeneratedAt} mode="absolute" />
            </span>
          </div>
          <h1>工作台</h1>
          <h2>{model.title}</h2>
        </div>

        <section
          className={`dashboard-primary-action dashboard-primary-action--${model.tone}`}
          aria-label="今日第一步"
        >
          <Link className="btn md primary" to={model.primaryAction.to}>
            {model.primaryAction.label}
          </Link>
        </section>
      </header>

      {overviewRefreshFailed ? (
        <p className="dashboard-refresh-failure" role="status">
          更新失败，显示上次结果
          <Timestamp value={model.snapshotGeneratedAt} mode="absolute" />
        </p>
      ) : null}

      {onRetrySupporting ? (
        <div className="dashboard-degradation" role="status" aria-label="局部数据不可用">
          <div>
            <strong>局部数据不可用</strong>
            <span>已成功的摘要仍保留，失败的部分不会显示成空库存。</span>
          </div>
          <button
            type="button"
            className="btn sm secondary"
            disabled={supportingLoading}
            onClick={onRetrySupporting}
          >
            {supportingLoading ? '重试中…' : '重试局部数据'}
          </button>
        </div>
      ) : null}

      {notificationGap ? (
        <div className="dashboard-degradation dashboard-notification-gap" role="note" aria-label="未配置通知渠道">
          <div>
            <strong>未配置通知渠道</strong>
            <span>主机失联和告警只会显示在页面里，不会推送给你。</span>
          </div>
          <Link className="btn sm secondary" to={DASHBOARD_LINKS.notificationSettings}>配置通知</Link>
        </div>
      ) : null}

      <section className="dashboard-judgement-rail" aria-label="判断摘要">
        {model.judgements.map((item) => (
          <JudgementItem item={item} key={item.id} trend={item.id === 'observability' ? incidentTrend : null} />
        ))}
      </section>

      {/* DOM 顺序即单栏顺序：可行动的即将续费排在证据之前；两栏时由 grid 区域放回右栏。 */}
      <div className="dashboard-workspace">
      <div className="dashboard-workspace__renewals">
        <DashboardRenewalsPanel panel={renewals} />
      </div>
      <div className="dashboard-workspace__main">
        <section className="dashboard-evidence-lane" aria-labelledby="dashboard-observation-title">
          <div className="dashboard-evidence-lane__header">
            <h2 id="dashboard-observation-title">观测证据</h2>
            <Link className="text-link text-link--action" to={DASHBOARD_LINKS.events24h}>查看事件流</Link>
          </div>
          <p className="dashboard-evidence-lane__provenance">
            来源：工作台摘要
            {' · '}
            摘要生成 <Timestamp value={model.snapshotGeneratedAt} mode="absolute" />
          </p>
          <p>已有历史观测不推断当前健康。</p>
          {model.monitoringCoverage ? <MonitoringCoverageFact coverage={model.monitoringCoverage} /> : null}
          {observation.unobservedTargetCount > 0 && model.mode !== 'stable' ? (
            <Link to={DASHBOARD_LINKS.targetsUnobserved}>
              尚无观测 <MonoDigits>{observation.unobservedTargetCount}</MonoDigits>
            </Link>
          ) : null}
          <StaleObservationFacts
            observation={observation}
            ownsGlobalCount={model.judgements.some((item) => item.id === 'observability' && item.to === DASHBOARD_LINKS.targetsStale)}
          />
          {observation.attentionItems.length === 0 ? (
            <p className="dashboard-evidence-lane__empty">
              {model.mode === 'onboarding'
                ? '尚未建立观测对象。'
                : observation.unobservedTargetCount > 0
                  ? '已知异常预览为空。'
                  : '当前摘要没有异常对象。'}
            </p>
          ) : (
            <ul className="dashboard-attention-list" aria-label="最高优先级异常对象">
              {observation.attentionItems.map((item) => (
                <li key={`${item.kind}-${item.id}`}>
                  <Link
                    className={`dashboard-attention-link dashboard-attention-link--${item.tone}`}
                    to={item.to}
                    aria-label={`${item.name}：${item.detail}`}
                  >
                    <StatusGlyph state={glyphState(item.tone)} size="sm" />
                    <span className="dashboard-attention-link__copy">
                      <strong>{item.name}</strong>
                      <span>{item.detail}</span>
                    </span>
                    <span className="dashboard-attention-link__meta">{item.meta}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="dashboard-evidence-lane" aria-labelledby="dashboard-assets-title">
          <div className="dashboard-evidence-lane__header">
            <h2 id="dashboard-assets-title">资产与账单证据</h2>
            <Link className="text-link text-link--action" to={DASHBOARD_LINKS.assetDecisionsNeedsDecision}>
              进入资产决策
            </Link>
          </div>
          <div className="dashboard-source-list">
            <article className={`dashboard-source dashboard-source--${model.assetEvidence.status}`}>
              <div className="dashboard-source__header">
                <h3>{model.assetEvidence.status === 'available' && (model.assetEvidence.vpsCount ?? 0) > 0 ? 'VPS 清单已读取' : model.assetEvidence.title}</h3>
                <span>VPS 清单</span>
              </div>
              {model.assetEvidence.status === 'available' && (model.assetEvidence.vpsCount ?? 0) > 0 ? (
                <p>来源为 VPS 清单。</p>
              ) : (
                <p>{model.assetEvidence.detail}</p>
              )}
              {model.assetEvidence.loadedAt ? (
                <small>读取于 <Timestamp value={model.assetEvidence.loadedAt} mode="absolute" /></small>
              ) : null}
            </article>
            <article className={`dashboard-source dashboard-source--${model.billingEvidence.status}`}>
              <div className="dashboard-source__header">
                <h3>{billingUnavailable ? '订阅摘要不可用' : model.billingEvidence.status === 'available' ? '订阅金额完整性' : model.billingEvidence.title}</h3>
                <span>
                  {model.billingEvidence.source === 'subscription-overview'
                    ? '订阅摘要'
                    : 'Dashboard 聚合摘要'}
                </span>
              </div>
              {billingUnavailable ? <strong>{model.billingEvidence.title}</strong> : null}
              <p>{model.billingEvidence.completeness}</p>
              {model.billingEvidence.status === 'available' ? null : <p>{model.billingEvidence.detail}</p>}
              <small>
                生成于 <Timestamp value={model.billingEvidence.generatedAt} mode="absolute" />
              </small>
            </article>
          </div>
        </section>
      </div>
      <div className="dashboard-workspace__activity">
        <DashboardActivityPanel items={activity} />
      </div>
      </div>
    </section>
  )
}

// 覆盖事实来自 VPS 清单，单独标注来源与读取时间，不与工作台摘要的时间混用。
function MonitoringCoverageFact({ coverage }: { coverage: DashboardMonitoringCoverage }) {
  if (coverage.status === 'loading') {
    return <p className="dashboard-coverage">监控覆盖读取中…</p>
  }
  if (coverage.status === 'unavailable') {
    return <p className="dashboard-coverage">监控覆盖暂不可用：VPS 清单读取失败。</p>
  }
  return (
    <p className="dashboard-coverage">
      <span>
        监控覆盖：在用 VPS 中 <MonoDigits>{coverage.linked}</MonoDigits>/<MonoDigits>{coverage.total}</MonoDigits> 台已关联监控实例
      </span>
      {coverage.linked < coverage.total ? (
        <Link className="text-link" to={DASHBOARD_LINKS.vpsUnlinked}>查看未关联</Link>
      ) : null}
      <small>
        来源：VPS 清单 · 读取于 <Timestamp value={coverage.loadedAt} mode="absolute" />
      </small>
    </p>
  )
}
