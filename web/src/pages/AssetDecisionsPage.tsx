import { useCallback, useState } from 'react'

import { AssetDecisionPageView } from './asset-decisions/components/AssetDecisionPageView'
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
import {
  type AssetDecisionGroupDetail,
  type AssetDecisionManualGroupDetail,
  type AssetDecisionManualGroupScenario,
  type AssetDecisionManualGroupStatus,
  type AssetDecisionRecordSummary,
  type VPSAssetRecord,
} from '../lib/types'
import type {
  PortfolioState,
  FormSubmitEvent,
  OpenStateKey,
} from './asset-decisions/types'
import {
  createManualMemberColumns,
  createMemberColumns,
} from './asset-decisions/tableColumns'
import {
  buildAssetDecisionPageModel,
} from './asset-decisions/businessLogic'
import { parseRenewalWindow } from './asset-decisions/utils'

export function AssetDecisionsPage() {
  const route = useAssetDecisionRouteState()
  const portfolioView = route.state.portfolioView
  const renewalWindow = route.state.renewalWindow
  const assetDecisionFilter = route.state.filter
  const contextFilterChips = route.state.contextFilterChips
  const selectedGroupID = route.state.open?.type === 'group_id' ? route.state.open.id : null
  const selectedManualGroupID = route.state.open?.type === 'manual_group_id' ? route.state.open.id : null
  const selectedRecordID = route.state.open?.type === 'record_id' ? route.state.open.id : null
  const selectedTemplateID = route.state.open?.type === 'template_id' ? route.state.open.id : null
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
    contextKey: route.state.open ? `${route.state.open.type}:${route.state.open.id}` : '',
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
  const groupState = automaticGroups.state
  const manualState = manualGroups.state
  const templateState = templates.state
  const recordState = records.state
  const renewalState = renewalQueue.state
  const closeRenewalDecision = renewalQueue.commands.closeVPS

  const pageModel = buildAssetDecisionPageModel({
    portfolioView,
    renewalWindow,
    portfolioState,
    recordsState: recordState.list,
    manualGroupsState: manualState.list,
    templatesState: templateState.list,
    queueState: renewalState.queue,
    recordDetail: recordState.detail.detail,
    manualDetail: manualState.detail.detail,
    automaticDetail: groupState.detail.detail,
    vpsCatalogRows: manualState.catalog.rows,
    contextFilterChips,
    visibleDecisionQueueCount: renewalState.visibleDecisionQueue.length,
    totalDecisionQueue: renewalState.totalDecisionQueue,
  })
  const {
    closedLoopMetrics,
    closedLoopPartialErrors,
    manualGroupProgress,
    portfolioLead,
    secondaryNavItems,
    selectedRecordAssessment,
    vpsByID,
  } = pageModel

  const memberColumns = createMemberColumns({
    onSelect: (member) => selectVPS(member.vps),
  })

  const manualMemberColumns = createManualMemberColumns({
    saving: manualState.memberSaving,
    onRequestRemoval: manualGroups.commands.requestMemberRemoval,
  })

  function openEntity(key: OpenStateKey, id: string, secondary?: 'records' | 'scenarios') {
    if (secondary) route.commands.setSecondary(secondary)
    automaticGroups.commands.resetDetailUI()
    manualGroups.commands.resetDetailUI()
    templates.commands.resetDetailUI()
    records.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    route.commands.openEntity(key, id)
  }

  function openGroup(groupID: string) { openEntity('group_id', groupID) }
  function openManualGroup(groupID: string) { openEntity('manual_group_id', groupID, 'scenarios') }
  function openTemplate(templateID: string) { openEntity('template_id', templateID, 'scenarios') }
  function openRecord(recordID: string) { openEntity('record_id', recordID, 'records') }

  function closeGroupDetail() {
    automaticGroups.commands.resetDetailUI()
    closeRenewalDecision()
    records.commands.cancelDraft()
    route.commands.closeEntity('group_id')
  }

  function closeManualGroupDetail() {
    manualGroups.commands.resetDetailUI()
    records.commands.cancelDraft()
    route.commands.closeEntity('manual_group_id')
  }

  function closeTemplateDetail() {
    templates.commands.resetDetailUI()
    route.commands.closeEntity('template_id')
  }

  function closeRecordDetail() {
    records.commands.resetDetailUI()
    route.commands.closeEntity('record_id')
  }

  async function createManualGroupFromAuto(detail: AssetDecisionGroupDetail) {
    const manualDetail = await manualGroups.commands.createFromAutomatic(detail)
    if (!manualDetail) return
    openManualGroup(manualDetail.manual_group_id)
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
    const sourceType = recordState.draft?.sourceType
    records.commands.cancelDraft()
    if (sourceType === 'auto_group') automaticGroups.commands.selectPanel('overview')
    if (sourceType === 'manual_group') manualGroups.commands.selectPanel('overview')
  }

  async function submitRecordSave(event: FormSubmitEvent) {
    event.preventDefault()
    const record = await records.commands.saveDraft()
    if (!record) return
    openRecord(record.record_id)
  }

  async function submitTemplateManualGroup(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = templateState.detail.detail
    if (!detail) return
    const manualDetail = await manualGroups.commands.createFromTemplate(detail, templateState.manualDraft)
    if (!manualDetail) return
    openManualGroup(manualDetail.manual_group_id)
  }

  async function saveManualGroupAsTemplate(detail: AssetDecisionManualGroupDetail) {
    const template = await templates.commands.createFromManualGroup(detail)
    if (!template) return
    openTemplate(template.template_id)
  }

  async function submitManualGroupPatch(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = manualState.detail.detail
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

  function selectVPS(vps: VPSAssetRecord) {
    renewalQueue.commands.selectVPS(vps)
    setDecisionNotice(null)
    if (selectedGroupID) automaticGroups.commands.selectPanel('vps')
    if (selectedManualGroupID) manualGroups.commands.selectPanel('add')
  }

  function openPortfolioLead() {
    if (portfolioLead.kind === 'stable') return
    const target = portfolioLead.primaryItem?.target
    if (target) {
      if (target.type === 'record') openRecord(target.id)
      else if (target.type === 'manual_group') openManualGroup(target.id)
      else if (target.type === 'template') openTemplate(target.id)
      else openGroup(target.id)
      return
    }
    if (portfolioLead.primaryGroupID) {
      openGroup(portfolioLead.primaryGroupID)
      return
    }
    route.commands.setWorkbench('needs_decision')
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

  return <AssetDecisionPageView
    decisionNotice={decisionNotice}
    portfolio={{
      portfolioView, renewalWindow, portfolioState, portfolioLead, contextFilterChips,
      closedLoopPartialErrors,
      closedLoopAnomalies: closedLoopMetrics.readbackDriftCount + closedLoopMetrics.readbackBlockedCount + closedLoopMetrics.readbackNeedsEvidenceCount,
      partialErrorCount: closedLoopMetrics.partialErrorCount,
      onSetWorkbenchView: route.commands.setWorkbench,
      onChangeRenewalWindow: (value) => route.commands.setRenewalWindow(parseRenewalWindow(value)),
      onOpenGroup: openGroup, onOpenPortfolioLead: openPortfolioLead,
      onClearContextFilter: route.commands.clearFilter, onClearAllContextFilters: route.commands.clearAllFilters,
    }}
    secondary={{
      secondaryWorkbench: route.state.secondary, secondaryNavItems,
      queueView: renewalState.queueView, renewalWindow, queueState: renewalState.queue,
      visibleDecisionQueue: renewalState.visibleDecisionQueue,
      totalDecisionQueue: renewalState.totalDecisionQueue,
      renewalDueQueueCount: renewalState.renewalDueQueueCount,
      missingSubscriptionCount: renewalState.missingSubscriptionCount,
      unlinkedCount: renewalState.unlinkedCount,
      cancellationAttentionCount: renewalState.cancellationAttentionCount,
      manualGroupsState: manualState.list, templatesState: templateState.list,
      recordsState: recordState.list, vpsByID,
      onSetSelectedSecondaryWorkbench: route.commands.setSecondary,
      onSetQueueView: renewalQueue.commands.selectQueueView, onSelectVPS: selectVPS,
      onNavigateToVPS: (vps) => route.commands.navigateToVPS(vps.vps_id),
      onNavigateToVPSSubscription: route.commands.navigateToVPSSubscription,
      onOpenManualGroup: openManualGroup, onOpenTemplate: openTemplate, onOpenRecord: openRecord,
    }}
    groupModal={{
      open: selectedGroupID != null, detailState: groupState.detail,
      groupDetailPanel: groupState.detailPanel,
      onSetGroupDetailPanel: automaticGroups.commands.selectPanel,
      recordDraft: recordState.draft, recordDraftEditingMemberID: recordState.draftEditingMemberID,
      recordSaving: recordState.saving, recordSaveError: recordState.saveError,
      onUpdateRecordDraft: records.commands.updateDraft, onUpdateRecordDraftMember: records.commands.updateDraftMember,
      onEditRecordDraftMember: records.commands.editDraftMember,
      manualGroupCreating: manualState.creatingFromAutomatic,
      manualGroupError: manualState.error ?? templateState.error,
      onClose: closeGroupDetail, onStartRecordSave: startRecordSave,
      onSubmitRecordSave: submitRecordSave, onCancelRecordSave: cancelRecordSave,
      onCreateManualGroupFromAuto: createManualGroupFromAuto,
      selectedVPS: renewalState.selectedVPS, decisionDraft: renewalState.draft,
      decisionSubmitting: renewalState.submitting, decisionError: renewalState.error,
      onUpdateDecisionDraft: renewalQueue.commands.updateDraft, onSelectVPS: selectVPS,
      onCloseDecisionDrawer: closeDecisionDrawer, onSubmitDecision: submitRenewal, memberColumns,
    }}
    manualGroupModal={{
      open: selectedManualGroupID != null, manualDetailState: manualState.detail,
      manualDetailPanel: manualState.detailPanel, manualGroupProgress,
      manualGroupError: manualState.error, manualGroupSaving: manualState.saving,
      templateSaving: templateState.saving, manualMemberSaving: manualState.memberSaving,
      pendingManualMemberRemoval: manualState.pendingMemberRemoval,
      manualMemberAddDraft: manualState.memberAddDraft,
      manualMemberAddAdvanced: manualState.memberAddAdvanced,
      vpsCatalogState: manualState.catalog, manualMemberCandidateRows: manualState.candidateRows,
      recordDraft: recordState.draft, recordDraftEditingMemberID: recordState.draftEditingMemberID,
      recordSaving: recordState.saving, recordSaveError: recordState.saveError,
      onClose: closeManualGroupDetail,
      onSelectManualDetailPanel: manualGroups.commands.selectPanel,
      onStartManualRecordSave: startManualRecordSave, onSubmitRecordSave: submitRecordSave,
      onCancelRecordSave: cancelRecordSave, onSubmitManualGroupPatch: submitManualGroupPatch,
      onSaveManualGroupAsTemplate: saveManualGroupAsTemplate, onSubmitManualMemberAdd: submitManualMemberAdd,
      onRequestManualMemberRemoval: manualGroups.commands.requestMemberRemoval,
      onCancelManualMemberRemoval: manualGroups.commands.cancelMemberRemoval,
      onDeleteManualMember: manualGroups.commands.removeMember,
      onUpdateMemberAddDraft: manualGroups.commands.updateMemberAddDraft,
      onSetManualMemberAddAdvancedVisible: manualGroups.commands.setMemberAddAdvanced,
      onUpdateRecordDraft: records.commands.updateDraft, onUpdateRecordDraftMember: records.commands.updateDraftMember,
      onEditRecordDraftMember: records.commands.editDraftMember, manualMemberColumns,
    }}
    templateModal={{
      open: selectedTemplateID != null, templateDetailState: templateState.detail,
      templateDetailPanel: templateState.detailPanel,
      templateError: manualState.error ?? templateState.error,
      templateSaving: templateState.saving || manualState.creatingFromTemplate,
      pendingTemplateStatus: templateState.pendingStatus,
      templateManualDraft: templateState.manualDraft, onClose: closeTemplateDetail,
      onSetTemplateDetailPanel: templates.commands.selectPanel,
      onRequestTemplateStatusUpdate: templates.commands.requestStatusUpdate,
      onCancelTemplateStatusUpdate: templates.commands.cancelStatusUpdate,
      onUpdateTemplateStatus: templates.commands.updateStatus,
      onSubmitTemplateManualGroup: submitTemplateManualGroup,
      onUpdateTemplateManualDraft: templates.commands.updateManualDraft,
    }}
    renewalModal={{
      open: renewalState.selectedVPS != null && selectedGroupID == null,
      selectedVPS: renewalState.selectedVPS, decisionDraft: renewalState.draft,
      submitting: renewalState.submitting, error: renewalState.error,
      onUpdateDraft: renewalQueue.commands.updateDraft, onSubmitDecision: submitRenewal,
      onClose: closeDecisionDrawer,
    }}
    recordModal={{
      open: selectedRecordID != null, recordDetailState: recordState.detail,
      recordDetailPanel: recordState.detailPanel,
      recordPatchError: recordState.patchError, recordPatchStatus: recordState.patchStatus,
      recordPatching: recordState.patching, selectedRecordAssessment,
      followupDrafts: recordState.followupDrafts, followupSaving: recordState.followupSaving,
      followupEditingMemberID: recordState.followupEditingMemberID, onClose: closeRecordDetail,
      onSetRecordDetailPanel: records.commands.selectPanel, onPatchRecordStatus: records.commands.patchStatus,
      onSetRecordPatchStatus: records.commands.setPatchStatus, onOpenRecordSource: openRecordSource,
      onUpdateFollowupDraft: records.commands.updateFollowupDraft,
      onEditFollowupMember: records.commands.editFollowupMember, onSaveFollowup: records.commands.saveFollowup,
      onReviewRecord: (member) => setDecisionNotice(`请在当前记录中复核：${member.display_name || member.vps_id}`),
    }}
  />
}
