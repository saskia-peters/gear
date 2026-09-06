// @vitest-environment jsdom
import { render, screen, within, waitFor, act, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import App, { AppRoutes } from './App.tsx'
import { ThemeProvider } from './context/ThemeContext.tsx'
import { Sidebar } from './components/Sidebar.tsx'

const TOKEN_STORAGE_KEY = 'gear.session_token'
const IS_ADMIN_KEY = 'gear.is_admin'

// validProfile is the GET /api/v1/auth/profile response the hardened RequireAuth
// guard uses to validate a stored session server-side (Story 1.8).
function validProfile(overrides: Record<string, unknown> = {}) {
  return {
    ok: true,
    status: 200,
    json: async () => ({
      id: 'u-1',
      email: 'max@example.com',
      first_name: 'Max',
      last_name: 'Mustermann',
      display_name: 'Max Mustermann',
      is_admin: false,
      ...overrides,
    }),
  }
}

// stubSessionValidation makes the RequireAuth server-side check resolve
// immediately. Subsequent fetch calls (pages themselves) fall through to
// secondary (or the default valid profile when only one is given).
function stubSessionValidation(response: unknown = validProfile()) {
  const mock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', mock)
  return mock
}

async function renderApp() {
  let view: ReturnType<typeof render>
  await act(async () => {
    view = render(<App />)
  })
  return view!
}

describe('App & Dashboard Foundation', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem(TOKEN_STORAGE_KEY, 'test-session-token')
    document.documentElement.removeAttribute('data-theme')
    // Reset the browser history so an App-level test never starts on a URL a
    // previous test navigated to (BrowserRouter keeps the location across
    // unmounts; only cleanup() resets the DOM).
    window.history.replaceState({}, '', '/')
    stubSessionValidation()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: renders Header, overview title, 2x2 summary grid, active "Alle" filter chip, and empty state at /', async () => {
    await renderApp()

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
    await renderApp()

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
    await renderApp()

    const toggleButton = screen.getByRole('button', { name: /dunkelmodus/i })
    await user.click(toggleButton)

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')

    const lightButton = screen.getByRole('button', { name: /hellmodus/i })
    await user.click(lightButton)

    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('SIDEBAR: the authenticated shell shows the GEAR module and hides ADMIN for a non-admin', async () => {
    await renderApp()

    // GEAR is always present; non-admin users never see ADMIN (the admin case
    // is covered by the dedicated Sidebar describe below).
    expect(screen.getByRole('link', { name: 'GEAR' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'ADMIN' })).not.toBeInTheDocument()
  })

  it('PROTECTED_DASHBOARD: redirects to /login when no session token is present and clears stale auth cache', async () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    // Stale cached auth data from a previous session must not linger
    // (review finding 1.8-8): the no-token branch clears it like the 401 path.
    localStorage.setItem('gear.display_name', 'Max Mustermann')
    localStorage.setItem('gear.is_mfa_enabled', 'true')
    localStorage.setItem(IS_ADMIN_KEY, 'true')
    await renderApp()

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Anmelden' })).toBeInTheDocument()
    expect(localStorage.getItem('gear.display_name')).toBeNull()
    expect(localStorage.getItem('gear.is_mfa_enabled')).toBeNull()
    expect(localStorage.getItem(IS_ADMIN_KEY)).toBeNull()
  })

  it('SIDEBAR_ADMIN_E2E: a stored token validated with is_admin=true renders the ADMIN link in the shell', async () => {
    // Pins review finding 1.8-12: RequireAuth's GET /auth/profile drives the
    // server-authoritative admin flag, and the sidebar reflects it.
    stubSessionValidation(validProfile({ is_admin: true }))
    await renderApp()

    expect(screen.getByRole('link', { name: 'GEAR' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'ADMIN' })).toHaveAttribute('href', '/admin')
  })

  it('ADMIN_ROUTE (CLIENT_ADMIN_NAV): a genuine admin navigating to /admin sees the module, resolved server-side', async () => {
    // The route guard resolves is_admin from GET /api/v1/auth/profile — NOT from
    // the forgeable cached flag (review finding 2.1-2) — so no stale/forged
    // cache is seeded here.
    stubSessionValidation(validProfile({ is_admin: true }))
    await act(async () => {
      render(
        <ThemeProvider>
          <MemoryRouter initialEntries={['/admin']}>
            <AppRoutes />
          </MemoryRouter>
        </ThemeProvider>,
      )
    })

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Admin-Modul' }),
    ).toBeInTheDocument()
    // The admin sees the ADMIN module navigation too (server-authoritative).
    expect(screen.getByRole('link', { name: 'ADMIN' })).toHaveAttribute('href', '/admin')
  })

  it('ADMIN_ROUTE_NONADMIN: a non-admin force-navigating to /admin is redirected to the Dashboard with no admin UI', async () => {
    // UX-DR6/FR-19: the route guard (not just the sidebar link) hides the admin
    // module. A force-navigating non-admin lands on the Dashboard and never
    // sees the admin module or its hints.
    stubSessionValidation(validProfile({ is_admin: false }))
    await act(async () => {
      render(
        <ThemeProvider>
          <MemoryRouter initialEntries={['/admin']}>
            <AppRoutes />
          </MemoryRouter>
        </ThemeProvider>,
      )
    })

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Übersicht' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Admin-Modul' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'ADMIN' })).not.toBeInTheDocument()
  })

  it('ADMIN_RECOVERY_ROUTE_NONADMIN: a non-admin force-navigating to /admin/recovery is redirected to the Dashboard', async () => {
    // The recovery surface is part of the isolated admin module: a non-admin
    // never sees it (server-side RequirePermission would 403 the requests).
    stubSessionValidation(validProfile({ is_admin: false }))
    await act(async () => {
      render(
        <ThemeProvider>
          <MemoryRouter initialEntries={['/admin/recovery']}>
            <AppRoutes />
          </MemoryRouter>
        </ThemeProvider>,
      )
    })

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Übersicht' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Admin-Wiederherstellung' }),
    ).not.toBeInTheDocument()
  })

  it('REQUIRE_AUTH_VALID: a stored token validated server-side grants access to the dashboard', async () => {
    await renderApp()
    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()
  })

  it('REQUIRE_AUTH_REVOKED: a revoked session (401) clears auth state and forces /login even with a stored token', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
      }),
    )
    await renderApp()

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    })
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(IS_ADMIN_KEY)).toBeNull()
  })

  it('REQUIRE_AUTH_PAGESHOW: re-validates on bfcache restore (pageshow persisted) so logout is enforced after back navigation', async () => {
    const mock = vi
      .fn()
      .mockResolvedValueOnce(validProfile())
      .mockResolvedValueOnce(
        Promise.resolve({
          ok: false,
          status: 401,
          json: async () => ({ error: { code: 'unauthorized' } }),
        }),
      )
    vi.stubGlobal('fetch', mock)

    await renderApp()
    expect(screen.getByRole('heading', { level: 2, name: 'Übersicht' })).toBeInTheDocument()

    // Simulate a back-forward-cache restore: the session is re-validated and,
    // being revoked, clears state and redirects to /login.
    const event = new Event('pageshow') as PageTransitionEvent
    Object.defineProperty(event, 'persisted', { value: true })
    act(() => {
      window.dispatchEvent(event)
    })

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    })
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
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

  it('FORGOT_PASSWORD_ROUTE: renders the forgot-password page at /forgot-password', async () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/forgot-password']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Passwort vergessen' })).toBeInTheDocument()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Link anfordern' })).toBeInTheDocument()
  })

  it('RESET_PASSWORD_ROUTE: renders the set-new-password page at /reset-password/:token', async () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/reset-password/opaque-token']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Neues Passwort festlegen' })).toBeInTheDocument()
    expect(screen.getByLabelText('Neues Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Wiederholung')).toBeInTheDocument()
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
      await screen.findByRole('heading', { level: 2, name: 'Zwei-Faktor-Authentifizierung' }),
    ).toBeInTheDocument()
  })

  it('MFA_ROUTE_PROTECTED: redirects to /login without a session token', async () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/mfa']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })

  it('PASSWORD_ROUTE: renders the change-password page at /password for an authenticated user', async () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/password']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Passwort ändern' })).toBeInTheDocument()
    expect(screen.getByLabelText('Aktuelles Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Neues Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Wiederholung')).toBeInTheDocument()
  })

  it('PASSWORD_ROUTE_PROTECTED: redirects to /login without a session token', async () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/password']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })

  it('PROFILE_ROUTE: renders the Profil page at /profil for an authenticated user', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(validProfile()),
    )
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/profil']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Profil' })).toBeInTheDocument()
    expect(await screen.findByLabelText('Vorname')).toHaveValue('Max')
    expect(
      screen.getByRole('link', { name: /Zwei-Faktor-Authentifizierung verwalten/ }),
    ).toHaveAttribute('href', '/mfa')
    expect(screen.getByRole('link', { name: 'Passwort ändern' })).toHaveAttribute('href', '/password')
  })

  it('PROFILE_ROUTE_PROTECTED: redirects to /login without a session token', async () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/profil']}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
  })
})

describe('Sidebar', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    cleanup()
  })

  it('GEAR module is always shown', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'GEAR' })).toHaveAttribute('href', '/')
    expect(screen.queryByRole('link', { name: 'ADMIN' })).not.toBeInTheDocument()
  })

  it('ADMIN module is shown only when the cached is_admin flag is true', () => {
    localStorage.setItem(IS_ADMIN_KEY, 'true')
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'GEAR' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'ADMIN' })).toHaveAttribute('href', '/admin')
  })
})