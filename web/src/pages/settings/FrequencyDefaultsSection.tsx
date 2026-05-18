import { DetailSection } from '../../components/DetailSection'
import type { ProbeFrequencyDefaults } from '../../lib/types'
import { SectionIntro } from './SectionIntro'

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

type FrequencySelectProps = {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}

function FrequencySelect({ ariaLabel, value, onChange }: FrequencySelectProps) {
  return (
    <div className="input-field">
      <label className="input-field__label">{ariaLabel}</label>
      <div className="input-field__shell">
        <select className="input" aria-label={ariaLabel} value={value} onChange={(event) => onChange(event.target.value)}>
          {FREQUENCY_TIER_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}

export function FrequencyDefaultsSection({
  hostSampleFrequencyTier,
  probeFrequencyDefaults,
  onHostSampleFrequencyChange,
  onProbeFrequencyDefaultsChange,
}: FrequencyDefaultsSectionProps) {
  return (
    <DetailSection eyebrow="频率档位" title="默认频率档位">
      <SectionIntro>当前节点主机样本默认频率已接入实时规划链；Probe 默认频率仍仅作为持久化策略保存。</SectionIntro>
      <div className="settings-form-grid">
        <FrequencySelect
          ariaLabel="当前节点主机样本频率"
          value={hostSampleFrequencyTier}
          onChange={onHostSampleFrequencyChange}
        />
        <FrequencySelect
          ariaLabel="TCP 默认频率"
          value={probeFrequencyDefaults.tcp}
          onChange={(value) => onProbeFrequencyDefaultsChange({ tcp: value })}
        />
        <FrequencySelect
          ariaLabel="HTTP 默认频率"
          value={probeFrequencyDefaults.http}
          onChange={(value) => onProbeFrequencyDefaultsChange({ http: value })}
        />
        <FrequencySelect
          ariaLabel="TLS 默认频率"
          value={probeFrequencyDefaults.tls}
          onChange={(value) => onProbeFrequencyDefaultsChange({ tls: value })}
        />
      </div>
    </DetailSection>
  )
}
