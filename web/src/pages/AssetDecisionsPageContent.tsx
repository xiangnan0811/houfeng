import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  type BadgeTone,
} from '../components/atoms'
import { PortfolioWorkbench } from './asset-decisions/components/PortfolioWorkbench'
import { SecondaryWorkbenches } from './asset-decisions/components/SecondaryWorkbenches'
import { useAssetDecisionGroups } from './asset-decisions/hooks/useAssetDecisionGroups'
import { useAssetDecisionManualGroups } from './asset-decisions/hooks/useAssetDecisionManualGroups'
import { useAssetDecisionPortfolio } from './asset-decisions/hooks/useAssetDecisionPortfolio'
import { useAssetDecisionRecords } from './asset-decisions/hooks/useAssetDecisionRecords'
import { useAssetDecisionRenewalQueue } from './asset-decisions/hooks/useAssetDecisionRenewalQueue'
import { useAssetDecisionRouteState } from './asset-decisions/hooks/useAssetDecisionRouteState'
import { useAssetDecisionTemplates } from './asset-decisions/hooks/useAssetDecisionTemplates'
import {
  applyAssetDecisionInvalidation,
  INITIAL_ASSET_DECISION_REVISIONS,
  type AssetDecisionInvalidationEvent,
} from './asset-decisions/hooks/invalidation'
import { GroupDetailModal } from './asset-decisions/modals/GroupDetailModal'
import { ManualGroupDetailModal } from './asset-decisions/modals/ManualGroupDetailModal'
import { TemplateDetailModal } from './asset-decisions/modals/TemplateDetailModal'
import { RecordDetailModal } from './asset-decisions/modals/RecordDetailModal'
import { RenewalDecisionModal } from './asset-decisions/modals/RenewalDecisionModal'
import {
  type AssetDecisionEvidenceAssessment,
  type AssetDecisionEvidenceDecisionBias,
  type AssetDecisionEvidenceQualityTier,
  type AssetDecisionEvidenceSnapshot,
  type AssetDecisionGroupDetail,
  type AssetDecisionManualGroupDetail,
  type AssetDecisionManualGroupMember,
  type AssetDecisionManualGroupScenario,
  type AssetDecisionManualGroupStatus,
  type AssetDecisionRecordSummary,
  type AssetDecisionScenarioTemplateStatus,
  type VPSAssetRecord,
} from '../lib/types'
import type {
  MainWorkbenchView,
  PortfolioState,
  ManualGroupsState,
  ScenarioTemplatesState,
  RecordsState,
  FormSubmitEvent,
  ContextFilterKey,
  OpenStateKey,
  AssetDecisionSecondaryNavItem,
  QueueState,
} from './asset-decisions/types'
import {
  createManualMemberColumns,
  createMemberColumns,
} from './asset-decisions/tableColumns'
import {
  deriveClosedLoopMetrics,
  deriveNextWorkItems,
  buildPortfolioLead,
  buildManualGroupProgress,
} from './asset-decisions/businessLogic'
import { parseRenewalWindow } from './asset-decisions/utils'

type ClosedLoopSourceErrors = {
  overview?: string | null
  groups?: string | null
  records?: string | null
  manualGroups?: string | null
  templates?: string | null
}

type AssetDecisionNextWorkKind =
  | 'record_drift'
  | 'record_blocked'
  | 'record_needs_evidence'
  | 'auto_group'
  | 'manual_group'
  | 'scenario_template'

type AssetDecisionNextWorkTarget =
  | { type: 'record'; id: string }
  | { type: 'group'; id: string }
  | { type: 'manual_group'; id: string }
  | { type: 'template'; id: string }

type AssetDecisionNextWorkItem = {
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

function buildSecondaryNavItems(
  recordsState: RecordsState,
  manualGroupsState: ManualGroupsState,
  templatesState: ScenarioTemplatesState,
  queueState: QueueState,
  visibleDecisionQueueCount: number,
  totalDecisionQueue: number,
): AssetDecisionSecondaryNavItem[] {
  const recordMeta = recordsState.loading
    ? '读取中'
    : recordsState.error
      ? '不可用'
      : `${recordsState.records.length} 条`
  const recordIssues = recordsState.records.reduce((count, record) => (
    count + (record.followup_blocked_count ?? 0) + (record.execution_readback?.needs_evidence_count ?? 0)
  ), 0)
  const scenarioMeta = [
    templatesState.loading ? '模板 ...' : templatesState.error ? '模板不可用' : `模板 ${templatesState.templates.length}`,
    manualGroupsState.loading ? '组合 ...' : manualGroupsState.error ? '组合不可用' : `组合 ${manualGroupsState.groups.length}`,
  ].join(' · ')
  const renewalMeta = queueState.renewalsLoading
    ? '读取中'
    : queueState.renewalsError
      ? '不可用'
      : `${queueState.renewals.length} 条`
  const singleQueueMeta = queueState.queueLoading
    ? '读取中'
    : queueState.queueError
      ? '不可用'
      : `${visibleDecisionQueueCount} / ${totalDecisionQueue}`

  return [
    {
      value: 'records',
      eyebrow: '历史记录',
      title: '保存记录',
      summary: recordIssues > 0 ? `待复核 ${recordIssues}` : '可回看',
      meta: recordMeta,
      actionLabel: '打开记录',
      tone: recordsState.error ? 'alert' : recordIssues > 0 ? 'notice' : 'normal',
    },
    {
      value: 'scenarios',
      eyebrow: '场景',
      title: '场景与组合',
      summary: manualGroupsState.error || templatesState.error ? '部分不可用' : '按需打开',
      meta: scenarioMeta,
      actionLabel: '打开场景',
      tone: manualGroupsState.error || templatesState.error ? 'alert' : 'normal',
    },
    {
      value: 'renewals',
      eyebrow: '续费事实',
      title: '续费窗口',
      summary: queueState.renewals.length > 0 ? '有临近项' : '无临近项',
      meta: renewalMeta,
      actionLabel: '查看续费',
      tone: queueState.renewalsError ? 'alert' : queueState.renewals.length > 0 ? 'notice' : 'normal',
    },
    {
      value: 'single_queue',
      eyebrow: '单台辅助',
      title: '单台队列',
      summary: totalDecisionQueue > 0 ? '可逐台处理' : '暂无待处理',
      meta: singleQueueMeta,
      actionLabel: '查看单台队列',
      tone: queueState.queueError ? 'alert' : totalDecisionQueue > 0 ? 'notice' : 'normal',
    },
  ]
}

function parseEvidenceAssessment(snapshot?: AssetDecisionEvidenceSnapshot | null): AssetDecisionEvidenceAssessment | null {
  const raw = snapshot?.evidence_assessment
  if (!raw || typeof raw !== 'object') return null
  const candidate = raw as Partial<AssetDecisionEvidenceAssessment>
  if (
    typeof candidate.confidence_score !== 'number' ||
    typeof candidate.pressure_score !== 'number' ||
    typeof candidate.readiness_score !== 'number' ||
    typeof candidate.quality_tier !== 'string' ||
    typeof candidate.decision_bias !== 'string'
  ) {
    return null
  }
  return {
    confidence_score: candidate.confidence_score,
    pressure_score: candidate.pressure_score,
    readiness_score: candidate.readiness_score,
    quality_tier: candidate.quality_tier as AssetDecisionEvidenceQualityTier,
    decision_bias: candidate.decision_bias as AssetDecisionEvidenceDecisionBias,
    support_signal_count: typeof candidate.support_signal_count === 'number' ? candidate.support_signal_count : 0,
    risk_signal_count: typeof candidate.risk_signal_count === 'number' ? candidate.risk_signal_count : 0,
    gap_signal_count: typeof candidate.gap_signal_count === 'number' ? candidate.gap_signal_count : 0,
    summary: typeof candidate.summary === 'string' ? candidate.summary : '证据评估快照',
  }
}

export function AssetDecisionsPageContent() {
  const route = useAssetDecisionRouteState()
  const portfolioView = route.state.portfolioView
  const renewalWindow = route.state.renewalWindow
  const assetDecisionFilter = route.state.filter
  const contextFilterChips = route.state.contextFilterChips
  const [selectedGroupID, setSelectedGroupID] = useState<string | null>(null)
  const [selectedManualGroupID, setSelectedManualGroupID] = useState<string | null>(null)
  const [selectedRecordID, setSelectedRecordID] = useState<string | null>(null)
  const [selectedTemplateID, setSelectedTemplateID] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [revisions, setRevisions] = useState(INITIAL_ASSET_DECISION_REVISIONS)
  const handleInvalidation = useCallback((event: AssetDecisionInvalidationEvent) => {
    setRevisions((current) => applyAssetDecisionInvalidation(current, event))
  }, [])
  const portfolio = useAssetDecisionPortfolio({
    filter: assetDecisionFilter,
    revision: revisions.portfolio,
  })
  const automaticGroups = useAssetDecisionGroups({
    filter: assetDecisionFilter,
    renewalWindow,
    selectedGroupID,
    revision: revisions.groups,
  })
  const manualGroups = useAssetDecisionManualGroups({
    filter: assetDecisionFilter,
    renewalWindow,
    selectedManualGroupID,
    revision: revisions.manualGroups,
    onNotice: setDecisionNotice,
  })
  const templates = useAssetDecisionTemplates({
    selectedTemplateID,
    renewalWindow,
    revision: revisions.templates,
    onNotice: setDecisionNotice,
  })
  const records = useAssetDecisionRecords({
    filter: assetDecisionFilter,
    selectedRecordID,
    revision: revisions.records,
    onNotice: setDecisionNotice,
  })
  const renewalQueue = useAssetDecisionRenewalQueue({
    renewalWindow,
    revision: revisions.renewalQueue,
    onNotice: setDecisionNotice,
    onInvalidate: handleInvalidation,
  })
  const portfolioState: PortfolioState = {
    overviewLoading: portfolio.state.loading,
    overviewError: portfolio.state.error,
    overview: portfolio.state.overview,
    groupsLoading: automaticGroups.state.list.loading,
    groupsError: automaticGroups.state.list.error,
    groups: automaticGroups.state.list.groups,
  }
  const detailState = automaticGroups.state.detail
  const groupDetailPanel = automaticGroups.state.detailPanel
  const resetGroupDetailUI = automaticGroups.commands.resetDetailUI
  const manualGroupsState = manualGroups.state.list
  const manualDetailState = manualGroups.state.detail
  const vpsCatalogState = manualGroups.state.catalog
  const manualMemberCandidateRows = manualGroups.state.candidateRows
  const manualDetailPanel = manualGroups.state.detailPanel
  const manualGroupCreating = manualGroups.state.creatingFromAutomatic
  const manualGroupSaving = manualGroups.state.saving
  const manualGroupError = manualGroups.state.error
  const manualMemberSaving = manualGroups.state.memberSaving
  const pendingManualMemberRemoval = manualGroups.state.pendingMemberRemoval
  const manualMemberAddDraft = manualGroups.state.memberAddDraft
  const manualMemberAddAdvanced = manualGroups.state.memberAddAdvanced
  const resetManualDetailUI = manualGroups.commands.resetDetailUI
  const templatesState = templates.state.list
  const templateDetailState = templates.state.detail
  const templateDetailPanel = templates.state.detailPanel
  const templateSaving = templates.state.saving
  const templateError = templates.state.error
  const pendingTemplateStatus = templates.state.pendingStatus
  const templateManualDraft = templates.state.manualDraft
  const resetTemplateDetailUI = templates.commands.resetDetailUI
  const recordsState = records.state.list
  const recordDetailState = records.state.detail
  const recordDetailPanel = records.state.detailPanel
  const recordDraft = records.state.draft
  const recordDraftEditingMemberID = records.state.draftEditingMemberID
  const recordSaving = records.state.saving
  const recordSaveError = records.state.saveError
  const recordPatchStatus = records.state.patchStatus
  const recordPatching = records.state.patching
  const recordPatchError = records.state.patchError
  const recordFollowupDrafts = records.state.followupDrafts
  const recordFollowupPatching = records.state.followupSaving
  const recordFollowupEditingMemberID = records.state.followupEditingMemberID
  const cancelRecordDraft = records.commands.cancelDraft
  const resetRecordDetailUI = records.commands.resetDetailUI
  const queueState = renewalQueue.state.queue
  const queueView = renewalQueue.state.queueView
  const selectedVPS = renewalQueue.state.selectedVPS
  const decisionDraft = renewalQueue.state.draft
  const decisionSubmitting = renewalQueue.state.submitting
  const decisionError = renewalQueue.state.error
  const visibleDecisionQueue = renewalQueue.state.visibleDecisionQueue
  const totalDecisionQueue = renewalQueue.state.totalDecisionQueue
  const renewalDueQueueCount = renewalQueue.state.renewalDueQueueCount
  const missingSubscriptionCount = renewalQueue.state.missingSubscriptionCount
  const unlinkedCount = renewalQueue.state.unlinkedCount
  const cancellationAttentionCount = renewalQueue.state.cancellationAttentionCount
  const closeRenewalDecision = renewalQueue.commands.closeVPS
  const searchParamSignature = route.state.searchSignature
  const secondaryWorkbench = route.state.secondary
  const setSelectedSecondaryWorkbench = route.commands.setSecondary
  const handledOpenStateRef = useRef('')

  const applyURLClearedOpenState = useCallback(() => {
    setSelectedGroupID(null)
    resetGroupDetailUI()
    setSelectedManualGroupID(null)
    resetManualDetailUI()
    setSelectedRecordID(null)
    resetRecordDetailUI()
    setSelectedTemplateID(null)
    resetTemplateDetailUI()
    closeRenewalDecision()
    cancelRecordDraft()
  }, [cancelRecordDraft, closeRenewalDecision, resetGroupDetailUI, resetManualDetailUI, resetRecordDetailUI, resetTemplateDetailUI])

  const applyURLGroupOpenState = useCallback((groupID: string) => {
    setSelectedManualGroupID(null)
    resetManualDetailUI()
    setSelectedRecordID(null)
    resetRecordDetailUI()
    setSelectedTemplateID(null)
    resetTemplateDetailUI()
    closeRenewalDecision()
    cancelRecordDraft()
    resetGroupDetailUI()
    setSelectedGroupID(groupID)
  }, [cancelRecordDraft, closeRenewalDecision, resetGroupDetailUI, resetManualDetailUI, resetRecordDetailUI, resetTemplateDetailUI])

  const applyURLManualGroupOpenState = useCallback((manualGroupID: string) => {
    setSelectedGroupID(null)
    resetGroupDetailUI()
    resetManualDetailUI()
    setSelectedRecordID(null)
    resetRecordDetailUI()
    setSelectedTemplateID(null)
    resetTemplateDetailUI()
    closeRenewalDecision()
    cancelRecordDraft()
    setSelectedManualGroupID(manualGroupID)
  }, [cancelRecordDraft, closeRenewalDecision, resetGroupDetailUI, resetManualDetailUI, resetRecordDetailUI, resetTemplateDetailUI])

  const applyURLRecordOpenState = useCallback((recordID: string) => {
    setSelectedGroupID(null)
    resetGroupDetailUI()
    setSelectedManualGroupID(null)
    resetManualDetailUI()
    setSelectedTemplateID(null)
    resetTemplateDetailUI()
    closeRenewalDecision()
    cancelRecordDraft()
    resetRecordDetailUI()
    setSelectedRecordID(recordID)
  }, [cancelRecordDraft, closeRenewalDecision, resetGroupDetailUI, resetManualDetailUI, resetRecordDetailUI, resetTemplateDetailUI])

  const applyURLTemplateOpenState = useCallback((templateID: string) => {
    setSelectedGroupID(null)
    resetGroupDetailUI()
    setSelectedManualGroupID(null)
    resetManualDetailUI()
    setSelectedRecordID(null)
    resetRecordDetailUI()
    closeRenewalDecision()
    cancelRecordDraft()
    resetTemplateDetailUI()
    setSelectedTemplateID(templateID)
  }, [cancelRecordDraft, closeRenewalDecision, resetGroupDetailUI, resetManualDetailUI, resetRecordDetailUI, resetTemplateDetailUI])

  useEffect(() => {
    const openSelection = route.state.open
    const groupID = openSelection?.type === 'group_id' ? openSelection.id : undefined
    const manualGroupID = openSelection?.type === 'manual_group_id' ? openSelection.id : undefined
    const recordID = openSelection?.type === 'record_id' ? openSelection.id : undefined
    const templateID = openSelection?.type === 'template_id' ? openSelection.id : undefined
    const openStateKey = openSelection ? `${openSelection.type}:${openSelection.id}` : ''

    if (openStateKey === handledOpenStateRef.current) return
    // Defer URL-driven drawer/modal selection so deep links do not cascade renders in the effect body.
    const timer = window.setTimeout(() => {
      if (!openStateKey && handledOpenStateRef.current) {
        handledOpenStateRef.current = ''
        applyURLClearedOpenState()
        return
      }
      if (openStateKey === handledOpenStateRef.current) return
      handledOpenStateRef.current = openStateKey
      if (groupID && groupID !== selectedGroupID) {
        applyURLGroupOpenState(groupID)
        return
      }
      if (manualGroupID && manualGroupID !== selectedManualGroupID) {
        applyURLManualGroupOpenState(manualGroupID)
        return
      }
      if (recordID && recordID !== selectedRecordID) {
        applyURLRecordOpenState(recordID)
        return
      }
      if (templateID && templateID !== selectedTemplateID) {
        applyURLTemplateOpenState(templateID)
      }
    }, 0)
    return () => {
      window.clearTimeout(timer)
    }
  }, [
    applyURLClearedOpenState,
    applyURLGroupOpenState,
    applyURLManualGroupOpenState,
    applyURLRecordOpenState,
    applyURLTemplateOpenState,
    route.state.open,
    searchParamSignature,
    selectedGroupID,
    selectedManualGroupID,
    selectedRecordID,
    selectedTemplateID,
  ])

  const vpsByID = useMemo(() => {
    const rows = new Map<string, VPSAssetRecord>()
    for (const vps of vpsCatalogState.rows) rows.set(vps.vps_id, vps)
    for (const vps of [...queueState.unreviewed, ...queueState.migrate, ...queueState.cancel]) {
      rows.set(vps.vps_id, vps)
    }
    for (const member of detailState.detail?.members ?? []) rows.set(member.vps.vps_id, member.vps)
    for (const member of manualDetailState.detail?.members ?? []) {
      if (member.current_fact_found && member.vps?.vps_id) {
        rows.set(member.vps.vps_id, member.vps)
      }
    }
    return rows
  }, [detailState.detail?.members, manualDetailState.detail?.members, queueState.cancel, queueState.migrate, queueState.unreviewed, vpsCatalogState.rows])
  const overview = portfolioState.overview
  const selectedRecordAssessment = recordDetailState.detail
    ? parseEvidenceAssessment(recordDetailState.detail.evidence_snapshot)
    : null
  const manualGroupProgress = manualDetailState.detail
    ? buildManualGroupProgress(manualDetailState.detail)
    : null
  const closedLoopSourceErrors: ClosedLoopSourceErrors = {
    overview: portfolioState.overviewError,
    groups: portfolioState.groupsError,
    records: recordsState.error,
    manualGroups: manualGroupsState.error,
    templates: templatesState.error,
  }
  const closedLoopMetrics = deriveClosedLoopMetrics(
    portfolioState.groups,
    recordsState.records,
    manualGroupsState.groups,
    closedLoopSourceErrors,
    overview,
  )
  const nextWorkItems = deriveNextWorkItems(
    portfolioState.groups,
    recordsState.records,
    closedLoopSourceErrors,
  )
  const portfolioLead = buildPortfolioLead(
    portfolioView,
    renewalWindow,
    overview,
    portfolioState.groups,
    nextWorkItems,
    closedLoopMetrics,
    contextFilterChips,
  )
  const closedLoopPartialErrors = [
    closedLoopSourceErrors.overview ? '组合概览' : '',
    closedLoopSourceErrors.groups ? '自动组' : '',
    closedLoopSourceErrors.records ? '决策记录' : '',
    closedLoopSourceErrors.manualGroups ? '自定义组合' : '',
    closedLoopSourceErrors.templates ? '场景模板' : '',
  ].filter(Boolean)
  const secondaryNavItems = buildSecondaryNavItems(
    recordsState,
    manualGroupsState,
    templatesState,
    queueState,
    visibleDecisionQueue.length,
    totalDecisionQueue,
  )

  const memberColumns = createMemberColumns({
    onSelect: (member) => selectVPS(member.vps),
  })

  const manualMemberColumns = createManualMemberColumns({
    saving: manualMemberSaving,
    onRequestRemoval: requestManualMemberRemoval,
  })

  function setWorkbenchView(next: MainWorkbenchView) {
    route.commands.setWorkbench(next)
  }

  function changeRenewalWindow(value: string) {
    const nextWindow = parseRenewalWindow(value)
    route.commands.setRenewalWindow(nextWindow)
  }

  function setOpenState(key: OpenStateKey, value: string) {
    route.commands.openEntity(key, value)
  }

  function clearOpenState(key: OpenStateKey) {
    route.commands.closeEntity(key)
  }

  function clearContextFilter(key: ContextFilterKey) {
    route.commands.clearFilter(key)
  }

  function clearAllContextFilters() {
    route.commands.clearAllFilters()
  }

  function openGroup(groupID: string) {
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    setSelectedRecordID(null)
    records.commands.resetDetailUI()
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    automaticGroups.commands.resetDetailUI()
    setSelectedGroupID(groupID)
    setOpenState('group_id', groupID)
  }

  function closeGroupDetail() {
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    clearOpenState('group_id')
  }

  function openManualGroup(manualGroupID: string) {
    setSelectedSecondaryWorkbench('scenarios')
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    setSelectedRecordID(null)
    records.commands.resetDetailUI()
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    manualGroups.commands.resetDetailUI()
    setSelectedManualGroupID(manualGroupID)
    setOpenState('manual_group_id', manualGroupID)
  }

  function closeManualGroupDetail() {
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    records.commands.cancelDraft()
    clearOpenState('manual_group_id')
  }

  function openTemplate(templateID: string) {
    setSelectedSecondaryWorkbench('scenarios')
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    setSelectedRecordID(null)
    records.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    templates.commands.resetDetailUI()
    setSelectedTemplateID(templateID)
    setOpenState('template_id', templateID)
  }

  function closeTemplateDetail() {
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    clearOpenState('template_id')
  }

  function openNextWorkItem(item: AssetDecisionNextWorkItem) {
    if (item.target.type === 'record') {
      openRecord(item.target.id)
      return
    }
    if (item.target.type === 'manual_group') {
      openManualGroup(item.target.id)
      return
    }
    if (item.target.type === 'template') {
      openTemplate(item.target.id)
      return
    }
    openGroup(item.target.id)
  }

  async function createManualGroupFromAuto(detail: AssetDecisionGroupDetail) {
    const manualDetail = await manualGroups.commands.createFromAutomatic(detail)
    if (!manualDetail) return
    setSelectedSecondaryWorkbench('scenarios')
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    closeRenewalDecision()
    setSelectedManualGroupID(manualDetail.manual_group_id)
    setOpenState('manual_group_id', manualDetail.manual_group_id)
  }

  function startRecordSave(detail: AssetDecisionGroupDetail) {
    records.commands.startFromAutomatic(detail, renewalWindow)
    automaticGroups.commands.selectPanel('save')
  }

  function startManualRecordSave(detail: AssetDecisionManualGroupDetail) {
    records.commands.startFromManual(detail)
    manualGroups.commands.selectPanel('save')
  }

  function cancelRecordSave() {
    const sourceType = recordDraft?.sourceType
    records.commands.cancelDraft()
    if (sourceType === 'auto_group') automaticGroups.commands.selectPanel('overview')
    if (sourceType === 'manual_group') manualGroups.commands.selectPanel('overview')
  }

  async function submitRecordSave(event: FormSubmitEvent) {
    event.preventDefault()
    const record = await records.commands.saveDraft()
    if (!record) return
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    closeRenewalDecision()
    setSelectedRecordID(record.record_id)
    setOpenState('record_id', record.record_id)
  }

  function openRecord(recordID: string) {
    setSelectedSecondaryWorkbench('records')
    setSelectedGroupID(null)
    automaticGroups.commands.resetDetailUI()
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    records.commands.resetDetailUI()
    setSelectedRecordID(recordID)
    setOpenState('record_id', recordID)
  }

  function closeRecordDetail() {
    setSelectedRecordID(null)
    records.commands.resetDetailUI()
    clearOpenState('record_id')
  }

  async function submitTemplateManualGroup(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = templateDetailState.detail
    if (!detail) return
    const manualDetail = await manualGroups.commands.createFromTemplate(detail, templateManualDraft)
    if (!manualDetail) return
    setSelectedTemplateID(null)
    templates.commands.resetDetailUI()
    setSelectedManualGroupID(manualDetail.manual_group_id)
    setOpenState('manual_group_id', manualDetail.manual_group_id)
  }

  async function updateTemplateStatus(status: AssetDecisionScenarioTemplateStatus) {
    await templates.commands.updateStatus(status)
  }

  function requestTemplateStatusUpdate(status: AssetDecisionScenarioTemplateStatus) {
    templates.commands.requestStatusUpdate(status)
  }

  function cancelTemplateStatusUpdate() {
    templates.commands.cancelStatusUpdate()
  }

  async function saveManualGroupAsTemplate(detail: AssetDecisionManualGroupDetail) {
    const template = await templates.commands.createFromManualGroup(detail)
    if (!template) return
    setSelectedManualGroupID(null)
    manualGroups.commands.resetDetailUI()
    setSelectedTemplateID(template.template_id)
    setOpenState('template_id', template.template_id)
  }

  async function submitManualGroupPatch(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = manualDetailState.detail
    if (!detail) return
    const form = new FormData(event.currentTarget)
    await manualGroups.commands.patchCurrent({
      title: String(form.get('title') ?? '').trim(),
      goal: String(form.get('goal') ?? '').trim(),
      note: String(form.get('note') ?? '').trim(),
      scenario: String(form.get('scenario') ?? detail.scenario) as AssetDecisionManualGroupScenario,
      status: String(form.get('status') ?? detail.status) as AssetDecisionManualGroupStatus,
    })
  }

  async function submitManualMemberAdd(event: FormSubmitEvent) {
    event.preventDefault()
    await manualGroups.commands.addMember()
  }

  async function deleteManualMember(member: AssetDecisionManualGroupMember) {
    await manualGroups.commands.removeMember(member)
  }

  function requestManualMemberRemoval(member: AssetDecisionManualGroupMember) {
    manualGroups.commands.requestMemberRemoval(member)
  }

  function cancelManualMemberRemoval() {
    manualGroups.commands.cancelMemberRemoval()
  }

  function selectVPS(vps: VPSAssetRecord) {
    renewalQueue.commands.selectVPS(vps)
    setDecisionNotice(null)
    if (selectedGroupID) automaticGroups.commands.selectPanel('vps')
    if (selectedManualGroupID) manualGroups.commands.selectPanel('add')
  }

  function navigateToVPS(vps: VPSAssetRecord) {
    route.commands.navigateToVPS(vps.vps_id)
  }

  function navigateToVPSSubscription(vpsID: string) {
    route.commands.navigateToVPSSubscription(vpsID)
  }

  function openPortfolioLead() {
    if (portfolioLead.kind === 'stable') return
    if (portfolioLead.primaryItem) {
      openNextWorkItem(portfolioLead.primaryItem)
      return
    }
    if (portfolioLead.primaryGroupID) {
      openGroup(portfolioLead.primaryGroupID)
      return
    }
    setWorkbenchView('needs_decision')
  }

  function openRecordSource(record: AssetDecisionRecordSummary) {
    if (record.source_type === 'manual_group') {
      openManualGroup(record.source_group_id)
      return
    }
    if (record.source_type === 'auto_group') {
      openGroup(record.source_group_id)
      return
    }
    setDecisionNotice(`记录来源仅作历史快照：${record.source_group_id}`)
  }

  function closeDecisionDrawer() {
    closeRenewalDecision()
    if (selectedGroupID) automaticGroups.commands.selectPanel('members')
  }

  async function submitRenewal() {
    const updated = await renewalQueue.commands.submitRenewal()
    if (updated && selectedGroupID) automaticGroups.commands.selectPanel('members')
  }

  return (
    <div className="animate-in asset-decision-workbench">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">决策台 · DECISIONS</div>
          <h1 className="page-title">资产组合决策</h1>
        </div>
        <div className="header-actions">
          <Link className="btn md secondary" to="/vps">VPS 库存</Link>
          <Link className="btn md secondary" to="/subscriptions">订阅列表</Link>
        </div>
      </div>

      {decisionNotice && (
        <div className="inline-alert ok" role="status">{decisionNotice}</div>
      )}

      <PortfolioWorkbench
        portfolioView={portfolioView}
        renewalWindow={renewalWindow}
        portfolioState={portfolioState}
        portfolioLead={portfolioLead}
        contextFilterChips={contextFilterChips}
        closedLoopPartialErrors={closedLoopPartialErrors}
        closedLoopAnomalies={closedLoopMetrics.readbackDriftCount + closedLoopMetrics.readbackBlockedCount + closedLoopMetrics.readbackNeedsEvidenceCount}
        partialErrorCount={closedLoopMetrics.partialErrorCount}
        onSetWorkbenchView={setWorkbenchView}
        onChangeRenewalWindow={changeRenewalWindow}
        onOpenGroup={openGroup}
        onOpenPortfolioLead={openPortfolioLead}
        onClearContextFilter={clearContextFilter}
        onClearAllContextFilters={clearAllContextFilters}
      />

      <SecondaryWorkbenches
        secondaryWorkbench={secondaryWorkbench}
        secondaryNavItems={secondaryNavItems}
        queueView={queueView}
        renewalWindow={renewalWindow}
        queueState={queueState}
        visibleDecisionQueue={visibleDecisionQueue}
        totalDecisionQueue={totalDecisionQueue}
        renewalDueQueueCount={renewalDueQueueCount}
        missingSubscriptionCount={missingSubscriptionCount}
        unlinkedCount={unlinkedCount}
        cancellationAttentionCount={cancellationAttentionCount}
        manualGroupsState={manualGroupsState}
        templatesState={templatesState}
        recordsState={recordsState}
        vpsByID={vpsByID}
        onSetSelectedSecondaryWorkbench={setSelectedSecondaryWorkbench}
        onSetQueueView={renewalQueue.commands.selectQueueView}
        onSelectVPS={selectVPS}
        onNavigateToVPS={navigateToVPS}
        onNavigateToVPSSubscription={navigateToVPSSubscription}
        onOpenManualGroup={openManualGroup}
        onOpenTemplate={openTemplate}
        onOpenRecord={openRecord}
      />

      <GroupDetailModal
        open={selectedGroupID != null}
        detailState={detailState}
        groupDetailPanel={groupDetailPanel}
        onSetGroupDetailPanel={automaticGroups.commands.selectPanel}
        recordDraft={recordDraft}
        recordDraftEditingMemberID={recordDraftEditingMemberID}
        onUpdateRecordDraft={records.commands.updateDraft}
        onUpdateRecordDraftMember={records.commands.updateDraftMember}
        onEditRecordDraftMember={records.commands.editDraftMember}
        recordSaving={recordSaving}
        recordSaveError={recordSaveError}
        manualGroupCreating={manualGroupCreating}
        manualGroupError={manualGroupError ?? templateError}
        onClose={closeGroupDetail}
        onStartRecordSave={startRecordSave}
        onSubmitRecordSave={submitRecordSave}
        onCancelRecordSave={cancelRecordSave}
        onCreateManualGroupFromAuto={createManualGroupFromAuto}
        selectedVPS={selectedVPS}
        decisionDraft={decisionDraft}
        onUpdateDecisionDraft={renewalQueue.commands.updateDraft}
        decisionSubmitting={decisionSubmitting}
        decisionError={decisionError}
        onSelectVPS={selectVPS}
        onCloseDecisionDrawer={closeDecisionDrawer}
        onSubmitDecision={submitRenewal}
        memberColumns={memberColumns}
      />

      <ManualGroupDetailModal
        open={selectedManualGroupID != null}
        manualDetailState={manualDetailState}
        manualDetailPanel={manualDetailPanel}
        manualGroupProgress={manualGroupProgress}
        manualGroupError={manualGroupError}
        manualGroupSaving={manualGroupSaving}
        templateSaving={templateSaving}
        manualMemberSaving={manualMemberSaving}
        pendingManualMemberRemoval={pendingManualMemberRemoval}
        manualMemberAddDraft={manualMemberAddDraft}
        manualMemberAddAdvanced={manualMemberAddAdvanced}
        vpsCatalogState={vpsCatalogState}
        manualMemberCandidateRows={manualMemberCandidateRows}
        recordDraft={recordDraft}
        recordDraftEditingMemberID={recordDraftEditingMemberID}
        recordSaving={recordSaving}
        recordSaveError={recordSaveError}
        onClose={closeManualGroupDetail}
        onSelectManualDetailPanel={manualGroups.commands.selectPanel}
        onStartManualRecordSave={startManualRecordSave}
        onSubmitRecordSave={submitRecordSave}
        onCancelRecordSave={cancelRecordSave}
        onSubmitManualGroupPatch={submitManualGroupPatch}
        onSaveManualGroupAsTemplate={saveManualGroupAsTemplate}
        onSubmitManualMemberAdd={submitManualMemberAdd}
        onRequestManualMemberRemoval={requestManualMemberRemoval}
        onCancelManualMemberRemoval={cancelManualMemberRemoval}
        onDeleteManualMember={deleteManualMember}
        onUpdateMemberAddDraft={manualGroups.commands.updateMemberAddDraft}
        onSetManualMemberAddAdvancedVisible={manualGroups.commands.setMemberAddAdvanced}
        onUpdateRecordDraft={records.commands.updateDraft}
        onUpdateRecordDraftMember={records.commands.updateDraftMember}
        onEditRecordDraftMember={records.commands.editDraftMember}
        manualMemberColumns={manualMemberColumns}
      />

      <TemplateDetailModal
        open={selectedTemplateID != null}
        templateDetailState={templateDetailState}
        templateDetailPanel={templateDetailPanel}
        templateError={manualGroupError ?? templateError}
        templateSaving={templateSaving || manualGroups.state.creatingFromTemplate}
        pendingTemplateStatus={pendingTemplateStatus}
        templateManualDraft={templateManualDraft}
        onClose={closeTemplateDetail}
        onSetTemplateDetailPanel={templates.commands.selectPanel}
        onRequestTemplateStatusUpdate={requestTemplateStatusUpdate}
        onCancelTemplateStatusUpdate={cancelTemplateStatusUpdate}
        onUpdateTemplateStatus={updateTemplateStatus}
        onSubmitTemplateManualGroup={submitTemplateManualGroup}
        onUpdateTemplateManualDraft={templates.commands.updateManualDraft}
      />

      <RenewalDecisionModal
        open={selectedVPS != null && selectedGroupID == null}
        selectedVPS={selectedVPS}
        decisionDraft={decisionDraft}
        submitting={decisionSubmitting}
        error={decisionError}
        onUpdateDraft={renewalQueue.commands.updateDraft}
        onSubmitDecision={submitRenewal}
        onClose={closeDecisionDrawer}
      />

      <RecordDetailModal
        open={selectedRecordID != null}
        recordDetailState={recordDetailState}
        recordDetailPanel={recordDetailPanel}
        recordPatchError={recordPatchError}
        recordPatchStatus={recordPatchStatus}
        recordPatching={recordPatching}
        selectedRecordAssessment={selectedRecordAssessment}
        followupDrafts={recordFollowupDrafts}
        followupSaving={recordFollowupPatching}
        followupEditingMemberID={recordFollowupEditingMemberID}
        onClose={closeRecordDetail}
        onSetRecordDetailPanel={records.commands.selectPanel}
        onPatchRecordStatus={records.commands.patchStatus}
        onSetRecordPatchStatus={records.commands.setPatchStatus}
        onOpenRecordSource={openRecordSource}
        onUpdateFollowupDraft={records.commands.updateFollowupDraft}
        onEditFollowupMember={records.commands.editFollowupMember}
        onSaveFollowup={records.commands.saveFollowup}
        onReviewRecord={(member) => setDecisionNotice(`请在当前记录中复核：${member.display_name || member.vps_id}`)}
      />
    </div>
  )
}
