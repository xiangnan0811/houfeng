import { useCallback, useEffect, useState } from 'react'

import {
  createAssetDecisionRecord,
  getAssetDecisionRecord,
  listAssetDecisionRecords,
  patchAssetDecisionRecord,
} from '../../../lib/api'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupListFilter,
  AssetDecisionManualGroupDetail,
  AssetDecisionRecordDetail,
  AssetDecisionRecordMember,
  AssetDecisionRecordSummary,
  AssetDecisionRecordStatus,
  AssetDecisionFollowupStatus,
} from '../../../lib/types'
import {
  FOLLOWUP_STATUS_LABELS,
  RECORD_STATUS_LABELS,
} from '../constants'
import {
  buildRecordFollowupDrafts,
  completeRecordDraftFromGroupDetail,
  completeRecordDraftFromManualDetail,
} from '../recordDrafts'
import type {
  RecordDetailPanel,
  RecordDetailState,
  RecordDraft,
  RecordFollowupDraft,
  RecordMemberDraft,
  RecordsState,
  RenewalWindow,
} from '../types'
import { describeError } from '../utils'

type SettledRecordList = Readonly<{
  filter: AssetDecisionGroupListFilter
  revision: number
  retryRevision: number
  error: string | null
  records: RecordsState['records']
}>

type SettledRecordDetail = Readonly<{
  recordID: string
  revision: number
  retryRevision: number
  error: string | null
  detail: RecordDetailState['detail']
}>

type RecordDetailUI = Readonly<{
  recordID: string
  panel: RecordDetailPanel
  patchStatus: AssetDecisionRecordStatus
  followupDrafts: Readonly<Record<string, RecordFollowupDraft>>
}>

export type AssetDecisionRecordsState = Readonly<{
  list: Readonly<RecordsState>
  detail: Readonly<RecordDetailState>
  detailPanel: RecordDetailPanel
  patchStatus: AssetDecisionRecordStatus
  followupDrafts: Readonly<Record<string, RecordFollowupDraft>>
  draft: Readonly<RecordDraft> | null
  draftEditingMemberID: string | null
  saving: boolean
  saveError: string | null
  patching: boolean
  patchError: string | null
  followupSaving: Readonly<Record<string, boolean>>
  followupErrors: Readonly<Record<string, string | null>>
  followupEditingMemberID: string | null
}>

type RecordDraftPatch = Partial<Pick<RecordDraft, 'title' | 'goal' | 'status'>>

export type AssetDecisionRecordsCommands = Readonly<{
  reloadList: () => void
  reloadDetail: () => void
  startFromAutomatic: (detail: AssetDecisionGroupDetail, renewalWindow: RenewalWindow) => void
  startFromManual: (detail: AssetDecisionManualGroupDetail) => void
  updateDraft: (patch: RecordDraftPatch) => void
  updateDraftMember: (vpsID: string, patch: Partial<RecordMemberDraft>) => void
  editDraftMember: (vpsID: string | null) => void
  cancelDraft: () => void
  saveDraft: () => Promise<AssetDecisionRecordDetail | null>
  setPatchStatus: (status: AssetDecisionRecordStatus) => void
  patchStatus: () => Promise<void>
  updateFollowupDraft: (vpsID: string, patch: Partial<RecordFollowupDraft>) => void
  editFollowupMember: (vpsID: string | null) => void
  saveFollowup: (member: AssetDecisionRecordMember, nextStatus?: AssetDecisionFollowupStatus) => Promise<void>
  selectPanel: (panel: RecordDetailPanel) => void
  resetDetailUI: () => void
}>

type UseAssetDecisionRecordsInput = Readonly<{
  filter: AssetDecisionGroupListFilter
  selectedRecordID: string | null
  revision: number
  onNotice: (notice: string) => void
}>

export function useAssetDecisionRecords({
  filter,
  selectedRecordID,
  revision,
  onNotice,
}: UseAssetDecisionRecordsInput): {
  state: AssetDecisionRecordsState
  commands: AssetDecisionRecordsCommands
} {
  const [listRetryRevision, setListRetryRevision] = useState(0)
  const [detailRetryRevision, setDetailRetryRevision] = useState(0)
  const [settledList, setSettledList] = useState<SettledRecordList | null>(null)
  const [settledDetail, setSettledDetail] = useState<SettledRecordDetail | null>(null)
  const [detailUI, setDetailUI] = useState<RecordDetailUI | null>(null)
  const [draft, setDraft] = useState<RecordDraft | null>(null)
  const [draftEditingMemberID, setDraftEditingMemberID] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [patching, setPatching] = useState(false)
  const [patchError, setPatchError] = useState<string | null>(null)
  const [followupSaving, setFollowupSaving] = useState<Record<string, boolean>>({})
  const [followupErrors, setFollowupErrors] = useState<Record<string, string | null>>({})
  const [followupEditingMemberID, setFollowupEditingMemberID] = useState<string | null>(null)
  const isListCurrent = settledList?.filter === filter &&
    settledList.revision === revision &&
    settledList.retryRevision === listRetryRevision
  const isDetailCurrent = selectedRecordID != null &&
    settledDetail?.recordID === selectedRecordID &&
    settledDetail.revision === revision &&
    settledDetail.retryRevision === detailRetryRevision

  useEffect(() => {
    let cancelled = false

    listAssetDecisionRecords(filter)
      .then((records) => {
        if (cancelled) return
        setSettledList({ filter, revision, retryRevision: listRetryRevision, error: null, records })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledList({
          filter,
          revision,
          retryRevision: listRetryRevision,
          error: describeError(error, '加载已保存组合决策失败'),
          records: [],
        })
      })

    return () => { cancelled = true }
  }, [filter, listRetryRevision, revision])

  useEffect(() => {
    if (!selectedRecordID) return
    let cancelled = false

    getAssetDecisionRecord(selectedRecordID)
      .then((detail) => {
        if (cancelled) return
        setSettledDetail({
          recordID: selectedRecordID,
          revision,
          retryRevision: detailRetryRevision,
          error: null,
          detail,
        })
        setDetailUI({
          recordID: selectedRecordID,
          panel: 'overview',
          patchStatus: detail.status,
          followupDrafts: buildRecordFollowupDrafts(detail),
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledDetail({
          recordID: selectedRecordID,
          revision,
          retryRevision: detailRetryRevision,
          error: describeError(error, '加载决策记录失败'),
          detail: null,
        })
      })

    return () => { cancelled = true }
  }, [detailRetryRevision, revision, selectedRecordID])

  const list: Readonly<RecordsState> = isListCurrent
    ? { loading: false, error: settledList.error, records: settledList.records }
    : { loading: true, error: null, records: settledList?.records ?? [] }
  const detail: Readonly<RecordDetailState> = selectedRecordID == null
    ? { loading: false, error: null, detail: null }
    : isDetailCurrent
      ? { loading: false, error: settledDetail.error, detail: settledDetail.detail }
      : {
          loading: true,
          error: null,
          detail: settledDetail?.recordID === selectedRecordID ? settledDetail.detail : null,
        }
  const currentDetailUI = detailUI?.recordID === selectedRecordID
    ? detailUI
    : {
        recordID: selectedRecordID ?? '',
        panel: 'overview' as const,
        patchStatus: detail.detail?.status ?? 'draft',
        followupDrafts: buildRecordFollowupDrafts(detail.detail),
      }
  const reloadList = useCallback(() => setListRetryRevision((current) => current + 1), [])
  const reloadDetail = useCallback(() => setDetailRetryRevision((current) => current + 1), [])

  const applyDetail = useCallback((
    nextDetail: AssetDecisionRecordDetail,
    options: Readonly<{ prepend?: boolean; resetPanel?: boolean }> = {},
  ) => {
    const summary: AssetDecisionRecordSummary = (() => {
      const { members, ...nextSummary } = nextDetail
      void members
      return nextSummary
    })()
    setSettledDetail({
      recordID: nextDetail.record_id,
      revision,
      retryRevision: detailRetryRevision,
      error: null,
      detail: nextDetail,
    })
    setSettledList((current) => ({
      filter,
      revision,
      retryRevision: listRetryRevision,
      error: null,
      records: options.prepend
        ? [summary, ...(current?.records ?? []).filter((record) => record.record_id !== summary.record_id)]
        : (current?.records ?? []).some((record) => record.record_id === summary.record_id)
          ? (current?.records ?? []).map((record) => record.record_id === summary.record_id ? summary : record)
          : [summary, ...(current?.records ?? [])],
    }))
    setDetailUI((current) => ({
      recordID: nextDetail.record_id,
      panel: options.resetPanel || current?.recordID !== nextDetail.record_id ? 'overview' : current.panel,
      patchStatus: nextDetail.status,
      followupDrafts: buildRecordFollowupDrafts(nextDetail),
    }))
  }, [detailRetryRevision, filter, listRetryRevision, revision])

  const startFromAutomatic = useCallback((
    groupDetail: AssetDecisionGroupDetail,
    renewalWindow: RenewalWindow,
  ) => {
    const keepsCurrent = draft?.sourceType === 'auto_group' && draft.sourceGroupID === groupDetail.group_id
    if (!keepsCurrent) setDraftEditingMemberID(null)
    setDraft((current) => completeRecordDraftFromGroupDetail(current, groupDetail, renewalWindow))
    setSaveError(null)
  }, [draft])

  const startFromManual = useCallback((manualDetail: AssetDecisionManualGroupDetail) => {
    const keepsCurrent = draft?.sourceType === 'manual_group' && draft.sourceGroupID === manualDetail.manual_group_id
    if (!keepsCurrent) setDraftEditingMemberID(null)
    setDraft((current) => completeRecordDraftFromManualDetail(current, manualDetail))
    setSaveError(null)
  }, [draft])

  const updateDraft = useCallback((patch: RecordDraftPatch) => {
    setDraft((current) => current ? { ...current, ...patch } : current)
  }, [])

  const updateDraftMember = useCallback((vpsID: string, patch: Partial<RecordMemberDraft>) => {
    setDraft((current) => {
      const existing = current?.members[vpsID]
      if (!current || !existing) return current
      return {
        ...current,
        members: {
          ...current.members,
          [vpsID]: { ...existing, ...patch },
        },
      }
    })
  }, [])

  const editDraftMember = useCallback((vpsID: string | null) => {
    setDraftEditingMemberID(vpsID)
  }, [])

  const cancelDraft = useCallback(() => {
    setDraft(null)
    setDraftEditingMemberID(null)
    setSaveError(null)
  }, [])

  const saveDraft = useCallback(async (): Promise<AssetDecisionRecordDetail | null> => {
    if (!draft) return null
    setSaveError(null)
    const title = draft.title.trim()
    if (!title) {
      setSaveError('请填写决策记录标题')
      return null
    }

    setSaving(true)
    try {
      const record = await createAssetDecisionRecord({
        source_type: draft.sourceType,
        source_group_id: draft.sourceGroupID,
        renew_within_days: draft.renewWithinDays,
        title,
        goal: draft.goal.trim(),
        status: draft.status,
        members: draft.memberOrder.map((vpsID) => {
          const memberDraft = draft.members[vpsID]
          return {
            vps_id: vpsID,
            decided_role: memberDraft?.decidedRole ?? 'observe_candidate',
            decided_action: memberDraft?.decidedAction ?? 'review',
            reason: memberDraft?.reason.trim() ?? '',
          }
        }),
      })
      applyDetail(record, { prepend: true, resetPanel: true })
      setDraft(null)
      setDraftEditingMemberID(null)
      onNotice(`已保存组合决策记录：${record.title}`)
      return record
    } catch (error) {
      setSaveError(describeError(error, '保存组合决策记录失败'))
      return null
    } finally {
      setSaving(false)
    }
  }, [applyDetail, draft, onNotice])

  const setPatchStatus = useCallback((status: AssetDecisionRecordStatus) => {
    if (!selectedRecordID) return
    setDetailUI((current) => {
      const base = current?.recordID === selectedRecordID
        ? current
        : {
            recordID: selectedRecordID,
            panel: 'overview' as const,
            patchStatus: detail.detail?.status ?? 'draft',
            followupDrafts: buildRecordFollowupDrafts(detail.detail),
          }
      return { ...base, patchStatus: status }
    })
  }, [detail.detail, selectedRecordID])

  const patchStatus = useCallback(async () => {
    const currentDetail = detail.detail
    if (!currentDetail) return
    setPatchError(null)
    setPatching(true)
    try {
      const record = await patchAssetDecisionRecord(currentDetail.record_id, {
        status: currentDetailUI.patchStatus,
      })
      applyDetail(record)
      onNotice(`决策记录状态已更新：${record.title} -> ${RECORD_STATUS_LABELS[record.status]}`)
    } catch (error) {
      setPatchError(describeError(error, '更新决策记录状态失败'))
    } finally {
      setPatching(false)
    }
  }, [applyDetail, currentDetailUI.patchStatus, detail.detail, onNotice])

  const updateFollowupDraft = useCallback((vpsID: string, patch: Partial<RecordFollowupDraft>) => {
    if (!selectedRecordID) return
    setDetailUI((current) => {
      const base = current?.recordID === selectedRecordID
        ? current
        : {
            recordID: selectedRecordID,
            panel: 'overview' as const,
            patchStatus: detail.detail?.status ?? 'draft',
            followupDrafts: buildRecordFollowupDrafts(detail.detail),
          }
      const existing = base.followupDrafts[vpsID] ?? { status: 'todo' as const, note: '' }
      return {
        ...base,
        followupDrafts: {
          ...base.followupDrafts,
          [vpsID]: { ...existing, ...patch },
        },
      }
    })
  }, [detail.detail, selectedRecordID])

  const editFollowupMember = useCallback((vpsID: string | null) => {
    setFollowupEditingMemberID(vpsID)
  }, [])

  const saveFollowup = useCallback(async (
    member: AssetDecisionRecordMember,
    nextStatus?: AssetDecisionFollowupStatus,
  ) => {
    const currentDetail = detail.detail
    if (!currentDetail) return
    const followupDraft = currentDetailUI.followupDrafts[member.vps_id] ?? {
      status: member.followup_status,
      note: member.followup_note,
    }
    const status = nextStatus ?? followupDraft.status
    setPatchError(null)
    setFollowupErrors((current) => ({ ...current, [member.vps_id]: null }))
    setFollowupSaving((current) => ({ ...current, [member.vps_id]: true }))
    try {
      const record = await patchAssetDecisionRecord(currentDetail.record_id, {
        members: [{
          vps_id: member.vps_id,
          followup_status: status,
          followup_note: followupDraft.note.trim(),
        }],
      })
      applyDetail(record)
      onNotice(`成员跟进已更新：${member.display_name || member.vps_id} -> ${FOLLOWUP_STATUS_LABELS[status]}`)
    } catch (error) {
      const message = describeError(error, '更新成员跟进失败')
      setPatchError(message)
      setFollowupErrors((current) => ({ ...current, [member.vps_id]: message }))
    } finally {
      setFollowupSaving((current) => ({ ...current, [member.vps_id]: false }))
    }
  }, [applyDetail, currentDetailUI.followupDrafts, detail.detail, onNotice])

  const selectPanel = useCallback((panel: RecordDetailPanel) => {
    if (!selectedRecordID) return
    setDetailUI((current) => {
      const base = current?.recordID === selectedRecordID
        ? current
        : {
            recordID: selectedRecordID,
            panel: 'overview' as const,
            patchStatus: detail.detail?.status ?? 'draft',
            followupDrafts: buildRecordFollowupDrafts(detail.detail),
          }
      return { ...base, panel }
    })
  }, [detail.detail, selectedRecordID])

  const resetDetailUI = useCallback(() => {
    setDetailUI(null)
    setPatchError(null)
    setFollowupSaving({})
    setFollowupErrors({})
    setFollowupEditingMemberID(null)
  }, [])

  return {
    state: {
      list,
      detail,
      detailPanel: currentDetailUI.panel,
      patchStatus: currentDetailUI.patchStatus,
      followupDrafts: currentDetailUI.followupDrafts,
      draft,
      draftEditingMemberID,
      saving,
      saveError,
      patching,
      patchError,
      followupSaving,
      followupErrors,
      followupEditingMemberID,
    },
    commands: {
      reloadList,
      reloadDetail,
      startFromAutomatic,
      startFromManual,
      updateDraft,
      updateDraftMember,
      editDraftMember,
      cancelDraft,
      saveDraft,
      setPatchStatus,
      patchStatus,
      updateFollowupDraft,
      editFollowupMember,
      saveFollowup,
      selectPanel,
      resetDetailUI,
    },
  }
}
