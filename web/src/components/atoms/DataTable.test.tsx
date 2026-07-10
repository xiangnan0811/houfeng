import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { DataTable, type DataTableColumn } from './DataTable'

interface Row {
  id: string
  name: string
  value: number
}

const rows: Row[] = [
  { id: 'a', name: 'Alpha', value: 1 },
  { id: 'b', name: 'Beta', value: 2 },
]
const columns: DataTableColumn<Row>[] = [
  { key: 'name', label: '名字', render: (r) => r.name },
  { key: 'value', label: '值', align: 'right', cellClassName: 'mono', render: (r) => r.value },
]
const interactiveColumns: DataTableColumn<Row>[] = [
  ...columns,
  {
    key: 'action',
    label: '操作',
    render: (r) => (
      <button type="button" aria-label={`编辑 ${r.name}`}>
        编辑
      </button>
    ),
  },
]

describe('DataTable', () => {
  it('renders rows with semantic table roles', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />)
    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getAllByRole('row')).toHaveLength(3) // 1 head + 2 body
    expect(screen.getByRole('row', { name: /Alpha/ })).toBeInTheDocument()
  })

  it('emits onRowClick on click', () => {
    const onRowClick = vi.fn()
    render(
      <DataTable columns={columns} rows={rows} rowKey={(r) => r.id} onRowClick={onRowClick} />,
    )
    fireEvent.click(screen.getByRole('row', { name: /Beta/ }))
    expect(onRowClick).toHaveBeenCalledWith(rows[1])
  })

  it('does not emit row click when an interactive cell action is used', () => {
    const onRowClick = vi.fn()
    render(
      <DataTable
        columns={interactiveColumns}
        rows={rows}
        rowKey={(r) => r.id}
        onRowClick={onRowClick}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '编辑 Alpha' }))
    expect(onRowClick).not.toHaveBeenCalled()
  })

  it('keeps row keyboard navigation on the row while ignoring child controls', () => {
    const onRowClick = vi.fn()
    render(
      <DataTable
        columns={interactiveColumns}
        rows={rows}
        rowKey={(r) => r.id}
        onRowClick={onRowClick}
      />,
    )

    fireEvent.keyDown(screen.getByRole('row', { name: /Beta/ }), { key: 'Enter' })
    expect(onRowClick).toHaveBeenCalledWith(rows[1])

    onRowClick.mockClear()
    fireEvent.keyDown(screen.getByRole('button', { name: '编辑 Beta' }), { key: 'Enter' })
    expect(onRowClick).not.toHaveBeenCalled()
  })

  it('applies cell class for column', () => {
    const { container } = render(
      <DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />,
    )
    const monoCell = container.querySelector('.data-table__cell.mono')
    expect(monoCell).toBeTruthy()
  })

  it('preserves column widths without emitting inline styles', () => {
    const widthColumns: DataTableColumn<Row>[] = [
      { key: 'name', label: '名字', width: 120, render: (r) => r.name },
      { key: 'value', label: '值', width: '25%', render: (r) => r.value },
    ]
    const { container } = render(
      <DataTable columns={widthColumns} rows={rows} rowKey={(r) => r.id} />,
    )
    const columnElements = Array.from(container.querySelectorAll('col'))

    expect(columnElements[0]).toHaveAttribute('width', '120')
    expect(columnElements[1]).toHaveAttribute('width', '25%')
    expect(container.querySelector('col[style]')).not.toBeInTheDocument()
  })

  it('shows empty content when rows is empty', () => {
    render(
      <DataTable
        columns={columns}
        rows={[]}
        rowKey={(r) => r.id}
        emptyContent={<span data-testid="empty">空</span>}
      />,
    )
    expect(screen.getByTestId('empty')).toBeInTheDocument()
  })
})
