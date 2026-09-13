import { Link, useLocation } from 'react-router-dom'

import { Button, StatusGlyph, Timestamp } from '../../components/atoms'
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
  const location = useLocation()
  const { nextAction, evidenceItems } = buildTargetDecisionModel({
    target,
    probeItems,
    recentObservations,
    latestObservationAt,
    latencySampleCount,
    onOpenHistory,
  })
  const primaryContext = assetContextPrimarySummary(assetContext)
  const contextAttention = assetContextHasAttention(assetContext)
  const showSummary = nextAction.tone !== 'normal' || Boolean(nextAction.onAction)

  return (
    <section className="target-decision-board" aria-label="目标判断摘要">
      <div className={`target-decision__conclusion target-decision__conclusion--${nextAction.tone}`}>
        {nextAction.tone !== 'normal' ? (
          <StatusGlyph state={toneToGlyphState(nextAction.tone)} size="sm" />
        ) : null}
        <div className="target-decision__copy">
          <h2>{nextAction.title}</h2>
          {showSummary ? <p>{nextAction.summary}</p> : null}
          {nextAction.onAction && nextAction.buttonLabel ? (
            <Button variant="primary" size="sm" onClick={nextAction.onAction}>
              {nextAction.buttonLabel}
            </Button>
          ) : null}
        </div>
      </div>

      <dl className="target-decision__facts" aria-label="目标判断证据状态">
        {evidenceItems.map((item) => (
          <div
            key={item.label}
            className={
              item.tone === 'normal'
                ? 'target-decision__fact'
                : `target-decision__fact target-decision__fact--${item.tone}`
            }
          >
            <dt>
              {item.tone !== 'normal' ? (
                <StatusGlyph state={toneToGlyphState(item.tone)} size="sm" />
              ) : null}
              {item.label}
            </dt>
            <dd>
              <strong>
                {item.label === '最近观测' && item.value === '有观测' && latestObservationAt ? (
                  <Timestamp value={latestObservationAt} mode="relative" />
                ) : (
                  item.value
                )}
              </strong>
              {item.meta ? <span>{item.meta}</span> : null}
            </dd>
          </div>
        ))}
      </dl>

      <div
        className={
          contextAttention
            ? 'target-decision__context target-decision__context--attention'
            : 'target-decision__context'
        }
      >
        {primaryContext ? (
          <>
            <span>{assetContextMessage(assetContext)}</span>
            <span>
              {vpsLifecycleLabel(primaryContext.lifecycle_status)}
              {' · '}
              续费 {vpsRenewalDecisionLabel(primaryContext.renewal_decision)}
              {' · '}
              订阅 {subscriptionStateLabel(primaryContext.subscription_state)}
            </span>
            {contextAttention ? <span>需联动处理</span> : null}
            <Link
              className="text-link"
              to={`/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement&vps_id=${encodeURIComponent(primaryContext.vps_id)}`}
            >
              组合决策
            </Link>
            <Link
              className="text-link"
              to={`/vps/${primaryContext.vps_id}?workbench=cancellation`}
              state={location.state}
            >
              打开工作台
            </Link>
          </>
        ) : (
          <span>{assetContextError ? `资产上下文暂不可用。${assetContextError}` : '未关联 VPS'}</span>
        )}
      </div>
    </section>
  )
}
