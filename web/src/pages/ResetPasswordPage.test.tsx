// @vitest-environment jsdom
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { ResetPasswordPage } from './ResetPasswordPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

function renderReset(token = 'opaque-token', state?: { notice?: string }) {
  const entry = state
    ? { pathname: `/reset-password/${token}`, state }
    : `/reset-password/${token}`
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/reset-password/:token" element={<ResetPasswordPage />} />
          <Route path="/forgot-password" element={<div>Passwort vergessen</div>} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetch(response: { ok: boolean; status: number; body: unknown }) {
  const mock = vi.fn().mockResolvedValue({
    ok: response.ok,
    status: response.status,
    json: async () => response.body,
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

async function submitNewPassword(pw: string, confirm = pw) {
  const user = userEvent.setup()
  renderReset()
  await user.type(screen.getByLabelText('Neues Passwort'), pw)
  await user.type(screen.getByLabelText('Wiederholung'), confirm)
  await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))
}

describe('ResetPasswordPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: a valid token + new password shows the success confirmation', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: { message: 'Passwort geändert. Du kannst dich jetzt anmelden.' },
    })

    await submitNewPassword('neuespasswort123')

    await waitFor(() => {
      expect(screen.getByText(/Passwort geändert/)).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/password/reset', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        token: 'opaque-token',
        new_password: 'neuespasswort123',
        new_password_confirm: 'neuespasswort123',
      }),
    })
  })

  it('INVALID_TOKEN: an expired/used token shows the invalid-link screen with a request-new-link action', async () => {
    stubFetch({
      ok: false,
      status: 400,
      body: {
        error: { code: 'invalid_token', message: 'Dieser Link ist ungültig oder abgelaufen. Bitte fordere einen neuen Link an.' },
      },
    })

    await submitNewPassword('neuespasswort123')

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Link ungültig' })).toBeInTheDocument()
    })
    expect(screen.getByRole('link', { name: 'Neuen Link anfordern' })).toHaveAttribute(
      'href',
      '/forgot-password',
    )
  })

  it('SHORT_PASSWORD: a new password under 10 characters is rejected client-side', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await submitNewPassword('kurz')

    expect(
      await screen.findByText('Das Passwort muss mindestens 10 Zeichen lang sein.'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('MISMATCH: differing password and confirmation is rejected client-side', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await submitNewPassword('neuespasswort123', 'anderspasswort456')

    expect(await screen.findByText('Die Passwörter stimmen nicht überein.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('NETWORK_ERROR: a fetch failure shows the connection error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    await submitNewPassword('neuespasswort123')

    await waitFor(() => {
      expect(
        screen.getByText('Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.'),
      ).toBeInTheDocument()
    })
  })

  it('NAVIGATION: a link back to /login is present', () => {
    renderReset()
    expect(screen.getByRole('link', { name: 'Zurück zur Anmeldung' })).toHaveAttribute(
      'href',
      '/login',
    )
  })

  it('FORCED_CHANGE_NOTICE: a notice from the login forced-change flow is shown (review finding 1.8-4)', () => {
    const notice =
      'Dein Passwort muss geändert werden, bevor du die Anwendung nutzen kannst. Die Administratoren wurden benachrichtigt.'
    renderReset('forced-change-token', { notice })

    expect(screen.getByRole('status')).toHaveTextContent(notice)
  })

  it('SUBMITTING_STATE: while the request is in flight the submit button and inputs are disabled and shows "Wird gesendet..."', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise((resolve) => { resolveFetch = resolve })),
    )
    const user = userEvent.setup()
    renderReset()

    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('Neues Passwort')).toBeDisabled()
    expect(screen.getByLabelText('Wiederholung')).toBeDisabled()

    // Recovery (finding 7): once the request settles, the submit button returns
    // to its original label and is re-enabled.
    await act(async () => {
      resolveFetch({
        ok: false,
        status: 500,
        json: async () => ({ error: { code: 'internal_error', message: 'kaputt' } }),
      })
    })
    expect(screen.getByRole('button', { name: 'Passwort ändern' })).toBeEnabled()
  })

  it('FOCUS_FIRST_ERROR: submitting empty fields moves focus to the first invalid input', async () => {
    const user = userEvent.setup()
    renderReset()

    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    // UX-DR9 SCREEN_READER: focus lands on the first failing field.
    expect(screen.getByLabelText('Neues Passwort')).toHaveFocus()
  })

  it('ACCESSIBILITY: the new-password inline error uses role="alert" and aria-describedby linkage', async () => {
    const user = userEvent.setup()
    renderReset()

    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    const passwordInput = screen.getByLabelText('Neues Passwort')
    const passwordError = screen.getByText('Bitte gib ein neues Passwort ein.')
    expect(passwordError).toHaveAttribute('role', 'alert')
    expect(passwordError).toHaveAttribute('id', 'newPassword-error')
    expect(passwordInput).toHaveAttribute('aria-invalid', 'true')
    expect(passwordInput).toHaveAttribute('aria-describedby', 'newPassword-error')
  })
})