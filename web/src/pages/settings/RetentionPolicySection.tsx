import type { SettingsRetentionPolicyForm } from './types'

type RetentionPolicySectionProps = {
  value: SettingsRetentionPolicyForm
  onChange: (patch: Partial<SettingsRetentionPolicyForm>) => void
}

export function RetentionPolicySection({ value, onChange }: RetentionPolicySectionProps) {
  return (
    <>
      <div className="ss-title">数据保留策略</div>
      <div className="ss-desc">历史数据自动清理周期；原始层至少保留 30 天以支撑 30d 监控趋势</div>
      <div className="settings-row">
        <span className="sr-label">原始层保留</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="原始层保留天数"
            inputMode="numeric"
            min={30}
            value={value.rawLayerDays}
            onChange={(e) => onChange({ rawLayerDays: e.target.value })}
          /> 天
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">聚合层保留</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="聚合层保留天数"
            inputMode="numeric"
            value={value.aggregateLayerDays}
            onChange={(e) => onChange({ aggregateLayerDays: e.target.value })}
          /> 天
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">事件保留</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="事件层保留天数"
            inputMode="numeric"
            value={value.eventLayerDays}
            onChange={(e) => onChange({ eventLayerDays: e.target.value })}
          /> 天
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">通知保留</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="通知层保留天数"
            inputMode="numeric"
            value={value.notificationLayerDays}
            onChange={(e) => onChange({ notificationLayerDays: e.target.value })}
          /> 天
        </span>
      </div>
    </>
  )
}
