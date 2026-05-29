import { type FormEvent, useEffect, useState } from 'react'

import { Modal } from '../components/atoms/Modal'
import { Tabs } from '../components/atoms'
import { PageState } from '../components/PageState'
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

const SETTINGS_TABS = [
  { value: 'appearance', label: '外观' },
  { value: 'notification', label: '通知' },
  { value: 'monitoring', label: '监控策略' },
  { value: 'advanced', label: '高级' },
]

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
  const n = value.trim()
  if (!/^[1-9]\d*$/.test(n)) throw new Error(`${label}必须为正整数。`)
  return Number.parseInt(n, 10)
}

function parsePositiveNumber(value: string, label: string) {
  const num = Number(value.trim())
  if (!Number.isFinite(num) || num <= 0) throw new Error(`${label}必须为正数。`)
  return num
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
  return {
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

  const common = {
    feishu: { enabled: form.feishuEnabled, webhook_url: form.feishuWebhookUrl.trim() },
    host_sample_frequency_tier: form.hostSampleFrequencyTier,
    probe_frequency_defaults: { tcp: form.probeFrequencyDefaults.tcp, http: form.probeFrequencyDefaults.http, tls: form.probeFrequencyDefaults.tls },
    incident_defaults: buildIncidentDefaults(form),
    override_rules: {
      node_labels: parseOverrideRuleArray<NodeLabelOverrideRule>(form.nodeLabelOverridesText, '节点标签覆盖'),
      target_types: parseOverrideRuleArray<TargetTypeOverrideRule>(form.targetTypeOverridesText, '目标类型覆盖'),
      target_labels: parseOverrideRuleArray<TargetLabelOverrideRule>(form.targetLabelOverridesText, '目标标签覆盖'),
    },
    retention_policy: {
      raw_layer_days: parsePositiveInteger(form.retentionPolicy.rawLayerDays, '原始层天数'),
      aggregate_layer_days: parsePositiveInteger(form.retentionPolicy.aggregateLayerDays, '聚合层天数'),
      event_layer_days: parsePositiveInteger(form.retentionPolicy.eventLayerDays, '事件层天数'),
      notification_layer_days: parsePositiveInteger(form.retentionPolicy.notificationLayerDays, '通知层天数'),
    },
  }
  return { ...common, telegram: { ...(newToken ? { bot_token: botToken } : {}), chat_id: chatId, runtime_managed: rm } }
}

export function SettingsPage() {
  const [state, setState] = useState<State>({
    loading: true, saving: false, error: null, saveError: null, saveSuccess: null, settings: null, form: null,
  })
  const [activeTab, setActiveTab] = useState('appearance')
  const [modalState, setModalState] = useState<'closed' | 'select' | 'configure-telegram' | 'configure-feishu'>('closed')
  const [channelDraft, setChannelDraft] = useState<SettingsFormState | null>(null)
  const [activeChannels, setActiveChannels] = useState<Set<NotificationChannel>>(new Set())
  const [expandedChannels, setExpandedChannels] = useState<Set<NotificationChannel>>(new Set())

  useEffect(() => {
    if (state.settings && state.form) {
      const active = new Set<NotificationChannel>()
      if (state.settings.telegram.token_present || state.settings.telegram.runtime_managed || state.form.telegramBotToken || state.form.telegramChatId) active.add('telegram')
      if (state.form.feishuEnabled || state.form.feishuWebhookUrl.trim()) active.add('feishu')
      setActiveChannels((prev) => (prev.size === 0 ? active : prev))
    }
  }, [state.settings, state.form])

  useEffect(() => {
    let cancelled = false
    getSettings()
      .then((settings) => { if (!cancelled) setState({ loading: false, saving: false, error: null, saveError: null, saveSuccess: null, settings, form: buildFormState(settings) }) })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, saving: false, error: describeError(err, '加载设置失败'), saveError: null, saveSuccess: null, settings: null, form: null }) })
    return () => { cancelled = true }
  }, [])

  if (state.loading) return <PageState kind="loading" title="正在加载设置…" />
  if (state.error || !state.settings || !state.form) return <PageState kind="error" title="设置不可用" description={state.error ?? '未获取到设置数据'} />

  const { settings, form } = state

  function patchForm(updater: (f: SettingsFormState) => SettingsFormState) {
    setState((c) => ({ ...c, form: c.form ? updater(c.form) : c.form, saveError: null, saveSuccess: null }))
  }
  function closeModal() { setChannelDraft(null); setModalState('closed') }
  function backToSelect() { setChannelDraft(null); setModalState('select') }
  function openChannelConfig(ch: NotificationChannel) { setChannelDraft(form); setModalState(ch === 'telegram' ? 'configure-telegram' : 'configure-feishu') }
  function patchDraft(updater: (f: SettingsFormState) => SettingsFormState) { setChannelDraft((d) => updater(d ?? form)) }
  function confirmChannel(ch: NotificationChannel) {
    setState((c) => ({ ...c, form: channelDraft ?? form, saveError: null, saveSuccess: null }))
    setActiveChannels((p) => new Set(p).add(ch)); setExpandedChannels((p) => new Set(p).add(ch)); closeModal()
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    let payload: SettingsUpdateDraft
    try { payload = buildUpdateInput(form, settings) } catch (err) { setState((c) => ({ ...c, saveError: describeError(err, '校验失败'), saveSuccess: null })); return }
    setState((c) => ({ ...c, saving: true, saveError: null, saveSuccess: null }))
    try {
      const updated = await updateSettings(payload)
      setState((c) => ({ ...c, saving: false, settings: updated, form: buildFormState(updated), saveError: null, saveSuccess: '设置已保存' }))
    } catch (err) { setState((c) => ({ ...c, saving: false, saveError: describeError(err, '保存失败'), saveSuccess: null })) }
  }

  return (
    <form className="page-stack animate-in" onSubmit={handleSubmit}>
      <div className="page-header">
        <div>
          <h1 className="page-title">系统设置</h1>
          <p className="page-sub">通知、阈值、策略配置</p>
        </div>
      </div>

      <div className="settings-tabs">
        <Tabs variant="pill" value={activeTab} onChange={setActiveTab} items={SETTINGS_TABS} />
      </div>

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
              settings={settings.telegram}
              form={form}
              isExpanded={expandedChannels.has('telegram')}
              onToggleExpand={() => setExpandedChannels((p) => { const n = new Set(p); if (n.has('telegram')) n.delete('telegram'); else n.add('telegram'); return n })}
              onChange={(patch) => patchForm((f) => ({ ...f, ...patch }))}
            />
          )}
          {activeChannels.has('feishu') && (
            <FeishuSettingsSection
              form={form}
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
              value={form.incidentDefaults}
              onChange={(next) => patchForm((f) => ({ ...f, incidentDefaults: next }))}
            />
          </div>
          <div className="settings-section animate-in">
            <FrequencyDefaultsSection
              hostSampleFrequencyTier={form.hostSampleFrequencyTier}
              probeFrequencyDefaults={form.probeFrequencyDefaults}
              onHostSampleFrequencyChange={(v) => patchForm((f) => ({ ...f, hostSampleFrequencyTier: v }))}
              onProbeFrequencyDefaultsChange={(patch) => patchForm((f) => ({ ...f, probeFrequencyDefaults: { ...f.probeFrequencyDefaults, ...patch } }))}
            />
          </div>
          <div className="settings-section animate-in">
            <RetentionPolicySection
              value={form.retentionPolicy}
              onChange={(patch) => patchForm((f) => ({ ...f, retentionPolicy: { ...f.retentionPolicy, ...patch } }))}
            />
          </div>
        </>
      )}

      {activeTab === 'advanced' && (
        <div className="settings-section animate-in">
          <OverrideRulesSection
            form={form}
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
            settings={settings.telegram}
            form={channelDraft ?? form}
            onChange={(patch) => patchDraft((f) => ({ ...f, ...patch }))}
          />
        )}
        {modalState === 'configure-feishu' && (
          <FeishuSettingsSection
            wrapper="none"
            form={channelDraft ?? form}
            onChange={(patch) => patchDraft((f) => ({ ...f, ...patch }))}
          />
        )}
      </Modal>
    </form>
  )
}
