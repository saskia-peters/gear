// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar.tsx'

const PERMISSIONS_KEY = 'gear.permissions'

// Story 2.3 task evidence: the ADMIN module's existence is hidden for callers
// with no admin-module code (no links/menu hints, FR-19) and shown only when
// the server-authoritative cached resolved permission set contains an
// admin-module code.
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

  it('ADMIN: the ADMIN module link is rendered when the cached resolved set contains an admin-module code', () => {
    localStorage.setItem(PERMISSIONS_KEY, JSON.stringify(['tools.manage']))
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'GEAR' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: 'ADMIN' })).toHaveAttribute('href', '/admin')
  })
})