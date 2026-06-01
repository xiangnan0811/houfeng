import type { CreateTargetInput, TargetRecord } from '../../lib/types'

export type TargetFilterState = {
  group: string | null
  type: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
  executionLabels: string[]
  abnormal: boolean
}

export type CreateTargetFormState = {
  name: string
  targetType: CreateTargetInput['target_type']
  host: string
  basePort: string
  executionMonitoringInstanceLabels: string
  runStatus: CreateTargetInput['run_status']
  group: string
  labels: string
  note: string
}

export type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

export type PendingTargetConfirmation = {
  targetId: string
  action: 'pause' | 'archive'
}

export type FocusRestoreRequest = {
  targetId: string
  preferredAction: TargetRuntimeAction
}

export type TargetFilterOption = {
  value: string
  label: string
}

export type TargetEvidenceActionKind =
  | 'abnormal'
  | 'paused'
  | 'archived'
  | 'coverage'
  | 'clear'
  | 'create'
  | 'asset'

export type TargetEvidenceLead = {
  eyebrow: string
  title: string
  description: string
  actionKind: TargetEvidenceActionKind
  actionLabel: string
  tone: 'normal' | 'notice' | 'alert' | 'maintenance' | 'offline'
}

export type TargetEvidenceItem = {
  target: TargetRecord
  title: string
  reason: string
  meta: string
  route: string
  actionLabel: string
}
