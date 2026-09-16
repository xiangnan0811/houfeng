import { useEffect, useId, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listMonitoringInstances, listProviders, listSubscriptions, listTargets, listVPSAssets } from '../../lib/api'
import { formatDate, formatMoney } from '../../lib/format'
import type { MonitoringInstanceRecord, ProviderRecord, SubscriptionRecord, TargetRecord, VPSAssetRecord } from '../../lib/types'

interface SearchResult {
  kind: 'vps' | 'monitoring_instance' | 'target' | 'provider' | 'subscription' | 'record'
  id: string
  label: string
  hint: string
  to: string
}

type ResultGroup = {
  kind: SearchResult['kind']
  label: string
  results: SearchResult[]
}

/** Cap on the client-side asset matches, which are unranked. */
const MAX_RESULTS = 10

/**
 * Records get their own quota rather than sharing the asset cap: they are ranked
 * by the server, and a query matching many assets should not push every record
 * out of the palette.
 */
const RECORD_RESULTS = 4

const SEARCH_GROUP_LABELS: Record<SearchResult['kind'], string> = {
  vps: 'VPS',
  monitoring_instance: '监控实例',
  target: '入口探测',
  provider: '服务商',
  subscription: '订阅',
  record: '运维记录',
}

const SEARCH_GROUP_ORDER: SearchResult['kind'][] = ['vps', 'monitoring_instance', 'target', 'provider', 'subscription', 'record']

/** Global command search with ⌘K / Ctrl+K shortcut. */
export function GlobalSearch() {
  const navigate = useNavigate()
  const baseId = useId()
  const listboxId = `${baseId}-listbox`
  const helpId = `${baseId}-help`
  const statusId = `${baseId}-status`

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [focusIndex, setFocusIndex] = useState(-1)
  const [emptySubmitted, setEmptySubmitted] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const searchGeneration = useRef(0)

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

  async function handleSearch(e: FormEvent) {
    e.preventDefault()
    const typed = query.trim()
    if (!typed) {
      setResults([])
      setFocusIndex(-1)
      setEmptySubmitted(true)
      setOpen(true)
      return
    }
    const generation = ++searchGeneration.current
    setOpen(true)
    setLoading(true)
    setError(null)
    setEmptySubmitted(false)

    const [assets, recordHits] = await Promise.all([
      searchAssets(typed.toLowerCase()),
      // The records transport is reached only through this dynamic import, which
      // keeps it out of the eager shell bundle. Its own failures resolve to no
      // hits, so an index that is still building leaves the palette usable.
      import('../../pages/records/globalRecordSearch')
        .then((module) => module.searchRecordsForGlobalSearch(typed, RECORD_RESULTS))
        .catch(() => []),
    ])
    if (generation !== searchGeneration.current) return

    const matches = [
      ...assets.matches,
      ...recordHits.map((hit) => ({ kind: 'record' as const, ...hit })),
    ]
    setResults(matches)
    setError(assets.error)
    setFocusIndex(matches.length > 0 ? 0 : -1)
    setLoading(false)
  }

  function clearSearch() {
    setOpen(false)
    setQuery('')
    setResults([])
    setFocusIndex(-1)
    setEmptySubmitted(false)
  }

  function activate(result: SearchResult) {
    clearSearch()
    navigate(result.to)
  }

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

    if (!loading && results.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setFocusIndex((i) => (i + 1) % results.length)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setFocusIndex((i) => (i - 1 + results.length) % results.length)
      } else if (e.key === 'Enter' && focusIndex >= 0) {
        e.preventDefault()
        const focusedResult = results[focusIndex]
        if (focusedResult) activate(focusedResult)
      }
    }
  }

  const groups = groupResults(results)
  const hasOptions = open && !loading && results.length > 0
  const activeOptionId = hasOptions && focusIndex >= 0 && results[focusIndex]
    ? `${baseId}-option-${focusIndex}`
    : undefined

  let describedBy: string | undefined
  if (open) {
    if (loading || error) {
      describedBy = statusId
    } else if (results.length === 0) {
      if (query.trim()) {
        describedBy = statusId
      } else {
        describedBy = emptySubmitted ? `${helpId} ${statusId}` : helpId
      }
    }
  }

  return (
    <div className="global-search" ref={containerRef} onBlur={handleContainerBlur}>
      <form onSubmit={handleSearch} role="search">
        <input
          ref={inputRef}
          id={`${baseId}-input`}
          type="search"
          className="global-search__input"
          placeholder="搜索"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setEmptySubmitted(false)
          }}
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
            <p id={statusId} className="global-search__hint" role="status" aria-live="polite">
              正在加载…
            </p>
          ) : (
            <>
              {/* A partial failure keeps whatever did answer instead of discarding it. */}
              {error ? (
                <p id={statusId} className="global-search__hint global-search__hint--error" role="alert">
                  {error}
                </p>
              ) : null}
              {!error && results.length === 0 ? (
                query.trim() ? (
                  <p id={statusId} className="global-search__hint" role="status">
                    没有匹配项
                  </p>
                ) : (
                  <div id={helpId} className="global-search__hint global-search__capabilities">
                    <p className="global-search__capabilities-title">支持检索范围</p>
                    <p className="global-search__capabilities-text">
                      VPS · 监控实例 · 入口探测 · 服务商 · 订阅 · 运维记录
                    </p>
                    <p
                      id={emptySubmitted ? statusId : undefined}
                      className="global-search__capabilities-sub"
                      role={emptySubmitted ? 'status' : undefined}
                    >
                      {emptySubmitted
                        ? '请输入搜索关键词 · 支持 ⌘K / Ctrl+K'
                        : '按 Enter 搜索 · 支持 ⌘K / Ctrl+K'}
                    </p>
                  </div>
                )
              ) : null}
              {hasOptions ? (
                <>
                  <span className="visually-hidden" role="status" aria-live="polite">
                    找到 {results.length} 个结果
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
                          const index = results.indexOf(result)
                          const optionId = `${baseId}-option-${index}`
                          const isFocused = index === focusIndex
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
                              onMouseEnter={() => setFocusIndex(index)}
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
            </>
          )}
        </div>
      )}
    </div>
  )
}

type AssetSearchOutcome = {
  matches: SearchResult[]
  error: string | null
}

async function searchAssets(q: string): Promise<AssetSearchOutcome> {
  try {
    const [vpsAssets, monitoring, targets, providers, subscriptions] = await Promise.all([
      listVPSAssets(),
      listMonitoringInstances(),
      listTargets(),
      listProviders(),
      listSubscriptions({ sort: 'renew_at', order: 'asc' }),
    ])
    return {
      matches: combine(vpsAssets, monitoring, targets, providers, subscriptions, q)
        .slice(0, MAX_RESULTS),
      error: null,
    }
  } catch (err) {
    return { matches: [], error: err instanceof Error ? err.message : '搜索失败' }
  }
}

function combine(
  vpsAssets: VPSAssetRecord[],
  monitoring: MonitoringInstanceRecord[],
  targets: TargetRecord[],
  providers: ProviderRecord[],
  subscriptions: SubscriptionRecord[],
  q: string,
): SearchResult[] {
  const out: SearchResult[] = []
  for (const vps of vpsAssets) {
    if (matchesVPS(vps, q)) {
      out.push({
        kind: 'vps',
        id: vps.vps_id,
        label: vps.display_name || vps.vps_id,
        hint: compactHint([vps.provider_name, vps.region || vps.city, vps.ssh_host || vps.ipv4]),
        to: `/vps/${vps.vps_id}`,
      })
    }
  }
  for (const monitoringInstance of monitoring) {
    if (matchesMonitoringInstance(monitoringInstance, q)) {
      out.push({
        kind: 'monitoring_instance',
        id: monitoringInstance.monitoring_instance_id,
        label: monitoringInstance.display_name || monitoringInstance.monitoring_instance_id,
        hint: compactHint([monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider]) || monitoringInstance.monitoring_instance_id,
        to: `/monitoring/${monitoringInstance.monitoring_instance_id}`,
      })
    }
  }
  for (const target of targets) {
    if (matchesTarget(target, q)) {
      out.push({
        kind: 'target',
        id: target.target_id,
        label: target.name || target.target_id,
        hint: compactHint([target.host, target.base_port ? String(target.base_port) : null]),
        to: `/targets/${target.target_id}`,
      })
    }
  }
  for (const provider of providers) {
    if (matchesProvider(provider, q)) {
      out.push({
        kind: 'provider',
        id: provider.provider_id,
        label: provider.name || provider.provider_id,
        hint: compactHint([provider.country, provider.account_hint]),
        to: '/providers',
      })
    }
  }
  for (const subscription of subscriptions) {
    if (matchesSubscription(subscription, q)) {
      out.push({
        kind: 'subscription',
        id: subscription.subscription_id,
        label: subscription.subscription_id,
        hint: compactHint([
          subscription.vps_id,
          formatMoney(subscription.monthly_price, subscription.currency),
          subscription.renew_at ? `续费 ${formatDate(subscription.renew_at)}` : null,
        ]),
        to: `/subscriptions?vps_id=${encodeURIComponent(subscription.vps_id)}&view=details`,
      })
    }
  }
  return out
}

function groupResults(results: SearchResult[]): ResultGroup[] {
  return SEARCH_GROUP_ORDER.map((kind) => ({
    kind,
    label: SEARCH_GROUP_LABELS[kind],
    results: results.filter((result) => result.kind === kind),
  })).filter((group) => group.results.length > 0)
}

function matchesVPS(vps: VPSAssetRecord, q: string): boolean {
  return (
    includesLower(vps.display_name, q) ||
    includesLower(vps.vps_id, q) ||
    includesLower(vps.provider_name, q) ||
    includesLower(vps.product_name, q) ||
    includesLower(vps.order_ref, q) ||
    includesLower(vps.country, q) ||
    includesLower(vps.region, q) ||
    includesLower(vps.city, q) ||
    includesLower(vps.datacenter, q) ||
    includesLower(vps.ipv4, q) ||
    includesLower(vps.ipv6, q) ||
    includesLower(vps.ssh_host, q) ||
    (vps.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesMonitoringInstance(monitoringInstance: MonitoringInstanceRecord, q: string): boolean {
  return (
    includesLower(monitoringInstance.display_name, q) ||
    includesLower(monitoringInstance.monitoring_instance_id, q) ||
    includesLower(monitoringInstance.region, q) ||
    includesLower(monitoringInstance.city, q) ||
    includesLower(monitoringInstance.provider, q) ||
    (monitoringInstance.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesTarget(target: TargetRecord, q: string): boolean {
  return (
    includesLower(target.name, q) ||
    includesLower(target.target_id, q) ||
    includesLower(target.host, q) ||
    (target.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesProvider(provider: ProviderRecord, q: string): boolean {
  return (
    includesLower(provider.name, q) ||
    includesLower(provider.provider_id, q) ||
    includesLower(provider.country, q) ||
    includesLower(provider.account_hint, q) ||
    (provider.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesSubscription(subscription: SubscriptionRecord, q: string): boolean {
  return (
    includesLower(subscription.subscription_id, q) ||
    includesLower(subscription.vps_id, q) ||
    includesLower(subscription.currency, q) ||
    includesLower(subscription.payment_method, q) ||
    includesLower(subscription.note, q) ||
    includesLower(subscription.renew_at, q)
  )
}

function compactHint(parts: Array<string | null | undefined>): string {
  return parts.filter((part) => part && part.trim()).join(' · ')
}

function includesLower(value: string | undefined | null, q: string): boolean {
  if (!value) return false
  return value.toLowerCase().includes(q)
}
