import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import {
  PRIMARY_NAV_ITEMS,
  PRODUCT_FULL_NAME_ZH,
  PRODUCT_NAME_ZH,
} from '../metadata'
import { AppShell } from './AppShell'

describe('AppShell', () => {
  it('renders the frozen Chinese-first shell chrome and title', () => {
    render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )

    expect(screen.getByText(PRODUCT_NAME_ZH)).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { level: 1, name: PRODUCT_FULL_NAME_ZH }),
    ).toBeInTheDocument()

    PRIMARY_NAV_ITEMS.forEach((item) => {
      expect(screen.getByRole('link', { name: item.label })).toBeInTheDocument()
    })

    expect(screen.getByText('单体中心')).toBeInTheDocument()
    expect(screen.getByText('PostgreSQL')).toBeInTheDocument()
    expect(screen.getByText('systemd agent')).toBeInTheDocument()
    expect(screen.getByText('当前视图')).toBeInTheDocument()
    expect(screen.getByText('V1 冻结基线')).toBeInTheDocument()

    expect(document.title).toBe(PRODUCT_FULL_NAME_ZH)
  })
})
