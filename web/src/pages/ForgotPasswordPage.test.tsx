// @vitest-environment jsdom
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { ForgotPasswordPage } from './ForgotPasswordPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const UNIFORM = 'Wenn deine E-Mail registriert ist, erhältst du einen Link.'

function renderForgot() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/forgot-password']}>
        <ForgotPasswordPage />
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

async function submitEmail(email: string) {
  const user = userEvent.setup()
  renderForgot()
  await user.type(screen.getByLabelText('E-Mail-Adresse'), email)
  await user.click(screen.getByRole('button', { name: 'Link anfordern' }))
}

describe('ForgotPasswordPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: submitting an email shows the uniform anti-enumeration confirmation', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: { message: UNIFORM },
    })

    await submitEmail('user@example.com')

    await waitFor(() => {
      expect(screen.getByText(UNIFORM)).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/password/forgot', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'user@example.com' }),
    })
  })

  it('ANTI_ENUMERATION: an unknown email shows the SAME uniform confirmation', async () => {
    stubFetch({
      ok: true,
      status: 200,
      body: { message: UNIFORM },
    })

    await submitEmail('nobody@example.com')

    await waitFor(() => {
      expect(screen.getByText(UNIFORM)).toBeInTheDocument()
    })
  })

  it('VALIDATION: an invalid email is rejected client-side without hitting the server', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await submitEmail('invalid-email')

    expect(await screen.findByText('Bitte gib eine gültige E-Mail-Adresse ein.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('MISSING_EMAIL: empty submission shows a required-field error', async () => {
    const user = userEvent.setup()
    renderForgot()
    await user.click(screen.getByRole('button', { name: 'Link anfordern' }))

    expect(screen.getByText('Bitte gib deine E-Mail-Adresse ein.')).toBeInTheDocument()
  })

  it('NETWORK_ERROR: a fetch failure shows the connection error, not the confirmation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    await submitEmail('user@example.com')

    await waitFor(() => {
      expect(
        screen.getByText('Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.'),
      ).toBeInTheDocument()
    })
    expect(screen.queryByText(UNIFORM)).not.toBeInTheDocument()
  })

  it('NAVIGATION: a link back to /login is present', () => {
    renderForgot()
    expect(screen.getByRole('link', { name: 'Zurück zur Anmeldung' })).toHaveAttribute(
      'href',
      '/login',
    )
  })

  it('SUBMITTING_STATE: while the request is in flight the submit button and input are disabled and shows "Wird gesendet..."', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise((resolve) => { resolveFetch = resolve })),
    )
    const user = userEvent.setup()
    renderForgot()

    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'user@example.com')
    await user.click(screen.getByRole('button', { name: 'Link anfordern' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeDisabled()

    // Recovery (finding 7): once the request settles the uniform confirmation
    // is shown (submitting state ends).
    await act(async () => {
      resolveFetch({ ok: true, status: 200, json: async () => ({ message: UNIFORM }) })
    })
    expect(screen.getByText(UNIFORM)).toBeInTheDocument()
  })

  it('ANTI_ENUM_LEAKY_BODY: a server message that leaks account existence is never rendered', async () => {
    // Even if the server body says "E-Mail nicht gefunden", the client always
    // renders the uniform anti-enumeration confirmation (UX-DR7/UX-DR8).
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ message: 'E-Mail nicht gefunden' }),
    }))

    await submitEmail('nobody@example.com')

    await waitFor(() => {
      expect(screen.getByText(UNIFORM)).toBeInTheDocument()
    })
    expect(screen.queryByText('E-Mail nicht gefunden')).not.toBeInTheDocument()
  })

  it('FOCUS_FIRST_ERROR: submitting an empty email moves focus to the email input', async () => {
    const user = userEvent.setup()
    renderForgot()

    await user.click(screen.getByRole('button', { name: 'Link anfordern' }))

    // UX-DR9 SCREEN_READER: focus lands on the first failing field.
    expect(screen.getByLabelText('E-Mail-Adresse')).toHaveFocus()
  })

  it('ACCESSIBILITY: the email inline error uses role="alert" and aria-describedby linkage', async () => {
    const user = userEvent.setup()
    renderForgot()

    await user.click(screen.getByRole('button', { name: 'Link anfordern' }))

    const emailInput = screen.getByLabelText('E-Mail-Adresse')
    const emailError = screen.getByText('Bitte gib deine E-Mail-Adresse ein.')
    expect(emailError).toHaveAttribute('role', 'alert')
    expect(emailError).toHaveAttribute('id', 'email-error')
    expect(emailInput).toHaveAttribute('aria-invalid', 'true')
    expect(emailInput).toHaveAttribute('aria-describedby', 'email-error')
  })
})