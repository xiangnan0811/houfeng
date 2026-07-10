export const DASHBOARD_LINKS = {
  eventsSevere: '/events?severity=严重',
  events24h: '/events?time_range=24h',
  eventsMaintenance: '/events?maintenance_only=1',
  monitoringAbnormal: '/monitoring?abnormal=1',
  targetsAbnormal: '/targets?abnormal=1',
  assetDecisionsNeedsDecision: '/asset-decisions?view=needs_decision&renew_within_days=30',
  assetDecisionsMigrationRetirement: '/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement',
  assetDecisionsRenewal: '/asset-decisions?view=renewal&renew_within_days=30',
  assetDecisionsEvidence: '/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup',
  vps: '/vps',
  subscriptions: '/subscriptions',
} as const
