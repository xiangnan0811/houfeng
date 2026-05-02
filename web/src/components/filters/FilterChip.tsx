export type FilterChipProps = {
  /** Display label, e.g. "类型: service". */
  label: string
  onRemove: () => void
  className?: string
}

export function FilterChip({ label, onRemove, className = '' }: FilterChipProps) {
  const classes = ['filter-chip', className].filter(Boolean).join(' ')
  return (
    <span className={classes}>
      <span className="filter-chip__label">{label}</span>
      <button
        type="button"
        className="filter-chip__remove"
        aria-label={`移除筛选 ${label}`}
        onClick={onRemove}
      >
        ×
      </button>
    </span>
  )
}
