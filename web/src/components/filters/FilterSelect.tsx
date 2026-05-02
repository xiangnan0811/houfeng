import type { ChangeEvent } from 'react'

export type FilterSelectOption = {
  value: string
  label: string
}

export type FilterSelectProps = {
  label: string
  value: string | null
  options: ReadonlyArray<FilterSelectOption>
  onChange: (value: string | null) => void
  /** Label for the "all" option (null value). Defaults to "全部". */
  placeholder?: string
  className?: string
  disabled?: boolean
}

export function FilterSelect({
  label,
  value,
  options,
  onChange,
  placeholder = '全部',
  className = '',
  disabled,
}: FilterSelectProps) {
  function handleChange(event: ChangeEvent<HTMLSelectElement>) {
    const next = event.target.value
    onChange(next === '' ? null : next)
  }

  const classes = ['filter-select', className].filter(Boolean).join(' ')
  return (
    <label className={classes}>
      <span className="filter-select__label">{label}</span>
      <select
        className="filter-select__control"
        value={value ?? ''}
        onChange={handleChange}
        disabled={disabled}
      >
        <option value="">{placeholder}</option>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}
