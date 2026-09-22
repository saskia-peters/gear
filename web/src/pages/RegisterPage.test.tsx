// @vitest-environment jsdom
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { RegisterPage } from './RegisterPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

function renderRegisterPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/register']}>
        <RegisterPage />
      </MemoryRouter>
    </ThemeProvider>,
  )
}

describe('RegisterPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: renders registration form with all required inputs and links', () => {
    renderRegisterPage()

    expect(screen.getByRole('heading', { level: 2, name: 'Registrierung' })).toBeInTheDocument()
    expect(screen.getByLabelText('Vorname')).toBeInTheDocument()
    expect(screen.getByLabelText('Nachname')).toBeInTheDocument()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeInTheDocument()
    expect(screen.getByLabelText('Passwort')).toBeInTheDocument()
    expect(screen.getByLabelText('Passwort bestätigen')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Registrieren' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Bereits registriert? Zur Anmeldung' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zurück zur Übersicht' })).toBeInTheDocument()
  })

  it('MISSING_FIELDS: shows validation errors when submitting with empty fields', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    expect(screen.getByText('Bitte gib deinen Vornamen ein.')).toBeInTheDocument()
    expect(screen.getByText('Bitte gib deinen Nachnamen ein.')).toBeInTheDocument()
    expect(screen.getByText('Bitte gib deine E-Mail-Adresse ein.')).toBeInTheDocument()
    expect(screen.getByText('Bitte gib ein Passwort ein.')).toBeInTheDocument()
    expect(screen.getByText('Bitte bestätige dein Passwort.')).toBeInTheDocument()
  })

  it('INVALID_EMAIL: shows validation error on invalid email address format', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'invalid-email')
    await user.type(screen.getByLabelText('Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'geheim123456')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    expect(screen.getByText('Bitte gib eine gültige E-Mail-Adresse ein.')).toBeInTheDocument()
  })

  it('SHORT_PASSWORD: shows validation error when password is shorter than 10 characters', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'max@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'kurz123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'kurz123')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    expect(screen.getByText('Das Passwort muss mindestens 10 Zeichen lang sein.')).toBeInTheDocument()
  })

  it('PASSWORD_MISMATCH: shows validation error when password and confirmation do not match', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'max@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'geheim123456')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'anders123456')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    expect(screen.getByText('Die Passwörter stimmen nicht überein.')).toBeInTheDocument()
  })

  it('HAPPY_PATH: successful registration displays pending approval notice and confirmation links', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        message: 'Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.',
        status: 'pending_approval',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Erika')
    await user.type(screen.getByLabelText('Nachname'), 'Musterfrau')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'erika@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'sicherespasswort123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'sicherespasswort123')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Registrierung eingegangen' })).toBeInTheDocument()
    })

    expect(
      screen.getByText('Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe.'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zur Anmeldung' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zurück zur Übersicht' })).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        first_name: 'Erika',
        last_name: 'Musterfrau',
        email: 'erika@example.com',
        password: 'sicherespasswort123',
        password_confirm: 'sicherespasswort123',
      }),
    })
  })

  it('DUPLICATE_EMAIL: submitting duplicate email returns uniform confirmation without error', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        message: 'Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.',
        status: 'pending_approval',
      }),
    }))

    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'existing@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'sicherespasswort123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'sicherespasswort123')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Registrierung eingegangen' })).toBeInTheDocument()
    })
    expect(
      screen.getByText('Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe.'),
    ).toBeInTheDocument()
  })

  it('SERVER_ERROR: displays server error message when API fails', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({
        error: {
          code: 'invalid_request',
          message: 'Das Passwort muss mindestens 10 Zeichen lang sein.',
        },
      }),
    }))

    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'max@example.com')
    await user.type(screen.getByLabelText('Passwort'), '1234567890')
    await user.type(screen.getByLabelText('Passwort bestätigen'), '1234567890')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    await waitFor(() => {
      expect(screen.getByText('Das Passwort muss mindestens 10 Zeichen lang sein.')).toBeInTheDocument()
    })
  })

  it('NETWORK_ERROR: displays network error message when fetch throws', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'max@example.com')
    await user.type(screen.getByLabelText('Passwort'), '1234567890')
    await user.type(screen.getByLabelText('Passwort bestätigen'), '1234567890')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    await waitFor(() => {
      expect(screen.getByText('Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.')).toBeInTheDocument()
    })
  })

  it('SUBMITTING_STATE: while the request is in flight the submit button is disabled and shows "Wird gesendet..."', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise((resolve) => { resolveFetch = resolve })),
    )
    const user = userEvent.setup()
    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'max@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'sicherespasswort123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'sicherespasswort123')
    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('Vorname')).toBeDisabled()
    expect(screen.getByLabelText('Nachname')).toBeDisabled()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeDisabled()
    expect(screen.getByLabelText('Passwort')).toBeDisabled()
    expect(screen.getByLabelText('Passwort bestätigen')).toBeDisabled()

    // Recovery (finding 7): once the request settles, the submitting state ends
    // and the confirmation surface appears.
    await act(async () => {
      resolveFetch({ ok: true, status: 200, json: async () => ({ status: 'pending_approval' }) })
    })
    expect(
      screen.getByRole('heading', { level: 2, name: 'Registrierung eingegangen' }),
    ).toBeInTheDocument()
  })

  it('ANTI_ENUM_LEAKY_BODY: a server message that leaks account existence is never rendered', async () => {
    // Even if the server body says "E-Mail existiert bereits", the client shows
    // the uniform anti-enumeration confirmation (UX-DR7/UX-DR8).
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        message: 'E-Mail existiert bereits',
        status: 'pending_approval',
      }),
    }))

    renderRegisterPage()

    await user.type(screen.getByLabelText('Vorname'), 'Max')
    await user.type(screen.getByLabelText('Nachname'), 'Mustermann')
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'existing@example.com')
    await user.type(screen.getByLabelText('Passwort'), 'sicherespasswort123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'sicherespasswort123')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    await waitFor(() => {
      expect(
        screen.getByText('Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.'),
      ).toBeInTheDocument()
    })
    expect(screen.queryByText('E-Mail existiert bereits')).not.toBeInTheDocument()
  })

  it('FOCUS_FIRST_ERROR: submitting invalid fields moves focus to the first invalid input', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    // UX-DR9 SCREEN_READER: focus lands on the first failing field (Vorname).
    expect(screen.getByLabelText('Vorname')).toHaveFocus()
  })

  it('ACCESSIBILITY: every input has an accessible label and inline errors use role="alert" + aria-describedby', async () => {
    const user = userEvent.setup()
    renderRegisterPage()

    expect(screen.getByLabelText('Vorname')).toHaveAttribute('id', 'firstName')
    expect(screen.getByLabelText('Nachname')).toHaveAttribute('id', 'lastName')
    expect(screen.getByLabelText('E-Mail-Adresse')).toHaveAttribute('id', 'email')
    expect(screen.getByLabelText('Passwort')).toHaveAttribute('id', 'password')
    expect(screen.getByLabelText('Passwort bestätigen')).toHaveAttribute('id', 'passwordConfirm')

    await user.click(screen.getByRole('button', { name: 'Registrieren' }))

    const firstNameInput = screen.getByLabelText('Vorname')
    const firstNameError = screen.getByText('Bitte gib deinen Vornamen ein.')
    expect(firstNameError).toHaveAttribute('role', 'alert')
    expect(firstNameError).toHaveAttribute('id', 'firstName-error')
    expect(firstNameInput).toHaveAttribute('aria-invalid', 'true')
    expect(firstNameInput).toHaveAttribute('aria-describedby', 'firstName-error')
  })
})
