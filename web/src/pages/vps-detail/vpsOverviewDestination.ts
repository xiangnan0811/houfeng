export type VPSOverviewCommand =
  | 'open_subscription'
  | 'open_renewal_decision'
  | 'open_management'
  | 'retry_overview'
  | 'open_monitoring_instances'
  | 'open_services'
  | 'open_domains'

export type VPSOverviewDestination =
  | { kind: 'route'; to: string }
  | { kind: 'command'; command: VPSOverviewCommand }

type ActionToken = {
  id: string
  route?: string
}

type RelationToken = {
  kind: string
  route?: string
}

type ExpectedDestination =
  | { kind: 'route'; to: (vpsId: string) => string }
  | { kind: 'scoped-route'; accept: (route: string) => boolean }
  | { kind: 'command'; command: VPSOverviewCommand }

const MONITORING_INSTANCE_ID = /^mi_[A-Za-z0-9][A-Za-z0-9_-]*$/
const MONITORING_INSTANCE_EVENTS_ROUTE = /^\/events\?object_type=monitoring_instance&object_id=(mi_[A-Za-z0-9][A-Za-z0-9_-]*)$/

function isAppRelativeRoute(route: string): boolean {
  return route.startsWith('/') && !route.startsWith('//')
}

function isMonitoringInstanceDetailRoute(route: string): boolean {
  if (!isAppRelativeRoute(route) || !route.startsWith('/monitoring/')) return false
  const rest = route.slice('/monitoring/'.length)
  return rest.length > 0 && !rest.includes('/') && !rest.includes('?') && !rest.includes('#') && MONITORING_INSTANCE_ID.test(rest)
}

function isMonitoringInstanceEventsRoute(route: string): boolean {
  if (!isAppRelativeRoute(route) || route.includes('#') || route.includes('\t')) return false
  if (route.includes('&&') || route.endsWith('&') || route.includes('%')) return false
  const match = route.match(MONITORING_INSTANCE_EVENTS_ROUTE)
  return match !== null && MONITORING_INSTANCE_ID.test(match[1] ?? '')
}

const anomalyDestinations: Readonly<Record<string, ExpectedDestination>> = {
  'monitoring.health.abnormal.v1\u0000open_monitoring': {
    kind: 'scoped-route',
    accept: (route) => route === '/monitoring?abnormal=1' || isMonitoringInstanceDetailRoute(route),
  },
  'monitoring.incidents.open.v1\u0000open_incidents': {
    kind: 'scoped-route',
    accept: (route) => {
      if (route === '/events?object_type=monitoring_instance') return true
      return isMonitoringInstanceEventsRoute(route)
    },
  },
  'ip_quality.risk.elevated.v1\u0000open_ip_quality': {
    kind: 'route',
    to: (vpsId) => `/vps/${encodeURIComponent(vpsId)}/ip-quality`,
  },
  'ip_quality.stale.v1\u0000open_ip_quality': {
    kind: 'route',
    to: (vpsId) => `/vps/${encodeURIComponent(vpsId)}/ip-quality`,
  },
  'ip_quality.partial.v1\u0000open_ip_quality': {
    kind: 'route',
    to: (vpsId) => `/vps/${encodeURIComponent(vpsId)}/ip-quality`,
  },
  'ip_quality.missing.v1\u0000open_ip_quality': {
    kind: 'route',
    to: (vpsId) => `/vps/${encodeURIComponent(vpsId)}/ip-quality`,
  },
  'monitoring.unlinked.v1\u0000open_monitoring_instances': {
    kind: 'command',
    command: 'open_monitoring_instances',
  },
  'renewal.subscription.missing.v1\u0000open_subscription': {
    kind: 'command',
    command: 'open_subscription',
  },
  'renewal.due.soon.v1\u0000open_renewal_decision': {
    kind: 'command',
    command: 'open_renewal_decision',
  },
  'renewal.overdue.v1\u0000open_renewal_decision': {
    kind: 'command',
    command: 'open_renewal_decision',
  },
  'lifecycle.blocker.v1\u0000open_management': {
    kind: 'command',
    command: 'open_management',
  },
  'source.unavailable.v1\u0000retry_overview': {
    kind: 'command',
    command: 'retry_overview',
  },
}

const relationDestinations: Readonly<Record<string, ExpectedDestination>> = {
  subscriptions: {
    kind: 'route',
    to: (vpsId) => `/subscriptions?${new URLSearchParams({ vps_id: vpsId }).toString()}`,
  },
  monitoring_instances: {
    kind: 'command',
    command: 'open_monitoring_instances',
  },
  services: {
    kind: 'command',
    command: 'open_services',
  },
  domains: {
    kind: 'command',
    command: 'open_domains',
  },
}

function ownExpectedDestination(
  table: Readonly<Record<string, ExpectedDestination>>,
  key: string,
): ExpectedDestination | undefined {
  return Object.hasOwn(table, key) ? table[key] : undefined
}

function resolveExpectedDestination(
  vpsId: string,
  route: string | undefined,
  expected: ExpectedDestination | undefined,
): VPSOverviewDestination | null {
  if (vpsId.length === 0 || !expected) return null
  if (expected.kind === 'command') {
    return route === undefined ? { kind: 'command', command: expected.command } : null
  }
  if (expected.kind === 'scoped-route') {
    return route && expected.accept(route) ? { kind: 'route', to: route } : null
  }

  const to = expected.to(vpsId)
  return route === to ? { kind: 'route', to } : null
}

export function resolveVPSOverviewAnomalyDestination(
  vpsId: string,
  ruleId: string,
  action: ActionToken,
): VPSOverviewDestination | null {
  const expected = ownExpectedDestination(anomalyDestinations, `${ruleId}\u0000${action.id}`)
  return resolveExpectedDestination(vpsId, action.route, expected)
}

export function resolveVPSOverviewRelationDestination(
  vpsId: string,
  relation: RelationToken,
): VPSOverviewDestination | null {
  return resolveExpectedDestination(vpsId, relation.route, ownExpectedDestination(relationDestinations, relation.kind))
}
