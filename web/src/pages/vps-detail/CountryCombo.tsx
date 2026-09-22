import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react'

import {
  COMMON_COUNTRY_CODES,
  COUNTRY_GROUPS,
  COUNTRY_ITEMS,
  OTHER_COUNTRY_COUNT,
  browseCountryItems,
  countryCommitted,
  resolveCountryInput,
  searchCountryItems,
  type CountryListItem,
} from './countryCatalog'

type CountryComboProps = {
  id: string
  value: string
  disabled?: boolean
  onChange: (value: string) => void
}

type ComboMode = 'browse' | 'search'

const COUNTRY_BY_CODE = new Map(COUNTRY_ITEMS.map((item) => [item.value, item]))
const COMMON_CODES_IN_ORDER = COUNTRY_GROUPS.flatMap((group) => [...group.codes])
const MORE_NAV_INDEX = COMMON_CODES_IN_ORDER.length
const OTHER_COUNTRIES = COUNTRY_ITEMS.filter((item) => !COMMON_COUNTRY_CODES.has(item.value))

function optionDomId(listId: string, value: string) {
  return `${listId}-opt-${value}`.replace(/[^a-zA-Z0-9_-]/g, '_')
}

function isImeEvent(event: KeyboardEvent<HTMLInputElement>) {
  return event.nativeEvent.isComposing || event.keyCode === 229
}

function commitLabelOf(item: CountryListItem) {
  return item.commitLabel || item.zh || item.label
}

export function CountryCombo({ id, value, disabled = false, onChange }: CountryComboProps) {
  const listId = `${id}-list`
  const moreId = `${listId}-more`
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const armedSelectRef = useRef(false)
  const highlightRef = useRef(false)
  const pendingMoveRef = useRef<0 | 1 | -1>(0)
  const moreFocusRef = useRef(false)
  const blurTimerRef = useRef(0)
  const composingRef = useRef(false)
  const initial = countryCommitted(value)
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<ComboMode>('browse')
  const [othersOpen, setOthersOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [text, setText] = useState(initial.label)
  const [committed, setCommitted] = useState(initial)

  const filtered = mode === 'search' ? searchCountryItems(text) : browseCountryItems(othersOpen)
  useEffect(() => () => window.clearTimeout(blurTimerRef.current), [])

  function hide() {
    setOpen(false)
    setOthersOpen(false)
    setMode('browse')
    setActive(-1)
  }

  function publishResolved(raw: string) {
    const resolved = resolveCountryInput(raw)
    if (resolved.value !== value) onChange(resolved.value)
    return resolved
  }

  function commit(nextValue: string, nextLabel: string, closeList = true) {
    setCommitted({ value: nextValue, label: nextLabel })
    setText(nextLabel)
    if (nextValue !== value) onChange(nextValue)
    if (closeList) hide()
  }

  function commitInput() {
    const resolved = publishResolved(text)
    setCommitted(resolved)
    setText(resolved.label)
  }

  function reveal(nextMode: ComboMode, nextOthersOpen = othersOpen) {
    setMode(nextMode)
    setOthersOpen(nextOthersOpen)
    highlightRef.current = true
    setOpen(true)
  }

  function showBrowse() {
    reveal('browse', open ? othersOpen : false)
  }

  function chooseItem(item: CountryListItem) {
    commit(item.value, commitLabelOf(item))
  }

  function toggleOthers() {
    moreFocusRef.current = true
    reveal('browse', !othersOpen)
  }

  function chooseEl(el: Element | undefined) {
    if (!el) return
    if ((el as HTMLElement).dataset.more === '1') {
      toggleOthers()
      return
    }
    const nextValue = (el as HTMLElement).dataset.value
    const custom = (el as HTMLElement).dataset.custom === '1'
    const item = filtered.find((row) => row.value === nextValue && Boolean(row.custom) === custom)
      ?? COUNTRY_ITEMS.find((row) => row.value === nextValue)
    if (!item) return
    chooseItem(item)
    inputRef.current?.focus()
  }

  useEffect(() => {
    if (!open) return
    const nav = [...(listRef.current?.querySelectorAll('[role="option"]') ?? [])]
    if (nav.length === 0) return

    if (moreFocusRef.current) {
      moreFocusRef.current = false
      highlightRef.current = false
      pendingMoveRef.current = 0
      const more = nav.findIndex((el) => (el as HTMLElement).dataset.more === '1')
      setActive(more)
      return
    }

    if (!highlightRef.current) return
    highlightRef.current = false
    let idx = -1
    if (committed.value) {
      idx = nav.findIndex((el) => (
        el.getAttribute('role') === 'option' && (el as HTMLElement).dataset.value === committed.value
      ))
    }
    const move = pendingMoveRef.current
    pendingMoveRef.current = 0
    if (move === 0) {
      setActive(idx)
      return
    }
    if (move > 0) {
      const start = idx < 0 ? -1 : idx
      setActive(Math.min(nav.length - 1, start + move))
      return
    }
    const start = idx < 0 ? nav.length : idx
    setActive(Math.max(0, start + move))
  }, [open, mode, othersOpen, text, committed.value])

  useEffect(() => {
    if (!open) {
      inputRef.current?.removeAttribute('aria-activedescendant')
      return
    }
    const nav = [...(listRef.current?.querySelectorAll('[role="option"]') ?? [])]
    const current = nav[active]
    if (!(current instanceof HTMLElement)) {
      inputRef.current?.removeAttribute('aria-activedescendant')
      return
    }
    if (!current.id) current.id = `${listId}-nav-${active}`
    inputRef.current?.setAttribute('aria-activedescendant', current.id)
    current.scrollIntoView?.({ block: 'nearest' })
  }, [active, open, listId, mode, othersOpen, text])

  function optionClass(item: CountryListItem, extraClass: string, index: number) {
    const selected = Boolean(committed.value && item.value === committed.value && !item.custom)
    return [
      extraClass,
      selected ? 'is-selected' : '',
      index === active ? 'is-active' : '',
    ].filter(Boolean).join(' ')
  }

  function renderOption(item: CountryListItem, extraClass: string, index: number) {
    return (
      <div
        key={`${item.custom ? 'custom' : 'iso'}-${item.value}`}
        className={optionClass(item, extraClass, index)}
        id={optionDomId(listId, item.value)}
        role="option"
        aria-selected={index === active}
        data-value={item.value}
        {...(item.custom ? { 'data-custom': '1' } : {})}
        {...(item.commitLabel ? { 'data-commit': item.commitLabel } : {})}
        title={extraClass === 'combo-chip' ? (item.en ? `${item.en} · ${item.value}` : item.value) : undefined}
      >
        {extraClass === 'combo-chip' ? (
          item.zh || item.label
        ) : (
          <>
            <span className="combo-option__label">{item.label}</span>
            {item.hint ? <span className="combo-option__hint">{item.hint}</span> : null}
          </>
        )}
      </div>
    )
  }

  function handleFocus(event: React.FocusEvent<HTMLInputElement>) {
    if (disabled) return
    setCommitted(countryCommitted(value))
    if (!open) showBrowse()
    armedSelectRef.current = false
    if (composingRef.current) return
    if (!text || text !== countryCommitted(value).label) return
    armedSelectRef.current = true
    if (event.relatedTarget) {
      event.currentTarget.select()
      armedSelectRef.current = false
    }
  }

  function handleMouseUp(event: ReactMouseEvent<HTMLInputElement>) {
    if (!armedSelectRef.current) return
    event.preventDefault()
    event.currentTarget.select()
    armedSelectRef.current = false
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (composingRef.current || isImeEvent(event)) return
    const nav = open ? [...(listRef.current?.querySelectorAll('[role="option"]') ?? [])] : []
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      if (!open) {
        pendingMoveRef.current = 1
        showBrowse()
        return
      }
      setActive((current) => (current < 0 ? 0 : Math.min(nav.length - 1, current + 1)))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      if (!open) {
        pendingMoveRef.current = -1
        showBrowse()
        return
      }
      setActive((current) => (current < 0 ? nav.length - 1 : Math.max(0, current - 1)))
    } else if (event.key === 'Enter') {
      if (!open) return
      event.preventDefault()
      if (active >= 0) chooseEl(nav[active])
      else commitInput()
    } else if (event.key === ' ' && open && (nav[active] as HTMLElement | undefined)?.dataset.more === '1') {
      event.preventDefault()
      toggleOthers()
    } else if (event.key === 'Escape') {
      if (!open) return
      event.preventDefault()
      event.stopPropagation()
      setText(committed.label)
      if (committed.value !== value) onChange(committed.value)
      hide()
    } else if (event.key === 'Tab') {
      const current = open && active >= 0 ? nav[active] : null
      if (current && (current as HTMLElement).dataset.more !== '1') chooseEl(current)
      else commitInput()
    }
  }

  function handleListMouseDown(event: ReactMouseEvent<HTMLDivElement>) {
    const option = (event.target as HTMLElement).closest('[role="option"]')
    if (!option) return
    event.preventDefault()
    chooseEl(option)
    if ((option as HTMLElement).dataset.more !== '1') inputRef.current?.focus()
  }

  function handleListMouseMove(event: ReactMouseEvent<HTMLDivElement>) {
    const nav = [...(listRef.current?.querySelectorAll('[role="option"]') ?? [])]
    const option = (event.target as HTMLElement).closest('[role="option"]')
    if (!option) return
    const index = nav.indexOf(option)
    if (index !== active) setActive(index)
  }


  return (
    <div className="field field--country">
      <label className="field__label" htmlFor={id}>国家 / 地区</label>
      <input
        ref={inputRef}
        id={id}
        className="input"
        type="text"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={open ? 'true' : 'false'}
        aria-controls={listId}
        aria-haspopup="listbox"
        autoComplete="off"
        spellCheck={false}
        placeholder="搜索中文、英文或代码"
        value={text}
        disabled={disabled}
        onChange={(event) => {
          const next = event.target.value
          setText(next)
          if (!composingRef.current) publishResolved(next)
          if (!next.trim()) reveal('browse', false)
          else reveal('search', othersOpen)
        }}
        onCompositionStart={() => {
          composingRef.current = true
        }}
        onCompositionEnd={() => {
          composingRef.current = false
          const raw = inputRef.current?.value ?? ''
          publishResolved(raw)
          if (document.activeElement === inputRef.current) {
            if (!raw.trim()) reveal('browse', false)
            else reveal('search', othersOpen)
          }
        }}
        onFocus={handleFocus}
        onMouseUp={handleMouseUp}
        onClick={() => {
          if (disabled) return
          if (!open) showBrowse()
        }}
        onKeyDown={handleKeyDown}
        onBlur={() => {
          window.clearTimeout(blurTimerRef.current)
          blurTimerRef.current = window.setTimeout(() => {
            if (listRef.current?.contains(document.activeElement)) return
            if (document.activeElement === inputRef.current) return
            commitInput()
            hide()
          }, 0)
        }}
      />
      <div
        ref={listRef}
        id={listId}
        className="combo-list combo-list--country"
        role="listbox"
        hidden={!open}
        onMouseDown={handleListMouseDown}
        onMouseMove={handleListMouseMove}
      >
        {open && mode === 'browse' ? (
          <>
            {COUNTRY_GROUPS.map((group) => (
              <div key={group.id} className="combo-section" role="presentation">
                <div className="combo-group" role="presentation">{group.label}</div>
                <div className="combo-chips" role="presentation">
                  {group.codes.map((code) => {
                    const item = COUNTRY_BY_CODE.get(code)
                    const index = COMMON_CODES_IN_ORDER.indexOf(code)
                    return item ? renderOption(item, 'combo-chip', index) : null
                  })}
                </div>
              </div>
            ))}
            <div
              role="option"
              className={['combo-more__btn', MORE_NAV_INDEX === active ? 'is-active' : ''].filter(Boolean).join(' ')}
              id={moreId}
              aria-selected={MORE_NAV_INDEX === active}
              data-more="1"
            >
              {othersOpen ? '收起其他国家／地区' : `展开其他 ${OTHER_COUNTRY_COUNT} 个国家／地区`}
            </div>
            {othersOpen ? (
              <div className="combo-others" role="presentation">
                {OTHER_COUNTRIES.map((item, offset) => (
                  renderOption(item, 'combo-option', MORE_NAV_INDEX + 1 + offset)
                ))}
              </div>
            ) : null}
          </>
        ) : null}
        {open && mode === 'search' ? (
          <div className="combo-results" role="presentation">
            {filtered.map((item, index) => renderOption(item, 'combo-option', index))}
          </div>
        ) : null}
      </div>
    </div>
  )
}
