import { type FormEvent, useEffect, useState } from 'react'

import { ApiError, getSettings, updateSettings } from '../lib/api'
import type {
  FeishuSettingsInput,
  NodeLabelOverrideRule,
  SettingsRecord,
  SettingsUpdateInput,
  TargetLabelOverrideRule,
  TargetTypeOverrideRule,
} from '../lib/types'
import { FeishuSettingsSection } from './settings/FeishuSettingsSection'
import { FrequencyDefaultsSection } from './settings/FrequencyDefaultsSection'
import { IncidentDefaultsSection } from './settings/IncidentDefaultsSection'
import { OverrideRulesSection } from './settings/OverrideRulesSection'
import { RetentionPolicySection } from './settings/RetentionPolicySection'
import { TelegramSettingsSection } from './settings/TelegramSettingsSection'
import { ThemeSettingsSection } from './settings/ThemeSettingsSection'
import type { SettingsFormState } from './settings/types'

type State = {
  loading: boolean
  saving: boolean
  error: string | null
  saveError: string | null
  saveSuccess: string | null
  settings: SettingsRecord | null
  form: SettingsFormState | null
}

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function formatJSON(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function buildFormState(settings: SettingsRecord): SettingsFormState {
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

function buildUpdateInput(form: SettingsFormState, currentSettings: SettingsRecord): SettingsUpdateDraft {
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

  function patchForm(updater: (form: SettingsFormState) => SettingsFormState) {
    setState((current) => ({
      ...current,
      form: current.form ? updater(current.form) : current.form,
      saveError: null,
      saveSuccess: null,
    }))
  }

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
    <form className="page-stack settings-page" onSubmit={handleSubmit}>
      <section className="page-panel">
        <p className="page-panel__eyebrow">设置</p>
        <h2 className="page-panel__title">设置 / Settings</h2>
        <p className="page-panel__description">
          集中维护 Telegram 通知、默认频率、全局规则、少量覆盖与保留策略，保持页面信息密度低于观测页。
        </p>
      </section>

      <ThemeSettingsSection />

      <TelegramSettingsSection
        settings={settings.telegram}
        form={form}
        onChange={(patch) => patchForm((currentForm) => ({ ...currentForm, ...patch }))}
      />

      <FeishuSettingsSection
        form={form}
        onChange={(patch) => patchForm((currentForm) => ({ ...currentForm, ...patch }))}
      />

      <FrequencyDefaultsSection
        hostSampleFrequencyTier={form.hostSampleFrequencyTier}
        probeFrequencyDefaults={form.probeFrequencyDefaults}
        onHostSampleFrequencyChange={(value) =>
          patchForm((currentForm) => ({ ...currentForm, hostSampleFrequencyTier: value }))
        }
        onProbeFrequencyDefaultsChange={(patch) =>
          patchForm((currentForm) => ({
            ...currentForm,
            probeFrequencyDefaults: { ...currentForm.probeFrequencyDefaults, ...patch },
          }))
        }
      />

      <IncidentDefaultsSection
        value={form.incidentDefaults}
        onChange={(next) => patchForm((currentForm) => ({ ...currentForm, incidentDefaults: next }))}
      />

      <OverrideRulesSection
        form={form}
        onChange={(patch) => patchForm((currentForm) => ({ ...currentForm, ...patch }))}
      />

      <RetentionPolicySection
        value={form.retentionPolicy}
        onChange={(patch) =>
          patchForm((currentForm) => ({
            ...currentForm,
            retentionPolicy: { ...currentForm.retentionPolicy, ...patch },
          }))
        }
      />

      {state.saveError ? <section className="page-panel">{state.saveError}</section> : null}
      {state.saveSuccess ? <section className="page-panel">{state.saveSuccess}</section> : null}

      <div className="settings-actions">
        <button type="submit" disabled={state.saving}>
          {state.saving ? '正在保存…' : '保存设置'}
        </button>
      </div>
    </form>
  )
}
