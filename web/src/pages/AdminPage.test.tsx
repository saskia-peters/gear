// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminPage } from './AdminPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

describe('AdminPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    cleanup()
  })

  it('ADMIN: renders the admin module home', () => {
    // The route guard (RequireAdmin) resolves admin status server-side and
    // redirects non-admins, so this page only ever renders for an admin (review
    // finding 2.1-3) — no existence-leaking "Zugriff verweigert" branch exists.
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Admin-Modul' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Dual-Admin-Wiederherstellung' })).toHaveAttribute(
      'href',
      '/admin/recovery',
    )
  })
})