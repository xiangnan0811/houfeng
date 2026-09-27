import type { SettingsRetentionPolicyForm } from './types'

type RetentionPolicySectionProps = {
  value: SettingsRetentionPolicyForm
  onChange: (patch: Partial<SettingsRetentionPolicyForm>) => void
}

export function RetentionPolicySection({ value, onChange }: RetentionPolicySectionProps) {
  return (
    <>
      <div className="ss-title">数据保留策略</div>
      <div className="ss-desc">心跳、性能与探测原始数据默认保留 30 天，最长 365 天；UTC 日聚合默认保留 365 天。事件、通知、生命周期审计、业务历史及 IP 质量报告长期保留，归档不改变保留起点。</div>
      <div className="settings-row-group settings-row-group--2">
        <div className="settings-row">
          <span className="sr-label">原始层保留</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="原始层保留天数"
              inputMode="numeric"
              min={30}
              max={365}
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
      </div>
    </>
  )
}
