import { Button, StatusGlyph, Timestamp } from '../../components/atoms'
import type { ProbeItemRecord, ProbeObservation, TargetRecord } from '../../lib/types'
import { buildTargetDecisionModel, toneToGlyphState } from './targetDecisionModel'

type TargetDecisionBoardProps = {
  target: TargetRecord
  probeItems: ProbeItemRecord[]
  recentObservations: ProbeObservation[]
  latestObservationAt: string | null
  latencySampleCount: number
  onOpenHistory: () => void
}

export function TargetDecisionBoard({
  target,
  probeItems,
  recentObservations,
  latestObservationAt,
  latencySampleCount,
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
    </section>
  )
}
