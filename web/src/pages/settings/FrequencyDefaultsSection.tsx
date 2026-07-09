import type { ProbeFrequencyDefaults } from '../../lib/types'

const FREQUENCY_TIER_OPTIONS = [
  { value: '5s', label: '5 秒' },
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
] as const

type FrequencyDefaultsSectionProps = {
  hostSampleFrequencyTier: string
  probeFrequencyDefaults: ProbeFrequencyDefaults
  onHostSampleFrequencyChange: (value: string) => void
  onProbeFrequencyDefaultsChange: (patch: Partial<ProbeFrequencyDefaults>) => void
}

export function FrequencyDefaultsSection({
  hostSampleFrequencyTier,
  probeFrequencyDefaults,
  onHostSampleFrequencyChange,
  onProbeFrequencyDefaultsChange,
}: FrequencyDefaultsSectionProps) {
  return (
    <>
      <div className="ss-title">采样频率</div>
      <div className="ss-desc">Agent 主机采样与探测默认间隔</div>
      <div className="settings-row-group settings-row-group--4">
        <div className="settings-row">
          <span className="sr-label">主机采样间隔</span>
          <span className="sr-value">
            <select
              className="input input--compact"
              aria-label="当前监控实例主机样本频率"
              value={hostSampleFrequencyTier}
              onChange={(e) => onHostSampleFrequencyChange(e.target.value)}
            >
              {FREQUENCY_TIER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">TCP 默认频率</span>
          <span className="sr-value">
            <select
              className="input input--compact"
              aria-label="TCP 默认频率"
              value={probeFrequencyDefaults.tcp}
              onChange={(e) => onProbeFrequencyDefaultsChange({ tcp: e.target.value })}
            >
              {FREQUENCY_TIER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">HTTP 默认频率</span>
          <span className="sr-value">
            <select
              className="input input--compact"
              aria-label="HTTP 默认频率"
              value={probeFrequencyDefaults.http}
              onChange={(e) => onProbeFrequencyDefaultsChange({ http: e.target.value })}
            >
              {FREQUENCY_TIER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">TLS 默认频率</span>
          <span className="sr-value">
            <select
              className="input input--compact"
              aria-label="TLS 默认频率"
              value={probeFrequencyDefaults.tls}
              onChange={(e) => onProbeFrequencyDefaultsChange({ tls: e.target.value })}
            >
              {FREQUENCY_TIER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </span>
        </div>
      </div>
    </>
  )
}
