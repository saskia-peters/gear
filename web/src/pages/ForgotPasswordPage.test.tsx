// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
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
})