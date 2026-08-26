import { Button, Timestamp } from '../../components/atoms'
import type { VPSOverviewSectionState } from '../../lib/types'

type Props = {
  section: VPSOverviewSectionState
  sourceLabel: string
  onRetry?: (() => void) | undefined
  retrying: boolean
}

const STATE_LABELS = {
  ready: '数据正常',
  stale: '数据陈旧',
  unavailable: '暂不可用',
} satisfies Record<VPSOverviewSectionState['state'], string>

function reasonDescription(section: VPSOverviewSectionState, sourceLabel: string): string | null {
  switch (section.reason_code) {
    case 'ip_quality_stale':
      return `${sourceLabel}数据超过新鲜度阈值。`
    case 'ip_quality_disabled_has_history':
      return '存在历史报告（当前未启用）。'
    case 'source_timestamp_invalid':
      return `${sourceLabel}来源时间异常，已标记为陈旧。`
    case 'monitoring_timeout':
    case 'ip_quality_timeout':
    case 'subscription_timeout':
    case 'relation_timeout':
    case 'activity_timeout':
      return `${sourceLabel}读取超时，请重试。`
    case 'monitoring_unavailable':
    case 'ip_quality_unavailable':
    case 'subscription_unavailable':
    case 'relation_unavailable':
    case 'activity_unavailable':
      return `${sourceLabel}数据暂不可用，请稍后重试。`
    case 'activity_projection_unavailable':
      return '活动投影暂不可用，请稍后重试。'
    case '':
      return section.state === 'ready' ? null : `${sourceLabel}数据暂不可用，请稍后重试。`
    default:
      // Ready sections may carry a non-judging note code. Never treat that as
      // source failure, and never echo an unknown reason_code into the DOM.
      return section.state === 'ready' ? null : `${sourceLabel}数据暂不可用，请稍后重试。`
  }
}

export function VPSOverviewFreshness({
  section,
  sourceLabel,
  onRetry,
  retrying,
}: Props) {
  const degraded = section.state === 'stale' || section.state === 'unavailable'
  const reason = reasonDescription(section, sourceLabel)

  return (
    <div
      className={`vps-overview-freshness vps-overview-freshness--${section.state}`}
      aria-label={`${sourceLabel}新鲜度`}
    >
      <div className="vps-overview-freshness__headline">
        <span className="vps-overview-freshness__state">{STATE_LABELS[section.state]}</span>
        {degraded && onRetry ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="vps-overview-freshness__retry"
            aria-label={`重试 ${sourceLabel}`}
            disabled={retrying}
            onClick={onRetry}
          >
            {retrying ? '重试中…' : '重试'}
          </Button>
        ) : null}
      </div>
      {reason ? <p className="vps-overview-freshness__reason">{reason}</p> : null}
      {section.observed_at || section.last_success_at ? (
        <dl className="vps-overview-freshness__times">
          {section.observed_at ? (
            <div>
              <dt>观测</dt>
              <dd><Timestamp value={section.observed_at} mode="absolute" /></dd>
            </div>
          ) : null}
          {section.last_success_at ? (
            <div>
              <dt>最近成功</dt>
              <dd><Timestamp value={section.last_success_at} mode="absolute" /></dd>
            </div>
          ) : null}
        </dl>
      ) : null}
    </div>
  )
}
