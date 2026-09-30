import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DOCUMENT_MARKDOWN_VERSION_V1 } from '../../../lib/documentMarkdown'
import { RecordOutline } from './RecordOutline'

describe('RecordOutline', () => {
  it('lists headings from the render model', () => {
    render(
      <RecordOutline
        source=""
        model={{
          version: DOCUMENT_MARKDOWN_VERSION_V1,
          nodes: [{ type: 'heading', level: 2, children: [{ type: 'text', text: 'Recovered' }] }],
        }}
      />,
    )
    expect(screen.getByText('Recovered')).toHaveAttribute('data-level', '2')
  })
})

describe('RecordOutline source fallback', () => {
  it('ignores fenced code comments, indents relative to the top level and hides short outlines', () => {
    const source = '## 现象\n\n```sh\n# 复现丢包\n```\n\n### 细节\n## 结论'
    const { rerender } = render(<RecordOutline source={source} />)
    expect(screen.queryByText('复现丢包')).not.toBeInTheDocument()
    expect(screen.getByText('现象')).toHaveAttribute('data-depth', '0')
    expect(screen.getByText('细节')).toHaveAttribute('data-depth', '1')
    rerender(<RecordOutline source="## 唯一" minHeadings={2} />)
    expect(screen.queryByRole('navigation', { name: '正文大纲' })).not.toBeInTheDocument()
  })
})

describe('RecordOutline fence closing', () => {
  it('closes a fence only on a same-character line at least as long with nothing after it', () => {
    const source = [
      '## 开头',
      '````md',
      '```javascript',
      '# 不是标题',
      '```',
      '## 仍在代码里',
      '````',
      '## 结尾',
    ].join('\n')
    render(<RecordOutline source={source} />)
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual(['开头', '结尾'])
  })
})

describe('RecordOutline line endings', () => {
  it.each([['LF', '\n'], ['CRLF', '\r\n'], ['CR', '\r']])('closes fences with %s line endings', (_name, eol) => {
    const source = ['## 开头', '```sh', '# 注释', '```', '## 结尾'].join(eol)
    render(<RecordOutline source={source} />)
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual(['开头', '结尾'])
  })
})
