import { Fragment, type ReactNode } from 'react'

const INTERACTIVE_ROW_TARGET_SELECTOR = [
  'a[href]',
  'button',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="link"]',
].join(',')

function isInteractiveRowTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(INTERACTIVE_ROW_TARGET_SELECTOR) != null
}

export interface DataTableColumn<T> {
  key: string
  label: ReactNode
  align?: 'left' | 'right' | 'center'
  width?: string | number
  /** Extra class applied to <td> cells in this column (e.g. 'mono'). */
  cellClassName?: string
  /** When true, the column header is clickable for sorting. Uses `key` as sort key unless `sortKey` is set. */
  sortable?: boolean
  /** Explicit sort key; defaults to column `key` when omitted. */
  sortKey?: string
  render: (row: T, rowIndex: number) => ReactNode
}

export interface DataTableSortState {
  key: string
  direction: 'asc' | 'desc'
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[]
  rows: T[]
  rowKey: (row: T) => string
  density?: 'compact' | 'normal'
  onRowClick?: (row: T) => void
  emptyContent?: ReactNode
  className?: string
  caption?: ReactNode
  /** Optional row-level class derivation (e.g. severity-tinted background). */
  rowClassName?: (row: T) => string | undefined
  /** Current sort state. When set, sort indicators render on matching column header. */
  sortState?: DataTableSortState | null
  /** Called when user clicks a sortable column header. The page is responsible for re-sorting data. */
  onSortChange?: (key: string) => void
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  density = 'compact',
  onRowClick,
  emptyContent,
  className = '',
  caption,
  rowClassName,
  sortState,
  onSortChange,
}: DataTableProps<T>) {
  const cls = [
    'data-table',
    `data-table--${density}`,
    onRowClick ? 'data-table--clickable' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  if (rows.length === 0) {
    return (
      <div className="data-table-empty">
        {emptyContent ?? <span className="empty-inline">暂无数据</span>}
      </div>
    )
  }

  return (
    <table className={cls} role="table">
      {caption && <caption className="data-table__caption">{caption}</caption>}
      <colgroup>
        {columns.map((col) => (
          <col
            key={col.key}
            width={col.width || undefined}
          />
        ))}
      </colgroup>
      <thead className="data-table__head">
        <tr role="row">
          {columns.map((col) => {
            const isSortable = col.sortable && onSortChange
            const sortKey = col.sortKey ?? col.key
            const isActive = sortState?.key === sortKey
            const dir = isActive ? sortState?.direction : null

            return (
              <th
                key={col.key}
                role="columnheader"
                className={[
                  'data-table__th',
                  `data-table__th--${col.align ?? 'left'}`,
                  isSortable ? 'data-table__th--sortable' : '',
                  isActive ? 'data-table__th--sorted' : '',
                ].filter(Boolean).join(' ')}
                scope="col"
                aria-sort={isActive ? (dir === 'asc' ? 'ascending' : 'descending') : undefined}
              >
                {isSortable ? (
                  <button
                    type="button"
                    className="data-table__sort-btn"
                    onClick={() => onSortChange(sortKey)}
                  >
                    {col.label}
                    <span className="data-table__sort-indicator" aria-hidden="true">
                      {isActive && dir === 'asc' ? ' ↑' : isActive && dir === 'desc' ? ' ↓' : ' ↕'}
                    </span>
                  </button>
                ) : (
                  col.label
                )}
              </th>
            )
          })}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, ri) => {
          const extra = rowClassName?.(row)
          const trCls = ['data-table__row', extra].filter(Boolean).join(' ')
          return (
            <Fragment key={rowKey(row)}>
              {/* a11y-allow-nonsemantic-click: keyboard-complete-row */}
              <tr
                role="row"
                className={trCls}
                onClick={onRowClick ? (e) => {
                  if (isInteractiveRowTarget(e.target)) return
                  onRowClick(row)
                } : undefined}
                tabIndex={onRowClick ? 0 : undefined}
                onKeyDown={
                  onRowClick
                    ? (e) => {
                        if (isInteractiveRowTarget(e.target)) return
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          onRowClick(row)
                        }
                      }
                    : undefined
                }
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    role="cell"
                    className={[
                      'data-table__cell',
                      `data-table__cell--${col.align ?? 'left'}`,
                      col.cellClassName,
                    ]
                      .filter(Boolean)
                      .join(' ')}
                  >
                    {col.render(row, ri)}
                  </td>
                ))}
              </tr>
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
