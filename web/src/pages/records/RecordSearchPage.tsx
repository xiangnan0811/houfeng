import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Button } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { ApiError } from '../../lib/apiRequest'
import { searchRecords } from '../../lib/recordsApi'
import type { RecordDetail } from '../../lib/types'
import { RecordSearchFilterDrawer } from './RecordSearchFilterDrawer'
import { RecordSearchFilterPanel } from './RecordSearchFilterPanel'
import { RecordSearchResultsTable } from './RecordSearchResultsTable'
import {
  comparisonEntryHref,
  comparisonSubjectsFromRecords,
} from './compare/comparisonQueryState'
import {
  DEFAULT_RECORD_SEARCH_FILTERS,
  recordSearchFilterKey,
  recordSearchFiltersFromSearchParams,
  recordSearchParamsFromFilters,
  recordSearchToAPIQuery,
  type RecordSearchFilters,
} from './searchFilterModel'

const ADVANCED_FIELDS = [
  'owner', 'participant', 'tag', 'follow_up', 'action',
  'occurred_from', 'occurred_to', 'updated_from', 'updated_to',
] as const

type SearchFailure = {
  message: string
  /** True when the index has not been built yet, which retrying can resolve. */
  indexUnavailable: boolean
}

function describeSearchFailure(error: unknown): SearchFailure {
  if (error instanceof ApiError) {
    if (error.code === 'search_unavailable') {
      return { message: '记录搜索索引尚未就绪。', indexUnavailable: true }
    }
    if (error.code === 'query_invalid') {
      return { message: '搜索条件无法被服务端接受。', indexUnavailable: false }
    }
    return { message: error.message, indexUnavailable: false }
  }
  if (error instanceof Error) return { message: error.message, indexUnavailable: false }
  return { message: '搜索记录失败', indexUnavailable: false }
}

function generationSuperseded(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'search_generation_superseded'
}

function dedupeRecords(current: RecordDetail[], incoming: RecordDetail[]): RecordDetail[] {
  const seen = new Set(current.map((record) => record.record_id))
  const result = [...current]
  for (const record of incoming) {
    if (seen.has(record.record_id)) continue
    seen.add(record.record_id)
    result.push(record)
  }
  return result
}

export function RecordSearchPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const rawSearchKey = searchParams.toString()
  const parsedFilters = useMemo(
    () => recordSearchFiltersFromSearchParams(new URLSearchParams(rawSearchKey)),
    [rawSearchKey],
  )
  const appliedFilterKey = useMemo(() => recordSearchFilterKey(parsedFilters), [parsedFilters])
  const appliedFilters = useMemo(
    () => recordSearchFiltersFromSearchParams(new URLSearchParams(appliedFilterKey)),
    [appliedFilterKey],
  )

  const [draftState, setDraftState] = useState(() => ({
    filterKey: appliedFilterKey,
    filters: appliedFilters,
  }))
  const draft = draftState.filterKey === appliedFilterKey ? draftState.filters : appliedFilters
  const [advancedDraft, setAdvancedDraft] = useState<RecordSearchFilters>(appliedFilters)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const [records, setRecords] = useState<RecordDetail[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [failure, setFailure] = useState<SearchFailure | null>(null)
  const [republished, setRepublished] = useState(false)
  const [resultFilterKey, setResultFilterKey] = useState<string | null>(null)
  const [reloadVersion, setReloadVersion] = useState(0)
  const requestGeneration = useRef(0)

  useEffect(() => {
    if (rawSearchKey !== appliedFilterKey) {
      setSearchParams(recordSearchParamsFromFilters(appliedFilters), { replace: true })
    }
  }, [appliedFilterKey, appliedFilters, rawSearchKey, setSearchParams])

  useEffect(() => {
    let active = true
    const generation = ++requestGeneration.current

    searchRecords(recordSearchToAPIQuery(appliedFilters))
      .then((response) => {
        if (!active || generation !== requestGeneration.current) return
        setRecords(Array.isArray(response.items) ? response.items : [])
        setNextCursor(response.next_cursor || undefined)
        setResultFilterKey(appliedFilterKey)
        setLoading(false)
        setFailure(null)
      })
      .catch((cause: unknown) => {
        if (!active || generation !== requestGeneration.current) return
        setRecords([])
        setNextCursor(undefined)
        setResultFilterKey(appliedFilterKey)
        setLoading(false)
        setFailure(describeSearchFailure(cause))
      })

    return () => {
      active = false
    }
  }, [appliedFilterKey, appliedFilters, reloadVersion])

  const restartSearch = useCallback(() => {
    requestGeneration.current++
    setRecords([])
    setNextCursor(undefined)
    setResultFilterKey(null)
    setLoading(true)
    setLoadingMore(false)
    setFailure(null)
    setReloadVersion((value) => value + 1)
  }, [])

  function commitFilters(filters: RecordSearchFilters) {
    const params = recordSearchParamsFromFilters(filters)
    const normalized = recordSearchFiltersFromSearchParams(params)
    const nextKey = recordSearchFilterKey(normalized)
    setDraftState({ filterKey: nextKey, filters: normalized })
    setAdvancedDraft(normalized)
    setRepublished(false)
    if (nextKey === appliedFilterKey) {
      restartSearch()
      return
    }
    requestGeneration.current++
    setRecords([])
    setNextCursor(undefined)
    setLoading(true)
    setLoadingMore(false)
    setFailure(null)
    setSearchParams(params)
  }

  function updateDraft(filters: RecordSearchFilters) {
    setDraftState({ filterKey: appliedFilterKey, filters })
  }

  function loadMore() {
    if (!nextCursor || loadingMore) return
    const cursor = nextCursor
    const generation = requestGeneration.current
    setLoadingMore(true)
    setFailure(null)
    // The cursor is bound to the query digest, so the filters travel with it.
    searchRecords(recordSearchToAPIQuery(appliedFilters, cursor))
      .then((response) => {
        if (generation !== requestGeneration.current) return
        setRecords((current) => dedupeRecords(
          current,
          Array.isArray(response.items) ? response.items : [],
        ))
        setNextCursor(response.next_cursor || undefined)
        setLoadingMore(false)
      })
      .catch((cause: unknown) => {
        if (generation !== requestGeneration.current) return
        // A republished index invalidates the cursor. Stitching its pages onto
        // results from the retired generation could duplicate or drop records,
        // so the only honest recovery is to read the query again from the top.
        if (generationSuperseded(cause)) {
          setRepublished(true)
          restartSearch()
          return
        }
        setLoadingMore(false)
        setFailure(describeSearchFailure(cause))
      })
  }

  function openAdvanced() {
    setAdvancedDraft(draft)
    setAdvancedOpen(true)
  }

  function closeAdvanced() {
    setAdvancedDraft(appliedFilters)
    setAdvancedOpen(false)
  }

  function resetAdvanced() {
    setAdvancedDraft((current) => {
      const next = { ...current }
      for (const field of ADVANCED_FIELDS) delete next[field]
      return next
    })
  }

  const resultsMatchFilters = resultFilterKey === appliedFilterKey
  const visibleRecords = resultsMatchFilters ? records : []
  const visibleFailure = resultsMatchFilters ? failure : null
  const visibleNextCursor = resultsMatchFilters ? nextCursor : undefined
  const initialLoading = loading || !resultsMatchFilters

  return (
    <div className="page-stack record-search-page">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">运维知识 · RECORDS</div>
          <h1 className="page-title">运维记录</h1>
          <p className="page-sub">
            搜索命中标题与正文，结果始终按当前授权过滤，只显示你有权读取的记录。
          </p>
        </div>
        <div className="header-actions">
          <Link className="btn sm secondary" to={comparisonEntryHref({
            subjects: comparisonSubjectsFromRecords(visibleRecords),
          })}>横向比较</Link>
          <Link className="btn sm secondary" to="/records/drafts">草稿</Link>
          <Link className="btn sm primary" to="/records/new">新建记录</Link>
        </div>
      </div>

      <RecordSearchFilterPanel
        filters={draft}
        onChange={updateDraft}
        onApply={() => commitFilters(draft)}
        onClear={() => commitFilters(DEFAULT_RECORD_SEARCH_FILTERS)}
        onOpenAdvanced={openAdvanced}
      />
      <RecordSearchFilterDrawer
        open={advancedOpen}
        filters={advancedDraft}
        onChange={setAdvancedDraft}
        onApply={() => {
          setAdvancedOpen(false)
          commitFilters(advancedDraft)
        }}
        onReset={resetAdvanced}
        onClose={closeAdvanced}
      />

      <section className="page-stack record-search-results" aria-label="记录搜索结果">
        {republished ? (
          <p role="status" className="page-sub record-search-results__notice">
            搜索索引已重建，已从第一页重新读取当前条件的结果。
          </p>
        ) : null}
        {initialLoading ? <PageState kind="loading" title="正在搜索记录" /> : null}
        {!initialLoading && visibleFailure && visibleRecords.length === 0 ? (
          <PageState
            kind="error"
            eyebrow="运维记录"
            title={visibleFailure.indexUnavailable ? '记录搜索暂不可用' : '搜索失败'}
            description={visibleFailure.message}
            action={<Button size="sm" onClick={restartSearch}>重试</Button>}
          />
        ) : null}
        {!initialLoading && !visibleFailure && visibleRecords.length === 0 ? (
          <PageState
            kind="empty"
            eyebrow="运维记录"
            title="没有匹配的记录"
            description="调整关键词或筛选条件后再试。"
          />
        ) : null}
        {visibleRecords.length > 0 ? (
          <>
            <RecordSearchResultsTable rows={visibleRecords} />
            <div className="section-heading__actions record-search-results__footer">
              {visibleFailure ? <p role="alert">加载更多失败：{visibleFailure.message}</p> : null}
              {visibleNextCursor ? (
                <Button variant="secondary" onClick={loadMore} disabled={loadingMore}>
                  {loadingMore ? '正在加载…' : '加载更多'}
                </Button>
              ) : <span>已加载当前条件下的全部结果</span>}
            </div>
          </>
        ) : null}
      </section>
    </div>
  )
}
