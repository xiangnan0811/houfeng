import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AttachmentMetadata } from '../../../lib/types'
import { useAttachmentMetadata } from './useAttachmentMetadata'

function metadata(id: string): AttachmentMetadata {
  return { attachment_id: id, state: 'available', display_name: `${id}.txt`, media_type: 'text/plain', size_bytes: 1, preview_available: false }
}

describe('useAttachmentMetadata', () => {
  it('reads each attachment once and keeps results across reordering', async () => {
    const load = vi.fn((id: string) => Promise.resolve(metadata(id)))
    const { result, rerender } = renderHook(({ ids }) => useAttachmentMetadata(ids, load), {
      initialProps: { ids: ['att_a', 'att_b'] },
    })
    await waitFor(() => expect(result.current.get('att_b')).toEqual({ status: 'ready', metadata: metadata('att_b') }))
    rerender({ ids: ['att_b', 'att_a', 'att_b'] })
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('marks unreadable attachments unavailable without telling denial from absence', async () => {
    const load = vi.fn()
      .mockRejectedValueOnce(new Error('not found'))
      .mockImplementationOnce(() => { throw new Error('sync failure') })
    const { result } = renderHook(() => useAttachmentMetadata(['att_gone', 'att_broken'], load))
    await waitFor(() => expect(result.current.get('att_gone')).toEqual({ status: 'unavailable' }))
    await waitFor(() => expect(result.current.get('att_broken')).toEqual({ status: 'unavailable' }))
  })

  it('aborts pending reads on unmount and ignores late results', async () => {
    let signal: AbortSignal | undefined
    let resolve: (value: AttachmentMetadata) => void = () => undefined
    const load = vi.fn((_id: string, nextSignal: AbortSignal) => {
      signal = nextSignal
      return new Promise<AttachmentMetadata>((next) => { resolve = next })
    })
    const { result, unmount } = renderHook(() => useAttachmentMetadata(['att_slow'], load))
    await waitFor(() => expect(load).toHaveBeenCalled())
    expect(result.current.get('att_slow')).toEqual({ status: 'loading' })
    unmount()
    expect(signal?.aborted).toBe(true)
    resolve(metadata('att_slow'))
  })
})
