import { useEffect, useState } from 'react'

import { getAssetDecisionOverview } from '../../../lib/api'
import type {
  AssetDecisionGroupListFilter,
  AssetDecisionOverview,
} from '../../../lib/types'
import { describeError } from '../utils'

export type AssetDecisionPortfolioState = Readonly<{
  loading: boolean
  error: string | null
  overview: AssetDecisionOverview | null
}>

export type AssetDecisionPortfolioCommands = Readonly<{
  reload: () => void
}>

type UseAssetDecisionPortfolioInput = Readonly<{
  filter: AssetDecisionGroupListFilter
  revision: number
}>

type SettledOverview = Readonly<{
  filter: AssetDecisionGroupListFilter
  revision: number
  retryRevision: number
  error: string | null
  overview: AssetDecisionOverview | null
}>

export function useAssetDecisionPortfolio({
  filter,
  revision,
}: UseAssetDecisionPortfolioInput): {
  state: AssetDecisionPortfolioState
  commands: AssetDecisionPortfolioCommands
} {
  const [retryRevision, setRetryRevision] = useState(0)
  const [settled, setSettled] = useState<SettledOverview | null>(null)
  const isCurrent = settled?.filter === filter &&
    settled.revision === revision &&
    settled.retryRevision === retryRevision

  useEffect(() => {
    let cancelled = false

    getAssetDecisionOverview(filter)
      .then((overview) => {
        if (cancelled) return
        setSettled({
          filter,
          revision,
          retryRevision,
          error: null,
          overview,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettled({
          filter,
          revision,
          retryRevision,
          error: describeError(error, '加载资产组合概览失败'),
          overview: null,
        })
      })

    return () => { cancelled = true }
  }, [filter, retryRevision, revision])

  return {
    state: isCurrent
      ? {
          loading: false,
          error: settled.error,
          overview: settled.overview,
        }
      : {
          loading: true,
          error: null,
          overview: settled?.overview ?? null,
        },
    commands: {
      reload: () => setRetryRevision((current) => current + 1),
    },
  }
}
