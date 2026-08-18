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
