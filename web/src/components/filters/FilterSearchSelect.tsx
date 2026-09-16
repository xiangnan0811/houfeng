import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react'

export type FilterSearchSelectOption = {
  value: string
  label: string
  hint?: string
  keywords?: string
}

export type FilterSearchSelectProps = {
  label: string
  value: string | null
  options: ReadonlyArray<FilterSearchSelectOption>
  onChange: (value: string | null) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyLabel?: string
  noMatchLabel?: string
  className?: string
  disabled?: boolean
}

const TABBABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function FilterSearchSelect({
  label,
  value,
  options,
  onChange,
  placeholder = '全部',
  searchPlaceholder = '搜索名称、IP、服务商、位置',
  emptyLabel = '暂无可选项',
  noMatchLabel = '没有匹配的项目',
  className = '',
  disabled,
}: FilterSearchSelectProps) {
  const labelId = useId()
  const valueId = useId()
  const listId = useId()
  const searchId = useId()
  const wrapperRef = useRef<HTMLDivElement | null>(null)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const searchRef = useRef<HTMLInputElement | null>(null)
  const listRef = useRef<HTMLDivElement | null>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)

  const selected = options.find((option) => option.value === value) ?? null
  const summary = selected?.label ?? (value || placeholder)
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter((option) => (
      [option.label, option.value, option.hint, option.keywords].filter(Boolean).join(' ').toLowerCase().includes(needle)
    ))
  }, [options, query])

  const rows = useMemo(
    () => [{ value: null as string | null, label: placeholder, hint: undefined as string | undefined }, ...filtered],
    [filtered, placeholder],
  )
  const safeHighlight = rows.length === 0 ? 0 : Math.min(highlight, rows.length - 1)

  function closeList() {
    setOpen(false)
    setQuery('')
    setHighlight(0)
  }

  function applyValue(next: string | null) {
    onChange(next)
    closeList()
  }

  function openList() {
    if (disabled) return
    const selectedIndex = value ? filtered.findIndex((option) => option.value === value) : -1
    setHighlight(selectedIndex >= 0 ? selectedIndex + 1 : 0)
    setOpen(true)
  }

  useEffect(() => {
    if (!open) return
    searchRef.current?.focus()
    function handlePointerDown(event: MouseEvent) {
      const target = event.target as Node | null
      if (wrapperRef.current && target && !wrapperRef.current.contains(target)) {
        closeList()
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    return () => document.removeEventListener('mousedown', handlePointerDown)
  }, [open])

  useEffect(() => {
    if (!open) return
    const list = listRef.current
    const active = list?.querySelector<HTMLElement>('[role="option"].is-active')
    if (!list || !active) return
    const listRect = list.getBoundingClientRect()
    const optionRect = active.getBoundingClientRect()
    if (optionRect.height === 0 && listRect.height === 0) return
    if (optionRect.bottom > listRect.bottom) {
      list.scrollTop += optionRect.bottom - listRect.bottom
    } else if (optionRect.top < listRect.top) {
      list.scrollTop -= listRect.top - optionRect.top
    }
  }, [open, safeHighlight, rows])

  function handleSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setHighlight(Math.min(rows.length - 1, safeHighlight + 1))
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      setHighlight(Math.max(0, safeHighlight - 1))
      return
    }
    if (event.key === 'Home') {
      event.preventDefault()
      setHighlight(0)
      return
    }
    if (event.key === 'End') {
      event.preventDefault()
      setHighlight(Math.max(0, rows.length - 1))
      return
    }
    if (event.key === 'Enter') {
      event.preventDefault()
      const row = rows[safeHighlight]
      if (!row) return
      applyValue(row.value)
      triggerRef.current?.focus()
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      closeList()
      triggerRef.current?.focus()
      return
    }
    if (event.key !== 'Tab') return
    event.preventDefault()
    const wrapper = wrapperRef.current
    const tabbable = [...document.querySelectorAll<HTMLElement>(TABBABLE)]
    if (event.shiftKey) {
      closeList()
      triggerRef.current?.focus()
      return
    }
    const search = searchRef.current
    const currentIndex = search ? tabbable.indexOf(search) : -1
    const next = currentIndex >= 0 ? tabbable[currentIndex + 1] : null
    const fallback = wrapper
      ? tabbable.find((node) => {
        const position = wrapper.compareDocumentPosition(node)
        return (position & Node.DOCUMENT_POSITION_FOLLOWING) !== 0 && !wrapper.contains(node)
      })
      : null
    closeList()
    ;(next && next !== search ? next : fallback)?.focus()
  }


  const classes = ['filter-searchselect', open && 'is-open', className].filter(Boolean).join(' ')
  const activeOption = rows[safeHighlight]
  const activeOptionId = `${listId}-opt-${activeOption?.value ?? 'all'}`

  return (
    <div className={classes} ref={wrapperRef}>
      <span className="filter-searchselect__label" id={labelId}>{label}</span>
      <button
        ref={triggerRef}
        type="button"
        className="filter-searchselect__trigger"
        aria-labelledby={`${labelId} ${valueId}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        disabled={disabled}
        onClick={() => {
          if (open) closeList()
          else openList()
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown' && !open) {
            event.preventDefault()
            openList()
          }
        }}
      >
        <span className="filter-searchselect__value" id={valueId}>{summary}</span>
      </button>
      {open ? (
        <div className="filter-searchselect__popover">
          <input
            ref={searchRef}
            id={searchId}
            className="filter-searchselect__search"
            type="search"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={activeOptionId}
            aria-label={`搜索${label}`}
            placeholder={searchPlaceholder}
            value={query}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setQuery(event.target.value)
              setHighlight(0)
            }}
            onKeyDown={handleSearchKeyDown}
          />
          <div
            ref={listRef}
            className="filter-searchselect__list"
            id={listId}
            role="listbox"
            aria-label={label}
            onMouseDown={(event) => event.preventDefault()}
          >
            {rows.map((row, index) => {
              const selectedRow = row.value == null ? value == null : row.value === value
              return (
                <div
                  key={row.value ?? 'all'}
                  id={`${listId}-opt-${row.value ?? 'all'}`}
                  role="option"
                  className={[
                    'filter-searchselect__option',
                    selectedRow && 'is-selected',
                    index === safeHighlight && 'is-active',
                  ].filter(Boolean).join(' ')}
                  aria-selected={selectedRow}
                  onMouseEnter={() => setHighlight(index)}
                  onClick={() => {
                    onChange(row.value)
                    setOpen(false)
                    setQuery('')
                    setHighlight(0)
                    triggerRef.current?.focus()
                  }}
                >
                  <span className="filter-searchselect__option-label">{row.label}</span>
                  {row.hint ? <span className="filter-searchselect__option-hint">{row.hint}</span> : null}
                </div>
              )
            })}
            {options.length === 0 && !query.trim() ? (
              <p className="filter-searchselect__empty">{emptyLabel}</p>
            ) : null}
            {query.trim() && filtered.length === 0 ? (
              <p className="filter-searchselect__empty">{noMatchLabel}</p>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
