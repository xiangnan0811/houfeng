import { describe, expect, it } from 'vitest'

import { eventPageRange, visiblePageItems } from './eventsPagination'

describe('eventPageRange', () => {
  it('returns a 1-based inclusive range for the current page', () => {
    expect(eventPageRange(1, 20, 45)).toEqual({ start: 1, end: 20 })
    expect(eventPageRange(2, 20, 45)).toEqual({ start: 21, end: 40 })
    expect(eventPageRange(3, 20, 45)).toEqual({ start: 41, end: 45 })
  })

  it('returns zeros when there is nothing to show', () => {
    expect(eventPageRange(1, 20, 0)).toEqual({ start: 0, end: 0 })
    expect(eventPageRange(4, 20, 20)).toEqual({ start: 0, end: 0 })
  })
})

describe('visiblePageItems', () => {
  it('lists every page when the set is short', () => {
    expect(visiblePageItems(1, 1)).toEqual([1])
    expect(visiblePageItems(2, 4)).toEqual([1, 2, 3, 4])
    expect(visiblePageItems(1, 7)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('keeps first, last, and a window around the current page', () => {
    expect(visiblePageItems(1, 10)).toEqual([1, 2, 'ellipsis', 10])
    expect(visiblePageItems(5, 10)).toEqual([1, 'ellipsis', 4, 5, 6, 'ellipsis', 10])
    expect(visiblePageItems(10, 10)).toEqual([1, 'ellipsis', 9, 10])
  })
})
