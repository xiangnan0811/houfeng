import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DOCUMENT_MARKDOWN_VERSION_V1 } from '../../../lib/documentMarkdown'
import { MarkdownPreview } from './MarkdownPreview'

describe('MarkdownPreview', () => {
  it('renders the closed model and maps authorized refs to cards', () => {
    render(
      <MarkdownPreview
        model={{
          version: DOCUMENT_MARKDOWN_VERSION_V1,
          nodes: [
            { type: 'heading', level: 1, children: [{ type: 'text', text: 'Outage' }] },
            {
              type: 'reference',
              kind: 'evidence',
              id: 'ev_7K2P',
              children: [{ type: 'text', text: '系统证据：第三晚 TCP 观测' }],
            },
          ],
        }}
        references={[{ kind: 'evidence', id: 'ev_7K2P' }]}
      />,
    )
    expect(screen.getByRole('heading', { name: 'Outage' })).toBeInTheDocument()
    expect(screen.getByText('系统证据：第三晚 TCP 观测').closest('[data-ref-id="ev_7K2P"]')).toHaveClass('card')
  })

  it('keeps hostile source from creating executable HTML', () => {
    const { container } = render(<MarkdownPreview source={'# Safe\n\n<script>window.pwned=true</script>\n\n[run](javascript:alert(1))'} />)
    expect(container.querySelector('script')).toBeNull()
    expect(screen.queryByRole('link', { name: 'run' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'Safe' })).toBeInTheDocument()
  })

  it('treats an empty reference catalog as unauthorized', () => {
    render(
      <MarkdownPreview source="[系统证据：未知](houfeng-evidence:ev_UNKNOWN)" />,
    )
    expect(screen.getByText('引用已失效')).toBeInTheDocument()
  })

  it('falls back to sanitized source when the closed model is invalid', () => {
    render(
      <MarkdownPreview
        source="# Fallback"
        model={{ version: 'houfeng_markdown/v1', nodes: [{ type: 'html', text: '<script>alert(1)</script>' }] }}
      />,
    )
    expect(screen.getByRole('heading', { name: 'Fallback' })).toBeInTheDocument()
  })

  // Reading source-rendered output while believing it came from the server model would
  // hide both an unmodelable body and a model that failed validation.
  it.each([
    ['unsupported body', { source: '- 排查\n  - 磁盘', modelStatus: 'unsupported' as const }],
    ['model that failed validation', {
      source: '- 排查\n  - 磁盘',
      modelStatus: 'ready' as const,
      model: { version: 'houfeng_markdown/v1', nodes: [{ type: 'html', text: 'x' }] },
    }],
  ])('tells the reader when the render model is unavailable: %s', (_name, props) => {
    render(<MarkdownPreview {...props} />)
    expect(screen.getByRole('status')).toHaveTextContent('按源码渲染')
    expect(screen.getByText('磁盘')).toBeInTheDocument()
  })

  it('stays quiet when the model is simply not requested', () => {
    render(<MarkdownPreview source="# Fallback" />)
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('marks unauthorized live refs as tombstones', () => {
    render(
      <MarkdownPreview
        source="[系统证据：未知](houfeng-evidence:ev_UNKNOWN)"
        references={[{ kind: 'evidence', id: 'ev_7K2P' }]}
      />,
    )
    expect(screen.getByText('引用已失效')).toBeInTheDocument()
    expect(screen.getByText('系统证据：未知').closest('.card--dim')).not.toBeNull()
  })
})
