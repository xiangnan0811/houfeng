import { Toggle } from '../../components/atoms'
import type { SettingsIPQualityForm } from './types'

type IPQualitySettingsSectionProps = {
  value: SettingsIPQualityForm
  onChange: (patch: Partial<SettingsIPQualityForm>) => void
}

export function IPQualitySettingsSection({ value, onChange }: IPQualitySettingsSectionProps) {
  return (
    <>
      <div className="ss-title">IP 质量采集</div>
      <div className="ss-desc">低频采集出口 IP 归属、风险信号与服务解锁结果；独立于主机实时采样</div>
      <div className="settings-row">
        <span className="sr-label">采集开关</span>
        <Toggle
          label="启用 IP 质量采集"
          checked={value.enabled}
          onChange={(enabled) => onChange({ enabled })}
        />
      </div>
      <div className="settings-row-group settings-row-group--3">
        <div className="settings-row">
          <span className="sr-label">采集周期</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量采集周期秒数"
              inputMode="numeric"
              value={value.frequencySeconds}
              onChange={(e) => onChange({ frequencySeconds: e.target.value })}
            /> 秒
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">过期窗口</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量过期窗口秒数"
              inputMode="numeric"
              value={value.staleAfterSeconds}
              onChange={(e) => onChange({ staleAfterSeconds: e.target.value })}
            /> 秒
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">请求超时</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量请求超时秒数"
              inputMode="numeric"
              value={value.timeoutSeconds}
              onChange={(e) => onChange({ timeoutSeconds: e.target.value })}
            /> 秒
          </span>
        </div>
      </div>
      <div className="settings-row-group settings-row-group--2">
        <div className="settings-row">
          <span className="sr-label">Raw JSON 保留</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量原始 JSON 保留天数"
              inputMode="numeric"
              value={value.rawRetentionDays}
              onChange={(e) => onChange({ rawRetentionDays: e.target.value })}
            /> 天
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">历史保留</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量历史保留天数"
              inputMode="numeric"
              value={value.historyRetentionDays}
              onChange={(e) => onChange({ historyRetentionDays: e.target.value })}
            /> 天
          </span>
        </div>
      </div>
      <div className="settings-row settings-row--block">
        <div className="sr-label-row">
          <span className="sr-label">服务集合</span>
          <span className="asset-table__muted">netflix, chatgpt, youtube-premium, amazon-prime-video, disney-plus, tiktok, reddit</span>
        </div>
        <textarea
          className="input settings-textarea"
          aria-label="IP 质量采集服务集合"
          value={value.servicesText}
          onChange={(e) => onChange({ servicesText: e.target.value })}
          rows={2}
        />
      </div>
    </>
  )
}
