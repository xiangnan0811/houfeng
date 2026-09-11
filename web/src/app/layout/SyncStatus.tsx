import { Timestamp } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'

export type ShellSummaryStatus =
  | 'loading'
  | 'clear'
  | 'anomaly'
  | 'stale'
  | 'unavailable'

export interface SyncStatusProps {
  state: ShellSummaryStatus
  label: string
  generatedAt?: string
}

const GENERATED_AT_LABEL = '系统摘要生成于'

export function SyncStatus({ state, label, generatedAt }: SyncStatusProps) {
  const accessibleLabel = generatedAt
    ? `${label}，${GENERATED_AT_LABEL} ${formatDateTime(generatedAt)}`
    : label

  return (
    <span className="tp-sync-summary" role="status" aria-label={accessibleLabel}>
      <span className={`tp-sync tp-sync--${state}`} title={label} aria-hidden="true" />
      <span className="tp-sync-summary__copy">
        <span className="tp-sync-summary__label">{label}</span>
        {generatedAt ? (
          <span className="tp-sync-summary__meta">
            <span className="tp-sync-summary__meta-label">{GENERATED_AT_LABEL}</span>
            {' '}
            <Timestamp value={generatedAt} mode="absolute" />
          </span>
        ) : null}
      </span>
    </span>
  )
}
