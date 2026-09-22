// @vitest-environment jsdom
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { ChangePasswordPage } from './ChangePasswordPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname}</span>
}

function renderChangePasswordPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/password']}>
        <LocationProbe />
        <ChangePasswordPage />
      </MemoryRouter>
    </ThemeProvider>,
  )
}

describe('ChangePasswordPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: renders the German form with all three fields', () => {
    renderChangePasswordPage()

    expect(screen.getByRole('heading', { level: 2, name: 'Passwort ändern' })).toBeInTheDocument()
    expect(screen.getByLabelText('Aktuelles Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Neues Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Wiederholung')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Passwort ändern' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeInTheDocument()
  })

  it('MISSING_CURRENT: shows validation error when the current password is empty', async () => {
    const user = userEvent.setup()
    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    expect(screen.getByText('Bitte gib dein aktuelles Passwort ein.')).toBeInTheDocument()
  })

  it('SHORT_NEW_PASSWORD: shows inline validation error when the new password is shorter than 10 characters', async () => {
    const user = userEvent.setup()
    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'kurz123')
    await user.type(screen.getByLabelText('Wiederholung'), 'kurz123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    expect(screen.getByText('Das Passwort muss mindestens 10 Zeichen lang sein.')).toBeInTheDocument()
  })

  it('PASSWORD_MISMATCH: shows inline validation error when new password and confirmation differ', async () => {
    const user = userEvent.setup()
    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'anders123456')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    expect(screen.getByText('Die Passwörter stimmen nicht überein.')).toBeInTheDocument()
  })

  it('HAPPY_PATH: successful change posts the payload with the bearer token and shows "→ Andere Sitzungen beendet"', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ message: 'Passwort geändert.', sessions_revoked: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(screen.getByText('→ Andere Sitzungen beendet')).toBeInTheDocument()
    })
    expect(screen.getByText('Passwort geändert.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/password/change', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
      body: JSON.stringify({
        current_password: 'geheim123456',
        new_password: 'neuespasswort123',
        new_password_confirm: 'neuespasswort123',
      }),
    })
  })

  it('REVOKE_FAILED: shows the warning message when the server reports sessions_revoked=false', async () => {
    // Review finding 1.7-8: "→ Andere Sitzungen beendet" is only shown when the
    // server actually revoked the other sessions; otherwise a warning is shown.
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ message: 'Passwort geändert.', sessions_revoked: false }),
      }),
    )

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(
        screen.getByText(
          'Das Passwort wurde geändert, aber andere Sitzungen konnten nicht beendet werden.',
        ),
      ).toBeInTheDocument()
    })
    expect(screen.queryByText('→ Andere Sitzungen beendet')).not.toBeInTheDocument()
  })

  it('MULTIBYTE_PASSWORD: counts Unicode code points so client and server agree', async () => {
    // Review finding 1.7-5: 5 emojis are 10 UTF-16 code units but only 5 code
    // points — the client must reject them as too short (server-side
    // RuneCountInString also counts 5) without calling the server.
    const user = userEvent.setup()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), '😀'.repeat(5))
    await user.type(screen.getByLabelText('Wiederholung'), '😀'.repeat(5))
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    expect(screen.getByText('Das Passwort muss mindestens 10 Zeichen lang sein.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('WRONG_CURRENT: displays the server error for an incorrect current password', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({
          error: { code: 'invalid_current_password', message: 'Das aktuelle Passwort ist falsch.' },
        }),
      }),
    )

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'falsch')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(screen.getByText('Das aktuelle Passwort ist falsch.')).toBeInTheDocument()
    })
  })

  it('UNAUTHENTICATED: clears stale auth state and redirects to /login on a 401', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.display_name', 'Erika Musterfrau')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
      }),
    )

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/login')
    })
    // Review finding 1.7-7: the stale token/display name must not linger.
    expect(localStorage.getItem('gear.session_token')).toBeNull()
    expect(localStorage.getItem('gear.display_name')).toBeNull()
  })

  it('CODE_BASED_ERROR: maps the current-password field from the envelope code, not the message text', async () => {
    // Review finding 1.7-6: even if the German microcopy for
    // invalid_current_password changes, the field attribution survives because
    // it is driven by error.code.
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({
          error: { code: 'invalid_current_password', message: 'Neue Formulierung.' },
        }),
      }),
    )

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'falsch')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(screen.getByText('Neue Formulierung.')).toBeInTheDocument()
    })
  })

  it('NETWORK_ERROR: displays the connection-failure message when fetch throws', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    await waitFor(() => {
      expect(
        screen.getByText('Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.'),
      ).toBeInTheDocument()
    })
  })

  it('SUBMITTING_STATE: while the request is in flight the submit button and inputs are disabled and shows "Wird gesendet..."', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise((resolve) => { resolveFetch = resolve })),
    )
    const user = userEvent.setup()
    renderChangePasswordPage()

    await user.type(screen.getByLabelText('Aktuelles Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuespasswort123')
    await user.type(screen.getByLabelText('Wiederholung'), 'neuespasswort123')
    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('Aktuelles Passwort')).toBeDisabled()
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
    renderChangePasswordPage()

    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    // UX-DR9 SCREEN_READER: focus lands on the first failing field.
    expect(screen.getByLabelText('Aktuelles Passwort')).toHaveFocus()
  })

  it('ACCESSIBILITY: the current-password inline error uses role="alert" and aria-describedby linkage', async () => {
    const user = userEvent.setup()
    renderChangePasswordPage()

    await user.click(screen.getByRole('button', { name: 'Passwort ändern' }))

    const currentInput = screen.getByLabelText('Aktuelles Passwort')
    const currentError = screen.getByText('Bitte gib dein aktuelles Passwort ein.')
    expect(currentError).toHaveAttribute('role', 'alert')
    expect(currentError).toHaveAttribute('id', 'currentPassword-error')
    expect(currentInput).toHaveAttribute('aria-invalid', 'true')
    expect(currentInput).toHaveAttribute('aria-describedby', 'currentPassword-error')
  })
})
