import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { payloadFromRevision, recordRevisionFixture } from '../testFixtures'
import { RecordSaveImpact } from './RecordSaveImpact'

describe('RecordSaveImpact', () => {
  it('lists formal revision field changes only', () => {
    const baseline = recordRevisionFixture()
    const payload = { ...payloadFromRevision(baseline), title: 'Next title', body_markdown: '# Changed' }
    render(<RecordSaveImpact baseline={baseline} payload={payload} />)
    expect(screen.getByText('标题将写入新修订')).toBeInTheDocument()
    expect(screen.getByText('正文将写入新修订')).toBeInTheDocument()
  })

  it('reports no impact for an untouched revision even though the save reason resets', () => {
    const baseline = recordRevisionFixture({ save_reason: '首次记录' })
    render(<RecordSaveImpact baseline={baseline} payload={payloadFromRevision(baseline)} />)
    expect(screen.getByText('正式修订字段尚未变化')).toBeInTheDocument()
  })

  it('covers metadata fields the earlier list left out', () => {
    const baseline = recordRevisionFixture()
    const payload = {
      ...payloadFromRevision(baseline),
      occurred_at: '2026-08-18T02:00:00Z',
      tags: ['migration'],
    }
    render(<RecordSaveImpact baseline={baseline} payload={payload} />)
    expect(screen.getByText('发生时间将写入新修订')).toBeInTheDocument()
    expect(screen.getByText('标签将写入新修订')).toBeInTheDocument()
  })
})
