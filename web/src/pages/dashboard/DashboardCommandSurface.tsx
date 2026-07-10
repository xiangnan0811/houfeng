import { Link } from 'react-router-dom'

import { MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import type {
  DashboardJudgement,
  DashboardReadyModel,
  DashboardTone,
} from './dashboardModel'
import { DASHBOARD_LINKS } from './dashboardLinks'

type DashboardCommandSurfaceProps = {
  model: DashboardReadyModel
  supportingLoading: boolean
  onRetrySupporting?: () => void
}

function glyphState(tone: DashboardTone) {
  if (tone === 'neutral') return 'offline' as const
  return tone
}

function signalLabel(model: DashboardReadyModel): string {
  if (model.mode === 'stable' && model.degradations.length > 0) return '局部数据不可用'
  if (model.mode === 'onboarding') return '待接入'
  if (model.mode === 'stable' && model.tone === 'normal') return '摘要无异常'
  return model.title
}

function JudgementItem({ item }: { item: DashboardJudgement }) {
  return (
    <Link
      className={`dashboard-judgement dashboard-judgement--${item.tone}`}
      to={item.to}
      aria-label={`${item.label}：${item.value}；${item.detail}`}
    >
      <span className="dashboard-judgement__glyph" aria-hidden="true">
        <StatusGlyph state={glyphState(item.tone)} size="sm" />
      </span>
      <span className="dashboard-judgement__label">{item.label}</span>
      <strong className="dashboard-judgement__value">{item.value}</strong>
      <span className="dashboard-judgement__detail">{item.detail}</span>
    </Link>
  )
}

export function DashboardCommandSurface({
  model,
  supportingLoading,
  onRetrySupporting,
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
          <p className="dashboard-decision-surface__eyebrow">日常入口</p>
          <h1>工作台</h1>
          <h2>{model.title}</h2>
          <p className="dashboard-decision-surface__summary">{model.summary}</p>
        </div>

        <section
          className={`dashboard-primary-action dashboard-primary-action--${model.tone}`}
          aria-labelledby="dashboard-primary-action-title"
        >
          <h2 id="dashboard-primary-action-title">今日第一步</h2>
          <p>{model.title}</p>
          <Link className="btn md primary" to={model.primaryAction.to}>
            {model.primaryAction.label}
          </Link>
        </section>
      </header>

      <section
        className="dashboard-judgement-rail"
        aria-label="判断摘要"
      >
        {model.judgements.map((item) => (
          <JudgementItem item={item} key={item.id} />
        ))}
      </section>

      {onRetrySupporting ? (
        <div className="dashboard-degradation" role="status" aria-label="局部数据不可用">
          <div>
            <strong>局部数据不可用</strong>
            <span>已保留成功的 Dashboard 摘要，不会把失败表示成真实空数据。</span>
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

      <div className="dashboard-evidence-grid">
        <section className="dashboard-evidence-lane" aria-labelledby="dashboard-observation-title">
          <div className="dashboard-evidence-lane__header">
            <div>
              <p>当前事实</p>
              <h2 id="dashboard-observation-title">观测证据</h2>
            </div>
            <Link className="text-link" to={DASHBOARD_LINKS.events24h}>查看事件流</Link>
          </div>
          <div className="dashboard-evidence-metrics">
            <span aria-label={`异常监控实例 ${observation.abnormalMonitoringCount}`}>
              异常监控实例 <MonoDigits>{observation.abnormalMonitoringCount}</MonoDigits>
            </span>
            <span aria-label={`其中严重监控实例 ${observation.severeMonitoringCount}`}>
              其中严重 <MonoDigits>{observation.severeMonitoringCount}</MonoDigits>
            </span>
            <span aria-label={`异常目标 ${observation.abnormalTargetCount}`}>
              异常目标 <MonoDigits>{observation.abnormalTargetCount}</MonoDigits>
            </span>
            <span aria-label={`维护对象 ${observation.maintenanceTotal}`}>
              维护对象 <MonoDigits>{observation.maintenanceTotal}</MonoDigits>
            </span>
          </div>
          {observation.attentionItems.length > 0 ? (
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
          ) : (
            <p className="dashboard-evidence-lane__empty">
              {model.mode === 'onboarding' ? '尚未建立观测对象。' : '当前摘要没有异常对象。'}
            </p>
          )}
        </section>

        <section className="dashboard-evidence-lane" aria-labelledby="dashboard-assets-title">
          <div className="dashboard-evidence-lane__header">
            <div>
              <p>来源与精度</p>
              <h2 id="dashboard-assets-title">资产与账单证据</h2>
            </div>
            <Link className="text-link" to={DASHBOARD_LINKS.assetDecisionsNeedsDecision}>
              进入资产决策
            </Link>
          </div>
          <div className="dashboard-source-list">
            <article className={`dashboard-source dashboard-source--${model.assetEvidence.status}`}>
              <div className="dashboard-source__header">
                <h3>{model.assetEvidence.title}</h3>
                <span>来源：VPS 清单</span>
              </div>
              <p>{model.assetEvidence.detail}</p>
              {model.assetEvidence.loadedAt ? (
                <small>读取于 <Timestamp value={model.assetEvidence.loadedAt} mode="absolute" /></small>
              ) : null}
            </article>
            <article className={`dashboard-source dashboard-source--${model.billingEvidence.status}`}>
              <div className="dashboard-source__header">
                <h3>{billingUnavailable ? '订阅摘要不可用' : model.billingEvidence.title}</h3>
                <span>
                  来源：{model.billingEvidence.source === 'subscription-overview'
                    ? '订阅摘要'
                    : 'Dashboard 聚合摘要'}
                </span>
              </div>
              {billingUnavailable ? <strong>{model.billingEvidence.title}</strong> : null}
              <p>{model.billingEvidence.detail}</p>
              <small>
                生成于 <Timestamp value={model.billingEvidence.generatedAt} mode="absolute" />
              </small>
            </article>
          </div>
        </section>
      </div>
    </section>
  )
}
