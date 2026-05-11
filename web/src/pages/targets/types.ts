import type { CreateTargetInput } from '../../lib/types'

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
  executionNodeLabels: string
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
