export type AssetDecisionReadDomain =
  | 'portfolio'
  | 'groups'
  | 'manualGroups'
  | 'templates'
  | 'records'
  | 'renewalQueue'

export type AssetDecisionInvalidationEvent = Readonly<{
  type: 'renewal-decision-saved'
  vpsID: string
}>

export type AssetDecisionRevisions = Record<AssetDecisionReadDomain, number>

export const INITIAL_ASSET_DECISION_REVISIONS: AssetDecisionRevisions = {
  portfolio: 0,
  groups: 0,
  manualGroups: 0,
  templates: 0,
  records: 0,
  renewalQueue: 0,
}

export function applyAssetDecisionInvalidation(
  current: Readonly<AssetDecisionRevisions>,
  event: AssetDecisionInvalidationEvent,
): AssetDecisionRevisions {
  switch (event.type) {
    case 'renewal-decision-saved':
      return {
        portfolio: current.portfolio + 1,
        groups: current.groups + 1,
        manualGroups: current.manualGroups + 1,
        templates: current.templates + 1,
        records: current.records + 1,
        renewalQueue: current.renewalQueue + 1,
      }
  }
}
