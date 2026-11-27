import type { SettingsRetentionPolicyForm } from './types'

type RetentionPolicySectionProps = {
  value: SettingsRetentionPolicyForm
  onChange: (patch: Partial<SettingsRetentionPolicyForm>) => void
}

export function RetentionPolicySection({ value, onChange }: RetentionPolicySectionProps) {
  return (
    <>
      <div className="ss-title">数据保留策略</div>
      <div className="ss-desc">原始层默认保留 30 天，UTC 日聚合默认保留 365 天。实时心跳留空时跟随原始层，填写较短天数后分批清理，恢复和证据所需的心跳仍按原始层保留。事件、通知、生命周期审计、业务历史及 IP 质量报告长期保留。</div>
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
          <span className="sr-label">实时心跳保留</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="实时心跳保留天数"
              inputMode="numeric"
              placeholder="跟随原始层"
              value={value.liveHeartbeatDays}
              onChange={(e) => onChange({ liveHeartbeatDays: e.target.value })}
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
