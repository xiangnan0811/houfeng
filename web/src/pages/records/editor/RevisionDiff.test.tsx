import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { payloadFromRevision, recordRevisionFixture } from '../testFixtures'
import { RevisionDiff } from './RevisionDiff'

describe('RevisionDiff', () => {
  it('renders markdown hunks for body changes', () => {
    const base = recordRevisionFixture()
    render(<RevisionDiff base={base} local={{ ...payloadFromRevision(base), body_markdown: '# Details\nStill open.' }} />)
    expect(screen.getByLabelText('正文差异')).toBeInTheDocument()
    expect(screen.getByText('Still open.', { exact: false })).toBeInTheDocument()
  })
})
