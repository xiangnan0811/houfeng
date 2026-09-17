export type MonitoringInstanceFilterState = {
  group: string | null
  region: string | null
  city: string | null
  provider: string | null
  lifecycle: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
}

export type MonitoringInstanceQuickView =
  | 'all'
  | 'abnormal'
  | 'onboarding'
  | 'runtime-attention'
  | 'binding-conflict'

export type MonitoringInstanceSortKey = 'identity' | 'issue' | 'location' | 'health' | 'heartbeat'

export type MonitoringInstanceSortState = {
  key: MonitoringInstanceSortKey
  direction: 'asc' | 'desc'
}

export type MonitoringInstanceFilterOption = {
  value: string
  label: string
}

export type HeartbeatFreshnessPolicy = {
  heartbeatIntervalMs: number
  missingThreshold: number
}

export type HeartbeatFreshness =
  | { kind: 'missing' }
  | { kind: 'invalid'; raw: string }
  | { kind: 'pending'; at: string }
  | { kind: 'policy-unavailable'; at: string }
  | { kind: 'fresh'; at: string }
  | { kind: 'stale'; at: string }
