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
  | { kind: 'command'; command: VPSOverviewCommand }

const anomalyDestinations: Readonly<Record<string, ExpectedDestination>> = {
  'monitoring.health.abnormal.v1\u0000open_monitoring': {
    kind: 'route',
    to: () => '/monitoring?abnormal=1',
  },
  'monitoring.incidents.open.v1\u0000open_incidents': {
    kind: 'route',
    to: () => '/events?object_type=monitoring_instance',
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
  'renewal.subscription.missing.v1\u0000open_subscription': {
    kind: 'command',
    command: 'open_subscription',
  },
  'renewal.due.soon.v1\u0000open_renewal_decision': {
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

function resolveExpectedDestination(
  vpsId: string,
  route: string | undefined,
  expected: ExpectedDestination | undefined,
): VPSOverviewDestination | null {
  if (vpsId.length === 0 || !expected) return null
  if (expected.kind === 'command') {
    return route === undefined ? { kind: 'command', command: expected.command } : null
  }

  const to = expected.to(vpsId)
  return route === to ? { kind: 'route', to } : null
}

export function resolveVPSOverviewAnomalyDestination(
  vpsId: string,
  ruleId: string,
  action: ActionToken,
): VPSOverviewDestination | null {
  const expected = anomalyDestinations[`${ruleId}\u0000${action.id}`]
  return resolveExpectedDestination(vpsId, action.route, expected)
}

export function resolveVPSOverviewRelationDestination(
  vpsId: string,
  relation: RelationToken,
): VPSOverviewDestination | null {
  return resolveExpectedDestination(vpsId, relation.route, relationDestinations[relation.kind])
}
