import { requestJSON, withQuery } from './api'
import type {
  ActiveIncidentRecord,
  CommandAuditListFilter,
  CommandAuditListResponse,
  EventListFilter,
  EventListResponse,
  IncidentListFilter,
  StateChangeEventRecord,
} from './types'

export function listEvents(filter?: EventListFilter) {
  return requestJSON<EventListResponse | StateChangeEventRecord[]>(
    withQuery(
      '/api/events',
      filter
        ? {
            object_type: filter.object_type,
            object_id: filter.object_id,
            severity: filter.severity,
            event_type: filter.event_type,
            limit: filter.limit,
            created_from: filter.created_from,
            created_to: filter.created_to,
            label: filter.label,
            notification_only: filter.notification_only,
            recovery_only: filter.recovery_only,
            maintenance_only: filter.maintenance_only,
            include_backfilled: filter.include_backfilled,
          }
        : undefined,
    ),
  ).then((response) => Array.isArray(response) ? response : response.items)
}

export function listIncidents(filter?: IncidentListFilter) {
  return requestJSON<ActiveIncidentRecord[]>(withQuery('/api/incidents', filter))
}

export function listHistoricalIncidents(objectType: string, objectId: string) {
  return requestJSON<ActiveIncidentRecord[]>(
    `/api/incidents?object_type=${encodeURIComponent(objectType)}&object_id=${encodeURIComponent(
      objectId,
    )}&include_resolved=true`,
  )
}

export function listCommandAudits(filter?: CommandAuditListFilter) {
  const cursor = filter?.cursor?.trim()
  if (cursor) {
    return requestJSON<CommandAuditListResponse>(
      withQuery('/api/command-audits', { cursor }),
    )
  }

  return requestJSON<CommandAuditListResponse>(
    withQuery('/api/command-audits', filter
      ? {
          window: filter.window === '30d' ? undefined : filter.window,
          started_from: filter.started_from,
          started_to: filter.started_to,
          monitoring_instance: filter.monitoring_instance,
          command_id: filter.command_id,
          sensitivity: filter.sensitivity,
          outcome: filter.outcome,
          actor: filter.actor,
          action_id: filter.action_id,
          limit: filter.limit,
        }
      : undefined),
  )
}
