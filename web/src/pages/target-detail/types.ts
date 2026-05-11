import type {
  ActiveIncidentRecord,
  ProbeItemRecord,
  StateChangeEventRecord,
  TargetRecord,
  TargetRuntimeFacts,
} from '../../lib/types'

export type TargetDetailPageState = {
  requestedTargetId: string | null
  error: string | null
  target: TargetRecord | null
  probeItems: ProbeItemRecord[]
  runtimeFacts: TargetRuntimeFacts | null
  requestedActivityTargetId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

export type PendingRuntimeConfirmation = {
  action: 'pause' | 'archive'
}

export type ProbeFocusRestoreRequest = {
  probeItemId?: string
}

export type MetadataFormState = {
  group: string
  labels: string
  note: string
}

export type TimeWindow = '24h' | '7d' | '30d'
export type HistoryTab = 'events' | 'incidents'
