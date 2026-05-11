import type {
  AssetDomainRecord,
  AssetDomainStatus,
  AssetServiceRecord,
  AssetServiceStatus,
  AssetServiceType,
  SubscriptionRecord,
  VPSAssetDetail,
  VPSExperienceCategory,
  VPSExperienceSeverity,
  VPSLifecycleStatus,
  VPSRenewalDecision,
  VPSTimeline,
  VPSUsageStatus,
} from '../../lib/types'

export type VPSDetailPageState = {
  vpsId: string | null
  error: string | null
  detail: VPSAssetDetail | null
  timeline: VPSTimeline | null
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  subscriptions: SubscriptionRecord[]
  subscriptionsError: string | null
}

export type VPSDetailDrawerMode =
  | 'decision'
  | 'facts'
  | 'node-link'
  | 'experience'
  | 'service'
  | 'domain'
  | null

export type FactEditFormState = {
  displayName: string
  providerID: string
  providerName: string
  productName: string
  orderRef: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  sshHost: string
  sshPort: string
  sshUser: string
  osName: string
  virtualization: string
  lifecycleStatus: VPSLifecycleStatus
  usageStatus: VPSUsageStatus
  importance: string
  labels: string
  note: string
}

export type DecisionDraftState = {
  renewalDecision: VPSRenewalDecision
  reason: string
}

export type LinkDraftState = {
  nodeId: string
  note: string
}

export type ExperienceDraftState = {
  category: VPSExperienceCategory
  severity: VPSExperienceSeverity
  summary: string
  details: string
  occurredAt: string
}

export type ServiceDraftState = {
  name: string
  serviceType: AssetServiceType
  status: AssetServiceStatus
  targetID: string
  url: string
  port: string
  labels: string
  note: string
}

export type DomainDraftState = {
  domainName: string
  status: AssetDomainStatus
  purpose: string
  serviceID: string
  targetID: string
  registrar: string
  expiresAt: string
  autoRenew: boolean
  httpsEnabled: boolean
  labels: string
  note: string
}
