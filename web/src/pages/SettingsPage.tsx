import { type FormEvent, useEffect, useState } from 'react'

import { DetailSection } from '../components/DetailSection'
import { MonoDigits, Tabs } from '../components/atoms'
import { useThemeOptional, type Preset, type Mode } from '../lib/theme-context'
import { ApiError, getSettings, updateSettings } from '../lib/api'
import type {
  FeishuSettingsInput,
  NodeLabelOverrideRule,
  ProbeFrequencyDefaults,
  SettingsRecord,
  SettingsUpdateInput,
  TargetLabelOverrideRule,
  TargetTypeOverrideRule,
} from '../lib/types'

const FREQUENCY_TIER_OPTIONS = [
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
] as const

const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

type FormState = {
  telegramBotToken: string
  telegramChatId: string
  telegramRuntimeManaged: boolean
  feishuEnabled: boolean
  feishuWebhookUrl: string
  hostSampleFrequencyTier: string
  probeFrequencyDefaults: ProbeFrequencyDefaults
  incidentDefaults: {
    heartbeatIntervalSeconds: string
    staleThresholdIntervals: string
    sweepIntervalSeconds: string
    notifyOnStarted: boolean
    notifyOnEscalated: boolean
    notifyOnRecovered: boolean
    cpuWarningPct: string
    cpuAlertPct: string
    cpuCriticalPct: string
    memWarningPct: string
    memAlertPct: string
    memCriticalPct: string
    diskWarningPct: string
    diskAlertPct: string
    diskCriticalPct: string
    inodeWarningPct: string
    inodeAlertPct: string
    inodeCriticalPct: string
    iowaitWarningPct: string
    iowaitCriticalPct: string
    load5Warning: string
    load5Critical: string
  }
  nodeLabelOverridesText: string
  targetTypeOverridesText: string
  targetLabelOverridesText: string
  retentionPolicy: {
    rawLayerDays: string
    aggregateLayerDays: string
    eventLayerDays: string
    notificationLayerDays: string
  }
}

type State = {
  loading: boolean
  saving: boolean
  error: string | null
  saveError: string | null
  saveSuccess: string | null
  settings: SettingsRecord | null
  form: FormState | null
}

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function formatJSON(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function buildFormState(settings: SettingsRecord): FormState {
  return {
    telegramBotToken: '',
    telegramChatId: settings.telegram.chat_id,
    telegramRuntimeManaged: settings.telegram.runtime_managed,
    feishuEnabled: settings.feishu.enabled,
    feishuWebhookUrl: settings.feishu.webhook_url,
    hostSampleFrequencyTier: settings.host_sample_frequency_tier,
    probeFrequencyDefaults: {
      tcp: settings.probe_frequency_defaults.tcp,
      http: settings.probe_frequency_defaults.http,
      tls: settings.probe_frequency_defaults.tls,
    },
    incidentDefaults: {
      heartbeatIntervalSeconds: String(settings.incident_defaults.heartbeat_interval_seconds),
      staleThresholdIntervals: String(settings.incident_defaults.stale_threshold_intervals),
      sweepIntervalSeconds: String(settings.incident_defaults.sweep_interval_seconds),
      notifyOnStarted: settings.incident_defaults.notify_on_started,
      notifyOnEscalated: settings.incident_defaults.notify_on_escalated,
      notifyOnRecovered: settings.incident_defaults.notify_on_recovered,
      cpuWarningPct: String(settings.incident_defaults.cpu_warning_pct),
      cpuAlertPct: String(settings.incident_defaults.cpu_alert_pct),
      cpuCriticalPct: String(settings.incident_defaults.cpu_critical_pct),
      memWarningPct: String(settings.incident_defaults.mem_warning_pct),
      memAlertPct: String(settings.incident_defaults.mem_alert_pct),
      memCriticalPct: String(settings.incident_defaults.mem_critical_pct),
      diskWarningPct: String(settings.incident_defaults.disk_warning_pct),
      diskAlertPct: String(settings.incident_defaults.disk_alert_pct),
      diskCriticalPct: String(settings.incident_defaults.disk_critical_pct),
      inodeWarningPct: String(settings.incident_defaults.inode_warning_pct),
      inodeAlertPct: String(settings.incident_defaults.inode_alert_pct),
      inodeCriticalPct: String(settings.incident_defaults.inode_critical_pct),
      iowaitWarningPct: String(settings.incident_defaults.iowait_warning_pct),
      iowaitCriticalPct: String(settings.incident_defaults.iowait_critical_pct),
      load5Warning: String(settings.incident_defaults.load5_warning),
      load5Critical: String(settings.incident_defaults.load5_critical),
    },
    nodeLabelOverridesText: formatJSON(settings.override_rules.node_labels),
    targetTypeOverridesText: formatJSON(settings.override_rules.target_types),
    targetLabelOverridesText: formatJSON(settings.override_rules.target_labels),
    retentionPolicy: {
      rawLayerDays: String(settings.retention_policy.raw_layer_days),
      aggregateLayerDays: String(settings.retention_policy.aggregate_layer_days),
      eventLayerDays: String(settings.retention_policy.event_layer_days),
      notificationLayerDays: String(settings.retention_policy.notification_layer_days),
    },
  }
}

function parsePositiveInteger(value: string, label: string) {
  const normalized = value.trim()
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

function parsePositiveNumber(value: string, label: string) {
  const normalized = value.trim()
  const num = Number(normalized)
  if (!Number.isFinite(num) || num <= 0) {
    throw new Error(`${label}必须为正数。`)
  }
  return num
}

function parseOverrideRuleArray<T>(value: string, label: string): T[] {
  try {
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) {
      throw new Error('not array')
    }
    return parsed as T[]
  } catch {
    throw new Error(`${label}必须是 JSON 数组。`)
  }
}

type SettingsUpdateDraft = Omit<SettingsUpdateInput, 'telegram' | 'feishu'> & {
  telegram: {
    chat_id: string
    runtime_managed: boolean
    bot_token?: string
  }
  feishu: FeishuSettingsInput
}

function buildUpdateInput(form: FormState, currentSettings: SettingsRecord): SettingsUpdateDraft {
  const botToken = form.telegramBotToken.trim()
  const chatId = form.telegramChatId.trim()
  const runtimeManaged = form.telegramRuntimeManaged
  const hasPersistedToken = currentSettings.telegram.token_present
  const replacementTokenProvided = botToken != ''

  if (runtimeManaged && !hasPersistedToken && !replacementTokenProvided) {
    throw new Error('启用运行时接管前，需要先提供 Telegram Bot Token 和 Chat ID。')
  }

  if (hasPersistedToken && !replacementTokenProvided) {
    if (runtimeManaged && chatId == '') {
      throw new Error('Telegram Bot Token 和 Chat ID 需要同时提供或同时清空。')
    }
    return {
      telegram: { chat_id: chatId, runtime_managed: runtimeManaged },
      feishu: {
        enabled: form.feishuEnabled,
        webhook_url: form.feishuWebhookUrl.trim(),
      },
      host_sample_frequency_tier: form.hostSampleFrequencyTier,
      probe_frequency_defaults: {
        tcp: form.probeFrequencyDefaults.tcp,
        http: form.probeFrequencyDefaults.http,
        tls: form.probeFrequencyDefaults.tls,
      },
      incident_defaults: {
        heartbeat_interval_seconds: parsePositiveInteger(
          form.incidentDefaults.heartbeatIntervalSeconds,
          '心跳间隔秒数',
        ),
        stale_threshold_intervals: parsePositiveInteger(
          form.incidentDefaults.staleThresholdIntervals,
          '失联判定阈值',
        ),
        sweep_interval_seconds: parsePositiveInteger(
          form.incidentDefaults.sweepIntervalSeconds,
          '扫描间隔秒数',
        ),
        notify_on_started: form.incidentDefaults.notifyOnStarted,
        notify_on_escalated: form.incidentDefaults.notifyOnEscalated,
        notify_on_recovered: form.incidentDefaults.notifyOnRecovered,
        cpu_warning_pct: parsePositiveInteger(form.incidentDefaults.cpuWarningPct, 'CPU 关注阈值'),
        cpu_alert_pct: parsePositiveInteger(form.incidentDefaults.cpuAlertPct, 'CPU 告警阈值'),
        cpu_critical_pct: parsePositiveInteger(form.incidentDefaults.cpuCriticalPct, 'CPU 严重阈值'),
        mem_warning_pct: parsePositiveInteger(form.incidentDefaults.memWarningPct, '内存关注阈值'),
        mem_alert_pct: parsePositiveInteger(form.incidentDefaults.memAlertPct, '内存告警阈值'),
        mem_critical_pct: parsePositiveInteger(form.incidentDefaults.memCriticalPct, '内存严重阈值'),
        disk_warning_pct: parsePositiveInteger(form.incidentDefaults.diskWarningPct, '磁盘关注阈值'),
        disk_alert_pct: parsePositiveInteger(form.incidentDefaults.diskAlertPct, '磁盘告警阈值'),
        disk_critical_pct: parsePositiveInteger(form.incidentDefaults.diskCriticalPct, '磁盘严重阈值'),
        inode_warning_pct: parsePositiveInteger(form.incidentDefaults.inodeWarningPct, 'Inode 关注阈值'),
        inode_alert_pct: parsePositiveInteger(form.incidentDefaults.inodeAlertPct, 'Inode 告警阈值'),
        inode_critical_pct: parsePositiveInteger(form.incidentDefaults.inodeCriticalPct, 'Inode 严重阈值'),
        iowait_warning_pct: parsePositiveInteger(form.incidentDefaults.iowaitWarningPct, 'IOWait 关注阈值'),
        iowait_critical_pct: parsePositiveInteger(form.incidentDefaults.iowaitCriticalPct, 'IOWait 严重阈值'),
        load5_warning: parsePositiveNumber(form.incidentDefaults.load5Warning, 'Load5 关注阈值'),
        load5_critical: parsePositiveNumber(form.incidentDefaults.load5Critical, 'Load5 严重阈值'),
      },
      override_rules: {
        node_labels: parseOverrideRuleArray<NodeLabelOverrideRule>(
          form.nodeLabelOverridesText,
          '节点标签覆盖规则',
        ),
        target_types: parseOverrideRuleArray<TargetTypeOverrideRule>(
          form.targetTypeOverridesText,
          '目标类型覆盖规则',
        ),
        target_labels: parseOverrideRuleArray<TargetLabelOverrideRule>(
          form.targetLabelOverridesText,
          '目标标签覆盖规则',
        ),
      },
      retention_policy: {
        raw_layer_days: parsePositiveInteger(form.retentionPolicy.rawLayerDays, '原始层保留天数'),
        aggregate_layer_days: parsePositiveInteger(
          form.retentionPolicy.aggregateLayerDays,
          '聚合层保留天数',
        ),
        event_layer_days: parsePositiveInteger(form.retentionPolicy.eventLayerDays, '事件层保留天数'),
        notification_layer_days: parsePositiveInteger(
          form.retentionPolicy.notificationLayerDays,
          '通知层保留天数',
        ),
      },
    }
  }

  if ((botToken == '') != (chatId == '')) {
    throw new Error('Telegram Bot Token 和 Chat ID 需要同时提供或同时清空。')
  }

  return {
    telegram: {
      ...(replacementTokenProvided ? { bot_token: botToken } : {}),
      chat_id: chatId,
      runtime_managed: runtimeManaged,
    },
    feishu: {
      enabled: form.feishuEnabled,
      webhook_url: form.feishuWebhookUrl.trim(),
    },
    host_sample_frequency_tier: form.hostSampleFrequencyTier,
    probe_frequency_defaults: {
      tcp: form.probeFrequencyDefaults.tcp,
      http: form.probeFrequencyDefaults.http,
      tls: form.probeFrequencyDefaults.tls,
    },
    incident_defaults: {
      heartbeat_interval_seconds: parsePositiveInteger(
        form.incidentDefaults.heartbeatIntervalSeconds,
        '心跳间隔秒数',
      ),
      stale_threshold_intervals: parsePositiveInteger(
        form.incidentDefaults.staleThresholdIntervals,
        '失联判定阈值',
      ),
      sweep_interval_seconds: parsePositiveInteger(
        form.incidentDefaults.sweepIntervalSeconds,
        '扫描间隔秒数',
      ),
      notify_on_started: form.incidentDefaults.notifyOnStarted,
      notify_on_escalated: form.incidentDefaults.notifyOnEscalated,
      notify_on_recovered: form.incidentDefaults.notifyOnRecovered,
      cpu_warning_pct: parsePositiveInteger(form.incidentDefaults.cpuWarningPct, 'CPU 关注阈值'),
      cpu_alert_pct: parsePositiveInteger(form.incidentDefaults.cpuAlertPct, 'CPU 告警阈值'),
      cpu_critical_pct: parsePositiveInteger(form.incidentDefaults.cpuCriticalPct, 'CPU 严重阈值'),
      mem_warning_pct: parsePositiveInteger(form.incidentDefaults.memWarningPct, '内存关注阈值'),
      mem_alert_pct: parsePositiveInteger(form.incidentDefaults.memAlertPct, '内存告警阈值'),
      mem_critical_pct: parsePositiveInteger(form.incidentDefaults.memCriticalPct, '内存严重阈值'),
      disk_warning_pct: parsePositiveInteger(form.incidentDefaults.diskWarningPct, '磁盘关注阈值'),
      disk_alert_pct: parsePositiveInteger(form.incidentDefaults.diskAlertPct, '磁盘告警阈值'),
      disk_critical_pct: parsePositiveInteger(form.incidentDefaults.diskCriticalPct, '磁盘严重阈值'),
      inode_warning_pct: parsePositiveInteger(form.incidentDefaults.inodeWarningPct, 'Inode 关注阈值'),
      inode_alert_pct: parsePositiveInteger(form.incidentDefaults.inodeAlertPct, 'Inode 告警阈值'),
      inode_critical_pct: parsePositiveInteger(form.incidentDefaults.inodeCriticalPct, 'Inode 严重阈值'),
      iowait_warning_pct: parsePositiveInteger(form.incidentDefaults.iowaitWarningPct, 'IOWait 关注阈值'),
      iowait_critical_pct: parsePositiveInteger(form.incidentDefaults.iowaitCriticalPct, 'IOWait 严重阈值'),
      load5_warning: parsePositiveNumber(form.incidentDefaults.load5Warning, 'Load5 关注阈值'),
      load5_critical: parsePositiveNumber(form.incidentDefaults.load5Critical, 'Load5 严重阈值'),
    },
    override_rules: {
      node_labels: parseOverrideRuleArray<NodeLabelOverrideRule>(
        form.nodeLabelOverridesText,
        '节点标签覆盖规则',
      ),
      target_types: parseOverrideRuleArray<TargetTypeOverrideRule>(
        form.targetTypeOverridesText,
        '目标类型覆盖规则',
      ),
      target_labels: parseOverrideRuleArray<TargetLabelOverrideRule>(
        form.targetLabelOverridesText,
        '目标标签覆盖规则',
      ),
    },
    retention_policy: {
      raw_layer_days: parsePositiveInteger(form.retentionPolicy.rawLayerDays, '原始层保留天数'),
      aggregate_layer_days: parsePositiveInteger(
        form.retentionPolicy.aggregateLayerDays,
        '聚合层保留天数',
      ),
      event_layer_days: parsePositiveInteger(form.retentionPolicy.eventLayerDays, '事件层保留天数'),
      notification_layer_days: parsePositiveInteger(
        form.retentionPolicy.notificationLayerDays,
        '通知层保留天数',
      ),
    },
  }
}

function SectionIntro({ children }: { children: string }) {
  return <p className="empty-inline">{children}</p>
}

function FrequencySelect({
  ariaLabel,
  value,
  onChange,
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <label className="summary-card">
      <span className="summary-card__label">{ariaLabel}</span>
      <select aria-label={ariaLabel} value={value} onChange={(event) => onChange(event.target.value)}>
        {FREQUENCY_TIER_OPTIONS.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}

function RetentionInput({
  ariaLabel,
  value,
  onChange,
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <label className="summary-card">
      <span className="summary-card__label">{ariaLabel}</span>
      <span className="input-with-suffix">
        <input
          aria-label={ariaLabel}
          inputMode="numeric"
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <span className="input-with-suffix__unit">天</span>
      </span>
    </label>
  )
}

function ThresholdInput({
  ariaLabel,
  value,
  onChange,
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <label className="summary-card">
      <span className="summary-card__label">{ariaLabel}</span>
      <span className="input-with-suffix">
        <input
          aria-label={ariaLabel}
          inputMode="numeric"
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <span className="input-with-suffix__unit">%</span>
      </span>
    </label>
  )
}

function MetricThresholdGroup({
  label,
  warning,
  alert,
  critical,
  hasAlert,
  onUpdate,
}: {
  label: string
  warning: string
  alert: string
  critical: string
  hasAlert: boolean
  onUpdate: (field: string, value: string) => void
}) {
  return (
    <div className="page-stack" style={{ gap: '8px' }}>
      <span className="section-heading__eyebrow">{label}</span>
      <div className="summary-grid summary-grid--numeric">
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

function IncidentDefaultsEditor({
  value,
  onChange,
}: {
  value: FormState['incidentDefaults']
  onChange: (next: FormState['incidentDefaults']) => void
}) {
  function update<K extends keyof FormState['incidentDefaults']>(
    field: K,
    nextValue: FormState['incidentDefaults'][K],
  ) {
    onChange({ ...value, [field]: nextValue })
  }

  return (
    <div className="page-stack" style={{ gap: '16px' }}>
      <div className="summary-grid">
        <label className="summary-card">
          <span className="summary-card__label">心跳间隔秒数</span>
          <input
            aria-label="心跳间隔秒数"
            inputMode="numeric"
            value={value.heartbeatIntervalSeconds}
            onChange={(event) => update('heartbeatIntervalSeconds', event.target.value)}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">失联判定阈值</span>
          <input
            aria-label="失联判定阈值"
            inputMode="numeric"
            value={value.staleThresholdIntervals}
            onChange={(event) => update('staleThresholdIntervals', event.target.value)}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">扫描间隔秒数</span>
          <input
            aria-label="扫描间隔秒数"
            inputMode="numeric"
            value={value.sweepIntervalSeconds}
            onChange={(event) => update('sweepIntervalSeconds', event.target.value)}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">异常开始通知</span>
          <input
            aria-label="异常开始通知"
            type="checkbox"
            checked={value.notifyOnStarted}
            onChange={(event) => update('notifyOnStarted', event.target.checked)}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">异常升级通知</span>
          <input
            aria-label="异常升级通知"
            type="checkbox"
            checked={value.notifyOnEscalated}
            onChange={(event) => update('notifyOnEscalated', event.target.checked)}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">异常恢复通知</span>
          <input
            aria-label="异常恢复通知"
            type="checkbox"
            checked={value.notifyOnRecovered}
            onChange={(event) => update('notifyOnRecovered', event.target.checked)}
          />
        </label>
      </div>

      <MetricThresholdGroup
        label="CPU 阈值"
        warning={value.cpuWarningPct}
        alert={value.cpuAlertPct}
        critical={value.cpuCriticalPct}
        hasAlert
        onUpdate={(field, nextValue) => {
          const key = cpuThresholdKey(field)
          if (key) update(key, nextValue)
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
          if (key) update(key, nextValue)
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
          if (key) update(key, nextValue)
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
          if (key) update(key, nextValue)
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

function thresholdKey(prefix: string, field: string): keyof FormState['incidentDefaults'] | null {
  switch (field) {
    case 'warning': return `${prefix}WarningPct` as keyof FormState['incidentDefaults']
    case 'alert': return `${prefix}AlertPct` as keyof FormState['incidentDefaults']
    case 'critical': return `${prefix}CriticalPct` as keyof FormState['incidentDefaults']
    default: return null
  }
}

const cpuThresholdKey = (field: string) => thresholdKey('cpu', field)
const memThresholdKey = (field: string) => thresholdKey('mem', field)
const diskThresholdKey = (field: string) => thresholdKey('disk', field)
const inodeThresholdKey = (field: string) => thresholdKey('inode', field)
const iowaitThresholdKey = (field: string) => thresholdKey('iowait', field)

function load5ThresholdKey(field: string): keyof FormState['incidentDefaults'] | null {
  switch (field) {
    case 'warning': return 'load5Warning'
    case 'critical': return 'load5Critical'
    default: return null
  }
}

function OverrideTextarea({
  ariaLabel,
  value,
  onChange,
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}) {
  let previewContent: string | null = null
  if (value.trim()) {
    try {
      previewContent = JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      previewContent = null
    }
  }

  return (
    <div className="override-rule-field">
      <label>
        <span>{ariaLabel}</span>
        <textarea
          aria-label={ariaLabel}
          className="mono"
          rows={10}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      </label>
      {previewContent ? (
        <details className="override-rule-preview">
          <summary>预览</summary>
          <pre><code>{previewContent}</code></pre>
        </details>
      ) : null}
    </div>
  )
}

function TargetTypeSummary() {
  return (
    <p className="empty-inline">
      允许的目标类型选择器：{TARGET_TYPE_OPTIONS.map((option) => option.value).join('、')}。
    </p>
  )
}

export function SettingsPage() {
  const [state, setState] = useState<State>({
    loading: true,
    saving: false,
    error: null,
    saveError: null,
    saveSuccess: null,
    settings: null,
    form: null,
  })

  useEffect(() => {
    let cancelled = false

    getSettings()
      .then((settings) => {
        if (cancelled) return
        setState({
          loading: false,
          saving: false,
          error: null,
          saveError: null,
          saveSuccess: null,
          settings,
          form: buildFormState(settings),
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          saving: false,
          error: describeError(error, '加载设置失败'),
          saveError: null,
          saveSuccess: null,
          settings: null,
          form: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (state.loading) {
    return <section className="page-panel">正在加载设置…</section>
  }

  if (state.error || !state.settings || !state.form) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">设置</p>
        <h2 className="page-panel__title">设置不可用</h2>
        <p className="page-panel__description">{state.error ?? '未获取到设置数据'}</p>
      </section>
    )
  }

  const { settings, form } = state

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()

    let payload: SettingsUpdateDraft
    try {
      payload = buildUpdateInput(form, settings)
    } catch (error) {
      setState((current) => ({
        ...current,
        saveError: describeError(error, '保存设置失败'),
        saveSuccess: null,
      }))
      return
    }

    setState((current) => ({ ...current, saving: true, saveError: null, saveSuccess: null }))

    try {
      const updated = await updateSettings(payload)
      setState((current) => ({
        ...current,
        saving: false,
        settings: updated,
        form: buildFormState(updated),
        saveError: null,
        saveSuccess: '设置已保存。保留策略会由中心后台执行；仍仅持久化保存的策略不会立即影响运行时。',
      }))
    } catch (error) {
      setState((current) => ({
        ...current,
        saving: false,
        saveError: describeError(error, '保存设置失败'),
        saveSuccess: null,
      }))
    }
  }

  return (
    <form className="page-stack" onSubmit={handleSubmit}>
      <section className="page-panel">
        <p className="page-panel__eyebrow">设置</p>
        <h2 className="page-panel__title">设置 / Settings</h2>
        <p className="page-panel__description">
          集中维护 Telegram 通知、默认频率、全局规则、少量覆盖与保留策略，保持页面信息密度低于观测页。
        </p>
      </section>

      <ThemeSettingsSection />

      <DetailSection
        eyebrow="Telegram"
        title="Telegram 通知设置"
        aside={settings.telegram.token_present ? '已存在持久化 Token' : '当前未配置'}
      >
        <div className="summary-grid">
          <label className="summary-card">
            <span className="summary-card__label">新的 Telegram Bot Token</span>
            <input
              aria-label="新的 Telegram Bot Token"
              type="password"
              autoComplete="off"
              value={form.telegramBotToken}
              onChange={(event) =>
                setState((current) => ({
                  ...current,
                  form: current.form
                    ? { ...current.form, telegramBotToken: event.target.value }
                    : current.form,
                  saveError: null,
                  saveSuccess: null,
                }))
              }
            />
          </label>

          <label className="summary-card">
            <span className="summary-card__label">Telegram Chat ID</span>
            <input
              aria-label="Telegram Chat ID"
              value={form.telegramChatId}
              onChange={(event) =>
                setState((current) => ({
                  ...current,
                  form: current.form
                    ? { ...current.form, telegramChatId: event.target.value }
                    : current.form,
                  saveError: null,
                  saveSuccess: null,
                }))
              }
            />
          </label>

          <article className="summary-card">
            <span className="summary-card__label">当前持久化状态</span>
            <strong className="summary-card__value summary-card__value--text">
              {settings.telegram.token_present ? (
                <>
                  已配置 Telegram Bot Token：
                  <MonoDigits>{settings.telegram.token_masked_summary}</MonoDigits>
                </>
              ) : (
                '当前未保存 Telegram Bot Token'
              )}
            </strong>
          </article>

          <label className="summary-card">
            <span className="summary-card__label">运行时接管</span>
            <input
              aria-label="使用持久化 Telegram 配置接管运行中的通知器"
              type="checkbox"
              checked={form.telegramRuntimeManaged}
              onChange={(event) =>
                setState((current) => ({
                  ...current,
                  form: current.form
                    ? { ...current.form, telegramRuntimeManaged: event.target.checked }
                    : current.form,
                  saveError: null,
                  saveSuccess: null,
                }))
              }
            />
          </label>
        </div>

        <SectionIntro>
          {!settings.telegram.runtime_managed
            ? '当前仅保存 Telegram 持久化配置，尚未驱动正在运行的通知器。'
            : settings.telegram.runtime_apply_active
              ? '当前持久化配置已接入正在运行的通知路径。'
              : '当前持久化配置正在接管通知路径，并已显式停用 Telegram 投递。'}
        </SectionIntro>
        <SectionIntro>
          接口不会回显明文 Token。留空会继续保留当前已保存的 Token；只有在需要替换时才输入新的 Token。
        </SectionIntro>
      </DetailSection>

      <DetailSection
        eyebrow="飞书"
        title="飞书通知设置"
        ribbon="accent-2"
        aside={form.feishuEnabled && form.feishuWebhookUrl.trim() ? '已配置' : '未配置'}
      >
        <div className="summary-grid">
          <label className="summary-card">
            <span className="summary-card__label">启用飞书通知</span>
            <input
              aria-label="启用飞书通知"
              type="checkbox"
              checked={form.feishuEnabled}
              onChange={(event) =>
                setState((current) => ({
                  ...current,
                  form: current.form
                    ? { ...current.form, feishuEnabled: event.target.checked }
                    : current.form,
                  saveError: null,
                  saveSuccess: null,
                }))
              }
            />
          </label>

          <label className="summary-card">
            <span className="summary-card__label">Webhook URL</span>
            <input
              aria-label="飞书 Webhook URL"
              type="text"
              placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
              value={form.feishuWebhookUrl}
              onChange={(event) =>
                setState((current) => ({
                  ...current,
                  form: current.form
                    ? { ...current.form, feishuWebhookUrl: event.target.value }
                    : current.form,
                  saveError: null,
                  saveSuccess: null,
                }))
              }
            />
          </label>
        </div>

        <SectionIntro>
          {form.feishuEnabled && form.feishuWebhookUrl.trim()
            ? '飞书通知已启用，incident 发生时将同时通过飞书群机器人推送消息。'
            : '当前未配置飞书通知。填写 Webhook URL 并启用后，incident 推送将同时投递到飞书群。'}
        </SectionIntro>
      </DetailSection>

      <DetailSection eyebrow="频率档位" title="默认频率档位">
        <SectionIntro>当前节点主机样本默认频率已接入实时规划链；Probe 默认频率仍仅作为持久化策略保存。</SectionIntro>
        <div className="summary-grid summary-grid--numeric">
          <FrequencySelect
            ariaLabel="当前节点主机样本频率"
            value={form.hostSampleFrequencyTier}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form ? { ...current.form, hostSampleFrequencyTier: value } : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <FrequencySelect
            ariaLabel="TCP 默认频率"
            value={form.probeFrequencyDefaults.tcp}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      probeFrequencyDefaults: { ...current.form.probeFrequencyDefaults, tcp: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <FrequencySelect
            ariaLabel="HTTP 默认频率"
            value={form.probeFrequencyDefaults.http}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      probeFrequencyDefaults: { ...current.form.probeFrequencyDefaults, http: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <FrequencySelect
            ariaLabel="TLS 默认频率"
            value={form.probeFrequencyDefaults.tls}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      probeFrequencyDefaults: { ...current.form.probeFrequencyDefaults, tls: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
        </div>
      </DetailSection>

      <DetailSection eyebrow="全局默认" title="全局默认规则">
        <SectionIntro>heartbeat/sweep 时间参数与通知时机开关已接入实时异常与通知链路。</SectionIntro>
        <IncidentDefaultsEditor
          value={form.incidentDefaults}
          onChange={(next) =>
            setState((current) => ({
              ...current,
              form: current.form ? { ...current.form, incidentDefaults: next } : current.form,
              saveError: null,
              saveSuccess: null,
            }))
          }
        />
      </DetailSection>

      <DetailSection eyebrow="覆盖规则" title="少量覆盖规则" aside={<TargetTypeSummary />}>
        <SectionIntro>
          仅保留节点标签、目标类型、目标标签三类结构化覆盖，不扩展为通用规则引擎。当前频率相关覆盖已接入实时规划链；异常默认覆盖仍仅作为持久化策略保存。
        </SectionIntro>
        <div className="page-stack">
          <OverrideTextarea
            ariaLabel="节点标签覆盖规则 JSON"
            value={form.nodeLabelOverridesText}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form ? { ...current.form, nodeLabelOverridesText: value } : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <OverrideTextarea
            ariaLabel="目标类型覆盖规则 JSON"
            value={form.targetTypeOverridesText}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form ? { ...current.form, targetTypeOverridesText: value } : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <OverrideTextarea
            ariaLabel="目标标签覆盖规则 JSON"
            value={form.targetLabelOverridesText}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form ? { ...current.form, targetLabelOverridesText: value } : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
        </div>
      </DetailSection>

      <DetailSection eyebrow="保留策略" title="数据保留策略">
        <div className="summary-grid summary-grid--numeric">
          <RetentionInput
            ariaLabel="原始层保留天数"
            value={form.retentionPolicy.rawLayerDays}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      retentionPolicy: { ...current.form.retentionPolicy, rawLayerDays: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <RetentionInput
            ariaLabel="聚合层保留天数"
            value={form.retentionPolicy.aggregateLayerDays}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      retentionPolicy: { ...current.form.retentionPolicy, aggregateLayerDays: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <RetentionInput
            ariaLabel="事件层保留天数"
            value={form.retentionPolicy.eventLayerDays}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      retentionPolicy: { ...current.form.retentionPolicy, eventLayerDays: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
          <RetentionInput
            ariaLabel="通知层保留天数"
            value={form.retentionPolicy.notificationLayerDays}
            onChange={(value) =>
              setState((current) => ({
                ...current,
                form: current.form
                  ? {
                      ...current.form,
                      retentionPolicy: { ...current.form.retentionPolicy, notificationLayerDays: value },
                    }
                  : current.form,
                saveError: null,
                saveSuccess: null,
              }))
            }
          />
        </div>
        <SectionIntro>中心后台会按这些窗口自动清理原始观测、事件和通知记录，并维护日级聚合数据作为后续趋势与摘要基础。</SectionIntro>
      </DetailSection>

      {state.saveError ? <section className="page-panel">{state.saveError}</section> : null}
      {state.saveSuccess ? <section className="page-panel">{state.saveSuccess}</section> : null}

      <div>
        <button type="submit" disabled={state.saving}>
          {state.saving ? '正在保存…' : '保存设置'}
        </button>
      </div>
    </form>
  )
}

const PRESET_TABS = [
  { value: 'houfeng' as const, label: '候风原色' },
  { value: 'classic' as const, label: '经典' },
]

const MODE_TABS = [
  { value: 'dark' as const, label: '深色' },
  { value: 'light' as const, label: '浅色' },
  { value: 'system' as const, label: '跟随系统' },
]

function ThemeSettingsSection() {
  const theme = useThemeOptional()
  if (!theme) return null
  const { preset, mode, setPreset, setMode } = theme
  return (
    <DetailSection
      eyebrow="主题"
      title="主题"
      aside="本地浏览器偏好"
    >
      <div className="page-stack">
        <div>
          <p className="section-heading__eyebrow" style={{ marginBottom: 8 }}>风格</p>
          <Tabs<Preset>
            variant="pill"
            value={preset}
            onChange={setPreset}
            items={PRESET_TABS}
          />
        </div>
        <div>
          <p className="section-heading__eyebrow" style={{ marginBottom: 8 }}>明暗</p>
          <Tabs<Mode>
            variant="pill"
            value={mode}
            onChange={setMode}
            items={MODE_TABS}
          />
        </div>
      </div>
    </DetailSection>
  )
}
