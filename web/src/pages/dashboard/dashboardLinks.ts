export const DASHBOARD_LINKS = {
  eventsSevere: '/events?severity=严重',
  events24h: '/events?time_range=24h',
  /** 最近动态不限时间窗口，入口也不加时间过滤，避免旧事件点进去是空列表。 */
  events: '/events',
  eventsMaintenance: '/events?maintenance_only=1',
  monitoringAbnormal: '/monitoring?abnormal=1',
  targetsAbnormal: '/targets?abnormal=1',
  targetsUnobserved: '/targets?view=unobserved',
  targetsStale: '/targets?view=stale',
  assetDecisionsNeedsDecision: '/asset-decisions?view=needs_decision&renew_within_days=30',
  assetDecisionsMigrationRetirement: '/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement',
  assetDecisionsRenewal: '/asset-decisions?view=renewal&renew_within_days=30',
  assetDecisionsEvidence: '/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup',
  vps: '/vps',
  subscriptions: '/subscriptions',
  notificationSettings: '/settings?tab=notification',
} as const

export function dashboardTargetsStaleLink(group?: string): string {
  const params = new URLSearchParams({ view: 'stale' })
  if (group) params.set('group', group)
  return `/targets?${params.toString()}`
}
