import { act, fireEvent, render, screen } from '@testing-library/react'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import { NotFoundPage } from './NotFoundPage'

const disposers: Array<() => void> = []
afterEach(() => {
  disposers.splice(0).forEach((dispose) => dispose())
  window.history.replaceState(null, '', '/')
})

function setup() {
  const router = createBrowserRouter([
    { path: '/', element: <h1>工作台</h1> },
    { path: '/known', element: <h1>已知页面</h1> },
    { path: '*', element: <NotFoundPage /> },
  ])
  disposers.push(() => router.dispose())
  render(<RouterProvider router={router} />)
  return router
}

describe('NotFoundPage', () => {
  it('preserves an unknown address and safely falls back on a direct visit', async () => {
    window.history.replaceState({ idx: 0 }, '', '/missing?x=1#anchor')
    setup()
    expect(screen.getByRole('heading', { name: '没有这一页' })).toBeInTheDocument()
    expect(window.location.pathname + window.location.search + window.location.hash).toBe('/missing?x=1#anchor')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '返回上一页' }))
    expect(await screen.findByRole('heading', { name: '工作台' })).toBeInTheDocument()
  })

  it('returns to a known in-app history entry', async () => {
    window.history.replaceState({ idx: 0 }, '', '/known')
    const router = setup()
    await act(async () => { await router.navigate('/missing?keep=yes') })
    fireEvent.click(screen.getByRole('button', { name: '返回上一页' }))
    expect(await screen.findByRole('heading', { name: '已知页面' })).toBeInTheDocument()
  })
})
