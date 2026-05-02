import { Toggle } from '../atoms/Toggle'

export type FilterToggleProps = {
  label: string
  checked: boolean
  onChange: (checked: boolean) => void
  className?: string
  disabled?: boolean
}

export function FilterToggle({
  label,
  checked,
  onChange,
  className = '',
  disabled,
}: FilterToggleProps) {
  const classes = ['filter-toggle', className].filter(Boolean).join(' ')
  return (
    <div className={classes}>
      <span className="filter-toggle__label">{label}</span>
      <Toggle checked={checked} onChange={onChange} label={label} disabled={disabled} />
    </div>
  )
}
