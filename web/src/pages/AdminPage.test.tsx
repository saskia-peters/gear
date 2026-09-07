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
    // The live PendingApprovals widget fetches the pending list on mount; a
    // stubbed empty response keeps every test hermetic (no real network) while
    // exercising the real component.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ users: [] }),
    }))
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

  it('APPROVALS_GATED: a tools-only caller (no users.approve) does not see the pending-approvals section', () => {
    // schirrmeister/fuehrende hold tools codes but no users.approve: they get
    // the cards + subtitle, not the approvals they cannot act on.
    seedPermissions(['tools.manage', 'tool_types.manage'])
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
  })

  it('APPROVALS_GATED: a users.view-only caller does NOT mount the widget (server requires users.approve)', () => {
    // The server /users/* endpoints require ONLY users.approve (AD-6/FR-20).
    // A users.view-only caller must not mount the widget: it would be 403'd
    // and adminForbiddenHandled would force them out of the admin module.
    seedPermissions(['users.view'])
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
    // The caller is NOT navigated away — the landing still renders.
    expect(
      screen.getByRole('heading', { level: 2, name: 'Verwaltung — Start' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Keine ausstehenden Anträge' })).not.toBeInTheDocument()
  })

  it('LANDING_EMPTY: the empty state announces "Keine ausstehenden Anträge"', async () => {
    // users.approve opens the pending-approvals section. The live widget's
    // fetch resolves to an empty list, so the empty state renders.
    seedPermissions(['users.approve'])
    render(
      <ThemeProvider>
        <MemoryRouter>
          <AdminPage />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(
      screen.getByRole('heading', { level: 3, name: 'Ausstehende Anträge' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('heading', { name: 'Keine ausstehenden Anträge' }),
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
})
