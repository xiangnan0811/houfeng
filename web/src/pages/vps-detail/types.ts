import type {
  AssetDomainRecord,
  AssetDomainStatus,
  AssetServiceRecord,
  AssetServiceStatus,
  AssetServiceType,
  CancellationPreview,
  LifecycleActionResult,
  MonitoringInstanceRecord,
  ProviderRecord,
  SubscriptionRecord,
  TargetRecord,
  VPSAssetDetail,
  VPSExperienceCategory,
  VPSExperienceSeverity,
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
  cancellationPreview: CancellationPreview | null
  cancellationPreviewError: string | null
  cancellationResult: LifecycleActionResult | null
}

export type VPSDetailSelectorState = {
  monitoringInstancesLoading: boolean
  monitoringInstancesError: string | null
  monitoring: MonitoringInstanceRecord[]
  providersLoading: boolean
  providersError: string | null
  providers: ProviderRecord[]
  targetsLoading: boolean
  targetsError: string | null
  targets: TargetRecord[]
}

export type VPSDetailDrawerMode =
  | 'decision'
  | 'cancellation'
  | 'facts'
  | 'subscription'
  | 'monitoring-instance-create'
  | 'monitoring-instance-link'
  | 'experience'
  | 'service'
  | 'domain'
  | 'monitoring-instance-evidence'
  | 'services-detail'
  | 'domains-detail'
  | 'timeline-detail'
  | 'facts-detail'
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
  monitoringInstanceId: string
  note: string
}

export type SubscriptionDraftState = {
  price: string
  currency: string
  billingCycle: string
  billingMonths: string
  startedAt: string
  renewAt: string
  autoRenew: boolean
  autoRenewCancelled: boolean
  paymentMethod: string
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
