import { useEffect, useRef, useState } from 'react'

export type FilterMultiSelectOption = {
  value: string
  label: string
}

export type FilterMultiSelectProps = {
  label: string
  values: string[]
  options: ReadonlyArray<FilterMultiSelectOption>
  onChange: (values: string[]) => void
  /** Optional label for the empty-options message. */
  emptyLabel?: string
  className?: string
  disabled?: boolean
}

export function FilterMultiSelect({
  label,
  values,
  options,
  onChange,
  emptyLabel = '暂无可选项',
  className = '',
  disabled,
}: FilterMultiSelectProps) {
  const [open, setOpen] = useState(false)
  const wrapperRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    function handleClickOutside(event: MouseEvent) {
      const target = event.target as Node | null
      if (wrapperRef.current && target && !wrapperRef.current.contains(target)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [open])

  function toggleValue(value: string) {
    if (values.includes(value)) {
      onChange(values.filter((item) => item !== value))
    } else {
      onChange([...values, value])
    }
  }

  const classes = ['filter-multiselect', open && 'is-open', className].filter(Boolean).join(' ')
  const summary = values.length === 0 ? '全部' : `已选 ${values.length}`

  return (
    <div className={classes} ref={wrapperRef}>
      <span className="filter-multiselect__label">{label}</span>
      <button
        type="button"
        className="filter-multiselect__trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        {summary}
      </button>
      {open ? (
        <div className="filter-multiselect__popover" role="listbox">
          {options.length === 0 ? (
            <p className="filter-multiselect__empty">{emptyLabel}</p>
          ) : (
            options.map((option) => {
              const checked = values.includes(option.value)
              return (
                <label key={option.value} className="filter-multiselect__option">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => toggleValue(option.value)}
                  />
                  <span>{option.label}</span>
                </label>
              )
            })
          )}
        </div>
      ) : null}
    </div>
  )
}
