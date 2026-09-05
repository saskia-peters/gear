// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { Header } from './Header.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

function renderHeader(initialEntries = ['/']) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={initialEntries}>
        <Header />
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname}</span>
}

describe('Header', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders G.E.A.R. branding', () => {
    renderHeader()

    expect(screen.getByRole('heading', { level: 1, name: 'G.E.A.R.' })).toBeInTheDocument()
  })

  it('toggles light/dark theme when theme button is clicked', async () => {
    const user = userEvent.setup()

    renderHeader()

    const toggleButton = screen.getByRole('button', { name: /dunkelmodus/i })
    expect(toggleButton).toBeInTheDocument()

    // Click to toggle to dark mode
    await user.click(toggleButton)
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(screen.getByRole('button', { name: /hellmodus/i })).toBeInTheDocument()

    // Click again to toggle back to light mode
    await user.click(screen.getByRole('button', { name: /hellmodus/i }))
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('MFA_ACTIVE: shows the "MFA aktiv" badge when the auth state says MFA is enabled', () => {
    localStorage.setItem('gear.is_mfa_enabled', 'true')
    renderHeader()
    expect(screen.getByText('MFA aktiv')).toBeInTheDocument()
  })

  it('MFA_INACTIVE: hides the badge when MFA is not enabled', () => {
    localStorage.clear()
    renderHeader()
    expect(screen.queryByText('MFA aktiv')).not.toBeInTheDocument()
  })

  it('USER_LOGGED_IN: shows the logged-in user name in the header', () => {
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.display_name', 'Erika Musterfrau')
    renderHeader()
    expect(screen.getByText('Erika Musterfrau')).toBeInTheDocument()
  })

  it('USER_LOGGED_IN_PROFILE_LINK: the logged-in user name links to /profil (Story 2.1)', () => {
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.display_name', 'Erika Musterfrau')
    renderHeader()
    const name = screen.getByText('Erika Musterfrau')
    expect(name.tagName).toBe('A')
    expect(name).toHaveAttribute('href', '/profil')
  })

  it('USER_LOGGED_OUT: hides the user name when not authenticated', () => {
    localStorage.clear()
    renderHeader()
    expect(screen.queryByText('Erika Musterfrau')).not.toBeInTheDocument()
  })

  it('STALE_DISPLAY_NAME: hides the name when a cached display name exists but there is no session token', () => {
    // Review finding 1.7-11: a logged-out visitor must never see a previous
    // user's cached display name.
    localStorage.clear()
    localStorage.setItem('gear.display_name', 'Erika Musterfrau')
    renderHeader()
    expect(screen.queryByText('Erika Musterfrau')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Erika Musterfrau' })).not.toBeInTheDocument()
  })

  it('LOGGED_IN: shows the "Abmelden" logout button', () => {
    localStorage.setItem('gear.session_token', 'sesstoken123')
    renderHeader()
    expect(screen.getByRole('button', { name: 'Abmelden' })).toBeInTheDocument()
  })

  it('LOGGED_OUT: hides the logout button when not authenticated', () => {
    localStorage.clear()
    renderHeader()
    expect(screen.queryByRole('button', { name: 'Abmelden' })).not.toBeInTheDocument()
  })

  it('LOGOUT: calls POST /api/v1/auth/logout with the bearer token, clears auth state and redirects to /login', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.display_name', 'Erika Musterfrau')
    localStorage.setItem('gear.is_mfa_enabled', 'true')

    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <LocationProbe />
          <Header />
        </MemoryRouter>
      </ThemeProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Abmelden' }))

    // Server-side invalidation (NFR-S2): the session token is sent as bearer.
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/logout', {
      method: 'POST',
      headers: { Authorization: 'Bearer sesstoken123' },
    })

    // Client auth state is cleared.
    await waitFor(() => {
      expect(localStorage.getItem('gear.session_token')).toBeNull()
      expect(localStorage.getItem('gear.display_name')).toBeNull()
      expect(localStorage.getItem('gear.is_mfa_enabled')).toBeNull()
    })

    // Redirected to /login.
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/login')
    })
  })

  it('LOGOUT_OFFLINE: clears local auth state and redirects even when the logout request fails', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.session_token', 'sesstoken123')

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <LocationProbe />
          <Header />
        </MemoryRouter>
      </ThemeProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Abmelden' }))

    await waitFor(() => {
      expect(localStorage.getItem('gear.session_token')).toBeNull()
      expect(screen.getByTestId('location').textContent).toBe('/login')
    })
  })
})
