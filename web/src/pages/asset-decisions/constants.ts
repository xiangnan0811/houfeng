import type {
  AssetDecisionComparisonLane,
  AssetDecisionComparisonPrimaryAxis,
  AssetDecisionEvidenceDecisionBias,
  AssetDecisionEvidenceKind,
  AssetDecisionEvidenceQualityTier,
  AssetDecisionExecutionPlanLane,
  AssetDecisionExecutionReadbackStatus,
  AssetDecisionFollowupStatus,
  AssetDecisionManualGroupScenario,
  AssetDecisionManualGroupStatus,
  AssetDecisionRecordStatus,
  AssetDecisionScenarioTemplateStatus,
  AssetDecisionSuggestedAction,
  AssetDecisionSuggestedRole,
  VPSRenewalDecision,
} from '../../lib/types'
import type {
  AssetDecisionDraft,
  DetailState,
  MainWorkbenchView,
  ManualDetailState,
  ManualGroupsState,
  PortfolioState,
  QueueState,
  RecordDetailState,
  RecordsState,
  RenewalWindow,
  ScenarioTemplatesState,
  TemplateDetailState,
  VPSCatalogState,
  WorkbenchView,
} from './types'

// 续费窗口
export const RENEWAL_WINDOWS: readonly RenewalWindow[] = [30, 60, 90]

// 决策队列值
export const DECISION_QUEUE_VALUES: VPSRenewalDecision[] = ['unreviewed', 'migrate', 'cancel']

// 初始状态常量
export const INITIAL_DECISION_DRAFT: AssetDecisionDraft = {
  renewalDecision: 'unreviewed',
  reason: '',
}

export const INITIAL_PORTFOLIO_STATE: PortfolioState = {
  overviewLoading: true,
  overviewError: null,
  overview: null,
  groupsLoading: true,
  groupsError: null,
  groups: [],
}

export const INITIAL_DETAIL_STATE: DetailState = {
  loading: false,
  error: null,
  detail: null,
}

export const INITIAL_MANUAL_GROUPS_STATE: ManualGroupsState = {
  loading: true,
  error: null,
  groups: [],
}

export const INITIAL_MANUAL_DETAIL_STATE: ManualDetailState = {
  loading: false,
  error: null,
  detail: null,
}

export const INITIAL_SCENARIO_TEMPLATES_STATE: ScenarioTemplatesState = {
  loading: true,
  error: null,
  templates: [],
}

export const INITIAL_TEMPLATE_DETAIL_STATE: TemplateDetailState = {
  loading: false,
  error: null,
  detail: null,
}

export const INITIAL_VPS_CATALOG_STATE: VPSCatalogState = {
  loading: true,
  error: null,
  rows: [],
}

export const INITIAL_RECORDS_STATE: RecordsState = {
  loading: true,
  error: null,
  records: [],
}

export const INITIAL_RECORD_DETAIL_STATE: RecordDetailState = {
  loading: false,
  error: null,
  detail: null,
}

export const INITIAL_QUEUE_STATE: QueueState = {
  renewalsLoading: true,
  renewalsError: null,
  queueLoading: true,
  queueError: null,
  renewals: [],
  subscriptions: [],
  unreviewed: [],
  migrate: [],
  cancel: [],
}

// 视图标签
export const VIEW_LABELS: Record<WorkbenchView, string> = {
  needs_decision: '需要决策',
  renewal: '续费取舍',
  region: '同区比较',
  provider: '服务商组合',
  cost: '预算压力',
  evidence: '资料缺口',
  single_queue: '单台队列',
}

// 角色标签
export const ROLE_LABELS: Record<AssetDecisionSuggestedRole, string> = {
  primary_candidate: '主力候选',
  standby_candidate: '备用候选',
  observe_candidate: '观察候选',
  retire_candidate: '退役候选',
  evidence_needed: '补证据',
}

// 动作标签
export const ACTION_LABELS: Record<AssetDecisionSuggestedAction, string> = {
  review: '复核',
  keep: '保留',
  observe: '观察',
  migrate: '迁移',
  cancel: '取消',
  open_cancellation_workbench: '进入取消台',
  complete_evidence: '补齐资料',
}

// 手动组合状态标签
export const MANUAL_GROUP_STATUS_LABELS: Record<AssetDecisionManualGroupStatus, string> = {
  active: '进行中',
  archived: '已归档',
}

// 场景模板状态标签
export const SCENARIO_TEMPLATE_STATUS_LABELS: Record<AssetDecisionScenarioTemplateStatus, string> = {
  active: '启用',
  archived: '已归档',
}

// 手动组合场景标签
export const MANUAL_GROUP_SCENARIO_LABELS: Record<AssetDecisionManualGroupScenario, string> = {
  general: '通用组合',
  primary_standby: '主备取舍',
  budget_reduction: '预算压缩',
  provider_review: '服务商评估',
  region_review: '同区比较',
  migration_retirement: '迁移退役',
  evidence_cleanup: '资料清理',
}

// 记录状态标签
export const RECORD_STATUS_LABELS: Record<AssetDecisionRecordStatus, string> = {
  draft: '草稿',
  decided: '已决策',
  in_progress: '推进中',
  completed: '已完成',
  abandoned: '已放弃',
}

// 后续跟进状态标签
export const FOLLOWUP_STATUS_LABELS: Record<AssetDecisionFollowupStatus, string> = {
  todo: '待处理',
  in_progress: '处理中',
  blocked: '阻塞',
  done: '已完成',
  skipped: '跳过',
}

// 回读状态标签
export const READBACK_STATUS_LABELS: Record<AssetDecisionExecutionReadbackStatus, string> = {
  open: '待回读',
  aligned: '已对齐',
  drift: '有漂移',
  blocked: '阻塞',
  needs_evidence: '需补证据',
  inactive: '不活跃',
}

// 执行计划泳道标签
export const EXECUTION_PLAN_LANE_LABELS: Record<AssetDecisionExecutionPlanLane, string> = {
  cancel_retire: '取消退役',
  migration: '迁移',
  keep_observe: '保留观察',
  evidence: '补证据',
  review: '复核',
}

// 执行计划泳道顺序
export const EXECUTION_PLAN_LANE_ORDER: AssetDecisionExecutionPlanLane[] = [
  'cancel_retire',
  'migration',
  'keep_observe',
  'evidence',
  'review',
]

// 资产决策详情预览限制
export const ASSET_DECISION_DETAIL_PREVIEW_LIMIT = 3

// 对比轴标签
export const COMPARISON_AXIS_LABELS: Record<AssetDecisionComparisonPrimaryAxis, string> = {
  renewal: '续费压力',
  cost: '成本取舍',
  service_context: '承载上下文',
  monitoring: '监控证据',
  evidence: '资料完整度',
  lifecycle: '生命周期',
  review: '人工复核',
}

// 对比泳道标签
export const COMPARISON_LANE_LABELS: Record<AssetDecisionComparisonLane, string> = {
  primary: '主力',
  standby: '备用',
  observe: '观察',
  retire: '退役',
  evidence: '补证据',
  review: '复核',
}

// 证据等级标签
export const EVIDENCE_TIER_LABELS: Record<AssetDecisionEvidenceQualityTier, string> = {
  strong: '证据强',
  usable: '可决策',
  weak: '证据弱',
  blocked: '先补证据',
}

// 证据偏向标签
export const EVIDENCE_BIAS_LABELS: Record<AssetDecisionEvidenceDecisionBias, string> = {
  keep: '偏保留',
  observe: '偏观察',
  complete_evidence: '补证据',
  retire: '偏退役',
  migrate: '偏迁移',
  review: '待复核',
}

// 证据类型标签
export const EVIDENCE_KIND_LABELS: Partial<Record<AssetDecisionEvidenceKind, string>> = {
  ip_quality_missing: 'IP 质量缺失',
  ip_quality_stale: 'IP 质量过期',
  ip_quality_risk: 'IP 质量风险',
  ip_egress_mismatch: '出口 IP 不一致',
  media_unlock_blocked: '服务解锁受阻',
}

// 角色选项
export const ROLE_OPTIONS: ReadonlyArray<{ value: AssetDecisionSuggestedRole; label: string }> = [
  { value: 'primary_candidate', label: ROLE_LABELS.primary_candidate },
  { value: 'standby_candidate', label: ROLE_LABELS.standby_candidate },
  { value: 'observe_candidate', label: ROLE_LABELS.observe_candidate },
  { value: 'retire_candidate', label: ROLE_LABELS.retire_candidate },
  { value: 'evidence_needed', label: ROLE_LABELS.evidence_needed },
]

// 动作选项
export const ACTION_OPTIONS: ReadonlyArray<{ value: AssetDecisionSuggestedAction; label: string }> = [
  { value: 'review', label: ACTION_LABELS.review },
  { value: 'keep', label: ACTION_LABELS.keep },
  { value: 'observe', label: ACTION_LABELS.observe },
  { value: 'migrate', label: ACTION_LABELS.migrate },
  { value: 'cancel', label: ACTION_LABELS.cancel },
  { value: 'open_cancellation_workbench', label: ACTION_LABELS.open_cancellation_workbench },
  { value: 'complete_evidence', label: ACTION_LABELS.complete_evidence },
]

// 手动组合场景选项
export const MANUAL_GROUP_SCENARIO_OPTIONS: ReadonlyArray<{ value: AssetDecisionManualGroupScenario; label: string }> = [
  { value: 'general', label: MANUAL_GROUP_SCENARIO_LABELS.general },
  { value: 'primary_standby', label: MANUAL_GROUP_SCENARIO_LABELS.primary_standby },
  { value: 'budget_reduction', label: MANUAL_GROUP_SCENARIO_LABELS.budget_reduction },
  { value: 'provider_review', label: MANUAL_GROUP_SCENARIO_LABELS.provider_review },
  { value: 'region_review', label: MANUAL_GROUP_SCENARIO_LABELS.region_review },
  { value: 'migration_retirement', label: MANUAL_GROUP_SCENARIO_LABELS.migration_retirement },
  { value: 'evidence_cleanup', label: MANUAL_GROUP_SCENARIO_LABELS.evidence_cleanup },
]

// 记录状态选项
export const RECORD_STATUS_OPTIONS: ReadonlyArray<{ value: AssetDecisionRecordStatus; label: string }> = [
  { value: 'draft', label: RECORD_STATUS_LABELS.draft },
  { value: 'decided', label: RECORD_STATUS_LABELS.decided },
  { value: 'in_progress', label: RECORD_STATUS_LABELS.in_progress },
  { value: 'completed', label: RECORD_STATUS_LABELS.completed },
  { value: 'abandoned', label: RECORD_STATUS_LABELS.abandoned },
]

// 后续跟进状态选项
export const FOLLOWUP_STATUS_OPTIONS: ReadonlyArray<{ value: AssetDecisionFollowupStatus; label: string }> = [
  { value: 'todo', label: FOLLOWUP_STATUS_LABELS.todo },
  { value: 'in_progress', label: FOLLOWUP_STATUS_LABELS.in_progress },
  { value: 'blocked', label: FOLLOWUP_STATUS_LABELS.blocked },
  { value: 'done', label: FOLLOWUP_STATUS_LABELS.done },
  { value: 'skipped', label: FOLLOWUP_STATUS_LABELS.skipped },
]

// 上下文过滤器键
export const CONTEXT_FILTER_KEYS = ['provider_id', 'vps_id', 'country', 'region', 'city', 'scenario'] as const

// 打开状态键
export const OPEN_STATE_KEYS = ['group_id', 'manual_group_id', 'record_id', 'template_id'] as const

// 工作台标签页
export const WORKBENCH_TABS: ReadonlyArray<{ value: MainWorkbenchView; label: string }> = [
  { value: 'needs_decision', label: '需要决策' },
  { value: 'renewal', label: '续费取舍' },
  { value: 'region', label: '同区比较' },
  { value: 'provider', label: '服务商组合' },
  { value: 'cost', label: '预算压力' },
  { value: 'evidence', label: '资料缺口' },
]
