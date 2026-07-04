import type { CSSProperties, ReactNode } from 'react'
import type {
  AssetDecisionComparisonInsight,
  AssetDecisionEvidenceAssessment,
  AssetDecisionEvidenceChip,
  AssetDecisionGroupDetail,
  AssetDecisionGroupSummary,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupSummary,
  AssetDecisionMemberComparisonInsight,
  AssetDecisionOverview,
  AssetDecisionRecommendation,
  AssetDecisionRecordDetail,
  AssetDecisionRecordStatus,
  AssetDecisionRecordSummary,
  AssetDecisionScenarioTemplateDetail,
  AssetDecisionScenarioTemplateSummary,
  AssetDecisionFollowupStatus,
  AssetDecisionSourceType,
  AssetDecisionSuggestedAction,
  AssetDecisionSuggestedRole,
  AssetDecisionView,
  SubscriptionRecord,
  VPSAssetRecord,
  VPSRenewalDecision,
} from '../../lib/types'

// 本地类型定义
export type BadgeTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline' | 'neutral'

export type AssetQualityIssue = {
  key: string
  label: string
  tone: BadgeTone
}

export type AssetDecisionDraft = {
  renewalDecision: VPSRenewalDecision
  reason: string
}

// 基础类型
export type RenewalWindow = 30 | 60 | 90
export type WorkbenchView = AssetDecisionView | 'single_queue'
export type MainWorkbenchView = AssetDecisionView
export type DecisionQueueView =
  | 'all'
  | 'unreviewed'
  | 'renewal'
  | 'migrate'
  | 'cancel'
  | 'cancellation_attention'
  | 'unlinked'
  | 'missing_subscription'

export type DecisionQueueItem = {
  vps: VPSAssetRecord
  subscription: SubscriptionRecord | null
  qualityIssues: AssetQualityIssue[]
  renewalDue: boolean
  priority: number
}

// 状态类型
export type PortfolioState = {
  overviewLoading: boolean
  overviewError: string | null
  overview: AssetDecisionOverview | null
  groupsLoading: boolean
  groupsError: string | null
  groups: AssetDecisionGroupSummary[]
}

export type DetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionGroupDetail | null
}

export type ManualGroupsState = {
  loading: boolean
  error: string | null
  groups: AssetDecisionManualGroupSummary[]
}

export type ManualDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionManualGroupDetail | null
}

export type ScenarioTemplatesState = {
  loading: boolean
  error: string | null
  templates: AssetDecisionScenarioTemplateSummary[]
}

export type TemplateDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionScenarioTemplateDetail | null
}

export type VPSCatalogState = {
  loading: boolean
  error: string | null
  rows: VPSAssetRecord[]
}

export type RecordsState = {
  loading: boolean
  error: string | null
  records: AssetDecisionRecordSummary[]
}

export type RecordDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionRecordDetail | null
}

// 草稿类型
export type RecordMemberDraft = {
  decidedRole: AssetDecisionSuggestedRole
  decidedAction: AssetDecisionSuggestedAction
  reason: string
}

export type RecordFollowupDraft = {
  status: AssetDecisionFollowupStatus
  note: string
}

export type RecordDraft = {
  sourceType: AssetDecisionSourceType
  sourceGroupID: string
  renewWithinDays: number
  title: string
  goal: string
  status: AssetDecisionRecordStatus
  memberOrder: string[]
  members: Record<string, RecordMemberDraft>
}

export type ManualMemberDraft = {
  intendedRole: AssetDecisionSuggestedRole
  intendedAction: AssetDecisionSuggestedAction
  reason: string
  note: string
  sortOrder: string
}

export type ManualMemberAddDraft = ManualMemberDraft & {
  vpsID: string
}

export type TemplateManualGroupDraft = {
  title: string
  goal: string
  note: string
  renewWithinDays: RenewalWindow
}

// 面板类型
export type GroupDetailPanel = 'overview' | 'members' | 'save' | 'raw' | 'vps'
export type ManualDetailPanel = 'overview' | 'edit' | 'members' | 'add' | 'save' | 'raw'
export type RecordDetailPanel = 'overview' | 'execution' | 'members' | 'source' | 'raw'
export type TemplateDetailPanel = 'overview' | 'create' | 'members' | 'status'

// 样式类型
export type ScoreStyle = CSSProperties & {
  '--score': number
}

export type CostSummaryLike = {
  monthly_cost_by_currency: AssetDecisionGroupSummary['monthly_cost_by_currency']
  monthly_cost_base?: number | null
  yearly_cost_base?: number | null
  base_currency?: string
}

// 队列状态
export type QueueState = {
  renewalsLoading: boolean
  renewalsError: string | null
  queueLoading: boolean
  queueError: string | null
  renewals: SubscriptionRecord[]
  subscriptions: SubscriptionRecord[]
  unreviewed: VPSAssetRecord[]
  migrate: VPSAssetRecord[]
  cancel: VPSAssetRecord[]
}

// 闭环指标
export type ClosedLoopSourceErrors = {
  overview?: string | null
  groups?: string | null
  records?: string | null
  manualGroups?: string | null
  templates?: string | null
}

export type ClosedLoopMetrics = {
  autoGroupCount: number
  manualActiveCount: number
  recordActiveCount: number
  readbackDriftCount: number
  readbackBlockedCount: number
  readbackNeedsEvidenceCount: number
  readbackOpenCount: number
  costPressureGroupCount: number
  evidenceGapGroupCount: number
  partialErrorCount: number
}

// 下一步工作项
export type AssetDecisionNextWorkKind =
  | 'record_drift'
  | 'record_blocked'
  | 'record_needs_evidence'
  | 'auto_group'
  | 'manual_group'
  | 'scenario_template'

export type AssetDecisionNextWorkTarget =
  | { type: 'record'; id: string }
  | { type: 'group'; id: string }
  | { type: 'manual_group'; id: string }
  | { type: 'template'; id: string }

export type AssetDecisionNextWorkItem = {
  id: string
  kind: AssetDecisionNextWorkKind
  tone: BadgeTone
  sourceLabel: string
  kindLabel: string
  title: string
  summary: string
  meta: string
  actionLabel: string
  priority: number
  target: AssetDecisionNextWorkTarget
}

// 投资组合主导
export type AssetDecisionPortfolioLead = {
  kind: 'work' | 'stable'
  tone: BadgeTone
  eyebrow: string
  title: string
  summary: string
  actionLabel?: string
  contextLabel: string
  riskLabel: string
  evidenceLabel: string
  renewalLabel: string
  primaryItem?: AssetDecisionNextWorkItem
  primaryGroupID?: string
}

// 手动组合进度
export type ManualGroupProgressItem = {
  key: string
  label: string
  summary: string
  tone: BadgeTone
  done: boolean
}

export type ManualGroupProgress = {
  readinessLabel: string
  readinessTone: BadgeTone
  readyToRecord: boolean
  doneCount: number
  totalCount: number
  items: ManualGroupProgressItem[]
}

// 对比矩阵成员
export type ComparisonMatrixMember = {
  key: string
  displayName: string
  href?: string
  meta: string
  product: string
  facts: string
  statusFacts: string
  sourceLabel: string
  role: AssetDecisionSuggestedRole
  action: AssetDecisionSuggestedAction
  intendedRole?: AssetDecisionSuggestedRole
  intendedAction?: AssetDecisionSuggestedAction
  comparison?: AssetDecisionMemberComparisonInsight | null
  evidenceChips?: AssetDecisionEvidenceChip[]
  currentFactFound?: boolean
}

// 详情面板选项
export type DetailCommandOptions = {
  ariaLabel: string
  title: string
  summary: string
  assessment?: AssetDecisionEvidenceAssessment | null
  recommendation?: AssetDecisionRecommendation | null
  insight?: AssetDecisionComparisonInsight | null
  chips?: AssetDecisionEvidenceChip[]
  badge?: ReactNode
  actions?: ReactNode
  footer?: ReactNode
}

export type MemberDecisionCardsOptions = {
  title: string
  ariaLabel: string
  summary?: string
  showIntent?: boolean
  action?: (member: ComparisonMatrixMember) => ReactNode
}

// 上下文过滤器
export type ContextFilterKey = 'provider_id' | 'vps_id' | 'country' | 'region' | 'city' | 'scenario'
export type OpenStateKey = 'group_id' | 'manual_group_id' | 'record_id' | 'template_id'

export type ContextFilterChip = {
  key: ContextFilterKey
  label: string
  value: string
}

// 次级工作台导航
export type SecondaryWorkbench = 'vps_catalog' | 'records' | 'manual_groups' | 'templates' | 'scenarios' | 'renewals' | 'single_queue'

export type AssetDecisionSecondaryNavItem = {
  value: SecondaryWorkbench
  eyebrow: string
  title: string
  summary: string
  meta: string
  tone: BadgeTone
  actionLabel: string
}
