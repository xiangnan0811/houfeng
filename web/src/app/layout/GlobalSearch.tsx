import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { listNodes, listTargets } from '../../lib/api'
import type { NodeRecord, TargetRecord } from '../../lib/types'

interface SearchResult {
  kind: 'node' | 'target'
  id: string
  label: string
  hint: string
  to: string
}

const MAX_RESULTS = 6

/**
 * Global search with ⌘K / Ctrl+K shortcut.
 *
 * On Enter: fetch the full Node and Target lists in parallel and filter
 * client-side by display_name / hostname / labels / id substring match
 * (case-insensitive). Render up to 6 hits in a dropdown; arrow keys move
 * focus, Enter navigates.
 */
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

  // ⌘K / Ctrl+K to toggle search; Escape to close
  useEffect(() => {
    function onKeyDown(e: globalThis.KeyboardEvent) {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key === 'k') {
        e.preventDefault()
        if (open) {
          setOpen(false)
        } else {
          setOpen(true)
          // Focus input on next frame so the keyup doesn't type 'k'
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
      const [nodes, targets] = await Promise.all([listNodes(), listTargets()])
      const matches = combine(nodes, targets, q).slice(0, MAX_RESULTS)
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

  function activate(result: SearchResult) {
    setOpen(false)
    setQuery('')
    setResults([])
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

  return (
    <div className="global-search" ref={containerRef}>
      <form onSubmit={handleSearch} role="search">
        <input
          ref={inputRef}
          type="search"
          className="global-search__input"
          placeholder="搜索节点 / 目标 (⌘K)"
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
            results.map((result, i) => (
              <button
                key={`${result.kind}-${result.id}`}
                type="button"
                role="option"
                aria-selected={i === focusIndex}
                className={`global-search__item ${i === focusIndex ? 'is-focused' : ''}`}
                onClick={() => activate(result)}
                onMouseEnter={() => setFocusIndex(i)}
              >
                <span className="global-search__item-kind">
                  {result.kind === 'node' ? '节点' : '目标'}
                </span>
                <span className="global-search__item-label">{result.label}</span>
                {result.hint ? (
                  <span className="global-search__item-hint">{result.hint}</span>
                ) : null}
              </button>
            ))
          )}
        </div>
      )}
    </div>
  )
}

function combine(nodes: NodeRecord[], targets: TargetRecord[], q: string): SearchResult[] {
  const out: SearchResult[] = []
  for (const node of nodes) {
    if (matchesNode(node, q)) {
      out.push({
        kind: 'node',
        id: node.node_id,
        label: node.display_name || node.node_id,
        hint: node.region ? `${node.region} · ${node.city}` : node.node_id,
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
        hint: target.host || target.target_id,
        to: `/targets/${target.target_id}`,
      })
    }
  }
  return out
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

function includesLower(value: string | undefined | null, q: string): boolean {
  if (!value) return false
  return value.toLowerCase().includes(q)
}
