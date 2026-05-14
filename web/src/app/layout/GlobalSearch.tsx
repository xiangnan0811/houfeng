import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listNodes, listProviders, listSubscriptions, listTargets, listVPSAssets } from '../../lib/api'
import { formatDate, formatMoney } from '../../lib/format'
import type { NodeRecord, ProviderRecord, SubscriptionRecord, TargetRecord, VPSAssetRecord } from '../../lib/types'

interface SearchResult {
  kind: 'vps' | 'node' | 'target' | 'provider' | 'subscription'
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

const MAX_RESULTS = 10

const SEARCH_GROUP_LABELS: Record<SearchResult['kind'], string> = {
  vps: 'VPS',
  node: '节点',
  target: '入口探测',
  provider: '服务商',
  subscription: '订阅',
}

const SEARCH_GROUP_ORDER: SearchResult['kind'][] = ['vps', 'node', 'target', 'provider', 'subscription']

/** Global command search with ⌘K / Ctrl+K shortcut. */
export function GlobalSearch() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [focusIndex, setFocusIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

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
    const q = query.trim().toLowerCase()
    if (!q) {
      setResults([])
      setOpen(false)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const [vpsAssets, nodes, targets, providers, subscriptions] = await Promise.all([
        listVPSAssets(),
        listNodes(),
        listTargets(),
        listProviders(),
        listSubscriptions({ sort: 'renew_at', order: 'asc' }),
      ])
      const matches = combine(vpsAssets, nodes, targets, providers, subscriptions, q).slice(0, MAX_RESULTS)
      setResults(matches)
      setOpen(true)
      setFocusIndex(matches.length > 0 ? 0 : -1)
    } catch (err) {
      const message = err instanceof Error ? err.message : '搜索失败'
      setError(message)
      setResults([])
      setOpen(true)
    } finally {
      setLoading(false)
    }
  }

  function clearSearch() {
    setOpen(false)
    setQuery('')
    setResults([])
  }

  function activate(result: SearchResult) {
    clearSearch()
    navigate(result.to)
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (!open || results.length === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setFocusIndex((i) => (i + 1) % results.length)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setFocusIndex((i) => (i - 1 + results.length) % results.length)
    } else if (e.key === 'Enter' && focusIndex >= 0) {
      e.preventDefault()
      activate(results[focusIndex])
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  const groups = groupResults(results)

  return (
    <div className="global-search" ref={containerRef}>
      <form onSubmit={handleSearch} role="search">
        <input
          ref={inputRef}
          type="search"
          className="global-search__input"
          placeholder="搜索 VPS / 节点 / 入口… (⌘ K)"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => results.length > 0 && setOpen(true)}
          aria-label="全局搜索"
        />
      </form>
      {open && (
        <div className="global-search__menu" role="listbox">
          {loading ? (
            <p className="global-search__hint">正在加载…</p>
          ) : error ? (
            <p className="global-search__hint global-search__hint--error">{error}</p>
          ) : results.length === 0 ? (
            <p className="global-search__hint">没有匹配项</p>
          ) : (
            groups.map((group) => (
              <div className="global-search__group" key={group.kind}>
                <p className="global-search__group-title">{group.label}</p>
                {group.results.map((result) => {
                  const index = results.indexOf(result)
                  return (
                    <Link
                      key={`${result.kind}-${result.id}`}
                      to={result.to}
                      role="option"
                      aria-selected={index === focusIndex}
                      className={`global-search__item ${index === focusIndex ? 'is-focused' : ''}`}
                      onClick={clearSearch}
                      onMouseEnter={() => setFocusIndex(index)}
                    >
                      <span className="global-search__item-kind">{SEARCH_GROUP_LABELS[result.kind]}</span>
                      <span className="global-search__item-label">{result.label}</span>
                      {result.hint ? (
                        <span className="global-search__item-hint">{result.hint}</span>
                      ) : null}
                    </Link>
                  )
                })}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

function combine(
  vpsAssets: VPSAssetRecord[],
  nodes: NodeRecord[],
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
  for (const node of nodes) {
    if (matchesNode(node, q)) {
      out.push({
        kind: 'node',
        id: node.node_id,
        label: node.display_name || node.node_id,
        hint: compactHint([node.region, node.city, node.provider]) || node.node_id,
        to: `/nodes/${node.node_id}`,
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
        to: `/subscriptions?vps_id=${encodeURIComponent(subscription.vps_id)}`,
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

function matchesNode(node: NodeRecord, q: string): boolean {
  return (
    includesLower(node.display_name, q) ||
    includesLower(node.node_id, q) ||
    includesLower(node.region, q) ||
    includesLower(node.city, q) ||
    includesLower(node.provider, q) ||
    (node.labels ?? []).some((label) => includesLower(label, q))
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
