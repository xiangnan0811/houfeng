import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { ApiError } from '../lib/api'
import { listCommandAudits } from '../lib/observabilityApi'
import type { CommandAuditAction } from '../lib/types'
import { CommandAuditFilterDrawer } from './command-audit/CommandAuditFilterDrawer'
import { CommandAuditFilterPanel } from './command-audit/CommandAuditFilterPanel'
import { CommandAuditTable } from './command-audit/CommandAuditTable'
import {
  commandAuditFilterKey,
  commandAuditFiltersFromSearchParams,
  commandAuditSearchParamsFromFilters,
  commandAuditToAPIQuery,
  normalizeCommandAuditFilters,
} from './command-audit/filterModel'
import type { CommandAuditFilters, CommandAuditPageState } from './command-audit/types'

const INITIAL_PAGE_STATE: CommandAuditPageState = {
  loading: true,
  loadingMore: false,
  error: null,
}

function describeCommandAuditError(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return '加载命令审计失败'
}

function dedupeCommandAuditActions(current: CommandAuditAction[], incoming: CommandAuditAction[]): CommandAuditAction[] {
  const seen = new Set(current.map((item) => item.id))
  const result = [...current]
  for (const item of incoming) {
    if (seen.has(item.id)) continue
    seen.add(item.id)
    result.push(item)
  }
  return result
}

export function CommandAuditPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const rawSearchKey = searchParams.toString()
  const parsedFilters = useMemo(
    () => commandAuditFiltersFromSearchParams(new URLSearchParams(rawSearchKey)),
    [rawSearchKey],
  )
  const appliedFilterKey = useMemo(() => commandAuditFilterKey(parsedFilters), [parsedFilters])
  const appliedFilters = useMemo(
    () => commandAuditFiltersFromSearchParams(new URLSearchParams(appliedFilterKey)),
    [appliedFilterKey],
  )
  const [primaryDraftState, setPrimaryDraftState] = useState(() => ({
    filterKey: appliedFilterKey,
    filters: appliedFilters,
  }))
  const primaryDraft = primaryDraftState.filterKey === appliedFilterKey
    ? primaryDraftState.filters
    : appliedFilters
  const [advancedDraft, setAdvancedDraft] = useState<CommandAuditFilters>(appliedFilters)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [items, setItems] = useState<CommandAuditAction[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [expandedIDs, setExpandedIDs] = useState<Set<string>>(new Set())
  const [state, setState] = useState<CommandAuditPageState>(INITIAL_PAGE_STATE)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [resultFilterKey, setResultFilterKey] = useState<string | null>(null)
  const requestGeneration = useRef(0)

  useEffect(() => {
    if (rawSearchKey !== appliedFilterKey) {
      setSearchParams(commandAuditSearchParamsFromFilters(appliedFilters), { replace: true })
    }
  }, [appliedFilterKey, appliedFilters, rawSearchKey, setSearchParams])

  useEffect(() => {
    let active = true
    const generation = ++requestGeneration.current

    listCommandAudits(commandAuditToAPIQuery(appliedFilters))
      .then((response) => {
        if (!active || generation !== requestGeneration.current) return
        setItems(Array.isArray(response.items) ? response.items : [])
        setNextCursor(response.next_cursor || undefined)
        setExpandedIDs(new Set())
        setResultFilterKey(appliedFilterKey)
        setState({ loading: false, loadingMore: false, error: null })
      })
      .catch((error: unknown) => {
        if (!active || generation !== requestGeneration.current) return
        setItems([])
        setNextCursor(undefined)
        setExpandedIDs(new Set())
        setResultFilterKey(appliedFilterKey)
        setState({ loading: false, loadingMore: false, error: describeCommandAuditError(error) })
      })

    return () => {
      active = false
    }
  }, [appliedFilterKey, appliedFilters, reloadVersion])

  function updatePrimaryDraft<K extends keyof CommandAuditFilters>(key: K, value: CommandAuditFilters[K]) {
    setPrimaryDraftState((current) => {
      const currentFilters = current.filterKey === appliedFilterKey ? current.filters : appliedFilters
      return {
        filterKey: appliedFilterKey,
        filters: {
          ...currentFilters,
          [key]: value,
          ...(key === 'window' && value !== 'custom' ? { started_from: '', started_to: '' } : {}),
        },
      }
    })
  }

  function commitFilters(filters: CommandAuditFilters) {
    const normalized = normalizeCommandAuditFilters(filters)
    const nextKey = commandAuditFilterKey(normalized)
    setPrimaryDraftState({ filterKey: nextKey, filters: normalized })
    setAdvancedDraft(normalized)
    if (nextKey === appliedFilterKey) return
    requestGeneration.current++
    setItems([])
    setNextCursor(undefined)
    setExpandedIDs(new Set())
    setState({ loading: true, loadingMore: false, error: null })
    setSearchParams(commandAuditSearchParamsFromFilters(normalized))
  }

  function openAdvancedFilters() {
    setAdvancedDraft(primaryDraft)
    setAdvancedOpen(true)
  }

  function closeAdvancedFilters() {
    setAdvancedDraft(appliedFilters)
    setAdvancedOpen(false)
  }

  function updateAdvancedDraft<K extends keyof CommandAuditFilters>(key: K, value: CommandAuditFilters[K]) {
    setAdvancedDraft((current) => ({ ...current, [key]: value }))
  }

  function resetAdvancedFilters() {
    setAdvancedDraft((current) => ({
      ...current,
      sensitivity: '',
      actor: '',
      action_id: '',
    }))
  }

  function applyAdvancedFilters() {
    setAdvancedOpen(false)
    commitFilters(advancedDraft)
  }

  function toggleExpanded(id: string) {
    setExpandedIDs((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function loadMore() {
    if (!nextCursor || state.loadingMore) return
    const cursor = nextCursor
    const generation = requestGeneration.current
    setState((current) => ({ ...current, loadingMore: true, error: null }))
    listCommandAudits({ cursor })
      .then((response) => {
        if (generation !== requestGeneration.current) return
        setItems((current) => dedupeCommandAuditActions(
          current,
          Array.isArray(response.items) ? response.items : [],
        ))
        setNextCursor(response.next_cursor || undefined)
        setState((current) => ({ ...current, loadingMore: false }))
      })
      .catch((error: unknown) => {
        if (generation !== requestGeneration.current) return
        setState((current) => ({
          ...current,
          loadingMore: false,
          error: describeCommandAuditError(error),
        }))
      })
  }

  function retryInitialLoad() {
    requestGeneration.current++
    setItems([])
    setNextCursor(undefined)
    setExpandedIDs(new Set())
    setResultFilterKey(null)
    setState({ loading: true, loadingMore: false, error: null })
    setReloadVersion((value) => value + 1)
  }

  const resultMatchesFilters = resultFilterKey === appliedFilterKey
  const visibleItems = resultMatchesFilters ? items : []
  const visibleError = resultMatchesFilters ? state.error : null
  const visibleNextCursor = resultMatchesFilters ? nextCursor : undefined
  const initialLoading = state.loading || !resultMatchesFilters

  return (
    <div className="page-stack command-audit-page">
      <div className="command-audit-page__header">
        <div>
          <div className="page-eyebrow">命令治理 · COMMAND AUDIT</div>
          <h1 className="page-title">命令审计</h1>
          <p className="page-sub">
            只展示命令、身份、时间和结果元数据，不保存或展示命令输出。
          </p>
        </div>
      </div>

      <CommandAuditFilterPanel
        filters={primaryDraft}
        onChange={updatePrimaryDraft}
        onApply={() => commitFilters(primaryDraft)}
        onOpenAdvanced={openAdvancedFilters}
      />
      <CommandAuditFilterDrawer
        open={advancedOpen}
        filters={advancedDraft}
        onChange={updateAdvancedDraft}
        onApply={applyAdvancedFilters}
        onReset={resetAdvancedFilters}
        onClose={closeAdvancedFilters}
      />

      <section className="page-stack command-audit-results" aria-label="命令审计结果">
        {initialLoading ? <PageState kind="loading" title="正在加载命令审计" /> : null}
        {!initialLoading && visibleError && visibleItems.length === 0 ? (
          <PageState
            kind="error"
            eyebrow="命令审计"
            title="命令审计不可用"
            description="暂时无法读取命令审计元数据。"
            technicalSummary={visibleError}
            action={<Button size="sm" onClick={retryInitialLoad}>重试</Button>}
          />
        ) : null}
        {!initialLoading && !visibleError && visibleItems.length === 0 ? (
          <PageState
            kind="empty"
            eyebrow="命令审计"
            title="没有匹配的命令审计"
            description="当前时间范围和筛选条件下没有命令尝试。"
          />
        ) : null}
        {visibleItems.length > 0 ? (
          <>
            <CommandAuditTable rows={visibleItems} expandedIDs={expandedIDs} onToggle={toggleExpanded} />
            <div className="section-heading__actions command-audit-results__footer">
              {visibleError ? <p role="alert">加载更多失败：{visibleError}</p> : null}
              {visibleNextCursor ? (
                <Button variant="secondary" onClick={loadMore} disabled={state.loadingMore}>
                  {state.loadingMore ? '正在加载…' : '加载更多'}
                </Button>
              ) : <span>已加载当前范围内的全部结果</span>}
            </div>
          </>
        ) : null}
      </section>
    </div>
  )
}

export default CommandAuditPage
