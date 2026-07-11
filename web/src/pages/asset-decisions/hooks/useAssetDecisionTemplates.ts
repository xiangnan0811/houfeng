import { useCallback, useEffect, useState } from 'react'

import {
  createAssetDecisionScenarioTemplate,
  getAssetDecisionScenarioTemplate,
  listAssetDecisionScenarioTemplates,
  patchAssetDecisionScenarioTemplate,
} from '../../../lib/api'
import type {
  AssetDecisionManualGroupDetail,
  AssetDecisionScenarioTemplateDetail,
  AssetDecisionScenarioTemplateStatus,
  AssetDecisionScenarioTemplateSummary,
} from '../../../lib/types'
import type {
  RenewalWindow,
  ScenarioTemplatesState,
  TemplateDetailPanel,
  TemplateDetailState,
  TemplateManualGroupDraft,
} from '../types'
import { describeError } from '../utils'

type SettledTemplateList = Readonly<{
  revision: number
  retryRevision: number
  error: string | null
  templates: AssetDecisionScenarioTemplateSummary[]
}>

type SettledTemplateDetail = Readonly<{
  templateID: string
  renewalWindow: RenewalWindow
  revision: number
  retryRevision: number
  error: string | null
  detail: AssetDecisionScenarioTemplateDetail | null
}>

type TemplateDetailUI = Readonly<{
  templateID: string
  panel: TemplateDetailPanel
  manualDraft: TemplateManualGroupDraft
}>

export type AssetDecisionTemplatesState = Readonly<{
  list: Readonly<ScenarioTemplatesState>
  detail: Readonly<TemplateDetailState>
  detailPanel: TemplateDetailPanel
  manualDraft: Readonly<TemplateManualGroupDraft>
  saving: boolean
  error: string | null
  pendingStatus: AssetDecisionScenarioTemplateStatus | null
}>

export type AssetDecisionTemplatesCommands = Readonly<{
  reloadList: () => void
  reloadDetail: () => void
  createFromManualGroup: (
    detail: AssetDecisionManualGroupDetail,
  ) => Promise<AssetDecisionScenarioTemplateDetail | null>
  requestStatusUpdate: (status: AssetDecisionScenarioTemplateStatus) => void
  cancelStatusUpdate: () => void
  updateStatus: (status: AssetDecisionScenarioTemplateStatus) => Promise<void>
  updateManualDraft: (patch: Partial<TemplateManualGroupDraft>) => void
  selectPanel: (panel: TemplateDetailPanel) => void
  resetDetailUI: () => void
}>

type UseAssetDecisionTemplatesInput = Readonly<{
  selectedTemplateID: string | null
  renewalWindow: RenewalWindow
  revision: number
  onNotice: (notice: string) => void
}>

function summaryFromDetail(
  detail: AssetDecisionScenarioTemplateDetail,
): AssetDecisionScenarioTemplateSummary {
  const { members, ...summary } = detail
  void members
  return summary
}

function upsertSummary(
  templates: AssetDecisionScenarioTemplateSummary[],
  detail: AssetDecisionScenarioTemplateDetail,
): AssetDecisionScenarioTemplateSummary[] {
  const summary = summaryFromDetail(detail)
  return [summary, ...templates.filter((template) => template.template_id !== summary.template_id)]
    .sort((left, right) => {
      if (left.builtin !== right.builtin) return left.builtin ? -1 : 1
      if (left.status !== right.status) return left.status === 'active' ? -1 : 1
      return right.updated_at.localeCompare(left.updated_at)
    })
}

function manualDraft(
  detail: AssetDecisionScenarioTemplateDetail | null,
  renewalWindow: RenewalWindow,
): TemplateManualGroupDraft {
  return {
    title: detail?.title ?? '',
    goal: detail?.goal ?? '',
    note: '',
    renewWithinDays: renewalWindow,
  }
}

export function useAssetDecisionTemplates({
  selectedTemplateID,
  renewalWindow,
  revision,
  onNotice,
}: UseAssetDecisionTemplatesInput): {
  state: AssetDecisionTemplatesState
  commands: AssetDecisionTemplatesCommands
} {
  const [listRetryRevision, setListRetryRevision] = useState(0)
  const [detailRetryRevision, setDetailRetryRevision] = useState(0)
  const [settledList, setSettledList] = useState<SettledTemplateList | null>(null)
  const [settledDetail, setSettledDetail] = useState<SettledTemplateDetail | null>(null)
  const [detailUI, setDetailUI] = useState<TemplateDetailUI | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pendingStatus, setPendingStatus] = useState<AssetDecisionScenarioTemplateStatus | null>(null)
  const isListCurrent = settledList?.revision === revision &&
    settledList.retryRevision === listRetryRevision
  const isDetailCurrent = selectedTemplateID != null &&
    settledDetail?.templateID === selectedTemplateID &&
    settledDetail.renewalWindow === renewalWindow &&
    settledDetail.revision === revision &&
    settledDetail.retryRevision === detailRetryRevision

  useEffect(() => {
    let cancelled = false

    listAssetDecisionScenarioTemplates()
      .then((templates) => {
        if (cancelled) return
        setSettledList({
          revision,
          retryRevision: listRetryRevision,
          error: null,
          templates,
        })
      })
      .catch((loadError: unknown) => {
        if (cancelled) return
        setSettledList({
          revision,
          retryRevision: listRetryRevision,
          error: describeError(loadError, '加载场景模板失败'),
          templates: [],
        })
      })

    return () => { cancelled = true }
  }, [listRetryRevision, revision])

  useEffect(() => {
    if (!selectedTemplateID) return
    let cancelled = false

    getAssetDecisionScenarioTemplate(selectedTemplateID)
      .then((detail) => {
        if (cancelled) return
        setSettledDetail({
          templateID: selectedTemplateID,
          renewalWindow,
          revision,
          retryRevision: detailRetryRevision,
          error: null,
          detail,
        })
        setDetailUI({
          templateID: selectedTemplateID,
          panel: 'overview',
          manualDraft: manualDraft(detail, renewalWindow),
        })
      })
      .catch((loadError: unknown) => {
        if (cancelled) return
        setSettledDetail({
          templateID: selectedTemplateID,
          renewalWindow,
          revision,
          retryRevision: detailRetryRevision,
          error: describeError(loadError, '加载场景模板失败'),
          detail: null,
        })
      })

    return () => { cancelled = true }
  }, [detailRetryRevision, renewalWindow, revision, selectedTemplateID])

  const list: Readonly<ScenarioTemplatesState> = isListCurrent
    ? { loading: false, error: settledList.error, templates: settledList.templates }
    : { loading: true, error: null, templates: settledList?.templates ?? [] }
  const detail: Readonly<TemplateDetailState> = selectedTemplateID == null
    ? { loading: false, error: null, detail: null }
    : isDetailCurrent
      ? { loading: false, error: settledDetail.error, detail: settledDetail.detail }
      : {
          loading: true,
          error: null,
          detail: settledDetail?.templateID === selectedTemplateID ? settledDetail.detail : null,
        }
  const currentDetailUI = detailUI?.templateID === selectedTemplateID
    ? detailUI
    : {
        templateID: selectedTemplateID ?? '',
        panel: 'overview' as const,
        manualDraft: manualDraft(detail.detail, renewalWindow),
      }

  const applyDetail = useCallback((nextDetail: AssetDecisionScenarioTemplateDetail) => {
    setSettledDetail({
      templateID: nextDetail.template_id,
      renewalWindow,
      revision,
      retryRevision: detailRetryRevision,
      error: null,
      detail: nextDetail,
    })
    setSettledList((current) => ({
      revision,
      retryRevision: listRetryRevision,
      error: null,
      templates: upsertSummary(current?.templates ?? [], nextDetail),
    }))
    setPendingStatus(null)
    setDetailUI({
      templateID: nextDetail.template_id,
      panel: 'overview',
      manualDraft: manualDraft(nextDetail, renewalWindow),
    })
  }, [detailRetryRevision, listRetryRevision, renewalWindow, revision])

  const createFromManualGroup = useCallback(async (
    manualDetailValue: AssetDecisionManualGroupDetail,
  ): Promise<AssetDecisionScenarioTemplateDetail | null> => {
    setError(null)
    setSaving(true)
    try {
      const created = await createAssetDecisionScenarioTemplate({
        source_manual_group_id: manualDetailValue.manual_group_id,
        title: `${manualDetailValue.title} 模板`,
        scenario: manualDetailValue.scenario,
        goal: manualDetailValue.goal,
        note: manualDetailValue.note,
      })
      applyDetail(created)
      onNotice(`已另存为场景模板：${created.title}`)
      return created
    } catch (createError) {
      setError(describeError(createError, '另存为场景模板失败'))
      return null
    } finally {
      setSaving(false)
    }
  }, [applyDetail, onNotice])

  const requestStatusUpdate = useCallback((status: AssetDecisionScenarioTemplateStatus) => {
    if (!detail.detail || detail.detail.builtin || saving) return
    setError(null)
    setPendingStatus(status)
    setDetailUI((current) => current?.templateID === selectedTemplateID
      ? { ...current, panel: 'status' }
      : current)
  }, [detail.detail, saving, selectedTemplateID])

  const cancelStatusUpdate = useCallback(() => {
    setPendingStatus(null)
    setError(null)
  }, [])

  const updateStatus = useCallback(async (status: AssetDecisionScenarioTemplateStatus) => {
    const currentDetail = detail.detail
    if (!currentDetail || currentDetail.builtin || pendingStatus !== status) return
    setError(null)
    setPendingStatus(null)
    setSaving(true)
    try {
      const updated = await patchAssetDecisionScenarioTemplate(currentDetail.template_id, { status })
      applyDetail(updated)
      onNotice(`模板状态已更新：${updated.title} -> ${status === 'archived' ? '已归档' : '启用'}`)
    } catch (patchError) {
      setError(describeError(patchError, '更新模板状态失败'))
    } finally {
      setSaving(false)
    }
  }, [applyDetail, detail.detail, onNotice, pendingStatus])

  const updateManualDraft = useCallback((patch: Partial<TemplateManualGroupDraft>) => {
    if (!selectedTemplateID) return
    setDetailUI((current) => {
      const base = current?.templateID === selectedTemplateID
        ? current
        : {
            templateID: selectedTemplateID,
            panel: 'overview' as const,
            manualDraft: manualDraft(detail.detail, renewalWindow),
          }
      return { ...base, manualDraft: { ...base.manualDraft, ...patch } }
    })
  }, [detail.detail, renewalWindow, selectedTemplateID])

  const selectPanel = useCallback((panel: TemplateDetailPanel) => {
    if (!selectedTemplateID) return
    setDetailUI((current) => {
      const base = current?.templateID === selectedTemplateID
        ? current
        : {
            templateID: selectedTemplateID,
            panel: 'overview' as const,
            manualDraft: manualDraft(detail.detail, renewalWindow),
          }
      return { ...base, panel }
    })
  }, [detail.detail, renewalWindow, selectedTemplateID])

  const resetDetailUI = useCallback(() => {
    setDetailUI(null)
    setError(null)
    setPendingStatus(null)
  }, [])

  return {
    state: {
      list,
      detail,
      detailPanel: currentDetailUI.panel,
      manualDraft: currentDetailUI.manualDraft,
      saving,
      error,
      pendingStatus,
    },
    commands: {
      reloadList: () => setListRetryRevision((current) => current + 1),
      reloadDetail: () => setDetailRetryRevision((current) => current + 1),
      createFromManualGroup,
      requestStatusUpdate,
      cancelStatusUpdate,
      updateStatus,
      updateManualDraft,
      selectPanel,
      resetDetailUI,
    },
  }
}
