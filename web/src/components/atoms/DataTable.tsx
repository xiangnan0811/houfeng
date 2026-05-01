import type { ReactNode } from 'react'

export interface DataTableColumn<T> {
  key: string
  label: ReactNode
  align?: 'left' | 'right' | 'center'
  width?: string | number
  /** Extra class applied to <td> cells in this column (e.g. 'mono'). */
  cellClassName?: string
  render: (row: T, rowIndex: number) => ReactNode
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
            style={col.width ? { width: typeof col.width === 'number' ? `${col.width}px` : col.width } : undefined}
          />
        ))}
      </colgroup>
      <thead className="data-table__head">
        <tr role="row">
          {columns.map((col) => (
            <th
              key={col.key}
              role="columnheader"
              className={`data-table__th data-table__th--${col.align ?? 'left'}`}
              scope="col"
            >
              {col.label}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, ri) => {
          const extra = rowClassName?.(row)
          const trCls = ['data-table__row', extra].filter(Boolean).join(' ')
          return (
            <tr
              key={rowKey(row)}
              role="row"
              className={trCls}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              tabIndex={onRowClick ? 0 : undefined}
              onKeyDown={
                onRowClick
                  ? (e) => {
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
          )
        })}
      </tbody>
    </table>
  )
}
