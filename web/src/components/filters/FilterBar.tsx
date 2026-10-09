import { useId, useState, type ReactNode } from 'react'

export type FilterBarProps = {
  /** Filter controls (FilterSelect / FilterMultiSelect / FilterToggle) laid out horizontally. */
  children: ReactNode
  /** Optional row of FilterChip rendered below the controls when any filter is active. */
  activeChips?: ReactNode
  /** Whether at least one filter is currently active; controls visibility of "清空所有" button. */
  hasActiveFilters?: boolean
  /** Called when user clicks "清空所有". */
  onClearAll?: () => void
  /**
   * 窄屏（≤760px）收起筛选：只保留带 `filter-bar__keep` 的控件（如搜索）、已生效筛选的 chip 和“筛选 (N)”按钮，
   * 其余筛选点开后在原位展开。桌面布局不受影响。
   */
  narrowCollapse?: { activeCount: number }
  className?: string
}

export function FilterBar({
  children,
  activeChips,
  hasActiveFilters = false,
  onClearAll,
  narrowCollapse,
  className = '',
}: FilterBarProps) {
  const [expanded, setExpanded] = useState(false)
  const rowId = useId()
  const classes = [
    'filter-bar',
    'filter-bar--stacked',
    narrowCollapse ? 'filter-bar--narrow-collapse' : '',
    className,
  ].filter(Boolean).join(' ')
  return (
    <div className={classes}>
      <div className="filter-bar__controls">
        {narrowCollapse ? (
          <button
            type="button"
            className="filter-bar__toggle"
            aria-expanded={expanded}
            aria-controls={rowId}
            onClick={() => setExpanded((current) => !current)}
          >
            {narrowCollapse.activeCount > 0 ? `筛选 (${narrowCollapse.activeCount})` : '筛选'}
          </button>
        ) : null}
        <div
          id={rowId}
          className="filter-bar__controls-row"
          {...(narrowCollapse && !expanded ? { 'data-narrow-collapsed': '' } : {})}
        >
          {children}
        </div>
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
