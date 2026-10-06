import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { insertMaterialToken } from '../../../lib/documentMarkdown'
import { RecordMaterialDrawer } from './RecordMaterialDrawer'

describe('RecordMaterialDrawer', () => {
  it('inserts authorized tokens and can remove current materials without rewriting history', () => {
    const onInsert = vi.fn()
    const onRemove = vi.fn()
    render(
      <MemoryRouter>
        <RecordMaterialDrawer
          open
          onClose={vi.fn()}
          onInsert={onInsert}
          onRemove={onRemove}
          items={[
            { kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测', available: true },
            { kind: 'attachment', id: 'att_old', label: '失效附件', available: false },
          ]}
        />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: '插入第三晚 TCP 观测' }))
    expect(onInsert).toHaveBeenCalledWith(expect.objectContaining({ id: 'ev_7K2P' }))
    fireEvent.click(screen.getByRole('button', { name: '移除第三晚 TCP 观测' }))
    expect(onRemove).toHaveBeenCalled()
    expect(screen.getByText('引用已失效')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute('href', '/evidence/ev_7K2P')
    expect(insertMaterialToken('', {
      kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测',
    })).toContain('houfeng-evidence:ev_7K2P')
  })

  it('shows attachment names and routes picked files, retries and removals to the upload queue', () => {
    const uploads = {
      rows: [
        { client_id: 'c1', display_name: 'alpha.png', size_bytes: 2048, status: 'processing' as const },
        { client_id: 'c2', display_name: 'beta.log', size_bytes: 10, status: 'failed' as const, error: '网络中断' },
      ],
      notice: '',
      onFiles: vi.fn(),
      onRetry: vi.fn(),
      onCancel: vi.fn(),
      onRemove: vi.fn(),
    }
    render(
      <MemoryRouter>
        <RecordMaterialDrawer
          open
          onClose={vi.fn()}
          onInsert={vi.fn()}
          onRemove={vi.fn()}
          uploads={uploads}
          items={[{
            kind: 'attachment', id: 'att_mtr', label: '附件 att_mtr', available: true,
            attachment: { attachment_id: 'att_mtr', state: 'available', display_name: 'mtr.txt', media_type: 'text/plain', size_bytes: 1024, preview_available: true },
          }]}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('mtr.txt')).toBeInTheDocument()
    expect(screen.queryByText('att_mtr')).not.toBeInTheDocument()
    expect(screen.getByText('安全检查中')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消上传alpha.png' }))
    expect(uploads.onCancel).toHaveBeenCalledWith('c1')
    expect(screen.getByText('网络中断')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试上传beta.log' }))
    expect(uploads.onRetry).toHaveBeenCalledWith('c2')
    fireEvent.click(screen.getByRole('button', { name: '移除上传beta.log' }))
    expect(uploads.onRemove).toHaveBeenCalledWith('c2')
    const file = new File(['x'], 'gamma.txt', { type: 'text/plain' })
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })
    expect(uploads.onFiles).toHaveBeenCalledWith([file])
  })

  it('hides raw attachment IDs while metadata is loading and blocks new files during publish', () => {
    render(
      <MemoryRouter>
        <RecordMaterialDrawer
          open
          onClose={vi.fn()}
          onInsert={vi.fn()}
          onRemove={vi.fn()}
          uploads={{ rows: [], notice: '', disabled: true, onFiles: vi.fn(), onRetry: vi.fn(), onCancel: vi.fn(), onRemove: vi.fn() }}
          items={[{ kind: 'attachment', id: 'att_secret', label: '附件', available: false, pending: true }]}
        />
      </MemoryRouter>,
    )
    expect(screen.queryByText('att_secret')).not.toBeInTheDocument()
    expect(screen.getByText('读取中')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选择文件' })).toBeDisabled()
  })

  it('disables insert and remove when the workspace is read-only', () => {
    const onInsert = vi.fn()
    const onRemove = vi.fn()
    render(
      <MemoryRouter>
        <RecordMaterialDrawer
          open
          readOnly
          onClose={vi.fn()}
          onInsert={onInsert}
          onRemove={onRemove}
          items={[{ kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测', available: true }]}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: '插入第三晚 TCP 观测' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '移除第三晚 TCP 观测' })).toBeDisabled()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute('href', '/evidence/ev_7K2P')
    fireEvent.click(screen.getByRole('button', { name: '插入第三晚 TCP 观测' }))
    fireEvent.click(screen.getByRole('button', { name: '移除第三晚 TCP 观测' }))
    expect(onInsert).not.toHaveBeenCalled()
    expect(onRemove).not.toHaveBeenCalled()
  })

  it('keeps non-default inventory state on the evidence deep link', () => {
    function Probe() {
      const { state } = useLocation()
      return <pre>{JSON.stringify(state)}</pre>
    }
    render(
      <MemoryRouter initialEntries={[{
        pathname: '/',
        state: { vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_001' },
      }]}>
        <Routes>
          <Route path="/" element={(
            <RecordMaterialDrawer
              open
              onClose={vi.fn()}
              onInsert={vi.fn()}
              onRemove={vi.fn()}
              items={[{ kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测', available: true }]}
            />
          )} />
          <Route path="/evidence/:evidenceId" element={<Probe />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('link', { name: '查看证据' }))
    expect(screen.getByText(/vpsInventoryHref/)).toHaveTextContent(
      '/vps?workspace=ledger&q=Tokyo&selected=vps_001',
    )
  })
})
