import { useCallback, useEffect, useRef, useState } from 'react'

import {
  getAssetDecisionGroup,
  listAssetDecisionGroups,
} from '../../../lib/api'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupListFilter,
  AssetDecisionGroupSummary,
} from '../../../lib/types'
import type {
  DetailState,
  GroupDetailPanel,
  RenewalWindow,
} from '../types'
import { assetDecisionFilterKey, describeError } from '../utils'

export type AssetDecisionGroupListState = Readonly<{
  loading: boolean
  error: string | null
  groups: AssetDecisionGroupSummary[]
}>

export type AssetDecisionGroupsState = Readonly<{
  list: AssetDecisionGroupListState
  detail: Readonly<DetailState>
  detailPanel: GroupDetailPanel
}>

export type AssetDecisionGroupsCommands = Readonly<{
  reloadList: () => void
  reloadDetail: () => void
  selectPanel: (panel: GroupDetailPanel) => void
  resetDetailUI: () => void
}>

type UseAssetDecisionGroupsInput = Readonly<{
  filter: AssetDecisionGroupListFilter
  renewalWindow: RenewalWindow
  selectedGroupID: string | null
  revision: number
}>

type SettledGroupList = Readonly<{
  filter: AssetDecisionGroupListFilter
  revision: number
  retryRevision: number
  error: string | null
  groups: AssetDecisionGroupSummary[]
}>

type SettledGroupDetail = Readonly<{
  groupID: string
  renewalWindow: RenewalWindow
  revision: number
  retryRevision: number
  error: string | null
  detail: AssetDecisionGroupDetail | null
}>

export function useAssetDecisionGroups({
  filter,
  renewalWindow,
  selectedGroupID,
  revision,
}: UseAssetDecisionGroupsInput): {
  state: AssetDecisionGroupsState
  commands: AssetDecisionGroupsCommands
} {
  const [listRetryRevision, setListRetryRevision] = useState(0)
  const [detailRetryRevision, setDetailRetryRevision] = useState(0)
  const [settledList, setSettledList] = useState<SettledGroupList | null>(null)
  const [settledDetail, setSettledDetail] = useState<SettledGroupDetail | null>(null)
  const [panelSelection, setPanelSelection] = useState<Readonly<{
    groupID: string
    panel: GroupDetailPanel
  }> | null>(null)
  const previousSelectedGroupIDRef = useRef(selectedGroupID)
  const currentFilterKey = assetDecisionFilterKey(filter)
  const isListCurrent = settledList != null &&
    assetDecisionFilterKey(settledList.filter) === currentFilterKey &&
    settledList.revision === revision &&
    settledList.retryRevision === listRetryRevision
  const isDetailCurrent = selectedGroupID != null &&
    settledDetail?.groupID === selectedGroupID &&
    settledDetail.renewalWindow === renewalWindow &&
    settledDetail.revision === revision &&
    settledDetail.retryRevision === detailRetryRevision

  useEffect(() => {
    let cancelled = false

    listAssetDecisionGroups(filter)
      .then((groups) => {
        if (cancelled) return
        setSettledList({
          filter,
          revision,
          retryRevision: listRetryRevision,
          error: null,
          groups,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledList({
          filter,
          revision,
          retryRevision: listRetryRevision,
          error: describeError(error, '加载资产决策组失败'),
          groups: [],
        })
      })

    return () => { cancelled = true }
  }, [filter, listRetryRevision, revision])

  useEffect(() => {
    if (!selectedGroupID) return
    let cancelled = false

    getAssetDecisionGroup(selectedGroupID, { renew_within_days: renewalWindow })
      .then((detail) => {
        if (cancelled) return
        setSettledDetail({
          groupID: selectedGroupID,
          renewalWindow,
          revision,
          retryRevision: detailRetryRevision,
          error: null,
          detail,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledDetail({
          groupID: selectedGroupID,
          renewalWindow,
          revision,
          retryRevision: detailRetryRevision,
          error: describeError(error, '加载决策组详情失败'),
          detail: null,
        })
      })

    return () => { cancelled = true }
  }, [detailRetryRevision, renewalWindow, revision, selectedGroupID])

  useEffect(() => {
    if (previousSelectedGroupIDRef.current === selectedGroupID) return
    previousSelectedGroupIDRef.current = selectedGroupID
    queueMicrotask(() => setPanelSelection(null))
  }, [selectedGroupID])

  const list: AssetDecisionGroupListState = isListCurrent
    ? {
        loading: false,
        error: settledList.error,
        groups: settledList.groups,
      }
    : {
        loading: true,
        error: null,
        groups: settledList?.groups ?? [],
      }
  const detail: Readonly<DetailState> = selectedGroupID == null
    ? { loading: false, error: null, detail: null }
    : isDetailCurrent
      ? {
          loading: false,
          error: settledDetail.error,
          detail: settledDetail.detail,
        }
      : {
          loading: true,
          error: null,
          detail: settledDetail?.groupID === selectedGroupID ? settledDetail.detail : null,
        }
  const detailPanel = panelSelection?.groupID === selectedGroupID
    ? panelSelection.panel
    : 'overview'
  const reloadList = useCallback(
    () => setListRetryRevision((current) => current + 1),
    [],
  )
  const reloadDetail = useCallback(
    () => setDetailRetryRevision((current) => current + 1),
    [],
  )
  const selectPanel = useCallback((panel: GroupDetailPanel) => {
    if (selectedGroupID) setPanelSelection({ groupID: selectedGroupID, panel })
  }, [selectedGroupID])
  const resetDetailUI = useCallback(() => {
    setPanelSelection(null)
  }, [])

  return {
    state: { list, detail, detailPanel },
    commands: {
      reloadList,
      reloadDetail,
      selectPanel,
      resetDetailUI,
    },
  }
}
