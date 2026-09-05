// @vitest-environment jsdom
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { LoginPage } from './LoginPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const TOKEN_STORAGE_KEY = 'gear.session_token'
const LOCKOUT_STORAGE_KEY = 'gear.login_lockout_until'

function renderLoginPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>Übersicht</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetch(response: {
  ok: boolean
  status: number
  body: unknown
}) {
  const mock = vi.fn().mockResolvedValue({
    ok: response.ok,
    status: response.status,
    json: async () => response.body,
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

async function submitLogin(email: string, password: string) {
  const user = userEvent.setup()
  renderLoginPage()
  await user.type(screen.getByLabelText('E-Mail-Adresse'), email)
  await user.type(screen.getByLabelText('Passwort'), password)
  await user.click(screen.getByRole('button', { name: 'Anmelden' }))
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: renders login form with email, password and navigation links', () => {
    renderLoginPage()

    expect(screen.getByRole('heading', { level: 2, name: 'Anmeldung' })).toBeInTheDocument()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeInTheDocument()
    expect(screen.getByLabelText('Passwort')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Anmelden' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Noch kein Konto? Jetzt registrieren' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zurück zur Übersicht' })).toBeInTheDocument()
  })

  it('MISSING_FIELDS: shows validation errors when submitting with empty fields', async () => {
    const user = userEvent.setup()
    renderLoginPage()

    await user.click(screen.getByRole('button', { name: 'Anmelden' }))

    expect(screen.getByText('Bitte gib deine E-Mail-Adresse ein.')).toBeInTheDocument()
    expect(screen.getByText('Bitte gib dein Passwort ein.')).toBeInTheDocument()
  })

  it('INVALID_EMAIL: shows validation error on invalid email format', async () => {
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'invalid-email')
    await user.type(screen.getByLabelText('Passwort'), 'geheim123456')
    await user.click(screen.getByRole('button', { name: 'Anmelden' }))

    expect(screen.getByText('Bitte gib eine gültige E-Mail-Adresse ein.')).toBeInTheDocument()
  })

  it('HAPPY_PATH: successful login stores token and navigates to the dashboard', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: {
        token: 'opaque-session-token',
        user: { id: 'u-1', email: 'erika@example.com', display_name: 'Erika Musterfrau' },
      },
    })

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('opaque-session-token')
    })

    expect(screen.getByText('Übersicht')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'erika@example.com', password: 'geheim123456' }),
    })
  })

  it('HAPPY_PATH_MFA: a successful login persists the is_mfa_enabled flag for the SPA indicator', async () => {
    stubFetch({
      ok: true,
      status: 200,
      body: {
        token: 'opaque-session-token',
        user: { id: 'u-1', email: 'erika@example.com', display_name: 'Erika Musterfrau', is_mfa_enabled: true },
      },
    })

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(localStorage.getItem('gear.is_mfa_enabled')).toBe('true')
    })
  })

  it('HAPPY_PATH_NO_MFA: a successful login without MFA clears the flag', async () => {
    localStorage.setItem('gear.is_mfa_enabled', 'true')
    stubFetch({
      ok: true,
      status: 200,
      body: {
        token: 'opaque-session-token',
        user: { id: 'u-1', email: 'erika@example.com', display_name: 'Erika Musterfrau', is_mfa_enabled: false },
      },
    })

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(localStorage.getItem('gear.is_mfa_enabled')).toBeNull()
    })
  })

  it('INVALID_CREDENTIALS: shows anti-enumeration microcopy on 401', async () => {
    stubFetch({
      ok: false,
      status: 401,
      body: { error: { code: 'invalid_credentials', message: 'E-Mail oder Passwort ist falsch.' } },
    })

    await submitLogin('nobody@example.com', 'falsches-passwort')

    await waitFor(() => {
      expect(screen.getByText('E-Mail oder Passwort ist falsch.')).toBeInTheDocument()
    })

    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('SERVER_ERROR: a 5xx failure shows the server-error message, not bad-credentials microcopy', async () => {
    stubFetch({
      ok: false,
      status: 500,
      body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } },
    })

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(
        screen.getByText('Ein interner Fehler ist aufgetreten. Bitte versuche es erneut.'),
      ).toBeInTheDocument()
    })

    expect(screen.queryByText('E-Mail oder Passwort ist falsch.')).not.toBeInTheDocument()
  })

  it('NETWORK_ERROR: displays connection error when fetch throws', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(
        screen.getByText('Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.'),
      ).toBeInTheDocument()
    })
  })

  it('LOCKOUT_3_FAILS: shows the German lockout screen and disables the retry button on 429', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: { get: (name: string) => (name === 'Retry-After' ? '30' : null) },
      json: async () => ({
        error: {
          code: 'too_many_attempts',
          message: 'Zu viele Fehlversuche. Bitte warte 30 Sekunden.',
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByText('Zu viele Fehlversuche. Bitte warte 30 Sekunden.')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /Bitte warten/ })).toBeDisabled()
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(LOCKOUT_STORAGE_KEY)).not.toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('LOCKOUT_EXPIRED: re-enables the retry button once the countdown expires', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: { get: (name: string) => (name === 'Retry-After' ? '30' : null) },
      json: async () => ({ error: { code: 'too_many_attempts' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderLoginPage()
    fireEvent.change(screen.getByLabelText('E-Mail-Adresse'), { target: { value: 'erika@example.com' } })
    fireEvent.change(screen.getByLabelText('Passwort'), { target: { value: 'geheim123456' } })
    fireEvent.click(screen.getByRole('button', { name: 'Anmelden' }))
    await act(async () => {})

    expect(screen.getByText(/Zu viele Fehlversuche/)).toBeInTheDocument()
    expect(screen.getByText(/bitte warte 30 Sekunden/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Bitte warten/ })).toBeDisabled()

    act(() => {
      vi.advanceTimersByTime(31_000)
    })

    expect(screen.queryByText(/Zu viele Fehlversuche/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Anmelden' })).toBeEnabled()
    expect(localStorage.getItem(LOCKOUT_STORAGE_KEY)).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('LOCKOUT_PERSIST: rehydrates an active lockout from localStorage after reload', () => {
    localStorage.setItem(LOCKOUT_STORAGE_KEY, String(Date.now() + 30_000))
    renderLoginPage()

    expect(screen.getByText(/Zu viele Fehlversuche\. Bitte warte \d+ Sekunden\./)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Bitte warten/ })).toBeDisabled()
  })

  it('LOCKOUT_SUBMIT_GUARD: an Enter submit during the countdown does not clear the lockout', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: { get: (name: string) => (name === 'Retry-After' ? '30' : null) },
      json: async () => ({ error: { code: 'too_many_attempts' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderLoginPage()
    fireEvent.change(screen.getByLabelText('E-Mail-Adresse'), { target: { value: 'erika@example.com' } })
    fireEvent.change(screen.getByLabelText('Passwort'), { target: { value: 'geheim123456' } })
    fireEvent.click(screen.getByRole('button', { name: 'Anmelden' }))
    await act(async () => {})

    expect(screen.getByRole('button', { name: /Bitte warten/ })).toBeDisabled()

    // A second submit attempt (e.g. implicit Enter submit) while locked must
    // neither clear nor restart the lockout and must not hit the server.
    const form = screen.getByLabelText('E-Mail-Adresse').closest('form')
    if (!form) {
      throw new Error('login form not found')
    }
    fireEvent.submit(form)
    await act(async () => {})

    expect(screen.getByText(/Zu viele Fehlversuche/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Bitte warten/ })).toBeDisabled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('MFA_CHALLENGE: a mfa_required response shows the TOTP step and the "MFA aktiv" indicator', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ mfa_required: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    })
    // UX-DR6: the login UI shows the "MFA aktiv" indicator during the challenge.
    expect(screen.getByText(/MFA aktiv/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Code prüfen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Zurück zur E-Mail-/ })).toBeInTheDocument()
    // No session token is stored yet (two-step login, FR-4).
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
    // The first POST must NOT carry a totp_code.
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'erika@example.com', password: 'geheim123456' }),
    })
  })

  it('MFA_VALID_CODE: submitting a valid code stores the token and navigates to the dashboard', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ mfa_required: true }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({
          token: 'opaque-session-token',
          user: { id: 'u-1', email: 'erika@example.com', is_mfa_enabled: true },
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    })

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Code aus der Authenticator-App'), '123456')
    await user.click(screen.getByRole('button', { name: 'Code prüfen' }))

    await waitFor(() => {
      expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('opaque-session-token')
    })
    expect(screen.getByText('Übersicht')).toBeInTheDocument()

    const secondCall = fetchMock.mock.calls[1]
    expect(secondCall[0]).toBe('/api/v1/auth/login')
    expect(JSON.parse(String(secondCall[1].body))).toEqual({
      email: 'erika@example.com',
      password: 'geheim123456',
      totp_code: '123456',
    })
  })

  it('MFA_INVALID_CODE: a 401 on the challenge step shows the same anti-enumeration microcopy', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ mfa_required: true }),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: async () => ({
          error: { code: 'invalid_credentials', message: 'E-Mail oder Passwort ist falsch.' },
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    })

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Code aus der Authenticator-App'), '000000')
    await user.click(screen.getByRole('button', { name: 'Code prüfen' }))

    await waitFor(() => {
      // UX-DR7: the rejection does not reveal why — identical microcopy.
      expect(screen.getByText('E-Mail oder Passwort ist falsch.')).toBeInTheDocument()
    })
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('MFA_INVALID_FORMAT: a non-6-digit code is rejected client-side without hitting the server', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ mfa_required: true }),
      })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    })

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Code aus der Authenticator-App'), '12ab')
    await user.click(screen.getByRole('button', { name: 'Code prüfen' }))

    await waitFor(() => {
      expect(screen.getByText(/Bitte gib den 6-stelligen Code/i)).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('MFA_BACK: the back link returns to the credentials step', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ mfa_required: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await submitLogin('erika@example.com', 'geheim123456')

    await waitFor(() => {
      expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Zurück zur E-Mail-/ }))

    await waitFor(() => {
      expect(screen.getByLabelText('E-Mail-Adresse')).toBeInTheDocument()
    })
    expect(screen.queryByLabelText('Code aus der Authenticator-App')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
