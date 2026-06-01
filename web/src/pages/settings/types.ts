import type { ProbeFrequencyDefaults } from '../../lib/types'

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
  eventLayerDays: string
  notificationLayerDays: string
}

export type SettingsFormState = {
  telegramBotToken: string
  telegramChatId: string
  telegramRuntimeManaged: boolean
  feishuEnabled: boolean
  feishuWebhookUrl: string
  hostSampleFrequencyTier: string
  probeFrequencyDefaults: ProbeFrequencyDefaults
  incidentDefaults: SettingsIncidentDefaultsForm
  monitoringInstanceLabelOverridesText: string
  targetTypeOverridesText: string
  targetLabelOverridesText: string
  retentionPolicy: SettingsRetentionPolicyForm
}
