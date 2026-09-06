// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar.tsx'

const IS_ADMIN_KEY = 'gear.is_admin'

// Story 2.1 task evidence: the ADMIN module's existence is hidden for
// non-admins (no links/menu hints, FR-19) and shown only when the
// server-authoritative cached is_admin flag is true.
describe('Sidebar', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    cleanup()
  })

  it('NONADMIN: no ADMIN link or hint is rendered', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'GEAR' })).toHaveAttribute('href', '/')
    expect(screen.queryByRole('link', { name: 'ADMIN' })).not.toBeInTheDocument()
    expect(screen.queryByText(/ADMIN/i)).not.toBeInTheDocument()
  })

  it('ADMIN: the ADMIN module link is rendered when the cached is_admin flag is true', () => {
    localStorage.setItem(IS_ADMIN_KEY, 'true')
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'GEAR' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: 'ADMIN' })).toHaveAttribute('href', '/admin')
  })
})