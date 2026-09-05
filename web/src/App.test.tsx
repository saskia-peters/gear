// @vitest-environment jsdom
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import App, { AppRoutes } from './App.tsx'
import { ThemeProvider } from './context/ThemeContext.tsx'

const TOKEN_STORAGE_KEY = 'gear.session_token'

describe('App & Dashboard Foundation', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem(TOKEN_STORAGE_KEY, 'test-session-token')
    document.documentElement.removeAttribute('data-theme')
  })

  it('HAPPY_PATH: renders Header, overview title, 2x2 summary grid, active "Alle" filter chip, and empty state at /', () => {
    render(<App />)

    // Header & Brand
    expect(screen.getByRole('heading', { level: 1, name: 'G.E.A.R.' })).toBeInTheDocument()

    // Page Title
    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()
    expect(screen.getByText(/Geräteverwaltung & Einsatzbereitschaft/i)).toBeInTheDocument()

    // Summary count cards in Statusübersicht
    const statusRegion = screen.getByRole('region', { name: 'Statusübersicht' })
    expect(within(statusRegion).getByText('Einsatzbereit')).toBeInTheDocument()
    expect(within(statusRegion).getByText('Ausstehend')).toBeInTheDocument()
    expect(within(statusRegion).getByText('Überfällig')).toBeInTheDocument()
    expect(within(statusRegion).getByText('Außer Betrieb')).toBeInTheDocument()
    const countZeros = within(statusRegion).getAllByText('0')
    expect(countZeros).toHaveLength(4)

    // Filter Chips in Statusfilter
    const filterNav = screen.getByRole('navigation', { name: 'Statusfilter' })
    const alleChip = within(filterNav).getByRole('button', { name: 'Alle' })
    expect(alleChip).toBeInTheDocument()
    expect(alleChip).toHaveAttribute('aria-pressed', 'true')

    // Empty state
    expect(screen.getByRole('heading', { level: 2, name: 'Keine Werkzeuge vorhanden' })).toBeInTheDocument()
  })

  it('FILTER_SELECT: clicking "Überfällig" filter chip marks it active while empty state remains visible', async () => {
    const user = userEvent.setup()
    render(<App />)

    const filterNav = screen.getByRole('navigation', { name: 'Statusfilter' })
    const ueberfaelligChip = within(filterNav).getByRole('button', { name: 'Überfällig' })
    const alleChip = within(filterNav).getByRole('button', { name: 'Alle' })

    expect(alleChip).toHaveAttribute('aria-pressed', 'true')
    expect(ueberfaelligChip).toHaveAttribute('aria-pressed', 'false')

    await user.click(ueberfaelligChip)

    expect(ueberfaelligChip).toHaveAttribute('aria-pressed', 'true')
    expect(alleChip).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('heading', { level: 2, name: 'Keine Werkzeuge vorhanden' })).toBeInTheDocument()
  })

  it('DARK_MODE: toggles theme and updates documentElement data-theme attribute', async () => {
    const user = userEvent.setup()
    render(<App />)

    const toggleButton = screen.getByRole('button', { name: /dunkelmodus/i })
    await user.click(toggleButton)

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')

    const lightButton = screen.getByRole('button', { name: /hellmodus/i })
    await user.click(lightButton)

    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('PROTECTED_DASHBOARD: redirects to /login when no session token is present', () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(<App />)

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Anmelden' })).toBeInTheDocument()
  })

  it('LOGIN_ROUTE: renders login form at /login and allows navigation back to dashboard', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/login']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeInTheDocument()
    expect(screen.getByLabelText('Passwort')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Anmelden' })).toBeInTheDocument()

    const registerLink = screen.getByRole('link', { name: 'Noch kein Konto? Jetzt registrieren' })
    expect(registerLink).toBeInTheDocument()

    const backLink = screen.getByRole('link', { name: 'Zurück zur Übersicht' })
    expect(backLink).toBeInTheDocument()
    await user.click(backLink)

    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()
  })

  it('REGISTER_ROUTE: renders register page at /register and allows navigation back to overview', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/register']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Registrierung' })).toBeInTheDocument()
    expect(screen.getByLabelText('Vorname')).toBeInTheDocument()

    const backLink = screen.getByRole('link', { name: 'Zurück zur Übersicht' })
    expect(backLink).toBeInTheDocument()
    await user.click(backLink)

    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()
  })

  it('UNKNOWN_ROUTE: renders clean 404 with return link when navigating to /unbekannt', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/unbekannt']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: '404 — Seite nicht gefunden' })).toBeInTheDocument()
    expect(screen.getByText(/Die aufgerufene Seite existiert nicht oder wurde verschoben/i)).toBeInTheDocument()

    const backLink = screen.getByRole('link', { name: 'Zurück zur Übersicht' })
    expect(backLink).toBeInTheDocument()
    await user.click(backLink)

    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()
  })

  it('MFA_ROUTE: renders the MFA settings page at /mfa for an authenticated user', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ enabled: false }),
      }),
    )
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/mfa']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(
      screen.getByRole('heading', { level: 2, name: 'Zwei-Faktor-Authentifizierung' }),
    ).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('MFA_ROUTE_PROTECTED: redirects to /login without a session token', () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/mfa']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })

  it('PASSWORD_ROUTE: renders the change-password page at /password for an authenticated user', () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/password']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Passwort ändern' })).toBeInTheDocument()
    expect(screen.getByLabelText('Aktuelles Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Neues Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Wiederholung')).toBeInTheDocument()
  })

  it('PASSWORD_ROUTE_PROTECTED: redirects to /login without a session token', () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/password']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })

  it('PROFILE_ROUTE: renders the Profil page at /profil for an authenticated user', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          id: 'u-1',
          email: 'max@example.com',
          first_name: 'Max',
          last_name: 'Mustermann',
          display_name: 'Max Mustermann',
        }),
      }),
    )
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/profil']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Profil' })).toBeInTheDocument()
    expect(screen.getByLabelText('Vorname')).toHaveValue('Max')
    expect(
      screen.getByRole('link', { name: /Zwei-Faktor-Authentifizierung verwalten/ }),
    ).toHaveAttribute('href', '/mfa')
    expect(screen.getByRole('link', { name: 'Passwort ändern' })).toHaveAttribute('href', '/password')
    vi.unstubAllGlobals()
  })

  it('PROFILE_ROUTE_PROTECTED: redirects to /login without a session token', () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/profil']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })
})
