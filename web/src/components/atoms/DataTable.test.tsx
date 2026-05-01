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

  it('applies cell class for column', () => {
    const { container } = render(
      <DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />,
    )
    const monoCell = container.querySelector('.data-table__cell.mono')
    expect(monoCell).toBeTruthy()
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
