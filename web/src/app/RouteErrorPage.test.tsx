import { lazy, Suspense, type ReactNode } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { RouteErrorPage } from './RouteErrorPage'

function renderRoute(element: ReactNode, reloadPage = vi.fn()) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element,
        errorElement: <RouteErrorPage reloadPage={reloadPage} />,
      },
    ],
    { initialEntries: ['/'] },
  )
  render(<RouterProvider router={router} />)
  return reloadPage
}

describe('RouteErrorPage', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('recovers from a route render error without exposing its details', async () => {
    let shouldThrow = true
    function ThrowingRoute() {
      if (shouldThrow) throw new Error('private-route-secret-17')
      return <p>页面已恢复</p>
    }

    renderRoute(<ThrowingRoute />)

    expect(
      await screen.findByRole('heading', { name: '页面暂时无法显示' }),
    ).toBeInTheDocument()
    expect(document.body).not.toHaveTextContent('private-route-secret-17')
    expect(screen.getByRole('link', { name: '返回工作台' })).toHaveAttribute('href', '/')

    shouldThrow = false
    fireEvent.click(screen.getByRole('button', { name: '重试当前页面' }))
    expect(await screen.findByText('页面已恢复')).toBeInTheDocument()
  })

  it('shows the safe recovery page when a lazy route import rejects', async () => {
    const reloadPage = vi.fn()
    const RejectedRoute = lazy(() =>
      Promise.reject(new Error('private-chunk-url-and-session')),
    )

    renderRoute(
      <Suspense fallback={<p>页面模块加载中</p>}>
        <RejectedRoute />
      </Suspense>,
      reloadPage,
    )

    expect(
      await screen.findByRole('heading', { name: '页面暂时无法显示' }),
    ).toBeInTheDocument()
    expect(document.body).not.toHaveTextContent('private-chunk-url-and-session')

    fireEvent.click(screen.getByRole('button', { name: '刷新页面' }))
    expect(reloadPage).toHaveBeenCalledTimes(1)
  })
})
