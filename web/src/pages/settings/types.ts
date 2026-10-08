import type {
  MonitoringInstanceLabelOverrideRule,
  ProbeFrequencyDefaults,
  TargetLabelOverrideRule,
  TargetTypeOverrideRule,
} from '../../lib/types'

export type SettingsIncidentDefaultsForm = {
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

export type SettingsRetentionPolicyForm = {
  rawLayerDays: string
  aggregateLayerDays: string
}

export type SettingsIPQualityForm = {
  enabled: boolean
  frequencySeconds: string
  staleAfterSeconds: string
  timeoutSeconds: string
  servicesText: string
}

export type OverrideScope = 'monitoring_instance_labels' | 'target_types' | 'target_labels'

export type StoredOverrideRule =
  | MonitoringInstanceLabelOverrideRule
  | TargetTypeOverrideRule
  | TargetLabelOverrideRule

export type FrequencyLeafDraft = {
  mode: 'inherit' | 'override'
  value: string
}

export type NumberLeafDraft = {
  mode: 'inherit' | 'override'
  text: string
}

export type TriBoolean = 'inherit' | 'on' | 'off'

export type IncidentIntegerKey =
  | 'heartbeat_interval_seconds'
  | 'stale_threshold_intervals'
  | 'sweep_interval_seconds'
  | 'cpu_warning_pct'
  | 'cpu_alert_pct'
  | 'cpu_critical_pct'
  | 'mem_warning_pct'
  | 'mem_alert_pct'
  | 'mem_critical_pct'
  | 'disk_warning_pct'
  | 'disk_alert_pct'
  | 'disk_critical_pct'
  | 'inode_warning_pct'
  | 'inode_alert_pct'
  | 'inode_critical_pct'
  | 'iowait_warning_pct'
  | 'iowait_critical_pct'
  | 'load5_warning'
  | 'load5_critical'

export type NotifyLeafKey = 'notify_on_started' | 'notify_on_escalated' | 'notify_on_recovered'

export type OverrideLeavesDraft = {
  hostSampleFrequencyTier: FrequencyLeafDraft
  probe: {
    tcp: FrequencyLeafDraft
    http: FrequencyLeafDraft
    tls: FrequencyLeafDraft
  }
  incidentNumbers: Record<IncidentIntegerKey, NumberLeafDraft>
  notifyOnStarted: TriBoolean
  notifyOnEscalated: TriBoolean
  notifyOnRecovered: TriBoolean
}

/** One settings-owned rule. `original` and `touched` stay off the wire. */
export type OverrideRuleDraft = {
  key: string
  scope: OverrideScope
  selector: string
  original: StoredOverrideRule | null
  touched: string[]
  leaves: OverrideLeavesDraft
}

export type SettingsFormState = {
  telegramBotToken: string
  telegramChatId: string
  telegramRuntimeManaged: boolean
  feishuEnabled: boolean
  feishuWebhookPresent: boolean
  feishuWebhookUrl: string
  hostSampleFrequencyTier: string
  probeFrequencyDefaults: ProbeFrequencyDefaults
  incidentDefaults: SettingsIncidentDefaultsForm
  monitoringInstanceLabelOverrides: OverrideRuleDraft[]
  targetTypeOverrides: OverrideRuleDraft[]
  targetLabelOverrides: OverrideRuleDraft[]
  retentionPolicy: SettingsRetentionPolicyForm
  ipQuality: SettingsIPQualityForm
}
