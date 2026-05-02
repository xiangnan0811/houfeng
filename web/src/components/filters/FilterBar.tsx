import type { ReactNode } from 'react'

export type FilterBarProps = {
  /** Filter controls (FilterSelect / FilterMultiSelect / FilterToggle) laid out horizontally. */
  children: ReactNode
  /** Optional row of FilterChip rendered below the controls when any filter is active. */
  activeChips?: ReactNode
  /** Whether at least one filter is currently active; controls visibility of "清空所有" button. */
  hasActiveFilters?: boolean
  /** Called when user clicks "清空所有". */
  onClearAll?: () => void
  className?: string
}

export function FilterBar({
  children,
  activeChips,
  hasActiveFilters = false,
  onClearAll,
  className = '',
}: FilterBarProps) {
  const classes = ['filter-bar', className].filter(Boolean).join(' ')
  return (
    <div className={classes}>
      <div className="filter-bar__controls">
        <div className="filter-bar__controls-row">{children}</div>
        {hasActiveFilters && onClearAll ? (
          <button
            type="button"
            className="filter-bar__clear"
            onClick={onClearAll}
          >
            清空所有
          </button>
        ) : null}
      </div>
      {hasActiveFilters && activeChips ? (
        <div className="filter-bar__chips">{activeChips}</div>
      ) : null}
    </div>
  )
}
