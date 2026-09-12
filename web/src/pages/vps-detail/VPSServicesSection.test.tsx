import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AssetServiceRecord } from '../../lib/types'
import { VPSServicesSection } from './VPSServicesSection'

const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard')
const ORIGINAL_SECURE_CONTEXT = Object.getOwnPropertyDescriptor(window, 'isSecureContext')

function service(overrides: Partial<AssetServiceRecord> = {}): AssetServiceRecord {
  return {
    service_id: 'svc_001',
    vps_id: 'vps_001',
    name: 'Gateway',
    service_type: 'web',
    status: 'active',
    url: 'https://example.invalid',
    labels: [],
    note: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

function renderSection(services: AssetServiceRecord[]) {
  return render(
    <MemoryRouter>
      <VPSServicesSection
        services={services}
        error={null}
        notice={null}
        onCreate={vi.fn()}
      />
    </MemoryRouter>,
  )
}

function setClipboardWriteText(impl: (text: string) => Promise<void>) {
  const writeText = vi.fn(impl)
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
  return writeText
}

function restoreClipboard() {
  if (ORIGINAL_CLIPBOARD) {
    Object.defineProperty(globalThis.navigator, 'clipboard', ORIGINAL_CLIPBOARD)
  } else {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: undefined,
    })
  }
}

function restoreSecureContext() {
  if (ORIGINAL_SECURE_CONTEXT) {
    Object.defineProperty(window, 'isSecureContext', ORIGINAL_SECURE_CONTEXT)
  }
}

function listitem(rows: HTMLElement[], index: number): HTMLElement {
  const row = rows[index]
  if (!row) throw new Error(`missing listitem ${index}`)
  return row
}

describe('VPSServicesSection', () => {
  afterEach(() => {
    restoreClipboard()
    restoreSecureContext()
  })

  it('keeps complete rows with HTTP-only open and full non-HTTP copy', () => {
    renderSection([
      service({
        port: 443,
        target_id: 'tg_001',
      }),
      service({
        service_id: 'svc_empty',
        name: 'Empty Gateway',
        url: '',
        port: null,
      }),
      service({
        service_id: 'svc_grpc',
        name: 'Long Stream',
        url: 'grpc://stream.example.invalid/very/long/path:50051',
        port: 50051,
      }),
    ])

    const rows = screen.getAllByRole('listitem')
    expect(rows).toHaveLength(3)

    const httpRow = within(listitem(rows, 0))
    const url = httpRow.getByText('https://example.invalid')
    expect(url.tagName).toBe('CODE')
    expect(httpRow.queryByRole('link', { name: 'https://example.invalid' })).not.toBeInTheDocument()
    expect(httpRow.getByRole('button', { name: '复制入口' })).toBeInTheDocument()
    expect(httpRow.getByRole('link', { name: '打开入口' })).toHaveAttribute('href', 'https://example.invalid')
    expect(httpRow.getByRole('link', { name: 'tg_001' })).toHaveAttribute('href', '/targets/tg_001')

    const emptyRow = within(listitem(rows, 1))
    expect(emptyRow.queryByRole('button', { name: '复制入口' })).not.toBeInTheDocument()
    expect(emptyRow.queryByRole('link', { name: '打开入口' })).not.toBeInTheDocument()

    const grpcRow = within(listitem(rows, 2))
    expect(grpcRow.getByText('grpc://stream.example.invalid/very/long/path:50051').tagName).toBe('CODE')
    expect(grpcRow.queryByRole('link', { name: /grpc:/ })).not.toBeInTheDocument()
    expect(grpcRow.queryByRole('link', { name: '打开入口' })).not.toBeInTheDocument()
    expect(grpcRow.getByRole('button', { name: '复制入口' })).toBeInTheDocument()
  })

  it('copies the raw URL and reports failure without a substitute', async () => {
    Object.defineProperty(window, 'isSecureContext', {
      configurable: true,
      get: () => true,
    })
    const writeText = setClipboardWriteText(async () => {
      throw new Error('denied')
    })
    renderSection([service({ url: '  https://example.invalid/raw  ' })])

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '复制入口' }))
    })

    expect(writeText).toHaveBeenCalledWith('https://example.invalid/raw')
    expect(screen.getByRole('button', { name: '复制入口' })).toHaveTextContent('复制失败')
    expect(screen.getByText('入口复制失败')).toBeInTheDocument()
  })
})
