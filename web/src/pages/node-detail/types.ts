import type {
  ActiveIncidentRecord,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'

export type NodeDetailPageState = {
  requestedNodeId: string | null
  error: string | null
  node: NodeRecord | null
  runtimeFacts: NodeRuntimeFacts | null
  requestedActivityNodeId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

export type BindingConflictState = {
  requestedNodeId: string | null
  onboarding: NodeOnboardingState | null
  loading: boolean
  error: string | null
}

export type BindingConflictAction = 'confirm' | 'reject' | 'reset'
export type NodeLifecycleAction = 'retire' | 'restore-to-observing'

export type PendingRuntimeConfirmation = {
  action: 'pause'
}

export type LinkedVPSState = {
  requestedNodeId: string | null
  records: VPSSummary[]
  loading: boolean
  loaded: boolean
  error: string | null
}

export type TimeWindow = '24h' | '7d' | '30d'
export type HistoryTab = 'events' | 'incidents'

export type NodeCommand = {
  id: string
  name: string
  description: string
}
