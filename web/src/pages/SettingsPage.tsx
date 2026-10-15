import { type FormEvent, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Modal } from '../components/atoms/Modal'
import { TabPanel, Tabs } from '../components/atoms'
import { PageState } from '../components/PageState'
import { ApiError, getSettings, updateSettings } from '../lib/api'
import { managementAccessForSession } from '../lib/auth-client'
import { useAuth } from '../lib/auth-context'
import { AccessManagementSection } from './settings/AccessManagementSection'
import { DeploymentSettingsSection } from './settings/DeploymentSettingsSection'
import type {
  FeishuSettingsInput,
  IncidentDefaults,
  IPQualitySettingsUpdateInput,
  IPQualitySourceCredential,
  SettingsRecord,
  SettingsTelegramInput,
  SettingsUpdateInput,
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
import {
  buildMonitoringOverrideDrafts,
  buildTargetLabelOverrideDrafts,
  buildTargetTypeOverrideDrafts,
  comparableSettingsForm,
  compileOverrideRules,
} from './settings/overrideRulesModel'
import type { SettingsFormState, SettingsIPQualityForm } from './settings/types'

const SETTINGS_TABS = [
  { value: 'appearance', label: '外观' },
  { value: 'notification', label: '通知' },
  { value: 'monitoring', label: '监控策略' },
  { value: 'subscriptions', label: '订阅' },
  { value: 'advanced', label: '高级' },
  { value: 'deployment', label: '部署配置' },
  { value: 'access', label: '用户与权限' },
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

function deploymentSweepSeconds(settings: SettingsRecord) {
  const seconds = settings.incident_sweep.deployment_seconds
  return Number.isInteger(seconds) && seconds > 0 ? seconds : 5
}

function persistedSettingsNote(source: SettingsRecord['settings_source']) {
  if (source === 'database') return '本分区保存在数据库中。这里没有部署来源的字段会保持已保存的值。'
  return '本分区尚未写入数据库，当前显示程序默认值。保存后才会持久化。'
}

function buildFormState(settings: SettingsRecord): SettingsFormState {
  return {
    telegramBotToken: '',
    telegramChatId: settings.telegram.chat_id,
    telegramMode: settings.telegram.mode,
    incidentSweepMode: settings.incident_sweep.mode,
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
    monitoringInstanceLabelOverrides: buildMonitoringOverrideDrafts(settings.override_rules.monitoring_instance_labels),
    targetTypeOverrides: buildTargetTypeOverrideDrafts(settings.override_rules.target_types),
    targetLabelOverrides: buildTargetLabelOverrideDrafts(settings.override_rules.target_labels),
    retentionPolicy: {
      rawLayerDays: String(settings.retention_policy.raw_layer_days),
      aggregateLayerDays: String(settings.retention_policy.aggregate_layer_days),
    },
    ipQuality: {
      enabled: settings.ip_quality_settings.enabled,
      frequencySeconds: String(settings.ip_quality_settings.frequency_seconds),
      staleAfterSeconds: String(settings.ip_quality_settings.stale_after_seconds),
      timeoutSeconds: String(settings.ip_quality_settings.timeout_seconds),
      servicesText: settings.ip_quality_settings.services.join(', '),
      mailEnabled: settings.ip_quality_settings.mail_enabled,
      ipapiKey: '',
      ipapiClear: false,
      abuseipdbKey: '',
      abuseipdbClear: false,
      maxmindAccountId: '',
      maxmindLicenseKey: '',
      maxmindClear: false,
    },
  }
}

function telegramChannelVisible(settings: SettingsRecord, form: SettingsFormState) {
  if (form.telegramMode === 'override' || form.telegramMode === 'disabled') return true
  if (settings.telegram.mode === 'override' || settings.telegram.mode === 'disabled') return true
  if (settings.telegram.deployment.configured) return true
  return form.telegramBotToken.trim() !== ''
}

function deriveActiveChannels(settings: SettingsRecord, form: SettingsFormState) {
  const active = new Set<NotificationChannel>()
  if (telegramChannelVisible(settings, form)) active.add('telegram')
  if (form.feishuEnabled || form.feishuWebhookPresent || form.feishuWebhookUrl.trim()) active.add('feishu')
  return active
}

function parsePositiveInteger(value: string, label: string) {
  const n = value.trim()
  if (!/^[1-9]\d*$/.test(n)) throw new Error(`${label}必须为正整数。`)
  return Number.parseInt(n, 10)
}

function parseRawRetentionDays(value: string) {
  const days = parsePositiveInteger(value, '原始层天数')
  if (days < 30) throw new Error('原始层天数必须至少为 30 天。')
  if (days > 365) throw new Error('原始层天数不能超过 365 天。')
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

function buildTelegramUpdate(form: SettingsFormState, current: SettingsRecord): SettingsTelegramInput | undefined {
  const mode = form.telegramMode
  const token = form.telegramBotToken.trim()
  const chatID = form.telegramChatId.trim()
  const modeChanged = mode !== current.telegram.mode
  const chatChanged = chatID !== current.telegram.chat_id
  if (!modeChanged && token === '' && !chatChanged) return undefined
  if (mode !== 'override') return modeChanged ? { mode } : undefined
  if (token.includes('*')) throw new Error('Bot Token 不能使用掩码。')
  if (chatID === '') throw new Error('页面覆盖需要填写 Chat ID。')
  if (!current.telegram.token_present && token === '') throw new Error('页面覆盖需要提供 Bot Token。')
  const telegram: SettingsTelegramInput = { mode: 'override' }
  if (token !== '') telegram.bot_token = token
  if (chatChanged) telegram.chat_id = chatID
  return telegram
}

function sweepModeChanged(form: SettingsFormState, current: SettingsRecord) {
  if (form.incidentSweepMode !== current.incident_sweep.mode) return true
  if (form.incidentSweepMode !== 'override') return false
  return form.incidentDefaults.sweepIntervalSeconds.trim() !== String(current.incident_defaults.sweep_interval_seconds)
}

function buildIncidentDefaults(f: SettingsFormState, inheritedSweepSeconds: number) {
  const incidentDefaults = {
    heartbeat_interval_seconds: parsePositiveInteger(f.incidentDefaults.heartbeatIntervalSeconds, '心跳间隔'),
    stale_threshold_intervals: parsePositiveInteger(f.incidentDefaults.staleThresholdIntervals, '失联阈值'),
    sweep_interval_seconds: parsePositiveInteger(
      f.incidentSweepMode === 'inherit' ? String(inheritedSweepSeconds) : f.incidentDefaults.sweepIntervalSeconds,
      '扫描间隔',
    ),
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

function readGlobalIncident(form: SettingsFormState, inheritedSweepSeconds: number): IncidentDefaults | null {
  try {
    return buildIncidentDefaults(form, inheritedSweepSeconds)
  } catch {
    return null
  }
}

// 凭据：空草稿=省略（保留）；清除=空字符串；新值=替换。掩码永远不作为 Key 回传。
function credentialUpdate(draft: string, clear: boolean, current: IPQualitySourceCredential, label: string): string | undefined {
  const key = draft.trim()
  if (key !== '') {
    if (key.includes('*') || (current.masked_summary !== '' && key === current.masked_summary)) throw new Error(`${label} Key 不能使用掩码。`)
    return key
  }
  return clear && current.configured ? '' : undefined
}

// GeoLite 账号 ID 与 License Key 成对写入：任一填写则两者都必须填写并一起替换；清除时两者同时为空字符串；
// 都留空即保留。掩码永远不作为凭据回传。
function maxmindUpdate(f: SettingsIPQualityForm, current: IPQualitySourceCredential): { maxmind_account_id: string, maxmind_license_key: string } | undefined {
  const accountId = f.maxmindAccountId.trim()
  const licenseKey = f.maxmindLicenseKey.trim()
  if (accountId === '' && licenseKey === '') {
    return f.maxmindClear && current.configured ? { maxmind_account_id: '', maxmind_license_key: '' } : undefined
  }
  if (accountId === '' || licenseKey === '') throw new Error('GeoLite 账号 ID 与 License Key 需同时填写。')
  for (const value of [accountId, licenseKey]) {
    if (value.includes('*') || (current.masked_summary !== '' && value === current.masked_summary)) throw new Error('GeoLite 凭据不能使用掩码。')
  }
  return { maxmind_account_id: accountId, maxmind_license_key: licenseKey }
}

function buildIPQualitySettings(f: SettingsFormState, cur: SettingsRecord): IPQualitySettingsUpdateInput {
  const frequencySeconds = parsePositiveInteger(f.ipQuality.frequencySeconds, 'IP 质量采集周期')
  if (frequencySeconds < 60) throw new Error('IP 质量采集周期必须至少为 60 秒。')
  const staleAfterSeconds = parsePositiveInteger(f.ipQuality.staleAfterSeconds, 'IP 质量过期窗口')
  if (staleAfterSeconds < frequencySeconds) throw new Error('IP 质量过期窗口必须大于或等于采集周期。')
  const timeoutSeconds = parsePositiveInteger(f.ipQuality.timeoutSeconds, 'IP 质量请求超时')
  if (timeoutSeconds > 300) throw new Error('IP 质量请求超时必须不超过 300 秒。')

  const update: IPQualitySettingsUpdateInput = {
    enabled: f.ipQuality.enabled,
    frequency_seconds: frequencySeconds,
    stale_after_seconds: staleAfterSeconds,
    timeout_seconds: timeoutSeconds,
    services: parseCommaList(f.ipQuality.servicesText, 'IP 质量采集服务集合'),
    mail_enabled: f.ipQuality.mailEnabled,
  }
  const sources = cur.ip_quality_settings.sources
  const ipapiKey = credentialUpdate(f.ipQuality.ipapiKey, f.ipQuality.ipapiClear, sources.ipapi_is, 'ipapi.is')
  const abuseipdbKey = credentialUpdate(f.ipQuality.abuseipdbKey, f.ipQuality.abuseipdbClear, sources.abuseipdb, 'AbuseIPDB')
  const maxmind = maxmindUpdate(f.ipQuality, sources.maxmind)
  if (ipapiKey !== undefined || abuseipdbKey !== undefined || maxmind !== undefined) {
    update.sources = {
      ...(ipapiKey !== undefined ? { ipapi_is_key: ipapiKey } : {}),
      ...(abuseipdbKey !== undefined ? { abuseipdb_key: abuseipdbKey } : {}),
      ...(maxmind ?? {}),
    }
  }
  return update
}

function buildUpdateInput(form: SettingsFormState, cur: SettingsRecord): SettingsUpdateInput {
  const telegram = buildTelegramUpdate(form, cur)
  const feishu: FeishuSettingsInput = { enabled: form.feishuEnabled }
  const feishuWebhookUrl = form.feishuWebhookUrl.trim()
  if (feishuWebhookUrl !== '') {
    feishu.webhook_url = feishuWebhookUrl
  }

  const incidentDefaults = buildIncidentDefaults(form, deploymentSweepSeconds(cur))
  const compiled = compileOverrideRules({
    monitoring: form.monitoringInstanceLabelOverrides,
    targetTypes: form.targetTypeOverrides,
    targetLabels: form.targetLabelOverrides,
    globalIncident: incidentDefaults,
  })
  if (compiled.errors.length > 0) {
    throw new Error(compiled.errors[0]?.message ?? '覆盖规则无效。')
  }
  const update: SettingsUpdateInput = {
    feishu,
    host_sample_frequency_tier: form.hostSampleFrequencyTier,
    probe_frequency_defaults: { tcp: form.probeFrequencyDefaults.tcp, http: form.probeFrequencyDefaults.http, tls: form.probeFrequencyDefaults.tls },
    incident_defaults: incidentDefaults,
    override_rules: compiled.rules,
    retention_policy: {
      raw_layer_days: parseRawRetentionDays(form.retentionPolicy.rawLayerDays),
      aggregate_layer_days: parsePositiveInteger(form.retentionPolicy.aggregateLayerDays, '聚合层天数'),
    },
    ip_quality_settings: buildIPQualitySettings(form, cur),
  }
  if (telegram) update.telegram = telegram
  if (sweepModeChanged(form, cur)) update.incident_sweep_mode = form.incidentSweepMode
  return update
}

function rebaseRecord<T extends Record<string, string | boolean>>(submitted: T, live: T, response: T): T {
  const next = { ...response }
  for (const key of Object.keys(submitted) as (keyof T)[]) {
    if (live[key] !== submitted[key]) next[key] = live[key]
  }
  return next
}

function rebaseAtomic<T>(submitted: T, live: T, response: T): T {
  return JSON.stringify(live) === JSON.stringify(submitted) ? response : live
}

function rebaseSettingsForm(submitted: SettingsFormState, live: SettingsFormState, response: SettingsFormState): SettingsFormState {
  const tokenEdited = live.telegramBotToken !== submitted.telegramBotToken
  const telegramEdited = tokenEdited
    || live.telegramMode !== submitted.telegramMode
    || live.telegramChatId !== submitted.telegramChatId
  const sweepEdited = live.incidentSweepMode !== submitted.incidentSweepMode
    || live.incidentDefaults.sweepIntervalSeconds !== submitted.incidentDefaults.sweepIntervalSeconds
  const incidentDefaults = rebaseRecord(submitted.incidentDefaults, live.incidentDefaults, response.incidentDefaults)
  incidentDefaults.sweepIntervalSeconds = sweepEdited
    ? live.incidentDefaults.sweepIntervalSeconds
    : response.incidentDefaults.sweepIntervalSeconds
  return {
    telegramBotToken: tokenEdited ? live.telegramBotToken : '',
    telegramChatId: telegramEdited ? live.telegramChatId : response.telegramChatId,
    telegramMode: telegramEdited ? live.telegramMode : response.telegramMode,
    incidentSweepMode: sweepEdited ? live.incidentSweepMode : response.incidentSweepMode,
    feishuEnabled: live.feishuEnabled === submitted.feishuEnabled ? response.feishuEnabled : live.feishuEnabled,
    feishuWebhookPresent: live.feishuWebhookPresent === submitted.feishuWebhookPresent ? response.feishuWebhookPresent : live.feishuWebhookPresent,
    feishuWebhookUrl: live.feishuWebhookUrl !== submitted.feishuWebhookUrl ? live.feishuWebhookUrl : '',
    hostSampleFrequencyTier: live.hostSampleFrequencyTier === submitted.hostSampleFrequencyTier ? response.hostSampleFrequencyTier : live.hostSampleFrequencyTier,
    probeFrequencyDefaults: rebaseRecord(submitted.probeFrequencyDefaults, live.probeFrequencyDefaults, response.probeFrequencyDefaults),
    incidentDefaults,
    monitoringInstanceLabelOverrides: rebaseAtomic(submitted.monitoringInstanceLabelOverrides, live.monitoringInstanceLabelOverrides, response.monitoringInstanceLabelOverrides),
    targetTypeOverrides: rebaseAtomic(submitted.targetTypeOverrides, live.targetTypeOverrides, response.targetTypeOverrides),
    targetLabelOverrides: rebaseAtomic(submitted.targetLabelOverrides, live.targetLabelOverrides, response.targetLabelOverrides),
    retentionPolicy: rebaseRecord(submitted.retentionPolicy, live.retentionPolicy, response.retentionPolicy),
    ipQuality: rebaseRecord(submitted.ipQuality, live.ipQuality, response.ipQuality),
  }
}

export function SettingsPage() {
  const auth = useAuth()
  const canManage = managementAccessForSession(auth)
  const capabilityPending = auth.loading || auth.status === 'loading'
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<State>({
    loading: true, saving: false, error: null, saveError: null, saveSuccess: null, settings: null, form: null,
  })
  const rawTab = searchParams.get('tab')
  const activeTab: SettingsTab = rawTab && SETTINGS_TAB_VALUES.has(rawTab) ? (rawTab as SettingsTab) : 'appearance'
  const visibleTabs = canManage
    ? SETTINGS_TABS
    : SETTINGS_TABS.filter((tab) => tab.value !== 'access' && tab.value !== 'deployment')
  const [modalState, setModalState] = useState<'closed' | 'select' | 'configure-telegram' | 'configure-feishu'>('closed')
  const [channelDraft, setChannelDraft] = useState<SettingsFormState | null>(null)
  const [activeChannels, setActiveChannels] = useState<Set<NotificationChannel>>(new Set())
  const [expandedChannels, setExpandedChannels] = useState<Set<NotificationChannel>>(new Set())
  const [unsavedOpen, setUnsavedOpen] = useState(false)
  const [restoreOpen, setRestoreOpen] = useState(false)
  const pendingTabRef = useRef<SettingsTab | null>(null)
  const draftVersionRef = useRef(0)
  const saveRequestRef = useRef(0)
  const formRef = useRef<SettingsFormState | null>(null)
  formRef.current = state.form
  const systemSettingsLoaded = state.settings !== null

  const isDirty =
    !!state.settings &&
    !!state.form &&
    JSON.stringify(comparableSettingsForm(buildFormState(state.settings))) !== JSON.stringify(comparableSettingsForm(state.form))

  useEffect(() => {
    if (activeTab === 'subscriptions' || activeTab === 'access' || activeTab === 'deployment' || systemSettingsLoaded) return
    let cancelled = false
    setState((current) => ({ ...current, loading: true, error: null }))
    getSettings()
      .then((settings) => {
        if (cancelled) return
        const form = buildFormState(settings)
        setState({ loading: false, saving: false, error: null, saveError: null, saveSuccess: null, settings, form })
        setActiveChannels(deriveActiveChannels(settings, form))
      })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, saving: false, error: describeError(err, '加载设置失败'), saveError: null, saveSuccess: null, settings: null, form: null }) })
    return () => { cancelled = true }
  }, [activeTab, systemSettingsLoaded])

  useEffect(() => () => { saveRequestRef.current += 1 }, [])

  const systemSettings = state.settings
  const systemForm = state.form
  const systemTabActive = activeTab !== 'subscriptions' && activeTab !== 'access' && activeTab !== 'deployment'
  const restrictedTab = activeTab === 'access' || activeTab === 'deployment' ? activeTab : null

  if (systemTabActive && state.loading) return <PageState kind="loading" title="正在加载设置…" />
  if (systemTabActive && (state.error || !systemSettings || !systemForm)) return <PageState kind="error" title="设置不可用" description={state.error ?? '未获取到设置数据'} />

  function applyLoadedChannels(settings: SettingsRecord, form: SettingsFormState) {
    const channels = deriveActiveChannels(settings, form)
    setActiveChannels(channels)
    setExpandedChannels((prev) => new Set(Array.from(prev).filter((channel) => channels.has(channel))))
  }
  function patchForm(updater: (f: SettingsFormState) => SettingsFormState) {
    draftVersionRef.current += 1
    setState((c) => ({ ...c, form: c.form ? updater(c.form) : c.form, saveError: null, saveSuccess: null }))
  }
  function closeModal() { setChannelDraft(null); setModalState('closed') }
  function backToSelect() { setChannelDraft(null); setModalState('select') }
  function openChannelConfig(ch: NotificationChannel) {
    if (!state.form || !state.settings) return
    const base = ch === 'telegram'
      ? { ...state.form, telegramMode: 'override' as const, telegramBotToken: '', telegramChatId: state.settings.telegram.chat_id }
      : state.form
    setChannelDraft(base)
    setModalState(ch === 'telegram' ? 'configure-telegram' : 'configure-feishu')
  }
  function patchDraft(updater: (f: SettingsFormState) => SettingsFormState) {
    setChannelDraft((draft) => {
      const base = draft ?? state.form
      return base ? updater(base) : draft
    })
  }
  function confirmChannel(ch: NotificationChannel) {
    draftVersionRef.current += 1
    setState((c) => ({ ...c, form: channelDraft ?? c.form, saveError: null, saveSuccess: null }))
    setActiveChannels((p) => new Set(p).add(ch)); setExpandedChannels((p) => new Set(p).add(ch)); closeModal()
  }
  function applyTab(tab: SettingsTab) {
    const next = new URLSearchParams(searchParams)
    if (tab === 'appearance') next.delete('tab')
    else next.set('tab', tab)
    setSearchParams(next, { replace: true })
  }
  function restoreTelegramDraft(form: SettingsFormState): SettingsFormState {
    return {
      ...form,
      telegramMode: 'inherit',
      telegramBotToken: '',
      telegramChatId: state.settings?.telegram.deployment.chat_id ?? '',
    }
  }
  function confirmTelegramRestore() {
    if (modalState !== 'closed') setChannelDraft((draft) => (draft ? restoreTelegramDraft(draft) : draft))
    else patchForm(restoreTelegramDraft)
    setRestoreOpen(false)
  }
  function setSweepMode(mode: SettingsFormState['incidentSweepMode']) {
    if (!state.settings || state.form?.incidentSweepMode === mode) return
    // Inherit already renders deployment seconds read-only. Keep the override draft so switching back restores it.
    patchForm((form) => ({ ...form, incidentSweepMode: mode }))
  }

  async function saveSettings(): Promise<boolean> {
    if (!state.settings || !state.form) {
      setState((c) => ({ ...c, saveError: '系统设置尚未加载完成', saveSuccess: null }))
      return false
    }
    let payload: SettingsUpdateInput
    try { payload = buildUpdateInput(state.form, state.settings) }
    catch (err) { setState((c) => ({ ...c, saveError: describeError(err, '校验失败'), saveSuccess: null })); return false }
    const submittedForm = structuredClone(state.form)
    const version = draftVersionRef.current
    const request = ++saveRequestRef.current
    setState((c) => ({ ...c, saving: true, saveError: null, saveSuccess: null }))
    try {
      const updated = await updateSettings(payload)
      if (request !== saveRequestRef.current) return false
      if (draftVersionRef.current !== version) {
        const responseForm = buildFormState(updated)
        const form = rebaseSettingsForm(submittedForm, formRef.current ?? submittedForm, responseForm)
        setState((c) => ({ ...c, saving: false, settings: updated, form, saveError: null, saveSuccess: null }))
        applyLoadedChannels(updated, form)
        return false
      }
      const form = buildFormState(updated)
      setState((c) => ({ ...c, saving: false, settings: updated, form, saveError: null, saveSuccess: '设置已保存' }))
      applyLoadedChannels(updated, form)
      return true
    } catch (err) {
      if (request !== saveRequestRef.current) return false
      setState((c) => ({ ...c, saving: false, saveError: describeError(err, '保存失败'), saveSuccess: null }))
      return false
    }
  }

  function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    void saveSettings()
  }

  function requestTab(tab: SettingsTab) {
    if (tab === activeTab) return
    if (systemTabActive && isDirty) {
      pendingTabRef.current = tab
      setUnsavedOpen(true)
      return
    }
    applyTab(tab)
  }

  async function confirmSaveAndLeave() {
    const target = pendingTabRef.current
    setUnsavedOpen(false)
    pendingTabRef.current = null
    const ok = await saveSettings()
    if (ok && target) applyTab(target)
  }

  function discardAndLeave() {
    const target = pendingTabRef.current
    setUnsavedOpen(false)
    pendingTabRef.current = null
    if (state.settings) {
      const resetForm = buildFormState(state.settings)
      const resetChannels = deriveActiveChannels(state.settings, resetForm)
      setState((c) => ({ ...c, form: resetForm, saveError: null, saveSuccess: null }))
      setActiveChannels(resetChannels)
      setExpandedChannels((prev) => new Set(Array.from(prev).filter((channel) => resetChannels.has(channel))))
    }
    if (target) applyTab(target)
  }

  return (
    <div className="page settings-page">
      <header className="page__head">
        <h1 className="page__title">系统设置</h1>
      </header>

      <div className="settings-tabs">
        <Tabs
          label="系统设置分区"
          idBase="settings-sections"
          variant="pill"
          value={activeTab}
          onChange={requestTab}
          items={visibleTabs}
        />
      </div>

      {restrictedTab && !canManage ? (
        capabilityPending ? (
          <PageState kind="loading" title="正在确认管理能力…" surface="empty" compact />
        ) : auth.status === 'error' ? (
          <PageState
            kind="error"
            title="能力读取失败"
            description={restrictedTab === 'deployment'
              ? '暂时无法确认管理能力，不会打开部署配置。'
              : '暂时无法确认管理能力，不会打开用户与权限目录。'}
            action={<button type="button" className="btn sm secondary" onClick={() => void auth.retry()}>重试</button>}
            surface="empty"
            compact
          />
        ) : restrictedTab === 'deployment' ? (
          <section className="settings-section" aria-labelledby="deployment-forbidden-title">
            <h2 className="ss-title" id="deployment-forbidden-title">无权查看部署配置</h2>
            <p className="ss-desc">当前账号没有部署配置查看能力。部署概览不会打开。</p>
          </section>
        ) : (
          <section className="settings-section" aria-labelledby="access-forbidden-title">
            <h2 className="ss-title" id="access-forbidden-title">无权管理用户与权限</h2>
            <p className="ss-desc">当前账号没有用户与权限管理能力。管理入口不会打开目录。</p>
          </section>
        )
      ) : (
      <TabPanel idBase="settings-sections" value={activeTab}>
        {activeTab === 'access' ? (
          <AccessManagementSection />
        ) : activeTab === 'deployment' ? (
          <DeploymentSettingsSection />
        ) : activeTab === 'subscriptions' ? (
          <SubscriptionSettingsSection />
        ) : systemSettings && systemForm ? (
          <form className="settings-system-form" onSubmit={handleSubmit}>
          {activeTab === 'appearance' && (
            <div className="settings-section">
              <ThemeSettingsSection />
            </div>
          )}

          {activeTab === 'notification' && (
            <div className="settings-section">
              <div className="ss-title">通知通道</div>
              <div className="ss-desc">Telegram / 飞书异常推送。{persistedSettingsNote(systemSettings.settings_source)}</div>
              {activeChannels.has('telegram') && (
                <TelegramSettingsSection
                  settings={systemSettings.telegram}
                  form={systemForm}
                  isExpanded={expandedChannels.has('telegram')}
                  onToggleExpand={() => {
                    const expanding = !expandedChannels.has('telegram')
                    setExpandedChannels((current) => {
                      const next = new Set(current)
                      if (expanding) next.add('telegram')
                      else next.delete('telegram')
                      return next
                    })
                    if (expanding && systemForm.telegramMode === 'inherit') {
                      patchForm((form) => ({ ...form, telegramMode: 'override', telegramBotToken: '' }))
                    }
                  }}
                  onExpand={() => setExpandedChannels((current) => new Set(current).add('telegram'))}
                  onRequestRestore={() => setRestoreOpen(true)}
                  onChange={(patch) => patchForm((form) => ({ ...form, ...patch }))}
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
              <div className="settings-section">
                <p className="ss-desc">{persistedSettingsNote(systemSettings.settings_source)}</p>
                <IncidentDefaultsSection
                  value={systemForm.incidentDefaults}
                  sweepMode={systemForm.incidentSweepMode}
                  sweepSource={systemSettings.incident_sweep.source}
                  deploymentSeconds={deploymentSweepSeconds(systemSettings)}
                  onChange={(next) => patchForm((form) => ({ ...form, incidentDefaults: next }))}
                  onSweepModeChange={setSweepMode}
                />
              </div>
              <div className="settings-section">
                <FrequencyDefaultsSection
                  hostSampleFrequencyTier={systemForm.hostSampleFrequencyTier}
                  probeFrequencyDefaults={systemForm.probeFrequencyDefaults}
                  onHostSampleFrequencyChange={(v) => patchForm((f) => ({ ...f, hostSampleFrequencyTier: v }))}
                  onProbeFrequencyDefaultsChange={(patch) => patchForm((f) => ({ ...f, probeFrequencyDefaults: { ...f.probeFrequencyDefaults, ...patch } }))}
                />
              </div>
              <div className="settings-section">
                <IPQualitySettingsSection
                  value={systemForm.ipQuality}
                  sources={systemSettings.ip_quality_settings.sources}
                  onChange={(patch) => patchForm((f) => ({ ...f, ipQuality: { ...f.ipQuality, ...patch } }))}
                />
              </div>
              <div className="settings-section">
                <RetentionPolicySection
                  value={systemForm.retentionPolicy}
                  onChange={(patch) => patchForm((f) => ({ ...f, retentionPolicy: { ...f.retentionPolicy, ...patch } }))}
                />
              </div>
            </>
          )}

          {activeTab === 'advanced' && (
            <div className="settings-section">
              <p className="ss-desc">{persistedSettingsNote(systemSettings.settings_source)}</p>
              <OverrideRulesSection
                monitoringInstanceLabelOverrides={systemForm.monitoringInstanceLabelOverrides}
                targetTypeOverrides={systemForm.targetTypeOverrides}
                targetLabelOverrides={systemForm.targetLabelOverrides}
                globalHostFrequency={systemForm.hostSampleFrequencyTier}
                globalProbeDefaults={systemForm.probeFrequencyDefaults}
                globalIncident={readGlobalIncident(systemForm, deploymentSweepSeconds(systemSettings))}
                onChange={(patch) => patchForm((f) => ({ ...f, ...patch }))}
              />
            </div>
          )}

          {/* 外观是即时生效的本地偏好；只有其他分区有未保存修改（或刚保存完的结果）时才在这里显示保存栏。 */}
          {activeTab !== 'appearance' || isDirty || state.saveSuccess || state.saveError ? (
          <div className="settings-save-footer">
            {isDirty && <span className="settings-save-footer__hint">有未保存的修改</span>}
            <div>
              {state.saveError && <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{state.saveError}</p>}
              {state.saveSuccess && <p className="settings-save-footer__message settings-save-footer__message--success">{state.saveSuccess}</p>}
            </div>
            <button type="submit" className="btn md primary" disabled={state.saving}>
              {state.saving ? '保存中…' : '保存设置'}
            </button>
          </div>
          ) : null}
          </form>
        ) : null}
      </TabPanel>
      )}

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
              onRequestRestore={() => setRestoreOpen(true)}
              onChange={(patch) => patchDraft((form) => ({ ...form, ...patch }))}
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

      <Modal
        open={restoreOpen}
        onClose={() => setRestoreOpen(false)}
        title="恢复部署配置"
        dialogRole="alertdialog"
        footer={
          <>
            <button type="button" className="btn md ghost" onClick={() => setRestoreOpen(false)}>取消</button>
            <button type="button" className="btn md primary" onClick={confirmTelegramRestore}>确认恢复</button>
          </>
        }
      >
        <p>保存后清除页面覆盖，重新使用部署配置。</p>
      </Modal>

      <Modal
        open={unsavedOpen}
        onClose={() => setUnsavedOpen(false)}
        title="未保存的修改"
        footer={
          <>
            <button type="button" className="btn md ghost" onClick={discardAndLeave}>不保存</button>
            <button type="button" className="btn md ghost" onClick={() => setUnsavedOpen(false)}>取消</button>
            <button type="button" className="btn md primary" onClick={confirmSaveAndLeave} disabled={state.saving}>
              {state.saving ? '保存中…' : '保存并切换'}
            </button>
          </>
        }
      >
        <p>当前标签页有未保存的修改，切换后将丢失。是否先保存当前修改？</p>
      </Modal>
    </div>
  )
}
