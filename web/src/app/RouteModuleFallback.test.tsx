import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { RouteModuleFallback } from './RouteModuleFallback'

describe('RouteModuleFallback', () => {
  it('renders a calm route-level loading state without a spinner', () => {
    render(<RouteModuleFallback label="正在加载事件时间线" />)

    expect(screen.getByRole('heading', { name: '正在加载事件时间线' })).toBeInTheDocument()
    expect(screen.getByText('正在读取页面模块…')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })
})
