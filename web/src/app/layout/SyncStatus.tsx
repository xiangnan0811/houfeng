import { Timestamp } from '../../components/atoms/Mono'
import { formatDateTime } from '../../lib/format'

export type ShellSummaryStatus =
  | 'loading'
  | 'clear'
  | 'anomaly'
  | 'unobserved'
  | 'notice'
  | 'stale'
  | 'unavailable'

export interface SyncStatusProps {
  state: ShellSummaryStatus
  label: string
  /** 可见文字被缩短时，读屏使用的完整说法；省略时与可见文字相同。 */
  spokenLabel?: string
  generatedAt?: string
}

const GENERATED_AT_LABEL = '系统摘要生成于'

export function SyncStatus({ state, label, spokenLabel, generatedAt }: SyncStatusProps) {
  const spoken = spokenLabel ?? label
  const accessibleLabel = generatedAt
    ? `${spoken}，${GENERATED_AT_LABEL} ${formatDateTime(generatedAt)}`
    : spoken

  return (
    <span className="tp-sync-summary" role="status" aria-label={accessibleLabel}>
      <span className={`tp-sync tp-sync--${state}`} title={spoken} aria-hidden="true" />
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
