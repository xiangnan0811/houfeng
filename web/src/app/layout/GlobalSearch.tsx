import { useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import type { AssetSearchOutcome, SearchResult } from './globalAssetSearch'

type ResultGroup = {
  kind: SearchResult['kind']
  label: string
  results: SearchResult[]
}

type SearchSession = {
  generation: number
  submittedQuery: string | null
  resultQuery: string | null
  assetMatches: SearchResult[]
  recordMatches: SearchResult[]
  assetError: string | null
  recordError: string | null
  assetPending: boolean
  recordPending: boolean
  focusIndex: number
}

/**
 * Records get their own quota rather than sharing the asset cap: they are ranked
 * by the server, and a query matching many assets should not push every record
 * out of the palette.
 */
const RECORD_RESULTS = 4

/** Fixed copy. Raw source errors stay off the palette. */
const ASSET_SEARCH_UNAVAILABLE = '资产搜索暂不可用'
const RECORD_SEARCH_UNAVAILABLE = '运维记录搜索暂不可用'

const SEARCH_GROUP_LABELS: Record<SearchResult['kind'], string> = {
  vps: 'VPS',
  monitoring_instance: '监控实例',
  target: '入口探测',
  provider: '服务商',
  subscription: '订阅',
  record: '运维记录',
}

const SEARCH_GROUP_ORDER: SearchResult['kind'][] = ['vps', 'monitoring_instance', 'target', 'provider', 'subscription', 'record']

// 快捷键提示按平台显示；两种组合键都由下方 keydown 处理。
const SEARCH_SHORTCUT_LABEL =
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent) ? '⌘K' : 'Ctrl K'

function emptySession(
  generation: number,
  submittedQuery: string | null,
  pending: { asset: boolean; record: boolean } = { asset: false, record: false },
): SearchSession {
  return {
    generation,
    submittedQuery,
    resultQuery: null,
    assetMatches: [],
    recordMatches: [],
    assetError: null,
    recordError: null,
    assetPending: pending.asset,
    recordPending: pending.record,
    focusIndex: -1,
  }
}

/** Results are visible only for the query that was submitted and is still in the field. */
function resultsFor(session: SearchSession, rawQuery: string, recordsEnabled: boolean): SearchResult[] {
  const trimmed = rawQuery.trim()
  if (session.submittedQuery !== trimmed || session.resultQuery !== trimmed) return []
  const combined = [...session.assetMatches, ...(recordsEnabled ? session.recordMatches : [])]
  return recordsEnabled ? combined : combined.filter((result) => result.kind !== 'record')
}

/** Global command search with ⌘K / Ctrl+K shortcut. */
export function GlobalSearch({ recordsEnabled = true }: { recordsEnabled?: boolean }) {
  const navigate = useNavigate()
  const baseId = useId()
  const listboxId = `${baseId}-listbox`
  const helpId = `${baseId}-help`
  const statusId = `${baseId}-status`
  const loadingId = `${baseId}-loading`
  const assetErrorId = `${baseId}-asset-error`
  const recordErrorId = `${baseId}-record-error`

  const generationRef = useRef(0)
  const submittedRef = useRef<string | null>(null)
  const queryRef = useRef('')
  const mountedRef = useRef(true)
  const recordsEnabledRef = useRef(recordsEnabled)
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState<SearchSession>(() => emptySession(0, null))
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // 卸载只作废世代。结果属于已卸下的实例，不能再落地。
  useLayoutEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      generationRef.current += 1
    }
  }, [])

  // 能力切换在绘制前清掉上一轮结果。在途响应的世代已经对不上。
  useLayoutEffect(() => {
    if (recordsEnabledRef.current === recordsEnabled) return
    recordsEnabledRef.current = recordsEnabled
    generationRef.current += 1
    submittedRef.current = null
    setSearch(emptySession(generationRef.current, null))
  }, [recordsEnabled])

  function handleContainerBlur(e: React.FocusEvent<HTMLDivElement>) {
    const nextTarget = e.relatedTarget as Node | null
    if (nextTarget && containerRef.current?.contains(nextTarget)) {
      return
    }
    setOpen(false)
  }
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  // 触屏浏览器（如 iOS Safari）点空白处未必移走焦点；点搜索区以外时主动失焦，
  // 让窄屏展开的搜索框按 :focus-within 收回，不再盖住标题与顶栏按钮。
  useEffect(() => {
    const blurOutside = (e: PointerEvent) => {
      const input = inputRef.current
      if (input && document.activeElement === input && !containerRef.current?.contains(e.target as Node)) {
        input.blur()
      }
    }
    document.addEventListener('pointerdown', blurOutside)
    return () => document.removeEventListener('pointerdown', blurOutside)
  }, [])

  useEffect(() => {
    function onKeyDown(e: globalThis.KeyboardEvent) {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key === 'k') {
        e.preventDefault()
        if (open) {
          setOpen(false)
        } else {
          setOpen(true)
          requestAnimationFrame(() => inputRef.current?.focus())
        }
        return
      }
      if (e.key === 'Escape' && open) {
        setOpen(false)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open])

  const session = search

  function discardInFlight() {
    generationRef.current += 1
    submittedRef.current = null
    setSearch(emptySession(generationRef.current, null))
  }

  function commitSource(
    source: 'asset' | 'record',
    generation: number,
    searched: string,
    matches: SearchResult[],
    error: string | null,
  ) {
    if (!mountedRef.current || generationRef.current !== generation) return
    if (submittedRef.current !== searched || queryRef.current.trim() !== searched) return
    setSearch((prev) => {
      if (!mountedRef.current || generationRef.current !== generation || prev.generation !== generation) return prev
      if (submittedRef.current !== searched || queryRef.current.trim() !== searched) return prev
      const next: SearchSession = {
        ...prev,
        resultQuery: searched,
        ...(source === 'asset'
          ? { assetMatches: matches, assetError: error, assetPending: false }
          : { recordMatches: matches, recordError: error, recordPending: false }),
      }
      const count = resultsFor(next, searched, recordsEnabledRef.current).length
      const focusStillValid = prev.focusIndex >= 0 && prev.focusIndex < count
      next.focusIndex = count === 0 ? -1 : focusStillValid ? prev.focusIndex : 0
      return next
    })
  }

  // 动态 import 可以在换代、改词、卸载或关掉记录能力之后才返回。
  // 对不上就不要再发源请求；已经发出的请求仍由 commitSource 丢掉晚到结果。
  function sourceRequestReady(generation: number, searched: string, records: boolean) {
    if (!mountedRef.current || generationRef.current !== generation) return false
    if (submittedRef.current !== searched || queryRef.current.trim() !== searched) return false
    return !records || recordsEnabledRef.current
  }

  async function runAssetSearch(generation: number, searched: string) {
    let matches: SearchResult[] = []
    let error: string | null = null
    try {
      const module = await import('./globalAssetSearch')
      if (!sourceRequestReady(generation, searched, false)) return
      const outcome: AssetSearchOutcome = await module.searchAssets(searched.toLowerCase())
      matches = outcome.matches
      error = outcome.error ? ASSET_SEARCH_UNAVAILABLE : null
    } catch {
      matches = []
      error = ASSET_SEARCH_UNAVAILABLE
    }
    commitSource('asset', generation, searched, matches, error)
  }

  async function runRecordSearch(generation: number, searched: string) {
    let matches: SearchResult[] = []
    let error: string | null = null
    try {
      // The records transport is reached only through this dynamic import, which
      // keeps it out of the eager shell bundle. Closed records capability must
      // not import or fetch that module.
      const module = await import('../../pages/records/globalRecordSearch')
      if (!sourceRequestReady(generation, searched, true)) return
      const outcome = await module.searchRecordsForGlobalSearch(searched, RECORD_RESULTS)
      matches = outcome.matches.map((hit) => ({ kind: 'record' as const, ...hit }))
      error = outcome.error ? RECORD_SEARCH_UNAVAILABLE : null
    } catch {
      matches = []
      error = RECORD_SEARCH_UNAVAILABLE
    }
    commitSource('record', generation, searched, matches, error)
  }

  function handleSearch(e: FormEvent) {
    e.preventDefault()
    const typed = queryRef.current.trim()
    const generation = ++generationRef.current
    submittedRef.current = typed
    const records = recordsEnabledRef.current
    setSearch(emptySession(generation, typed, {
      asset: typed.length > 0,
      record: typed.length > 0 && records,
    }))
    setOpen(true)
    if (!typed) return
    void runAssetSearch(generation, typed)
    if (records) void runRecordSearch(generation, typed)
  }

  function clearSearch() {
    queryRef.current = ''
    setQuery('')
    discardInFlight()
    setOpen(false)
  }

  function activate(result: SearchResult) {
    clearSearch()
    navigate(result.to)
  }

  const trimmed = query.trim()
  const aligned = session.submittedQuery === trimmed
  const loading = aligned && (session.assetPending || (recordsEnabled && session.recordPending))
  const assetError = aligned ? session.assetError : null
  const recordError = aligned && recordsEnabled ? session.recordError : null
  const displayedResults = resultsFor(session, query, recordsEnabled)
  const showNoMatches = aligned
    && session.resultQuery === trimmed
    && trimmed !== ''
    && !loading
    && !assetError
    && !recordError
    && displayedResults.length === 0
  const showUnsubmitted = trimmed !== '' && session.submittedQuery !== trimmed
  const showCapabilities = trimmed === ''
  const showEmptySubmitted = showCapabilities && session.submittedQuery === ''

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      if (open) {
        e.preventDefault()
        setOpen(false)
      }
      return
    }

    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        setOpen(true)
      }
      return
    }

    if (displayedResults.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSearch((prev) => {
          const count = resultsFor(prev, queryRef.current, recordsEnabledRef.current).length
          if (count === 0 || prev.focusIndex < 0) return prev
          return { ...prev, focusIndex: (prev.focusIndex + 1) % count }
        })
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSearch((prev) => {
          const count = resultsFor(prev, queryRef.current, recordsEnabledRef.current).length
          if (count === 0 || prev.focusIndex < 0) return prev
          return { ...prev, focusIndex: (prev.focusIndex - 1 + count) % count }
        })
      } else if (e.key === 'Enter' && session.focusIndex >= 0) {
        const focusedResult = displayedResults[session.focusIndex]
        if (focusedResult) {
          e.preventDefault()
          activate(focusedResult)
        }
      }
    }
  }

  function handleQueryChange(value: string) {
    if (value === queryRef.current) return
    queryRef.current = value
    setQuery(value)
    discardInFlight()
  }

  const groups = groupResults(displayedResults)
  const hasOptions = open && displayedResults.length > 0
  const activeOptionId = hasOptions && session.focusIndex >= 0 && displayedResults[session.focusIndex]
    ? `${baseId}-option-${session.focusIndex}`
    : undefined

  let describedBy: string | undefined
  if (open) {
    const describedIds: string[] = []
    if (showCapabilities) describedIds.push(helpId)
    if (loading) describedIds.push(loadingId)
    if (assetError) describedIds.push(assetErrorId)
    if (recordError) describedIds.push(recordErrorId)
    if (showEmptySubmitted || showUnsubmitted || showNoMatches) describedIds.push(statusId)
    if (describedIds.length > 0) describedBy = describedIds.join(' ')
  }

  return (
    <div className="global-search" ref={containerRef} onBlur={handleContainerBlur}>
      <form onSubmit={handleSearch} role="search" className="global-search__form">
        <svg className="global-search__icon" viewBox="0 0 16 16" aria-hidden="true">
          <circle cx="7" cy="7" r="4.5" />
          <path d="M10.5 10.5L14 14" />
        </svg>
        <input
          ref={inputRef}
          id={`${baseId}-input`}
          type="search"
          className="global-search__input"
          placeholder={recordsEnabled ? '搜索 VPS、IP、记录…' : '搜索 VPS、IP…'}
          aria-keyshortcuts="Control+K Meta+K"
          value={query}
          onChange={(e) => handleQueryChange(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => setOpen(true)}
          aria-label="全局搜索"
          role="combobox"
          aria-expanded={hasOptions}
          aria-haspopup="listbox"
          aria-autocomplete="list"
          aria-controls={hasOptions ? listboxId : undefined}
          aria-activedescendant={activeOptionId}
          aria-describedby={describedBy}
        />
        <kbd className="global-search__kbd" aria-hidden="true">{SEARCH_SHORTCUT_LABEL}</kbd>
      </form>
      {open && (
        <div
          className="global-search__menu"
          role="region"
          aria-label="搜索面板"
          tabIndex={hasOptions ? 0 : undefined}
          onMouseDown={(e) => {
            // Prevent blurring search input when interacting with menu
            e.preventDefault()
          }}
        >
          {loading ? (
            <p id={loadingId} className="global-search__hint" role="status" aria-live="polite">
              正在加载…
            </p>
          ) : null}
          {assetError ? (
            <p id={assetErrorId} className="global-search__hint global-search__hint--error" role="alert">
              {assetError}
            </p>
          ) : null}
          {recordError ? (
            <p id={recordErrorId} className="global-search__hint global-search__hint--error" role="alert">
              {recordError}
            </p>
          ) : null}
          {showUnsubmitted ? (
            <p id={statusId} className="global-search__hint" role="status">
              按 Enter 搜索
            </p>
          ) : null}
          {showNoMatches ? (
            <p id={statusId} className="global-search__hint" role="status">
              没有匹配项
            </p>
          ) : null}
          {showCapabilities ? (
            <div id={helpId} className="global-search__hint global-search__capabilities">
              <p className="global-search__capabilities-title">支持检索范围</p>
              <p className="global-search__capabilities-text">
                {recordsEnabled
                  ? 'VPS · 监控实例 · 入口探测 · 服务商 · 订阅 · 运维记录'
                  : 'VPS · 监控实例 · 入口探测 · 服务商 · 订阅'}
              </p>
              <p
                id={showEmptySubmitted ? statusId : undefined}
                className="global-search__capabilities-sub"
                role={showEmptySubmitted ? 'status' : undefined}
              >
                {showEmptySubmitted
                  ? '请输入搜索关键词 · 支持 ⌘K / Ctrl+K'
                  : '按 Enter 搜索 · 支持 ⌘K / Ctrl+K'}
              </p>
            </div>
          ) : null}
          {hasOptions ? (
            <>
              <span className="visually-hidden" role="status" aria-live="polite">
                找到 {displayedResults.length} 个结果
              </span>
              <div id={listboxId} role="listbox" aria-label="搜索结果">
              {groups.map((group) => {
                const groupId = `${baseId}-group-${group.kind}`
                return (
                  <div
                    className="global-search__group"
                    key={group.kind}
                    role="group"
                    aria-labelledby={groupId}
                  >
                    <p id={groupId} className="global-search__group-title">
                      {group.label}
                     </p>
                    {group.results.map((result) => {
                      const index = displayedResults.indexOf(result)
                      const optionId = `${baseId}-option-${index}`
                      const isFocused = index === session.focusIndex
                      return (
                        <Link
                          key={`${result.kind}-${result.id}`}
                          id={optionId}
                          to={result.to}
                          role="option"
                          aria-selected={isFocused}
                          tabIndex={-1}
                          className={`global-search__item ${isFocused ? 'is-focused' : ''}`}
                          onClick={clearSearch}
                          onMouseEnter={() => {
                            setSearch((prev) => prev.focusIndex === index ? prev : { ...prev, focusIndex: index })
                          }}
                        >
                          <span className="global-search__item-kind">
                            {SEARCH_GROUP_LABELS[result.kind]}
                          </span>
                          <span className="global-search__item-label">{result.label}</span>
                          {result.hint ? (
                            <span className="global-search__item-hint">{result.hint}</span>
                          ) : null}
                        </Link>
                      )
                    })}
                  </div>
                )
              })}
              </div>
            </>
          ) : null}
        </div>
      )}
    </div>
  )
}

function groupResults(results: SearchResult[]): ResultGroup[] {
  return SEARCH_GROUP_ORDER.map((kind) => ({
    kind,
    label: SEARCH_GROUP_LABELS[kind],
    results: results.filter((result) => result.kind === kind),
  })).filter((group) => group.results.length > 0)
}
