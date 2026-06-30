import { type FormEvent, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Modal } from '../components/atoms/Modal'
import { Tabs } from '../components/atoms'
import { PageState } from '../components/PageState'
import { ApiError, getSettings, updateSettings } from '../lib/api'
import type {
  FeishuSettingsInput,
  MonitoringInstanceLabelOverrideRule,
  SettingsRecord,
  SettingsUpdateInput,
  TargetLabelOverrideRule,
  TargetTypeOverrideRule,
} from '../lib/types'
import { FeishuSettingsSection } from './settings/FeishuSettingsSection'
import { FrequencyDefaultsSection } from './settings/FrequencyDefaultsSection'
import { IncidentDefaultsSection } from './settings/IncidentDefaultsSection'
import { IPQualitySettingsSection } from './settings/IPQualitySettingsSection'
import { OverrideRulesSection } from './settings/OverrideRulesSection'
import { RetentionPolicySection } from './settings/RetentionPolicySection'
import { SubscriptionSettingsSection } from './settings/SubscriptionSettingsSection'
import { TelegramSettingsSection } from './settings/TelegramSettingsSection'
import { ThemeSettingsSection } from './settings/ThemeSettingsSection'
import type { SettingsFormState } from './settings/types'

const SETTINGS_TABS = [
  { value: 'appearance', label: '外观' },
  { value: 'notification', label: '通知' },
  { value: 'monitoring', label: '监控策略' },
  { value: 'subscriptions', label: '订阅' },
  { value: 'advanced', label: '高级' },
] as const

type SettingsTab = (typeof SETTINGS_TABS)[number]['value']
const SETTINGS_TAB_VALUES = new Set<string>(SETTINGS_TABS.map((tab) => tab.value))

type State = {
  loading: boolean; saving: boolean; error: string | null
  saveError: string | null; saveSuccess: string | null
  settings: SettingsRecord | null; form: SettingsFormState | null
}

type NotificationChannel = 'telegram' | 'feishu'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function formatJSON(value: unknown) { return JSON.stringify(value, null, 2) }

function buildFormState(settings: SettingsRecord): SettingsFormState {
  return {
    telegramBotToken: '',
    telegramChatId: settings.telegram.chat_id,
    telegramRuntimeManaged: settings.telegram.runtime_managed,
    feishuEnabled: settings.feishu.enabled,
    feishuWebhookPresent: settings.feishu.webhook_url_present,
    feishuWebhookUrl: '',
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
    monitoringInstanceLabelOverridesText: formatJSON(settings.override_rules.monitoring_instance_labels),
    targetTypeOverridesText: formatJSON(settings.override_rules.target_types),
    targetLabelOverridesText: formatJSON(settings.override_rules.target_labels),
    retentionPolicy: {
      rawLayerDays: String(settings.retention_policy.raw_layer_days),
      aggregateLayerDays: String(settings.retention_policy.aggregate_layer_days),
      eventLayerDays: String(settings.retention_policy.event_layer_days),
      notificationLayerDays: String(settings.retention_policy.notification_layer_days),
    },
    ipQuality: {
      enabled: settings.ip_quality_settings.enabled,
      frequencySeconds: String(settings.ip_quality_settings.frequency_seconds),
      staleAfterSeconds: String(settings.ip_quality_settings.stale_after_seconds),
      timeoutSeconds: String(settings.ip_quality_settings.timeout_seconds),
      rawRetentionDays: String(settings.ip_quality_settings.raw_retention_days),
      historyRetentionDays: String(settings.ip_quality_settings.history_retention_days),
      servicesText: settings.ip_quality_settings.services.join(', '),
    },
  }
}

function parsePositiveInteger(value: string, label: string) {
  const n = value.trim()
  if (!/^[1-9]\d*$/.test(n)) throw new Error(`${label}必须为正整数。`)
  return Number.parseInt(n, 10)
}

function parseRawRetentionDays(value: string) {
  const days = parsePositiveInteger(value, '原始层天数')
  if (days < 30) throw new Error('原始层天数必须至少为 30 天。')
  return days
}

function parsePositiveNumber(value: string, label: string) {
  const num = Number(value.trim())
  if (!Number.isFinite(num) || num <= 0) throw new Error(`${label}必须为正数。`)
  return num
}

function assertThreeLevelThresholdOrder(metric: string, warning: number, alert: number, critical: number) {
  if (!(warning < alert && alert < critical)) {
    throw new Error(`${metric} 阈值必须满足 关注 < 告警 < 严重。`)
  }
}

function assertTwoLevelThresholdOrder(metric: string, warning: number, critical: number) {
  if (!(warning < critical)) {
    throw new Error(`${metric} 阈值必须满足 关注 < 严重。`)
  }
}

function parseCommaList(value: string, label: string) {
  const items = value
    .split(',')
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean)
  if (items.length === 0) throw new Error(`${label}不能为空。`)
  return Array.from(new Set(items))
}

function parseOverrideRuleArray<T>(value: string, label: string): T[] {
  try { const p = JSON.parse(value); if (!Array.isArray(p)) throw 0; return p as T[] }
  catch { throw new Error(`${label}必须是 JSON 数组。`) }
}

type SettingsUpdateDraft = Omit<SettingsUpdateInput, 'telegram' | 'feishu'> & {
  telegram: { chat_id: string; runtime_managed: boolean; bot_token?: string }
  feishu: FeishuSettingsInput
}

function buildIncidentDefaults(f: SettingsFormState) {
  const incidentDefaults = {
    heartbeat_interval_seconds: parsePositiveInteger(f.incidentDefaults.heartbeatIntervalSeconds, '心跳间隔'),
    stale_threshold_intervals: parsePositiveInteger(f.incidentDefaults.staleThresholdIntervals, '失联阈值'),
    sweep_interval_seconds: parsePositiveInteger(f.incidentDefaults.sweepIntervalSeconds, '扫描间隔'),
    notify_on_started: f.incidentDefaults.notifyOnStarted,
    notify_on_escalated: f.incidentDefaults.notifyOnEscalated,
    notify_on_recovered: f.incidentDefaults.notifyOnRecovered,
    cpu_warning_pct: parsePositiveInteger(f.incidentDefaults.cpuWarningPct, 'CPU 关注'),
    cpu_alert_pct: parsePositiveInteger(f.incidentDefaults.cpuAlertPct, 'CPU 告警'),
    cpu_critical_pct: parsePositiveInteger(f.incidentDefaults.cpuCriticalPct, 'CPU 严重'),
    mem_warning_pct: parsePositiveInteger(f.incidentDefaults.memWarningPct, '内存关注'),
    mem_alert_pct: parsePositiveInteger(f.incidentDefaults.memAlertPct, '内存告警'),
    mem_critical_pct: parsePositiveInteger(f.incidentDefaults.memCriticalPct, '内存严重'),
    disk_warning_pct: parsePositiveInteger(f.incidentDefaults.diskWarningPct, '磁盘关注'),
    disk_alert_pct: parsePositiveInteger(f.incidentDefaults.diskAlertPct, '磁盘告警'),
    disk_critical_pct: parsePositiveInteger(f.incidentDefaults.diskCriticalPct, '磁盘严重'),
    inode_warning_pct: parsePositiveInteger(f.incidentDefaults.inodeWarningPct, 'Inode 关注'),
    inode_alert_pct: parsePositiveInteger(f.incidentDefaults.inodeAlertPct, 'Inode 告警'),
    inode_critical_pct: parsePositiveInteger(f.incidentDefaults.inodeCriticalPct, 'Inode 严重'),
    iowait_warning_pct: parsePositiveInteger(f.incidentDefaults.iowaitWarningPct, 'IOWait 关注'),
    iowait_critical_pct: parsePositiveInteger(f.incidentDefaults.iowaitCriticalPct, 'IOWait 严重'),
    load5_warning: parsePositiveNumber(f.incidentDefaults.load5Warning, 'Load5 关注'),
    load5_critical: parsePositiveNumber(f.incidentDefaults.load5Critical, 'Load5 严重'),
  }
  assertThreeLevelThresholdOrder('CPU', incidentDefaults.cpu_warning_pct, incidentDefaults.cpu_alert_pct, incidentDefaults.cpu_critical_pct)
  assertThreeLevelThresholdOrder('内存', incidentDefaults.mem_warning_pct, incidentDefaults.mem_alert_pct, incidentDefaults.mem_critical_pct)
  assertThreeLevelThresholdOrder('磁盘', incidentDefaults.disk_warning_pct, incidentDefaults.disk_alert_pct, incidentDefaults.disk_critical_pct)
  assertThreeLevelThresholdOrder('Inode', incidentDefaults.inode_warning_pct, incidentDefaults.inode_alert_pct, incidentDefaults.inode_critical_pct)
  assertTwoLevelThresholdOrder('IOWait', incidentDefaults.iowait_warning_pct, incidentDefaults.iowait_critical_pct)
  assertTwoLevelThresholdOrder('Load5', incidentDefaults.load5_warning, incidentDefaults.load5_critical)
  return incidentDefaults
}

function buildIPQualitySettings(f: SettingsFormState) {
  const frequencySeconds = parsePositiveInteger(f.ipQuality.frequencySeconds, 'IP 质量采集周期')
  if (frequencySeconds < 60) throw new Error('IP 质量采集周期必须至少为 60 秒。')
  const staleAfterSeconds = parsePositiveInteger(f.ipQuality.staleAfterSeconds, 'IP 质量过期窗口')
  if (staleAfterSeconds < frequencySeconds) throw new Error('IP 质量过期窗口必须大于或等于采集周期。')
  const timeoutSeconds = parsePositiveInteger(f.ipQuality.timeoutSeconds, 'IP 质量请求超时')
  if (timeoutSeconds > 300) throw new Error('IP 质量请求超时必须不超过 300 秒。')
  const rawRetentionDays = parsePositiveInteger(f.ipQuality.rawRetentionDays, 'IP 质量原始 JSON 保留天数')
  if (rawRetentionDays < 7) throw new Error('IP 质量原始 JSON 保留天数必须至少为 7 天。')
  const historyRetentionDays = parsePositiveInteger(f.ipQuality.historyRetentionDays, 'IP 质量历史保留天数')
  if (historyRetentionDays < rawRetentionDays) throw new Error('IP 质量历史保留天数必须大于或等于原始 JSON 保留天数。')

  return {
    enabled: f.ipQuality.enabled,
    frequency_seconds: frequencySeconds,
    stale_after_seconds: staleAfterSeconds,
    timeout_seconds: timeoutSeconds,
    raw_retention_days: rawRetentionDays,
    history_retention_days: historyRetentionDays,
    services: parseCommaList(f.ipQuality.servicesText, 'IP 质量采集服务集合'),
  }
}

function buildUpdateInput(form: SettingsFormState, cur: SettingsRecord): SettingsUpdateDraft {
  const botToken = form.telegramBotToken.trim()
  const chatId = form.telegramChatId.trim()
  const rm = form.telegramRuntimeManaged
  const hasToken = cur.telegram.token_present
  const newToken = botToken !== ''

  if (rm && !hasToken && !newToken) throw new Error('启用运行时接管前需提供 Bot Token。')
  if (!newToken && !hasToken) { /* no token anywhere, that's fine */ }
  else if (newToken && chatId === '') throw new Error('提供新 Bot Token 时需同时填写 Chat ID。')
  else if (hasToken && rm && chatId === '') throw new Error('启用运行时接管时 Chat ID 不能为空。')

  const feishu: FeishuSettingsInput = { enabled: form.feishuEnabled }
  const feishuWebhookUrl = form.feishuWebhookUrl.trim()
  if (feishuWebhookUrl !== '') {
    feishu.webhook_url = feishuWebhookUrl
  }

  const common = {
    feishu,
    host_sample_frequency_tier: form.hostSampleFrequencyTier,
    probe_frequency_defaults: { tcp: form.probeFrequencyDefaults.tcp, http: form.probeFrequencyDefaults.http, tls: form.probeFrequencyDefaults.tls },
    incident_defaults: buildIncidentDefaults(form),
    override_rules: {
      monitoring_instance_labels: parseOverrideRuleArray<MonitoringInstanceLabelOverrideRule>(form.monitoringInstanceLabelOverridesText, '监控实例标签覆盖'),
      target_types: parseOverrideRuleArray<TargetTypeOverrideRule>(form.targetTypeOverridesText, '目标类型覆盖'),
      target_labels: parseOverrideRuleArray<TargetLabelOverrideRule>(form.targetLabelOverridesText, '目标标签覆盖'),
    },
    retention_policy: {
      raw_layer_days: parseRawRetentionDays(form.retentionPolicy.rawLayerDays),
      aggregate_layer_days: parsePositiveInteger(form.retentionPolicy.aggregateLayerDays, '聚合层天数'),
      event_layer_days: parsePositiveInteger(form.retentionPolicy.eventLayerDays, '事件层天数'),
      notification_layer_days: parsePositiveInteger(form.retentionPolicy.notificationLayerDays, '通知层天数'),
    },
    ip_quality_settings: buildIPQualitySettings(form),
  }
  return { ...common, telegram: { ...(newToken ? { bot_token: botToken } : {}), chat_id: chatId, runtime_managed: rm } }
}

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<State>({
    loading: true, saving: false, error: null, saveError: null, saveSuccess: null, settings: null, form: null,
  })
  const rawTab = searchParams.get('tab')
  const activeTab: SettingsTab = rawTab && SETTINGS_TAB_VALUES.has(rawTab) ? (rawTab as SettingsTab) : 'appearance'
  const [modalState, setModalState] = useState<'closed' | 'select' | 'configure-telegram' | 'configure-feishu'>('closed')
  const [channelDraft, setChannelDraft] = useState<SettingsFormState | null>(null)
  const [activeChannels, setActiveChannels] = useState<Set<NotificationChannel>>(new Set())
  const [expandedChannels, setExpandedChannels] = useState<Set<NotificationChannel>>(new Set())
  const systemSettingsLoaded = state.settings !== null

  useEffect(() => {
    if (state.settings && state.form) {
      const active = new Set<NotificationChannel>()
      if (state.settings.telegram.token_present || state.settings.telegram.runtime_managed || state.form.telegramBotToken || state.form.telegramChatId) active.add('telegram')
      if (state.form.feishuEnabled || state.form.feishuWebhookPresent || state.form.feishuWebhookUrl.trim()) active.add('feishu')
      setActiveChannels((prev) => (prev.size === 0 ? active : prev))
    }
  }, [state.settings, state.form])

  useEffect(() => {
    if (activeTab === 'subscriptions' || systemSettingsLoaded) return
    let cancelled = false
    setState((current) => ({ ...current, loading: true, error: null }))
    getSettings()
      .then((settings) => { if (!cancelled) setState({ loading: false, saving: false, error: null, saveError: null, saveSuccess: null, settings, form: buildFormState(settings) }) })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, saving: false, error: describeError(err, '加载设置失败'), saveError: null, saveSuccess: null, settings: null, form: null }) })
    return () => { cancelled = true }
  }, [activeTab, systemSettingsLoaded])

  const systemSettings = state.settings
  const systemForm = state.form
  const systemTabActive = activeTab !== 'subscriptions'

  if (systemTabActive && state.loading) return <PageState kind="loading" title="正在加载设置…" />
  if (systemTabActive && (state.error || !systemSettings || !systemForm)) return <PageState kind="error" title="设置不可用" description={state.error ?? '未获取到设置数据'} />

  function patchForm(updater: (f: SettingsFormState) => SettingsFormState) {
    setState((c) => ({ ...c, form: c.form ? updater(c.form) : c.form, saveError: null, saveSuccess: null }))
  }
  function closeModal() { setChannelDraft(null); setModalState('closed') }
  function backToSelect() { setChannelDraft(null); setModalState('select') }
  function openChannelConfig(ch: NotificationChannel) {
    if (!state.form) return
    setChannelDraft(state.form)
    setModalState(ch === 'telegram' ? 'configure-telegram' : 'configure-feishu')
  }
  function patchDraft(updater: (f: SettingsFormState) => SettingsFormState) {
    setChannelDraft((draft) => {
      const base = draft ?? state.form
      return base ? updater(base) : draft
    })
  }
  function confirmChannel(ch: NotificationChannel) {
    setState((c) => ({ ...c, form: channelDraft ?? c.form, saveError: null, saveSuccess: null }))
    setActiveChannels((p) => new Set(p).add(ch)); setExpandedChannels((p) => new Set(p).add(ch)); closeModal()
  }
  function changeTab(tab: SettingsTab) {
    const next = new URLSearchParams(searchParams)
    if (tab === 'appearance') next.delete('tab')
    else next.set('tab', tab)
    setSearchParams(next, { replace: true })
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    let payload: SettingsUpdateDraft
    if (!state.settings || !state.form) {
      setState((c) => ({ ...c, saveError: '系统设置尚未加载完成', saveSuccess: null }))
      return
    }
    try { payload = buildUpdateInput(state.form, state.settings) } catch (err) { setState((c) => ({ ...c, saveError: describeError(err, '校验失败'), saveSuccess: null })); return }
    setState((c) => ({ ...c, saving: true, saveError: null, saveSuccess: null }))
    try {
      const updated = await updateSettings(payload)
      setState((c) => ({ ...c, saving: false, settings: updated, form: buildFormState(updated), saveError: null, saveSuccess: '设置已保存' }))
    } catch (err) { setState((c) => ({ ...c, saving: false, saveError: describeError(err, '保存失败'), saveSuccess: null })) }
  }

  return (
    <div className="page-stack animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">系统设置</h1>
          <p className="page-sub">通知、阈值、策略配置</p>
        </div>
      </div>

      <div className="settings-tabs">
        <Tabs variant="pill" value={activeTab} onChange={changeTab} items={SETTINGS_TABS} />
      </div>

      {activeTab === 'subscriptions' ? (
        <SubscriptionSettingsSection />
      ) : systemSettings && systemForm ? (
        <form className="settings-system-form" onSubmit={handleSubmit}>
          {activeTab === 'appearance' && (
            <div className="settings-section animate-in">
              <ThemeSettingsSection />
            </div>
          )}

          {activeTab === 'notification' && (
            <div className="settings-section animate-in">
              <div className="ss-title">通知通道</div>
              <div className="ss-desc">Telegram / 飞书异常推送</div>
              {activeChannels.has('telegram') && (
                <TelegramSettingsSection
                  settings={systemSettings.telegram}
                  form={systemForm}
                  isExpanded={expandedChannels.has('telegram')}
                  onToggleExpand={() => setExpandedChannels((p) => { const n = new Set(p); if (n.has('telegram')) n.delete('telegram'); else n.add('telegram'); return n })}
                  onChange={(patch) => patchForm((f) => ({ ...f, ...patch }))}
                />
              )}
              {activeChannels.has('feishu') && (
                <FeishuSettingsSection
                  settings={systemSettings.feishu}
                  form={systemForm}
                  isExpanded={expandedChannels.has('feishu')}
                  onToggleExpand={() => setExpandedChannels((p) => { const n = new Set(p); if (n.has('feishu')) n.delete('feishu'); else n.add('feishu'); return n })}
                  onChange={(patch) => patchForm((f) => ({ ...f, ...patch }))}
                />
              )}
              <div className="settings-channel-manager">
                <button type="button" className="btn sm secondary" onClick={() => { setChannelDraft(null); setModalState('select') }}>
                  + 新增通知渠道
                </button>
              </div>
            </div>
          )}

          {activeTab === 'monitoring' && (
            <>
              <div className="settings-section animate-in">
                <IncidentDefaultsSection
                  value={systemForm.incidentDefaults}
                  onChange={(next) => patchForm((f) => ({ ...f, incidentDefaults: next }))}
                />
              </div>
              <div className="settings-section animate-in">
                <FrequencyDefaultsSection
                  hostSampleFrequencyTier={systemForm.hostSampleFrequencyTier}
                  probeFrequencyDefaults={systemForm.probeFrequencyDefaults}
                  onHostSampleFrequencyChange={(v) => patchForm((f) => ({ ...f, hostSampleFrequencyTier: v }))}
                  onProbeFrequencyDefaultsChange={(patch) => patchForm((f) => ({ ...f, probeFrequencyDefaults: { ...f.probeFrequencyDefaults, ...patch } }))}
                />
              </div>
              <div className="settings-section animate-in">
                <IPQualitySettingsSection
                  value={systemForm.ipQuality}
                  onChange={(patch) => patchForm((f) => ({ ...f, ipQuality: { ...f.ipQuality, ...patch } }))}
                />
              </div>
              <div className="settings-section animate-in">
                <RetentionPolicySection
                  value={systemForm.retentionPolicy}
                  onChange={(patch) => patchForm((f) => ({ ...f, retentionPolicy: { ...f.retentionPolicy, ...patch } }))}
                />
              </div>
            </>
          )}

          {activeTab === 'advanced' && (
            <div className="settings-section animate-in">
              <OverrideRulesSection
                form={systemForm}
                onChange={(patch) => patchForm((f) => ({ ...f, ...patch }))}
              />
            </div>
          )}

          <div className="settings-save-footer">
            <div>
              {state.saveError && <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{state.saveError}</p>}
              {state.saveSuccess && <p className="settings-save-footer__message settings-save-footer__message--success">{state.saveSuccess}</p>}
            </div>
            <button type="submit" className="btn md primary" disabled={state.saving}>
              {state.saving ? '保存中…' : '保存设置'}
            </button>
          </div>
        </form>
      ) : null}

      {systemSettings && systemForm ? (
        <Modal
          open={modalState !== 'closed'}
          onClose={closeModal}
          title={modalState === 'select' ? '新增通知渠道' : modalState === 'configure-telegram' ? '配置 Telegram' : modalState === 'configure-feishu' ? '配置飞书' : ''}
          footer={
            modalState === 'configure-telegram' ? (
              <>
                <button type="button" className="btn md ghost" onClick={backToSelect}>返回</button>
                <button type="button" className="btn md primary" onClick={() => confirmChannel('telegram')}>添加并编辑</button>
              </>
            ) : modalState === 'configure-feishu' ? (
              <>
                <button type="button" className="btn md ghost" onClick={backToSelect}>返回</button>
                <button type="button" className="btn md primary" onClick={() => confirmChannel('feishu')}>添加并编辑</button>
              </>
            ) : null
          }
        >
          {modalState === 'select' && (
            <div className="settings-channel-modal">
              <button type="button" className="settings-channel-option" aria-label="Telegram" disabled={activeChannels.has('telegram')} onClick={() => openChannelConfig('telegram')}>
                <div className="settings-channel-option__content">
                  <span className="settings-channel-option__icon">TG</span>
                  <span className="settings-channel-option__text">
                    <span className="settings-channel-option__title">Telegram</span>
                    <span className="settings-channel-option__description">通过 Telegram Bot 推送异常事件通知</span>
                  </span>
                </div>
              </button>
              <button type="button" className="settings-channel-option" aria-label="飞书 (Feishu)" disabled={activeChannels.has('feishu')} onClick={() => openChannelConfig('feishu')}>
                <div className="settings-channel-option__content">
                  <span className="settings-channel-option__icon">FS</span>
                  <span className="settings-channel-option__text">
                    <span className="settings-channel-option__title">飞书 (Feishu)</span>
                    <span className="settings-channel-option__description">通过飞书群 Webhook 推送异常事件通知</span>
                  </span>
                </div>
              </button>
            </div>
          )}
          {modalState === 'configure-telegram' && (
            <TelegramSettingsSection
              wrapper="none"
              settings={systemSettings.telegram}
              form={channelDraft ?? systemForm}
              onChange={(patch) => patchDraft((f) => ({ ...f, ...patch }))}
            />
          )}
          {modalState === 'configure-feishu' && (
            <FeishuSettingsSection
              wrapper="none"
              settings={systemSettings.feishu}
              form={channelDraft ?? systemForm}
              onChange={(patch) => patchDraft((f) => ({ ...f, ...patch }))}
            />
          )}
        </Modal>
      ) : null}
    </div>
  )
}
