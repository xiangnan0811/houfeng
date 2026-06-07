import type {
  ActiveIncidentRecord,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeFacts,
  StateChangeEventRecord,
  VPSSummary,
} from '../../lib/types'

export type MonitoringDetailPageState = {
  requestedMonitoringInstanceId: string | null
  error: string | null
  monitoringInstance: MonitoringInstanceRecord | null
  runtimeFacts: MonitoringInstanceRuntimeFacts | null
  requestedActivityMonitoringInstanceId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

export type BindingConflictState = {
  requestedMonitoringInstanceId: string | null
  onboarding: MonitoringInstanceOnboardingState | null
  loading: boolean
  error: string | null
}

export type BindingConflictAction = 'confirm' | 'reject' | 'reset'

export type PendingBindingConfirmation = {
  action: BindingConflictAction
}

export type PendingRuntimeConfirmation = {
  action: 'pause'
}

export type LinkedVPSState = {
  requestedMonitoringInstanceId: string | null
  records: VPSSummary[]
  loading: boolean
  loaded: boolean
  error: string | null
}

export type TimeWindow = 'realtime' | '24h' | '7d' | '30d'
export type RuntimeStreamStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'disconnected'
export type HistoryTab = 'events' | 'incidents'

export type MonitoringInstanceCommand = {
  id: string
  name: string
  description: string
}
