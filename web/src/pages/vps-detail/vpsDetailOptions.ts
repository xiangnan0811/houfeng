import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  type AssetDomainStatus,
  type AssetServiceStatus,
  type AssetServiceType,
  type VPSExperienceCategory,
  type VPSExperienceSeverity,
  type VPSRenewalDecision,
} from '../../lib/types'

export const RENEWAL_DECISION_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS) as Array<[
  VPSRenewalDecision,
  string,
]>

export const USAGE_SUGGESTIONS = ['生产', '测试', '备用', '闲置', '迁移中']

export const AUTO_RENEW_CHECK_OPTIONS = [
  ['unchecked', '尚未核对'],
  ['enabled', '已开启'],
  ['disabled', '已关闭'],
  ['never_enabled', '从未开启'],
  ['unsupported', '服务商不支持'],
] as const

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
