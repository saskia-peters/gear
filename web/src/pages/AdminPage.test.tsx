// @vitest-environment jsdom
import { render, screen, within, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminPage } from './AdminPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const PERMISSIONS_KEY = 'gear.permissions'

function seedPermissions(codes: string[]) {
  localStorage.setItem(PERMISSIONS_KEY, JSON.stringify(codes))
}

describe('AdminPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('ADMIN: renders the "Verwaltung — Start" landing hub with the plain-language subtitle', () => {
    seedPermissions([
      'users.manage',
      'tools.manage',
      'dsgvo.delete',
    ])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(
      screen.getByRole('heading', { level: 2, name: 'Verwaltung — Start' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/Willkommen in der Verwaltung/i)).toBeInTheDocument()
  })

  it('NAV_FILTERED: only entries the resolved set allows become cards on the landing', () => {
    // A caller holding only tools.manage sees exactly Übersicht + Werkzeuge
    // cards — no users/roles/dsgvo surfaces are enumerated (FR-19).
    seedPermissions(['tools.manage'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )

    const cards = screen.getByRole('region', { name: 'Verwaltungsbereiche' })
    expect(within(cards).getByRole('link', { name: /Werkzeuge/ })).toHaveAttribute('href', '/admin/werkzeuge')
    expect(within(cards).queryByRole('link', { name: /Benutzer/ })).not.toBeInTheDocument()
    expect(within(cards).queryByRole('link', { name: /Rollen/ })).not.toBeInTheDocument()
    expect(within(cards).queryByRole('link', { name: /DSGVO/ })).not.toBeInTheDocument()
  })

  it('APPROVALS_MOVED: the landing no longer hosts the pending-approvals section (it lives on the Benutzer surface)', () => {
    // users.approve holders no longer see the widget on the landing — it moved
    // to the dedicated "Ausstehende Anträge" page under /admin/benutzer.
    seedPermissions(['users.approve'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(
      screen.queryByRole('heading', { level: 3, name: 'Ausstehende Anträge' }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Keine ausstehenden Anträge' }),
    ).not.toBeInTheDocument()
    // The landing still renders its own hub title.
    expect(
      screen.getByRole('heading', { level: 2, name: 'Verwaltung — Start' }),
    ).toBeInTheDocument()
  })

  it('DUAL_ADMIN: the recovery link is only shown to callers holding admin.recovery.approve', () => {
    // No recovery code → no link.
    seedPermissions(['users.manage'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen.queryByRole('link', { name: 'Dual-Admin-Wiederherstellung' }),
    ).not.toBeInTheDocument()

    cleanup()
    localStorage.clear()
    seedPermissions(['admin.recovery.approve'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen.getByRole('link', { name: 'Dual-Admin-Wiederherstellung' }),
    ).toHaveAttribute('href', '/admin/recovery')
  })

  it('DUAL_ADMIN: the recovery link is only shown to callers holding admin.recovery.approve', () => {
    // No recovery code → no link.
    seedPermissions(['users.manage'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen.queryByRole('link', { name: 'Dual-Admin-Wiederherstellung' }),
    ).not.toBeInTheDocument()

    cleanup()
    localStorage.clear()
    seedPermissions(['admin.recovery.approve'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen.getByRole('link', { name: 'Dual-Admin-Wiederherstellung' }),
    ).toHaveAttribute('href', '/admin/recovery')
  })

  it('GROUPS_CARD: a "Benutzergruppen" card is shown to user_groups.manage holders and links to its own surface', () => {
    localStorage.clear()
    seedPermissions(['users.view', 'user_groups.manage'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    const cards = screen.getByRole('region', { name: 'Verwaltungsbereiche' })
    const card = within(cards).getByRole('link', { name: /Benutzergruppen/ })
    expect(card).toHaveAttribute('href', '/admin/benutzergruppen')
    cleanup()
    localStorage.clear()
    seedPermissions(['users.view'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )
    const cardsAfter = screen.getByRole('region', { name: 'Verwaltungsbereiche' })
    expect(within(cardsAfter).queryByRole('link', { name: /Benutzergruppen/ })).not.toBeInTheDocument()
  })
})
