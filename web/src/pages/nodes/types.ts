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

export type NodeRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume'

export type NodeListView = 'all' | 'binding-conflict'

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
