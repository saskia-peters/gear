// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { MfaPage } from './MfaPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const TOKEN_STORAGE_KEY = 'gear.session_token'

function renderMfaPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/mfa']}>
        <MfaPage />
      </MemoryRouter>
    </ThemeProvider>,
  )
}

// stubFetchSequence returns a fetch mock that answers calls in order. Under
// React StrictMode a mount triggers the fetch effect twice (mount → abort →
// remount), so the mock also carries a safe default resolved response for any
// call beyond the queued ones — otherwise an exhausted mockResolvedValueOnce
// queue returns `undefined`, and MfaPage's `fetch(...).then(...)` throws.
function stubFetchSequence(responses: Array<{ ok: boolean; status?: number; body: unknown }>) {
  const mock = vi.fn()
  responses.forEach((r) =>
    mock.mockResolvedValueOnce({
      ok: r.ok,
      status: r.status ?? (r.ok ? 200 : 400),
      json: async () => r.body,
    }),
  )
  mock.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
  vi.stubGlobal('fetch', mock)
  return mock
}

function stubFetch(response: { ok: boolean; status?: number; body: unknown }) {
  return stubFetchSequence([response])
}

describe('MfaPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
    localStorage.setItem(TOKEN_STORAGE_KEY, 'test-session-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('RENDERS: shows the German settings surface with the header and back link', async () => {
    stubFetch({ ok: true, body: { enabled: false } })
    renderMfaPage()

    expect(screen.getByRole('heading', { level: 2, name: 'Zwei-Faktor-Authentifizierung' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })).toBeInTheDocument()
    })
  })

  it('ENROLL_REQUEST: activating shows the secret, QR code and code input', async () => {
    const fetchMock = stubFetchSequence([
      { ok: true, body: { enabled: false } },
      {
        ok: true,
        body: {
          secret: 'JBSWY3DPEHPK3PXP',
          uri: 'otpauth://totp/G.E.A.R.:max@example.com?secret=JBSWY3DPEHPK3PXP&issuer=G.E.A.R.',
        },
      },
    ])
    renderMfaPage()

    const activateButton = await screen.findByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })
    const user = userEvent.setup()
    await user.click(activateButton)

    await waitFor(() => {
      expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument()
    })
    // QR code is rendered as an SVG from the provisioning URI.
    expect(document.querySelector('svg')).toBeInTheDocument()
    expect(screen.getByLabelText('Code aus der Authenticator-App')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Aktivierung bestätigen/i })).toBeInTheDocument()

    const statusCall = fetchMock.mock.calls[0]
    expect(statusCall[0]).toBe('/api/v1/auth/mfa/status')
    expect(statusCall[1].headers.Authorization).toBe('Bearer test-session-token')

    const enrollCall = fetchMock.mock.calls[1]
    expect(enrollCall[0]).toBe('/api/v1/auth/mfa/enroll')
  })

  it('ENROLL_CONFIRM_VALID: confirming with a valid code enables MFA, clears auth state and redirects to /login', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'sess123')
    stubFetchSequence([
      { ok: true, body: { enabled: false } },
      {
        ok: true,
        body: {
          secret: 'JBSWY3DPEHPK3PXP',
          uri: 'otpauth://totp/G.E.A.R.:max@example.com?secret=JBSWY3DPEHPK3PXP&issuer=G.E.A.R.',
        },
      },
      { ok: true, body: { enabled: true } },
    ])
    renderMfaPage()

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i }))
    await user.type(await screen.findByLabelText('Code aus der Authenticator-App'), '123456')
    await user.click(screen.getByRole('button', { name: /Aktivierung bestätigen/i }))

    // After enabling MFA all sessions (incl. this one) are revoked, so the
    // client clears auth state and redirects to /login for a fresh TOTP login.
    await waitFor(() => {
      expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
    })
  })

  it('ENROLL_CONFIRM_INVALID: confirming with a wrong code shows a German error and MFA stays off', async () => {
    stubFetchSequence([
      { ok: true, body: { enabled: false } },
      {
        ok: true,
        body: {
          secret: 'JBSWY3DPEHPK3PXP',
          uri: 'otpauth://totp/G.E.A.R.:max@example.com?secret=JBSWY3DPEHPK3PXP&issuer=G.E.A.R.',
        },
      },
      {
        ok: false,
        status: 400,
        body: { error: { code: 'invalid_totp', message: 'Der Bestätigungscode ist ungültig oder abgelaufen.' } },
      },
    ])
    renderMfaPage()

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i }))
    await user.type(await screen.findByLabelText('Code aus der Authenticator-App'), '000000')
    await user.click(screen.getByRole('button', { name: /Aktivierung bestätigen/i }))

    await waitFor(() => {
      expect(screen.getByText('Der Bestätigungscode ist ungültig oder abgelaufen.')).toBeInTheDocument()
    })
  })

  it('DISABLED_VIEW: an MFA-enabled account shows the "MFA aktiv" indicator and the disable form', async () => {
    stubFetch({ ok: true, body: { enabled: true } })
    renderMfaPage()

    await waitFor(() => {
      expect(screen.getByText(/MFA aktiv/i)).toBeInTheDocument()
    })
    expect(screen.getByLabelText('Aktueller Code aus der Authenticator-App')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /deaktivieren/i })).toBeInTheDocument()
  })

  it('DISABLE_VALID: disabling with a valid code deactivates MFA and shows the notice', async () => {
    stubFetchSequence([
      { ok: true, body: { enabled: true } },
      { ok: true, body: { enabled: false } },
    ])
    renderMfaPage()

    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Aktueller Code aus der Authenticator-App'), '123456')
    await user.click(screen.getByRole('button', { name: /deaktivieren/i }))

    await waitFor(() => {
      expect(screen.getByText('Zwei-Faktor-Authentifizierung wurde deaktiviert.')).toBeInTheDocument()
    })
    // Back to the enable offer.
    expect(screen.getByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })).toBeInTheDocument()
  })

  it('DISABLE_INVALID: disabling with a wrong code shows a German error and MFA stays on', async () => {
    stubFetchSequence([
      { ok: true, body: { enabled: true } },
      {
        ok: false,
        status: 400,
        body: { error: { code: 'invalid_totp', message: 'Der Bestätigungscode ist ungültig oder abgelaufen.' } },
      },
    ])
    renderMfaPage()

    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Aktueller Code aus der Authenticator-App'), '000000')
    await user.click(screen.getByRole('button', { name: /deaktivieren/i }))

    await waitFor(() => {
      expect(screen.getByText('Der Bestätigungscode ist ungültig oder abgelaufen.')).toBeInTheDocument()
    })
    expect(screen.getByText(/MFA aktiv/i)).toBeInTheDocument()
  })

  it('DISABLE_INVALID_FORMAT: a non-6-digit code is rejected client-side without hitting the server', async () => {
    stubFetch({ ok: true, body: { enabled: true } })
    renderMfaPage()

    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Aktueller Code aus der Authenticator-App'), '12ab')
    await user.click(screen.getByRole('button', { name: /deaktivieren/i }))

    await waitFor(() => {
      expect(screen.getByText(/Bitte gib den 6-stelligen Code/i)).toBeInTheDocument()
    })
  })

  it('UNAUTHENTICATED: a missing session token still sends the request (the server rejects it)', async () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    const fetchMock = stubFetch({ ok: false, status: 401, body: { error: { code: 'unauthorized' } } })
    renderMfaPage()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v1/auth/mfa/status',
        expect.objectContaining({
          headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
        }),
      )
    })
  })
})
function renderMfaPageWithRoutes() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/mfa']}>
        <Routes>
          <Route path="/mfa" element={<MfaPage />} />
          <Route path="/login" element={<div>Anmelden-Seite</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

it('STATUS_401: a 401 on the status fetch redirects to /login instead of showing the enable form', async () => {
  stubFetch({ ok: false, status: 401, body: { error: { code: 'unauthorized' } } })
  renderMfaPageWithRoutes()

  await waitFor(() => {
    expect(screen.getByText('Anmelden-Seite')).toBeInTheDocument()
  })
  // The settings surface must not be rendered as "MFA disabled".
  expect(screen.queryByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })).not.toBeInTheDocument()
})

it('STATUS_ERROR: a failed status fetch shows an error state, not the enable form', async () => {
  // Network failure (fetch rejects) must not be treated as "MFA disabled".
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))
  renderMfaPage()

  await waitFor(() => {
    expect(screen.getByText('Status konnte nicht geladen werden. Bitte versuche es erneut.')).toBeInTheDocument()
  })
  expect(screen.queryByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })).not.toBeInTheDocument()
  expect(screen.queryByText(/MFA aktiv/i)).not.toBeInTheDocument()
})

it('STATUS_5XX: a non-2xx, non-401 status shows the error state too', async () => {
  stubFetch({ ok: false, status: 500, body: { error: { code: 'internal_error' } } })
  renderMfaPage()

  await waitFor(() => {
    expect(screen.getByText('Status konnte nicht geladen werden. Bitte versuche es erneut.')).toBeInTheDocument()
  })
  expect(screen.queryByRole('button', { name: /Zwei-Faktor-Authentifizierung aktivieren/i })).not.toBeInTheDocument()
})
