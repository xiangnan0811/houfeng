import type { BadgeTone } from '../../components/atoms'
import type { IncidentSeverity } from '../../lib/types'

export type FleetStateTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance'

export type FleetState = {
  title: string
  description: string
  tone: FleetStateTone
  primaryCta: {
    label: string
    to: string
  }
  secondaryCtas: Array<{
    label: string
    to: string
  }>
}

export type AttentionItem = {
  kind: 'node' | 'target'
  id: string
  name: string
  route: string
  health: IncidentSeverity
  incidentCount: number
  issueSummary: string
  location: string
  technicalId: string
  freshnessLabel: string
  freshnessAt?: string | null
  meta: string
}

export type DashboardMetric = {
  label: string
  value: number | string
  detail: string
  to: string
  tone?: BadgeTone
}

export type ManagementEntry = {
  title: string
  stat: string
  to: string
}

export type ContextItem = {
  label: string
  title: string
  detail: string
  to: string
  tone?: BadgeTone
  timestampAt?: string | null
}
