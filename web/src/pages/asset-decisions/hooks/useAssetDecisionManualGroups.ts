import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  addAssetDecisionManualGroupMember,
  createAssetDecisionManualGroup,
  createManualGroupFromScenarioTemplate,
  deleteAssetDecisionManualGroupMember,
  getAssetDecisionManualGroup,
  listAssetDecisionManualGroups,
  listVPSAssets,
  patchAssetDecisionManualGroup,
} from '../../../lib/api'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupListFilter,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupMember,
  AssetDecisionManualGroupScenario,
  AssetDecisionManualGroupSummary,
  AssetDecisionScenarioTemplateDetail,
  PatchAssetDecisionManualGroupInput,
  VPSAssetRecord,
} from '../../../lib/types'
import type {
  ManualDetailPanel,
  ManualDetailState,
  ManualGroupsState,
  ManualMemberAddDraft,
  RenewalWindow,
  TemplateManualGroupDraft,
  VPSCatalogState,
} from '../types'
import { describeError } from '../utils'

const INITIAL_MEMBER_ADD_DRAFT: ManualMemberAddDraft = {
  vpsID: '',
  intendedRole: 'observe_candidate',
  intendedAction: 'review',
  reason: '',
  note: '',
  sortOrder: '',
}

type ManualDetailUI = Readonly<{
  manualGroupID: string
  panel: ManualDetailPanel
  memberAddDraft: ManualMemberAddDraft
  memberAddAdvanced: boolean
}>

type SettledManualList = Readonly<{
  filter: AssetDecisionGroupListFilter
  filterKey: string
  revision: number
  retryRevision: number
  error: string | null
  groups: AssetDecisionManualGroupSummary[]
}>

type SettledManualDetail = Readonly<{
  manualGroupID: string
  revision: number
  retryRevision: number
  error: string | null
  detail: AssetDecisionManualGroupDetail | null
}>

type SettledVPSCatalog = Readonly<{
  revision: number
  retryRevision: number
  error: string | null
  rows: VPSAssetRecord[]
}>

export type AssetDecisionManualGroupsState = Readonly<{
  list: Readonly<ManualGroupsState>
  detail: Readonly<ManualDetailState>
  catalog: Readonly<VPSCatalogState>
  candidateRows: VPSAssetRecord[]
  detailPanel: ManualDetailPanel
  memberAddDraft: Readonly<ManualMemberAddDraft>
  memberAddAdvanced: boolean
  creatingFromAutomatic: boolean
  creatingFromTemplate: boolean
  saving: boolean
  error: string | null
  memberSaving: Record<string, boolean>
  pendingMemberRemoval: AssetDecisionManualGroupMember | null
}>

export type AssetDecisionManualGroupsCommands = Readonly<{
  reloadList: () => void
  reloadDetail: () => void
  reloadCatalog: () => void
  createFromAutomatic: (detail: AssetDecisionGroupDetail) => Promise<AssetDecisionManualGroupDetail | null>
  createFromTemplate: (
    template: AssetDecisionScenarioTemplateDetail,
    draft: TemplateManualGroupDraft,
  ) => Promise<AssetDecisionManualGroupDetail | null>
  patchCurrent: (input: PatchAssetDecisionManualGroupInput) => Promise<void>
  addMember: () => Promise<void>
  requestMemberRemoval: (member: AssetDecisionManualGroupMember) => void
  cancelMemberRemoval: () => void
  removeMember: (member: AssetDecisionManualGroupMember) => Promise<void>
  updateMemberAddDraft: (patch: Partial<ManualMemberAddDraft>) => void
  setMemberAddAdvanced: (visible: boolean) => void
  selectPanel: (panel: ManualDetailPanel) => void
  resetDetailUI: () => void
}>

type UseAssetDecisionManualGroupsInput = Readonly<{
  filter: AssetDecisionGroupListFilter
  renewalWindow: RenewalWindow
  selectedManualGroupID: string | null
  revision: number
  onNotice: (notice: string) => void
}>

function filterKey(filter: AssetDecisionGroupListFilter): string {
  return [
    filter.view ?? '',
    filter.renew_within_days ?? '',
    filter.provider_id ?? '',
    filter.vps_id ?? '',
    filter.country ?? '',
    filter.region ?? '',
    filter.city ?? '',
    filter.scenario ?? '',
  ].join('|')
}

function summaryFromDetail(detail: AssetDecisionManualGroupDetail): AssetDecisionManualGroupSummary {
  const { members, ...summary } = detail
  void members
  return summary
}

function mergeSummaries(
  rows: AssetDecisionManualGroupSummary[],
  additions: AssetDecisionManualGroupSummary[],
): AssetDecisionManualGroupSummary[] {
  let next = [...rows]
  for (const summary of additions) {
    next = [summary, ...next.filter((row) => row.manual_group_id !== summary.manual_group_id)]
  }
  return next.sort((left, right) => {
    if (left.status !== right.status) return left.status === 'active' ? -1 : 1
    return right.updated_at.localeCompare(left.updated_at)
  })
}

function scenarioForAutomaticGroup(
  group: Pick<AssetDecisionGroupDetail, 'group_type'>,
): AssetDecisionManualGroupScenario {
  if (group.group_type === 'region_portfolio') return 'region_review'
  if (group.group_type === 'provider_portfolio') return 'provider_review'
  if (group.group_type === 'cost_pressure') return 'budget_reduction'
  if (group.group_type === 'cancellation_attention') return 'migration_retirement'
  if (group.group_type === 'evidence_gap') return 'evidence_cleanup'
  return 'general'
}

export function useAssetDecisionManualGroups({
  filter,
  renewalWindow,
  selectedManualGroupID,
  revision,
  onNotice,
}: UseAssetDecisionManualGroupsInput): {
  state: AssetDecisionManualGroupsState
  commands: AssetDecisionManualGroupsCommands
} {
  const [listRetryRevision, setListRetryRevision] = useState(0)
  const [detailRetryRevision, setDetailRetryRevision] = useState(0)
  const [catalogRetryRevision, setCatalogRetryRevision] = useState(0)
  const [settledList, setSettledList] = useState<SettledManualList | null>(null)
  const [settledDetail, setSettledDetail] = useState<SettledManualDetail | null>(null)
  const [settledCatalog, setSettledCatalog] = useState<SettledVPSCatalog | null>(null)
  const [detailUI, setDetailUI] = useState<ManualDetailUI | null>(null)
  const [creatingFromAutomatic, setCreatingFromAutomatic] = useState(false)
  const [creatingFromTemplate, setCreatingFromTemplate] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [memberSaving, setMemberSaving] = useState<Record<string, boolean>>({})
  const [pendingMemberRemoval, setPendingMemberRemoval] = useState<AssetDecisionManualGroupMember | null>(null)
  const preservedSummariesRef = useRef(new Map<string, {
    filterKey: string
    summary: AssetDecisionManualGroupSummary
  }>())
  const currentFilterKey = filterKey(filter)
  const isListCurrent = settledList?.filter === filter &&
    settledList.revision === revision &&
    settledList.retryRevision === listRetryRevision
  const isDetailCurrent = selectedManualGroupID != null &&
    settledDetail?.manualGroupID === selectedManualGroupID &&
    settledDetail.revision === revision &&
    settledDetail.retryRevision === detailRetryRevision
  const isCatalogCurrent = settledCatalog?.revision === revision &&
    settledCatalog.retryRevision === catalogRetryRevision

  useEffect(() => {
    let cancelled = false

    listAssetDecisionManualGroups(filter)
      .then((groups) => {
        if (cancelled) return
        const preserved = Array.from(preservedSummariesRef.current.values())
          .filter((item) => item.filterKey === currentFilterKey)
          .map((item) => item.summary)
        setSettledList({
          filter,
          filterKey: currentFilterKey,
          revision,
          retryRevision: listRetryRevision,
          error: null,
          groups: mergeSummaries(groups, preserved),
        })
      })
      .catch((loadError: unknown) => {
        if (cancelled) return
        setSettledList({
          filter,
          filterKey: currentFilterKey,
          revision,
          retryRevision: listRetryRevision,
          error: describeError(loadError, '加载自定义组合失败'),
          groups: [],
        })
      })

    return () => { cancelled = true }
  }, [currentFilterKey, filter, listRetryRevision, revision])

  useEffect(() => {
    let cancelled = false

    listVPSAssets()
      .then((rows) => {
        if (cancelled) return
        setSettledCatalog({
          revision,
          retryRevision: catalogRetryRevision,
          error: null,
          rows,
        })
      })
      .catch((loadError: unknown) => {
        if (cancelled) return
        setSettledCatalog({
          revision,
          retryRevision: catalogRetryRevision,
          error: describeError(loadError, '加载 VPS 候选失败'),
          rows: [],
        })
      })

    return () => { cancelled = true }
  }, [catalogRetryRevision, revision])

  useEffect(() => {
    if (!selectedManualGroupID) return
    let cancelled = false

    getAssetDecisionManualGroup(selectedManualGroupID)
      .then((detail) => {
        if (cancelled) return
        setSettledDetail({
          manualGroupID: selectedManualGroupID,
          revision,
          retryRevision: detailRetryRevision,
          error: null,
          detail,
        })
      })
      .catch((loadError: unknown) => {
        if (cancelled) return
        setSettledDetail({
          manualGroupID: selectedManualGroupID,
          revision,
          retryRevision: detailRetryRevision,
          error: describeError(loadError, '加载自定义组合详情失败'),
          detail: null,
        })
      })

    return () => { cancelled = true }
  }, [detailRetryRevision, revision, selectedManualGroupID])

  const list: Readonly<ManualGroupsState> = isListCurrent
    ? { loading: false, error: settledList.error, groups: settledList.groups }
    : {
        loading: true,
        error: null,
        groups: settledList?.filterKey === currentFilterKey ? settledList.groups : [],
      }
  const detail: Readonly<ManualDetailState> = selectedManualGroupID == null
    ? { loading: false, error: null, detail: null }
    : isDetailCurrent
      ? { loading: false, error: settledDetail.error, detail: settledDetail.detail }
      : {
          loading: true,
          error: null,
          detail: settledDetail?.manualGroupID === selectedManualGroupID ? settledDetail.detail : null,
        }
  const catalog: Readonly<VPSCatalogState> = isCatalogCurrent
    ? { loading: false, error: settledCatalog.error, rows: settledCatalog.rows }
    : { loading: true, error: null, rows: settledCatalog?.rows ?? [] }
  const currentDetailUI = detailUI?.manualGroupID === selectedManualGroupID
    ? detailUI
    : {
        manualGroupID: selectedManualGroupID ?? '',
        panel: 'overview' as const,
        memberAddDraft: INITIAL_MEMBER_ADD_DRAFT,
        memberAddAdvanced: false,
      }
  const candidateRows = useMemo(() => {
    const existing = new Set((detail.detail?.members ?? []).map((member) => member.vps_id))
    return catalog.rows
      .filter((vps) => !existing.has(vps.vps_id))
      .sort((left, right) => left.display_name.localeCompare(right.display_name))
  }, [catalog.rows, detail.detail?.members])

  const applyDetail = useCallback((nextDetail: AssetDecisionManualGroupDetail) => {
    const summary = summaryFromDetail(nextDetail)
    preservedSummariesRef.current.set(summary.manual_group_id, {
      filterKey: currentFilterKey,
      summary,
    })
    setSettledDetail({
      manualGroupID: nextDetail.manual_group_id,
      revision,
      retryRevision: detailRetryRevision,
      error: null,
      detail: nextDetail,
    })
    setPendingMemberRemoval((current) =>
      current && nextDetail.members.some((member) => member.vps_id === current.vps_id) ? current : null,
    )
    setSettledList((current) => ({
      filter,
      filterKey: currentFilterKey,
      revision,
      retryRevision: listRetryRevision,
      error: null,
      groups: mergeSummaries(
        current?.filterKey === currentFilterKey ? current.groups : [],
        [summary],
      ),
    }))
  }, [currentFilterKey, detailRetryRevision, filter, listRetryRevision, revision])

  const updateMemberAddDraft = useCallback((patch: Partial<ManualMemberAddDraft>) => {
    if (!selectedManualGroupID) return
    setDetailUI((current) => {
      const base = current?.manualGroupID === selectedManualGroupID
        ? current
        : {
            manualGroupID: selectedManualGroupID,
            panel: 'overview' as const,
            memberAddDraft: INITIAL_MEMBER_ADD_DRAFT,
            memberAddAdvanced: false,
          }
      return {
        ...base,
        memberAddDraft: { ...base.memberAddDraft, ...patch },
      }
    })
  }, [selectedManualGroupID])

  const setMemberAddAdvanced = useCallback((visible: boolean) => {
    if (!selectedManualGroupID) return
    setDetailUI((current) => {
      const base = current?.manualGroupID === selectedManualGroupID
        ? current
        : {
            manualGroupID: selectedManualGroupID,
            panel: 'overview' as const,
            memberAddDraft: INITIAL_MEMBER_ADD_DRAFT,
            memberAddAdvanced: false,
          }
      return {
        ...base,
        memberAddAdvanced: visible,
        memberAddDraft: visible
          ? base.memberAddDraft
          : { ...base.memberAddDraft, reason: '', note: '', sortOrder: '' },
      }
    })
  }, [selectedManualGroupID])

  const createFromAutomatic = useCallback(async (
    automaticDetail: AssetDecisionGroupDetail,
  ): Promise<AssetDecisionManualGroupDetail | null> => {
    setError(null)
    setCreatingFromAutomatic(true)
    try {
      const created = await createAssetDecisionManualGroup({
        source_type: 'auto_group',
        source_group_id: automaticDetail.group_id,
        renew_within_days: renewalWindow,
        scenario: scenarioForAutomaticGroup(automaticDetail),
        title: automaticDetail.title,
        goal: '',
        note: `由自动组 ${automaticDetail.group_id} 创建`,
      })
      applyDetail(created)
      onNotice(`已创建自定义组合：${created.title}`)
      return created
    } catch (createError) {
      setError(describeError(createError, '创建自定义组合失败'))
      return null
    } finally {
      setCreatingFromAutomatic(false)
    }
  }, [applyDetail, onNotice, renewalWindow])

  const createFromTemplate = useCallback(async (
    template: AssetDecisionScenarioTemplateDetail,
    draft: TemplateManualGroupDraft,
  ): Promise<AssetDecisionManualGroupDetail | null> => {
    const title = draft.title.trim()
    if (!title) {
      setError('请填写要创建的自定义组合标题')
      return null
    }
    setError(null)
    setCreatingFromTemplate(true)
    try {
      const created = await createManualGroupFromScenarioTemplate(template.template_id, {
        title,
        goal: draft.goal.trim(),
        note: draft.note.trim(),
        scenario: template.scenario,
        status: 'active',
        renew_within_days: draft.renewWithinDays,
      })
      applyDetail(created)
      onNotice(`已从模板创建自定义组合：${created.title}`)
      return created
    } catch (createError) {
      setError(describeError(createError, '从模板创建自定义组合失败'))
      return null
    } finally {
      setCreatingFromTemplate(false)
    }
  }, [applyDetail, onNotice])

  const patchCurrent = useCallback(async (input: PatchAssetDecisionManualGroupInput) => {
    const currentDetail = detail.detail
    if (!currentDetail) return
    const title = input.title?.trim() ?? ''
    if (!title) {
      setError('请填写自定义组合标题')
      return
    }
    setError(null)
    setSaving(true)
    try {
      const updated = await patchAssetDecisionManualGroup(currentDetail.manual_group_id, {
        title,
        goal: input.goal?.trim() ?? '',
        note: input.note?.trim() ?? '',
        scenario: input.scenario ?? currentDetail.scenario,
        status: input.status ?? currentDetail.status,
      })
      applyDetail(updated)
      onNotice(`自定义组合已更新：${updated.title}`)
    } catch (patchError) {
      setError(describeError(patchError, '更新自定义组合失败'))
    } finally {
      setSaving(false)
    }
  }, [applyDetail, detail.detail, onNotice])

  const addMember = useCallback(async () => {
    const currentDetail = detail.detail
    if (!currentDetail) return
    const vpsID = currentDetailUI.memberAddDraft.vpsID.trim()
    if (!vpsID) {
      setError('请选择要加入组合的 VPS')
      return
    }
    const draft = currentDetailUI.memberAddDraft
    const parsedSortOrder = Number.parseInt(draft.sortOrder, 10)
    setError(null)
    setSaving(true)
    try {
      const updated = await addAssetDecisionManualGroupMember(currentDetail.manual_group_id, {
        vps_id: vpsID,
        intended_role: draft.intendedRole,
        intended_action: draft.intendedAction,
        reason: draft.reason.trim(),
        note: draft.note.trim(),
        ...(Number.isFinite(parsedSortOrder) ? { sort_order: parsedSortOrder } : {}),
      })
      applyDetail(updated)
      setDetailUI((current) => current?.manualGroupID === selectedManualGroupID
        ? { ...current, memberAddDraft: INITIAL_MEMBER_ADD_DRAFT, memberAddAdvanced: false }
        : current)
      onNotice('自定义组合成员已加入')
    } catch (addError) {
      setError(describeError(addError, '新增自定义组合成员失败'))
    } finally {
      setSaving(false)
    }
  }, [applyDetail, currentDetailUI.memberAddDraft, detail.detail, onNotice, selectedManualGroupID])

  const requestMemberRemoval = useCallback((member: AssetDecisionManualGroupMember) => {
    if (memberSaving[member.vps_id]) return
    setError(null)
    setPendingMemberRemoval(member)
  }, [memberSaving])

  const cancelMemberRemoval = useCallback(() => {
    setPendingMemberRemoval(null)
    setError(null)
  }, [])

  const removeMember = useCallback(async (member: AssetDecisionManualGroupMember) => {
    const currentDetail = detail.detail
    if (!currentDetail || pendingMemberRemoval?.vps_id !== member.vps_id) return
    setError(null)
    setPendingMemberRemoval(null)
    setMemberSaving((current) => ({ ...current, [member.vps_id]: true }))
    try {
      const updated = await deleteAssetDecisionManualGroupMember(
        currentDetail.manual_group_id,
        member.vps_id,
      )
      applyDetail(updated)
      onNotice(`成员已移出自定义组合：${member.current_fact_found ? member.vps.display_name : member.vps_id}`)
    } catch (removeError) {
      setError(describeError(removeError, '移除成员失败'))
    } finally {
      setMemberSaving((current) => ({ ...current, [member.vps_id]: false }))
    }
  }, [applyDetail, detail.detail, onNotice, pendingMemberRemoval])

  const selectPanel = useCallback((panel: ManualDetailPanel) => {
    if (!selectedManualGroupID) return
    if (panel === 'add') setMemberAddAdvanced(false)
    setDetailUI((current) => {
      const base = current?.manualGroupID === selectedManualGroupID
        ? current
        : {
            manualGroupID: selectedManualGroupID,
            panel: 'overview' as const,
            memberAddDraft: INITIAL_MEMBER_ADD_DRAFT,
            memberAddAdvanced: false,
          }
      return { ...base, panel }
    })
  }, [selectedManualGroupID, setMemberAddAdvanced])

  const resetDetailUI = useCallback(() => {
    setDetailUI(null)
    setError(null)
    setPendingMemberRemoval(null)
    setMemberSaving({})
    setDetailRetryRevision((current) => current + 1)
  }, [])

  return {
    state: {
      list,
      detail,
      catalog,
      candidateRows,
      detailPanel: currentDetailUI.panel,
      memberAddDraft: currentDetailUI.memberAddDraft,
      memberAddAdvanced: currentDetailUI.memberAddAdvanced,
      creatingFromAutomatic,
      creatingFromTemplate,
      saving,
      error,
      memberSaving,
      pendingMemberRemoval,
    },
    commands: {
      reloadList: () => setListRetryRevision((current) => current + 1),
      reloadDetail: () => setDetailRetryRevision((current) => current + 1),
      reloadCatalog: () => setCatalogRetryRevision((current) => current + 1),
      createFromAutomatic,
      createFromTemplate,
      patchCurrent,
      addMember,
      requestMemberRemoval,
      cancelMemberRemoval,
      removeMember,
      updateMemberAddDraft,
      setMemberAddAdvanced,
      selectPanel,
      resetDetailUI,
    },
  }
}
