import type { ReactNode } from 'react'

import { Badge, Timestamp } from '../../components/atoms'
import type { BadgeTone } from '../../components/atoms/Badge'
import type { VPSOverviewSectionState } from '../../lib/types'
import type { OverviewObservationTone } from '../../lib/vpsOverviewPresentation'
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
      {...(labelledBy ? { 'aria-labelledby': labelledBy } : ariaLabel ? { 'aria-label': ariaLabel } : {})}
    >


      <div className="vps-observation__head vps-observation__cols" role="row">
        <span role="columnheader">项目</span>
        <span role="columnheader">主要结论</span>
        <span role="columnheader">说明</span>
        <span role="columnheader">数据时间</span>
        <span role="columnheader">操作</span>
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

        return (
          <div
            key={row.key}
            className="vps-observation__object"
            role="rowgroup"
            aria-label={row.ariaLabel || row.project}
          >
            <div className="vps-observation__row vps-observation__cols" role="row">
              <div className="vps-observation__project" role="cell" data-label="项目">
                <span className="vps-observation__slot">{row.project}</span>
              </div>
              <div className="vps-observation__conclusion" role="cell" data-label="主要结论">
                <span className="vps-observation__slot">
                  {conclusion ? (
                    <Badge variant="state" tone={conclusionBadgeTone}>{conclusion}</Badge>
                  ) : null}
                </span>
              </div>
              <div
                className="vps-observation__description vps-overview-summary__detail"
                role="cell"
                data-label="说明"
              >
                <span className="vps-observation__slot">
                  {description && description !== conclusion ? description : null}
                </span>
              </div>
              <div className="vps-observation__time" role="cell" data-label="数据时间">
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

              <div className="vps-observation__action" role="cell" data-label="操作">
                <span className="vps-observation__slot">
                  {row.action}
                  {degraded && row.onRetry ? (
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
