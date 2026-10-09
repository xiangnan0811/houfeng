import type { ReactNode } from 'react'

import { Badge, Timestamp } from '../../components/atoms'
import type { BadgeTone } from '../../components/atoms/Badge'
import type { VPSOverviewSectionState } from '../../lib/types'
import type { DiagnosticNote, OverviewObservationTone } from '../../lib/vpsOverviewPresentation'
import { VPSOverviewFreshnessRetry, VPSOverviewFreshnessTime } from './VPSOverviewFreshness'
import {
  overviewSourceReason,
  overviewSourceStateLabel,
} from './vpsOverviewFreshness'

export type VPSObservationRowModel = {
  key: string
  project: string
  ariaLabel?: string
  conclusion: string
  conclusionTone?: OverviewObservationTone | ''
  description?: string
  diagnostics?: DiagnosticNote[]
  section?: VPSOverviewSectionState
  sourceLabel?: string
  time?: string | null
  lastSuccessAt?: string | null
  timeLabel?: string
  action?: ReactNode
  onRetry?: () => void
  retrying?: boolean
  retryScope?: string
  retryMode?: 'refresh' | 'retry'
  retained?: boolean
  secondaryReason?: string | null
}

const TONE_TO_BADGE: Record<string, BadgeTone> = {
  ok: 'normal',
  notice: 'notice',
  alert: 'alert',
  unknown: 'neutral',
}

// 窄屏键值堆叠时，没有内容的单元不能只剩一个标签占行；桌面表格仍保留该列以维持对齐。
// 隐藏的单元会离开无障碍树，所以表头与单元都写固定的 aria-colindex，剩余单元不会被读成前一列。
function cellClass(base: string, hasContent: boolean): string {
  return hasContent ? base : `${base} vps-observation__cell--empty`
}

export function VPSObservationRows({
  rows,
  labelledBy,
  ariaLabel,
}: {
  rows: VPSObservationRowModel[]
  labelledBy?: string
  ariaLabel?: string
}) {
  const multiAction = rows.some((row) => Boolean(row.action) && Boolean(row.onRetry))
  return (
    <div
      className={['vps-observation', multiAction ? 'vps-observation--multi-action' : ''].filter(Boolean).join(' ')}
      role="table"
      aria-colcount={5}
      {...(labelledBy ? { 'aria-labelledby': labelledBy } : ariaLabel ? { 'aria-label': ariaLabel } : {})}
    >


      <div className="vps-observation__head vps-observation__cols" role="row">
        <span role="columnheader" aria-colindex={1}>项目</span>
        <span role="columnheader" aria-colindex={2}>主要结论</span>
        <span role="columnheader" aria-colindex={3}>说明</span>
        <span role="columnheader" aria-colindex={4}>数据时间</span>
        <span role="columnheader" aria-colindex={5}>操作</span>
      </div>
      {rows.map((row) => {
        const sourceLabel = row.sourceLabel || row.project
        const section = row.section
        const retained = row.retained === true
        const reason = row.secondaryReason ?? (section ? overviewSourceReason(section, sourceLabel, { retained }) : null)
        const stateLabel = section ? overviewSourceStateLabel(section) : null
        const degraded = section?.state === 'stale' || section?.state === 'unavailable'
        const conclusion = row.conclusion.trim()
        const description = row.description?.trim() ?? ''
        const showFreshnessBadge = Boolean(stateLabel)
          && stateLabel !== conclusion
          && !(retained && section?.state === 'unavailable')
        const conclusionBadgeTone = (row.conclusionTone && TONE_TO_BADGE[row.conclusionTone]) || 'neutral'
        const retryMode = row.retryMode ?? (section?.state === 'stale' ? 'refresh' : 'retry')
        const showDescription = Boolean(description) && description !== conclusion
        const hasDescription = showDescription || Boolean(row.diagnostics?.length)
        const showRetry = degraded && Boolean(row.onRetry)
        const hasAction = Boolean(row.action) || showRetry
        // 与 VPSOverviewFreshnessTime（bare）的取值一致：有来源但还没有任何时间时，这一格是空的。
        const timeValue = section
          ? section.observed_at || row.time || (row.lastSuccessAt ?? section.last_success_at)
          : row.time
        const hasTime = showFreshnessBadge || Boolean(timeValue)

        return (
          <div
            key={row.key}
            className="vps-observation__object"
            role="rowgroup"
            aria-label={row.ariaLabel || row.project}
          >
            <div className="vps-observation__row vps-observation__cols" role="row">
              <div className="vps-observation__project" role="cell" aria-colindex={1} data-label="项目">
                <span className="vps-observation__slot">{row.project}</span>
              </div>
              <div className="vps-observation__conclusion" role="cell" aria-colindex={2} data-label="主要结论">
                <span className="vps-observation__slot">
                  {conclusion ? (
                    <Badge variant="state" tone={conclusionBadgeTone}>{conclusion}</Badge>
                  ) : null}
                </span>
              </div>
              <div
                className={cellClass('vps-observation__description vps-overview-summary__detail', hasDescription)}
                role="cell"
                aria-colindex={3}
                data-label="说明"
              >
                <span className="vps-observation__slot">
                  {showDescription ? description : null}
                  {row.diagnostics && row.diagnostics.length > 0 ? (
                    <details className="vps-observation__diagnostic">
                      <summary>诊断信息</summary>
                      <dl>
                        {row.diagnostics.map((item, index) => (
                          <div key={`${item.label}-${index}`}>
                            <dt>{item.label}</dt>
                            <dd>{item.detail}</dd>
                          </div>
                        ))}
                      </dl>
                    </details>
                  ) : null}
                </span>
              </div>
              <div className={cellClass('vps-observation__time', hasTime)} role="cell" aria-colindex={4} data-label="数据时间">
                <span className="vps-observation__slot">
                  {showFreshnessBadge ? (
                    <Badge variant="state" tone={section?.state === 'stale' ? 'notice' : 'alert'}>{stateLabel}</Badge>
                  ) : null}
                  {section ? (
                    <VPSOverviewFreshnessTime
                      section={section}
                      fallback={row.time ?? null}
                      lastSuccessAt={row.lastSuccessAt ?? null}
                      timeLabel={row.timeLabel || '数据时间'}
                      bare
                    />
                  ) : row.time ? (
                    <Timestamp value={row.time} mode="absolute" />
                  ) : null}
                </span>
              </div>

              <div className={cellClass('vps-observation__action', hasAction)} role="cell" aria-colindex={5} data-label="操作">
                <span className="vps-observation__slot">
                  {row.action}
                  {showRetry && row.onRetry ? (
                    <VPSOverviewFreshnessRetry
                      sourceLabel={sourceLabel}
                      onRetry={row.onRetry}
                      retrying={Boolean(row.retrying)}
                      mode={retryMode}
                      scopeLabel={row.retryScope ?? '概览'}
                    />
                  ) : null}
                </span>
              </div>
            </div>
            {reason ? (
              <div className="vps-observation__secondary vps-observation__cols" role="row">
                <div className="vps-observation__reason" role="cell" aria-colindex={3}>{reason}</div>
              </div>
            ) : null}
          </div>
        )
      })}
    </div>
  )
}
