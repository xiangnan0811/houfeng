import { Badge, Button, Timestamp } from '../../components/atoms'
import type { VPSOverviewSectionState } from '../../lib/types'
import {
  overviewSourceReason,
  overviewSourceStateLabel,
  overviewSourceTimes,
} from './vpsOverviewFreshness'

type Props = {
  section: VPSOverviewSectionState
  sourceLabel: string
  onRetry?: (() => void) | undefined
  retrying: boolean
  omitReadyTime?: boolean
  hideStateLabel?: boolean
  retained?: boolean
  timeLabel?: string
  scopeLabel?: string
}

export function VPSOverviewFreshnessRetry({
  sourceLabel,
  onRetry,
  retrying,
  mode = 'retry',
  scopeLabel = '概览',
}: {
  sourceLabel: string
  onRetry: () => void
  retrying: boolean
  mode?: 'refresh' | 'retry'
  scopeLabel?: string
}) {
  const verb = mode === 'refresh' ? '刷新' : '重试'
  const scope = scopeLabel.trim()
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      className="vps-overview-freshness__retry"
      aria-label={scope ? `${verb} ${scope} ${sourceLabel}` : `${verb} ${sourceLabel}`}
      disabled={retrying}
      onClick={onRetry}
    >
      {retrying ? `${verb}中…` : verb}
    </Button>
  )
}


export function VPSOverviewFreshnessTime({
  section,
  fallback,
  lastSuccessAt,
  timeLabel = '数据时间',
  omitReadyTime = false,
  bare = false,
}: {
  section?: VPSOverviewSectionState
  fallback?: string | null
  lastSuccessAt?: string | null
  timeLabel?: string
  omitReadyTime?: boolean
  bare?: boolean
}) {
  const times = section ? overviewSourceTimes(section) : {
    observedAt: fallback ?? null,
    lastSuccessAt: lastSuccessAt ?? null,
    sameSuccess: Boolean(fallback && lastSuccessAt && fallback === lastSuccessAt),
    displayAt: fallback ?? lastSuccessAt ?? null,
  }
  const degraded = section?.state === 'stale' || section?.state === 'unavailable'
  const observedAt = times.observedAt || fallback || null
  const successAt = lastSuccessAt ?? times.lastSuccessAt
  const readyTime = !degraded && !omitReadyTime ? (observedAt || successAt) : null

  if (bare) {
    const value = observedAt || successAt
    if (!value) return null
    return (
      <>
        <Timestamp value={value} mode="absolute" />
        {observedAt && successAt && !times.sameSuccess ? (
          <span className="vps-observation__time-success">
            最近成功
            {' '}
            <Timestamp value={successAt} mode="absolute" />
          </span>
        ) : null}
      </>
    )
  }

  if (readyTime) {
    return (
      <p className="vps-overview-freshness__quiet">
        <span>{timeLabel}</span>
        {' '}
        <Timestamp value={readyTime} mode="absolute" />
      </p>
    )
  }

  if (!degraded) return null
  if (!observedAt && !successAt) return null

  return (
    <dl className="vps-overview-freshness__times">
      {observedAt ? (
        <div>
          <dt>{timeLabel}</dt>
          <dd><Timestamp value={observedAt} mode="absolute" /></dd>
        </div>
      ) : null}
      {successAt && !times.sameSuccess ? (
        <div>
          <dt>最近成功</dt>
          <dd><Timestamp value={successAt} mode="absolute" /></dd>
        </div>
      ) : null}
    </dl>
  )
}
export function VPSOverviewFreshness({
  section,
  sourceLabel,
  onRetry,
  retrying,
  omitReadyTime = false,
  hideStateLabel = false,
  retained = false,
  timeLabel = '数据时间',
  scopeLabel = '概览',
}: Props) {
  const degraded = section.state === 'stale' || section.state === 'unavailable'
  const reason = overviewSourceReason(section, sourceLabel, { retained })
  const times = overviewSourceTimes(section)
  const stateLabel = overviewSourceStateLabel(section)
  const showState = Boolean(stateLabel) && !hideStateLabel
  const readyTime = !degraded && !omitReadyTime ? times.displayAt : null
  const retryMode = section.state === 'stale' ? 'refresh' : 'retry'

  if (!degraded && !reason && !readyTime) return null
  if (hideStateLabel && !reason && !times.displayAt && !onRetry) return null

  return (
    <div
      className={`vps-overview-freshness vps-overview-freshness--${section.state}`}
      aria-label={`${sourceLabel}新鲜度`}
    >
      {showState ? (
        <div className="vps-overview-freshness__headline">
          <Badge variant="state" tone={section.state === 'stale' ? 'notice' : 'alert'}>{stateLabel}</Badge>
          {onRetry ? (
            <VPSOverviewFreshnessRetry
              sourceLabel={sourceLabel}
              onRetry={onRetry}
              retrying={retrying}
              mode={retryMode}
              scopeLabel={scopeLabel}
            />
          ) : null}
        </div>
      ) : null}
      {hideStateLabel && onRetry ? (
        <div className="vps-overview-freshness__headline">
          <VPSOverviewFreshnessRetry
            sourceLabel={sourceLabel}
            onRetry={onRetry}
            retrying={retrying}
            mode={retryMode}
            scopeLabel={scopeLabel}
          />
        </div>
      ) : null}
      {reason ? <p className="vps-overview-freshness__reason">{reason}</p> : null}
      {readyTime ? (
        <p className="vps-overview-freshness__quiet">
          <span>{sourceLabel}更新</span>
          {' '}
          <Timestamp value={readyTime} mode="absolute" />
        </p>
      ) : null}

      {times.displayAt && degraded ? (
        <VPSOverviewFreshnessTime section={section} timeLabel={timeLabel} />
      ) : null}
    </div>
  )
}
