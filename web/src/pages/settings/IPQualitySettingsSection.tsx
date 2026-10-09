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
            <DurationHint seconds={value.frequencySeconds} />
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
            <DurationHint seconds={value.staleAfterSeconds} />
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
      <div className="ss-desc">IP 质量报告及脱敏后的原始结果长期保留。</div>
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

// 采集周期、过期窗口按秒保存（与 API 一致），旁边给出换算后的天/小时/分钟，避免手算 86400、604800。
function DurationHint({ seconds }: { seconds: string }) {
  const value = Number(seconds.trim())
  if (!Number.isInteger(value) || value < 60) return null
  const units: Array<[number, string]> = [[86400, '天'], [3600, '小时'], [60, '分钟']]
  const [size, unit] = units.find(([unitSeconds]) => value >= unitSeconds) ?? units[2]!
  const amount = value / size
  const text = Number.isInteger(amount) ? `${amount} ${unit}` : `约 ${amount.toFixed(1)} ${unit}`
  return <span className="settings-duration-hint">（{text}）</span>
}
