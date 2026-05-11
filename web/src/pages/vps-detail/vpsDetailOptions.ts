import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type AssetDomainStatus,
  type AssetServiceStatus,
  type AssetServiceType,
  type VPSExperienceCategory,
  type VPSExperienceSeverity,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../../lib/types'

export const RENEWAL_DECISION_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS) as Array<[
  VPSRenewalDecision,
  string,
]>

export const LIFECYCLE_OPTIONS = Object.entries(VPS_LIFECYCLE_STATUS_LABELS) as Array<[
  VPSLifecycleStatus,
  string,
]>

export const USAGE_OPTIONS = Object.entries(VPS_USAGE_STATUS_LABELS) as Array<[
  VPSUsageStatus,
  string,
]>

export const EXPERIENCE_CATEGORY_OPTIONS = Object.entries(VPS_EXPERIENCE_CATEGORY_LABELS) as Array<[
  VPSExperienceCategory,
  string,
]>

export const EXPERIENCE_SEVERITY_OPTIONS = Object.entries(VPS_EXPERIENCE_SEVERITY_LABELS) as Array<[
  VPSExperienceSeverity,
  string,
]>

export const SERVICE_TYPE_OPTIONS = Object.entries(ASSET_SERVICE_TYPE_LABELS) as Array<[
  AssetServiceType,
  string,
]>

export const SERVICE_STATUS_OPTIONS = Object.entries(ASSET_SERVICE_STATUS_LABELS) as Array<[
  AssetServiceStatus,
  string,
]>

export const DOMAIN_STATUS_OPTIONS = Object.entries(ASSET_DOMAIN_STATUS_LABELS) as Array<[
  AssetDomainStatus,
  string,
]>
