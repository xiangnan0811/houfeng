import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import type { AssetDecisionGroupListFilter } from '../../../lib/types'
import { CONTEXT_FILTER_KEYS, OPEN_STATE_KEYS } from '../constants'
import { vpsDetailPath, vpsWorkbenchPath } from '../paths'
import type {
  ContextFilterChip,
  ContextFilterKey,
  MainWorkbenchView,
  OpenStateKey,
  RenewalWindow,
  SecondaryWorkbench,
  WorkbenchView,
} from '../types'
import {
  buildAssetDecisionFilter,
  buildContextFilterChips,
  parseRenewalWindow,
  parseWorkbenchView,
  portfolioViewForWorkbench,
  trimParam,
} from '../utils'

export type AssetDecisionOpenSelection = {
  type: OpenStateKey
  id: string
} | null

export type AssetDecisionRouteState = {
  filter: AssetDecisionGroupListFilter
  workbench: WorkbenchView
  portfolioView: MainWorkbenchView
  renewalWindow: RenewalWindow
  secondary: SecondaryWorkbench | null
  open: AssetDecisionOpenSelection
  contextFilterChips: ContextFilterChip[]
  searchSignature: string
}

export type AssetDecisionRouteCommands = {
  setWorkbench: (value: MainWorkbenchView) => void
  setRenewalWindow: (value: RenewalWindow) => void
  setSecondary: (value: SecondaryWorkbench | null) => void
  openEntity: (type: OpenStateKey, id: string) => void
  closeEntity: (type: OpenStateKey) => void
  clearFilter: (key: ContextFilterKey) => void
  clearAllFilters: () => void
  navigateToVPS: (vpsID: string) => void
  navigateToVPSSubscription: (vpsID: string) => void
}

export function useAssetDecisionRouteState(): {
  state: AssetDecisionRouteState
  commands: AssetDecisionRouteCommands
} {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const workbench = parseWorkbenchView(searchParams.get('view'))
  const portfolioView = portfolioViewForWorkbench(workbench)
  const renewalWindow = parseRenewalWindow(searchParams.get('renew_within_days'))
  const filter = useMemo(
    () => buildAssetDecisionFilter(searchParams, portfolioView, renewalWindow),
    [portfolioView, renewalWindow, searchParams],
  )
  const contextFilterChips = useMemo(
    () => buildContextFilterChips(filter),
    [filter],
  )
  const groupID = trimParam(searchParams.get('group_id'))
  const manualGroupID = trimParam(searchParams.get('manual_group_id'))
  const recordID = trimParam(searchParams.get('record_id'))
  const templateID = trimParam(searchParams.get('template_id'))
  const open: AssetDecisionOpenSelection = useMemo(() => (
    groupID
      ? { type: 'group_id', id: groupID }
      : manualGroupID
        ? { type: 'manual_group_id', id: manualGroupID }
        : recordID
          ? { type: 'record_id', id: recordID }
          : templateID
            ? { type: 'template_id', id: templateID }
            : null
  ), [groupID, manualGroupID, recordID, templateID])
  const urlSecondary: SecondaryWorkbench | null = recordID
    ? 'records'
    : manualGroupID || templateID
      ? 'scenarios'
      : workbench === 'single_queue'
        ? 'single_queue'
        : portfolioView === 'renewal'
          ? 'renewals'
          : null
  const [manualSecondary, setManualSecondary] = useState<{
    searchSignature: string
    value: SecondaryWorkbench | null
  } | null>(null)
  const searchSignature = searchParams.toString()
  const selectedSecondary = manualSecondary?.searchSignature === searchSignature
    ? manualSecondary.value
    : null
  const secondary = selectedSecondary ?? urlSecondary

  function updateSearchParams(mutator: (next: URLSearchParams) => void) {
    const next = new URLSearchParams(searchParams)
    mutator(next)
    if (next.toString() !== searchSignature) setSearchParams(next)
  }

  function setWorkbench(value: MainWorkbenchView) {
    updateSearchParams((next) => {
      next.set('view', value)
      next.set('renew_within_days', String(renewalWindow))
    })
  }

  function setRenewalWindow(value: RenewalWindow) {
    updateSearchParams((next) => {
      next.set('view', portfolioView)
      next.set('renew_within_days', String(value))
    })
  }

  function setSecondary(value: SecondaryWorkbench | null) {
    setManualSecondary({ searchSignature, value })
  }

  function openEntity(type: OpenStateKey, id: string) {
    updateSearchParams((next) => {
      for (const key of OPEN_STATE_KEYS) {
        if (key !== type) next.delete(key)
      }
      next.set(type, id)
    })
  }

  function closeEntity(type: OpenStateKey) {
    const next = new URLSearchParams(searchParams)
    next.delete(type)
    const nextSignature = next.toString()
    if (nextSignature === searchSignature) return
    setManualSecondary((current) => ({
      searchSignature: nextSignature,
      value: current?.value ?? null,
    }))
    setSearchParams(next)
  }

  function clearFilter(key: ContextFilterKey) {
    updateSearchParams((next) => next.delete(key))
  }

  function clearAllFilters() {
    updateSearchParams((next) => {
      for (const key of CONTEXT_FILTER_KEYS) next.delete(key)
    })
  }

  return {
    state: {
      filter,
      workbench,
      portfolioView,
      renewalWindow,
      secondary,
      open,
      contextFilterChips,
      searchSignature,
    },
    commands: {
      setWorkbench,
      setRenewalWindow,
      setSecondary,
      openEntity,
      closeEntity,
      clearFilter,
      clearAllFilters,
      navigateToVPS: (vpsID) => { void navigate(vpsDetailPath(vpsID)) },
      navigateToVPSSubscription: (vpsID) => { void navigate(vpsWorkbenchPath(vpsID, 'subscription')) },
    },
  }
}
