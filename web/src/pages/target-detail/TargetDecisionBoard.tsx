import { Link } from 'react-router-dom'

import { Badge, Button, StatusGlyph, Timestamp } from '../../components/atoms'
import type { AssetContextForTarget, ProbeItemRecord, ProbeObservation, TargetRecord } from '../../lib/types'
import {
  assetContextHasAttention,
  assetContextMessage,
  assetContextPrimarySummary,
  subscriptionStateLabel,
  vpsLifecycleLabel,
  vpsRenewalDecisionLabel,
} from '../assetContextSummary'
import { buildTargetDecisionModel, toneToGlyphState } from './targetDecisionModel'

type TargetDecisionBoardProps = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  recentObservations: ProbeObservation[]
  latestObservationAt: string | null
  latencySampleCount: number
  assetContext: AssetContextForTarget | null
  assetContextError: string | null
  onOpenHistory: () => void
}

export function TargetDecisionBoard({
  target,
  probeItems,
  recentObservations,
  latestObservationAt,
  latencySampleCount,
  assetContext,
  assetContextError,
  onOpenHistory,
}: TargetDecisionBoardProps) {
  const { nextAction, evidenceItems } = buildTargetDecisionModel({
    target,
    probeItems,
    recentObservations,
    latestObservationAt,
    latencySampleCount,
    onOpenHistory,
  })
  const primaryContext = assetContextPrimarySummary(assetContext)

  return (
    <section className="target-decision-board page-panel" aria-label="目标判断摘要">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">TARGET DECISION</p>
          <h2>目标判断</h2>
          <p className="section-heading__description">先看下一步动作，再核对健康状态、ProbeItem 覆盖与观测证据。</p>
        </div>
      </div>

      <div className={`target-decision-board__lead target-decision-board__lead--${nextAction.tone}`}>
        <article className="target-decision-next">
          <p>下一步动作</p>
          <h3>{nextAction.title}</h3>
          <span>{nextAction.summary}</span>
          <div className="target-decision-next__actions">
            {nextAction.onAction && nextAction.buttonLabel ? (
              <Button variant="primary" size="sm" onClick={nextAction.onAction}>
                {nextAction.buttonLabel}
              </Button>
            ) : (
              <span className="target-decision-next__hint">在右上角操作菜单处理</span>
            )}
          </div>
        </article>
        <div className="target-decision-evidence" aria-label="目标判断证据状态">
          {evidenceItems.map((item) => (
            <article
              key={item.label}
              className={`target-decision-evidence__item target-decision-evidence__item--${item.tone}`}
            >
              <div className="target-decision-evidence__label">
                <span aria-hidden="true">
                  <StatusGlyph state={toneToGlyphState(item.tone)} size="sm" />
                </span>
                <span>{item.label}</span>
              </div>
              <strong>
                {item.label === '最近观测' && item.value === '有观测' && latestObservationAt ? (
                  <Timestamp value={latestObservationAt} mode="relative" />
                ) : (
                  item.value
                )}
              </strong>
              <small>{item.meta}</small>
            </article>
          ))}
        </div>
      </div>

      <div className="asset-context-panel">
        <div>
          <span className="asset-context-panel__label">资产上下文</span>
          {primaryContext ? (
            <>
              <strong>{assetContextMessage(assetContext)}</strong>
              <small>
                {primaryContext.display_name} · {vpsLifecycleLabel(primaryContext.lifecycle_status)} ·
                续费 {vpsRenewalDecisionLabel(primaryContext.renewal_decision)} ·
                订阅 {subscriptionStateLabel(primaryContext.subscription_state)}
              </small>
            </>
          ) : (
            <>
              <strong>{assetContextError ? '资产上下文暂不可用' : '未关联 VPS'}</strong>
              <small>{assetContextError ?? '当前 Target 没有通过服务或域名挂载到 VPS。'}</small>
            </>
          )}
        </div>
        <Badge variant="state" tone={assetContextHasAttention(assetContext) ? 'notice' : 'normal'}>
          {assetContextHasAttention(assetContext) ? '需联动处理' : '已同步'}
        </Badge>
        {primaryContext ? (
          <>
            <Link className="btn sm secondary" to={`/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement&vps_id=${encodeURIComponent(primaryContext.vps_id)}`}>
              组合决策
            </Link>
            <Link className="btn sm secondary" to={`/vps/${primaryContext.vps_id}?workbench=cancellation`}>
              打开工作台
            </Link>
          </>
        ) : null}
      </div>
    </section>
  )
}
