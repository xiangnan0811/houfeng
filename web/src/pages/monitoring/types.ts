import type { MonitoringInstanceRecord } from '../../lib/types'

export type MonitoringInstanceFilterState = {
  group: string | null
  region: string | null
  city: string | null
  provider: string | null
  lifecycle: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
  abnormal: boolean
  onboardingPending: boolean
}

export type MonitoringInstanceRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume' | 'retire' | 'restore-to-observing'

export type MonitoringInstanceListView = 'all' | 'runtime-attention' | 'binding-conflict'

export type MonitoringInstanceQuickView = 'all' | 'abnormal' | 'onboarding' | 'runtime-attention' | 'binding-conflict'

export type PendingMonitoringInstanceConfirmation = {
  monitoringInstanceId: string
  action: 'pause'
}

export type FocusRestoreRequest = {
  monitoringInstanceId: string
  preferredAction: MonitoringInstanceRuntimeAction
}

export type MonitoringInstanceFilterOption = {
  value: string
  label: string
}

export type MonitoringInstanceEvidenceActionKind = 'abnormal' | 'onboarding' | 'runtime' | 'clear' | 'create' | 'asset'

export type MonitoringInstanceEvidenceLead = {
  eyebrow: string
  title: string
  description: string
  actionKind: MonitoringInstanceEvidenceActionKind
  actionLabel: string
  tone: 'normal' | 'notice' | 'alert' | 'maintenance' | 'offline'
}

export type MonitoringInstanceEvidenceItem = {
  monitoringInstance: MonitoringInstanceRecord
  title: string
  reason: string
  meta: string
  route: string
  actionLabel: string
}
