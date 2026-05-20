import { DetailSection } from '../../components/DetailSection'
import { Input, Toggle } from '../../components/atoms'
import type { SettingsIncidentDefaultsForm } from './types'
import { SectionIntro } from './SectionIntro'

type IncidentDefaultsSectionProps = {
  value: SettingsIncidentDefaultsForm
  onChange: (next: SettingsIncidentDefaultsForm) => void
}

type ThresholdField = 'warning' | 'alert' | 'critical'

type ThresholdInputProps = {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}

function ThresholdInput({ ariaLabel, value, onChange }: ThresholdInputProps) {
  return (
    <Input
      label={ariaLabel}
      inputMode="numeric"
      value={value}
      trailingIcon="%"
      onChange={(event) => onChange(event.target.value)}
    />
  )
}

type MetricThresholdGroupProps = {
  label: string
  warning: string
  alert: string
  critical: string
  hasAlert: boolean
  onUpdate: (field: ThresholdField, value: string) => void
}

function MetricThresholdGroup({
  label,
  warning,
  alert,
  critical,
  hasAlert,
  onUpdate,
}: MetricThresholdGroupProps) {
  return (
    <div className="settings-fieldset">
      <span className="section-heading__eyebrow">{label}</span>
      <div className="settings-form-grid settings-form-grid--tight">
        <ThresholdInput
          ariaLabel={`${label} 关注阈值`}
          value={warning}
          onChange={(value) => onUpdate('warning', value)}
        />
        {hasAlert ? (
          <ThresholdInput
            ariaLabel={`${label} 告警阈值`}
            value={alert}
            onChange={(value) => onUpdate('alert', value)}
          />
        ) : null}
        <ThresholdInput
          ariaLabel={`${label} 严重阈值`}
          value={critical}
          onChange={(value) => onUpdate('critical', value)}
        />
      </div>
    </div>
  )
}

function IncidentDefaultsEditor({ value, onChange }: IncidentDefaultsSectionProps) {
  function update<K extends keyof SettingsIncidentDefaultsForm>(
    field: K,
    nextValue: SettingsIncidentDefaultsForm[K],
  ) {
    onChange({ ...value, [field]: nextValue })
  }

  return (
    <div className="settings-cluster">
      <div className="settings-form-grid">
        <Input
          label="心跳间隔秒数"
          inputMode="numeric"
          value={value.heartbeatIntervalSeconds}
          onChange={(event) => update('heartbeatIntervalSeconds', event.target.value)}
        />

        <Input
          label="失联判定阈值"
          inputMode="numeric"
          value={value.staleThresholdIntervals}
          onChange={(event) => update('staleThresholdIntervals', event.target.value)}
        />

        <Input
          label="扫描间隔秒数"
          inputMode="numeric"
          value={value.sweepIntervalSeconds}
          onChange={(event) => update('sweepIntervalSeconds', event.target.value)}
        />

        <div className="settings-fieldset settings-fieldset--wide">
          <Toggle
            label="异常开始通知"
            checked={value.notifyOnStarted}
            onChange={(checked) => update('notifyOnStarted', checked)}
          />
          <Toggle
            label="异常升级通知"
            checked={value.notifyOnEscalated}
            onChange={(checked) => update('notifyOnEscalated', checked)}
          />
          <Toggle
            label="异常恢复通知"
            checked={value.notifyOnRecovered}
            onChange={(checked) => update('notifyOnRecovered', checked)}
          />
        </div>
      </div>

      <MetricThresholdGroup
        label="CPU 阈值"
        warning={value.cpuWarningPct}
        alert={value.cpuAlertPct}
        critical={value.cpuCriticalPct}
        hasAlert
        onUpdate={(field, nextValue) => {
          const key = cpuThresholdKey(field)
          update(key, nextValue)
        }}
      />

      <MetricThresholdGroup
        label="内存阈值"
        warning={value.memWarningPct}
        alert={value.memAlertPct}
        critical={value.memCriticalPct}
        hasAlert
        onUpdate={(field, nextValue) => {
          const key = memThresholdKey(field)
          update(key, nextValue)
        }}
      />

      <MetricThresholdGroup
        label="磁盘阈值"
        warning={value.diskWarningPct}
        alert={value.diskAlertPct}
        critical={value.diskCriticalPct}
        hasAlert
        onUpdate={(field, nextValue) => {
          const key = diskThresholdKey(field)
          update(key, nextValue)
        }}
      />

      <MetricThresholdGroup
        label="Inode 阈值"
        warning={value.inodeWarningPct}
        alert={value.inodeAlertPct}
        critical={value.inodeCriticalPct}
        hasAlert
        onUpdate={(field, nextValue) => {
          const key = inodeThresholdKey(field)
          update(key, nextValue)
        }}
      />

      <MetricThresholdGroup
        label="IOWait 阈值"
        warning={value.iowaitWarningPct}
        alert=""
        critical={value.iowaitCriticalPct}
        hasAlert={false}
        onUpdate={(field, nextValue) => {
          const key = iowaitThresholdKey(field)
          if (key) update(key, nextValue)
        }}
      />

      <MetricThresholdGroup
        label="Load5 阈值"
        warning={value.load5Warning}
        alert=""
        critical={value.load5Critical}
        hasAlert={false}
        onUpdate={(field, nextValue) => {
          const key = load5ThresholdKey(field)
          if (key) update(key, nextValue)
        }}
      />
    </div>
  )
}

function thresholdKey(
  prefix: 'cpu' | 'mem' | 'disk' | 'inode',
  field: ThresholdField,
): keyof SettingsIncidentDefaultsForm {
  switch (field) {
    case 'warning': return `${prefix}WarningPct` as keyof SettingsIncidentDefaultsForm
    case 'alert': return `${prefix}AlertPct` as keyof SettingsIncidentDefaultsForm
    case 'critical': return `${prefix}CriticalPct` as keyof SettingsIncidentDefaultsForm
  }
}

const cpuThresholdKey = (field: ThresholdField) => thresholdKey('cpu', field)
const memThresholdKey = (field: ThresholdField) => thresholdKey('mem', field)
const diskThresholdKey = (field: ThresholdField) => thresholdKey('disk', field)
const inodeThresholdKey = (field: ThresholdField) => thresholdKey('inode', field)

function iowaitThresholdKey(field: ThresholdField): keyof SettingsIncidentDefaultsForm | null {
  switch (field) {
    case 'warning': return 'iowaitWarningPct'
    case 'critical': return 'iowaitCriticalPct'
    case 'alert': return null
  }
}

function load5ThresholdKey(field: ThresholdField): keyof SettingsIncidentDefaultsForm | null {
  switch (field) {
    case 'warning': return 'load5Warning'
    case 'critical': return 'load5Critical'
    case 'alert': return null
  }
}

export function IncidentDefaultsSection({ value, onChange }: IncidentDefaultsSectionProps) {
  return (
    <DetailSection eyebrow="全局默认" title="全局默认规则">
      <SectionIntro>heartbeat/sweep 时间参数与通知时机开关已接入实时异常与通知链路。</SectionIntro>
      <IncidentDefaultsEditor value={value} onChange={onChange} />
    </DetailSection>
  )
}
