import type { NodeRecord } from '../../lib/types'

export type NodeFilterState = {
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

export type NodeRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume' | 'retire' | 'restore-to-observing'

export type NodeListView = 'all' | 'runtime-attention' | 'binding-conflict'

export type NodeQuickView = 'all' | 'abnormal' | 'onboarding' | 'runtime-attention' | 'binding-conflict'

export type PendingNodeConfirmation = {
  nodeId: string
  action: 'pause'
}

export type FocusRestoreRequest = {
  nodeId: string
  preferredAction: NodeRuntimeAction
}

export type NodeFilterOption = {
  value: string
  label: string
}

export type NodeEvidenceActionKind = 'abnormal' | 'onboarding' | 'runtime' | 'clear' | 'create' | 'asset'

export type NodeEvidenceLead = {
  eyebrow: string
  title: string
  description: string
  actionKind: NodeEvidenceActionKind
  actionLabel: string
  tone: 'normal' | 'notice' | 'alert' | 'maintenance' | 'offline'
}

export type NodeEvidenceItem = {
  node: NodeRecord
  title: string
  reason: string
  meta: string
  route: string
  actionLabel: string
}
