import { readFileSync } from 'node:fs'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { COMMENT_MARKDOWN_VERSION_V1, InvalidCommentRenderModelError } from '../lib/commentMarkdown'
import { RecordCommentMarkdown } from './RecordCommentMarkdown'

describe('RecordCommentMarkdown', () => {
  it('renders the closed model as React DOM with safe links, lists, and code', () => {
    const { container } = render(<RecordCommentMarkdown model={{
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: [
        {
          type: 'paragraph',
          children: [
            { type: 'text', text: 'Use ' },
            { type: 'strong', children: [{ type: 'text', text: 'care' }] },
            { type: 'line_break' },
            { type: 'link', href: 'https://example.com/runbook', children: [{ type: 'text', text: 'runbook' }] },
          ],
        },
        { type: 'fenced_code', text: 'fmt.Println("safe")\n' },
        { type: 'ordered_list', start: 3, items: [[{ type: 'text', text: 'verify' }]] },
      ],
    }} />)

    expect(screen.getByText('care').closest('strong')).not.toBeNull()
    const link = screen.getByRole('link', { name: 'runbook' })
    expect(link).toHaveAttribute('href', 'https://example.com/runbook')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(container.querySelector('pre code')).toHaveTextContent('fmt.Println("safe")')
    expect(container.querySelector('ol')).toHaveAttribute('start', '3')
  })

  it('keeps hostile-looking text literal and creates no HTML element fallback', () => {
    const hostile = '<script>window.pwned = true</script>'
    const { container } = render(<RecordCommentMarkdown model={{
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: [{ type: 'paragraph', children: [{ type: 'text', text: hostile }] }],
    }} />)

    expect(screen.getByText(hostile)).toBeInTheDocument()
    expect(container.querySelector('script')).toBeNull()
  })

  it('fails closed for an untrusted node instead of rendering HTML or JSON fallback', () => {
    expect(() => render(<RecordCommentMarkdown model={{
      version: COMMENT_MARKDOWN_VERSION_V1,
      nodes: [{ type: 'html', text: '<img src=x onerror=alert(1)>' }],
    }} />)).toThrow(InvalidCommentRenderModelError)
  })

  it('contains no raw HTML rendering escape hatch', () => {
    const source = readFileSync('src/components/RecordCommentMarkdown.tsx', 'utf8')
    expect(source).not.toContain('dangerouslySetInnerHTML')
    expect(source).not.toContain('innerHTML')
  })
})
