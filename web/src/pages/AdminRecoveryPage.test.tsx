// @vitest-environment jsdom
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminRecoveryPage } from './AdminRecoveryPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const REQUEST_CONFIRM =
  'Deine Wiederherstellungsanfrage wurde erstellt. Der andere Administrator muss sie freigeben.'

function renderRecovery() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/recovery']}>
        <AdminRecoveryPage />
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

describe('AdminRecoveryPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('RENDER: shows both the request (A) and approve (B) surfaces', () => {
    renderRecovery()
    expect(screen.getByRole('heading', { level: 2, name: 'Admin-Wiederherstellung' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Wiederherstellung anfordern' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Anfrage freigeben' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Wiederherstellung anfordern' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Freigeben' })).toBeInTheDocument()
  })

  it('REQUEST_HAPPY_PATH: submitting an email posts with the bearer token and shows the confirmation', async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, body: { message: REQUEST_CONFIRM, target_email: 'admina@gear.local' } })
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'admina@gear.local')
    await user.click(screen.getByRole('button', { name: 'Wiederherstellung anfordern' }))

    await waitFor(() => {
      expect(screen.getByText(REQUEST_CONFIRM)).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/admin/recovery/request', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
      body: JSON.stringify({ email: 'admina@gear.local' }),
    })
  })

  it('REQUEST_VALIDATION: an empty email is rejected client-side without hitting the server', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    renderRecovery()

    await user.click(screen.getByRole('button', { name: 'Wiederherstellung anfordern' }))

    expect(screen.getByText('Bitte gib deine E-Mail-Adresse ein.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/auth/admin/recovery/request', expect.anything())
  })

  it('APPROVE_HAPPY_PATH: approving with Begründung + confirmation posts and shows the recovery token', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: { message: 'Freigabe erteilt.', recovery_token: 'raw-recovery-token' },
    })
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators'), 'admina@gear.local')
    await user.type(screen.getByLabelText('Begründung'), 'Admin A ist ausgesperrt')
    await user.click(screen.getByLabelText('Ich bestätige, dass ich die Anfrage geprüft habe und der betroffene Administrator der Freigabe zustimmt.'))
    await user.click(screen.getByRole('button', { name: 'Freigeben' }))

    await waitFor(() => {
      expect(screen.getByText('Freigabe erteilt.')).toBeInTheDocument()
    })
    // The token is rendered as a READ-ONLY COPYABLE INPUT, never a clickable
    // link embedding the token in a URL (review finding 1.10 / FR-27).
    const tokenInput = screen.getByTestId('recovery-token')
    expect(tokenInput).toHaveValue('raw-recovery-token')
    expect(tokenInput).toHaveAttribute('readonly')
    const links = screen.getAllByRole('link')
    for (const link of links) {
      const href = link.getAttribute('href') || ''
      expect(href).not.toContain('raw-recovery-token')
    }
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/admin/recovery/approve', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
      body: JSON.stringify({
        email: 'admina@gear.local',
        reason: 'Admin A ist ausgesperrt',
        confirmed: true,
      }),
    })
  })

  it('COMPLETE_HAPPY_PATH: the completion form posts the token via the POST body', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: { message: 'Passwort gesetzt. Der Administrator kann sich jetzt mit dem neuen Passwort anmelden.' },
    })
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('Einmal-Token'), 'raw-recovery-token')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuesadminpass123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'neuesadminpass123')
    await user.click(screen.getByRole('button', { name: 'Passwort setzen' }))

    await waitFor(() => {
      expect(screen.getByText('Passwort gesetzt.')).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/password/reset', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
      body: JSON.stringify({
        token: 'raw-recovery-token',
        new_password: 'neuesadminpass123',
        new_password_confirm: 'neuesadminpass123',
      }),
    })
  })

  it('PENDING_LIST: pending recovery requests are fetched and rendered', async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      body: {
        requests: [{ id: 'tok-1', user: { email: 'admina@gear.local', display_name: 'Admin A' } }],
      },
    })
    renderRecovery()

    await waitFor(() => {
      expect(screen.getByText('Admin A')).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/admin/recovery/pending', {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('APPROVE_MISSING_REASON: a missing Begründung is rejected client-side without hitting the server', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators'), 'admina@gear.local')
    await user.click(screen.getByLabelText('Ich bestätige, dass ich die Anfrage geprüft habe und der betroffene Administrator der Freigabe zustimmt.'))
    await user.click(screen.getByRole('button', { name: 'Freigeben' }))

    expect(screen.getByText('Bitte gib eine Begründung für die Freigabe an.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/auth/admin/recovery/approve', expect.anything())
  })

  it('APPROVE_NOT_CONFIRMED: an unchecked confirmation is rejected client-side', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators'), 'admina@gear.local')
    await user.type(screen.getByLabelText('Begründung'), 'Begründung')
    await user.click(screen.getByRole('button', { name: 'Freigeben' }))

    expect(screen.getByText('Bitte bestätige die Freigabe mit der Checkbox.')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/auth/admin/recovery/approve', expect.anything())
  })

  it('NAVIGATION: a link back to the ADMIN module is present', () => {
    renderRecovery()
    expect(screen.getByRole('link', { name: 'Zurück zum Admin-Modul' })).toHaveAttribute(
      'href',
      '/admin',
    )
  })

  it('ACCESSIBILITY: inline errors are linked to their fields via aria-describedby', async () => {
    const user = userEvent.setup()
    renderRecovery()

    await user.click(screen.getByRole('button', { name: 'Wiederherstellung anfordern' }))

    const reqEmail = screen.getByLabelText('E-Mail-Adresse')
    const reqError = screen.getByText('Bitte gib deine E-Mail-Adresse ein.')
    expect(reqError).toHaveAttribute('role', 'alert')
    expect(reqError).toHaveAttribute('id', 'reqEmail-error')
    expect(reqEmail).toHaveAttribute('aria-invalid', 'true')
    expect(reqEmail).toHaveAttribute('aria-describedby', 'reqEmail-error')

    await user.click(screen.getByRole('button', { name: 'Freigeben' }))

    const apprEmail = screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators')
    const reason = screen.getByLabelText('Begründung')
    expect(apprEmail).toHaveAttribute('aria-describedby', 'apprEmail-error')
    expect(reason).toHaveAttribute('aria-describedby', 'reason-error')
    // The checkbox error is now field-attributed to the confirmation control.
    const confirmError = screen.getByText('Bitte bestätige die Freigabe mit der Checkbox.')
    expect(confirmError).toHaveAttribute('id', 'confirmed-error')
    const checkbox = screen.getByLabelText(/Ich bestätige, dass ich die Anfrage geprüft habe/)
    expect(checkbox).toHaveAttribute('aria-invalid', 'true')
    expect(checkbox).toHaveAttribute('aria-describedby', 'confirmed-error')
  })

  it('SUBMITTING_STATE_REQUEST: while the request is in flight the request button and input are disabled and shows "Wird gesendet..."', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url === '/api/v1/auth/admin/recovery/pending') {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ requests: [] }) })
        }
        return new Promise((resolve) => { resolveFetch = resolve })
      }),
    )
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'admina@gear.local')
    await user.click(screen.getByRole('button', { name: 'Wiederherstellung anfordern' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('E-Mail-Adresse')).toBeDisabled()

    // Recovery (finding 7): once the request settles, the submitting state ends
    // and the confirmation box appears.
    await act(async () => {
      resolveFetch({
        ok: true,
        status: 200,
        json: async () => ({ message: REQUEST_CONFIRM, target_email: 'admina@gear.local' }),
      })
    })
    expect(screen.getByText(REQUEST_CONFIRM)).toBeInTheDocument()
  })

  it('SUBMITTING_STATE_APPROVE: while the approve request is in flight the button and fields are disabled', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url === '/api/v1/auth/admin/recovery/pending') {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ requests: [] }) })
        }
        return new Promise((resolve) => { resolveFetch = resolve })
      }),
    )
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators'), 'admina@gear.local')
    await user.type(screen.getByLabelText('Begründung'), 'Admin A ist ausgesperrt')
    await user.click(screen.getByLabelText(/Ich bestätige, dass ich die Anfrage geprüft habe/))
    await user.click(screen.getByRole('button', { name: 'Freigeben' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('E-Mail-Adresse des betroffenen Administrators')).toBeDisabled()
    expect(screen.getByLabelText('Begründung')).toBeDisabled()

    await act(async () => {
      resolveFetch({ ok: true, status: 200, json: async () => ({ message: 'Freigabe erteilt.', recovery_token: 'raw-token' }) })
    })
    expect(screen.getByText('Freigabe erteilt.')).toBeInTheDocument()
  })

  it('SUBMITTING_STATE_COMPLETE: while the completion request is in flight the button and fields are disabled', async () => {
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url === '/api/v1/auth/admin/recovery/pending') {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ requests: [] }) })
        }
        return new Promise((resolve) => { resolveFetch = resolve })
      }),
    )
    const user = userEvent.setup()
    renderRecovery()

    await user.type(screen.getByLabelText('Einmal-Token'), 'raw-recovery-token')
    await user.type(screen.getByLabelText('Neues Passwort'), 'neuesadminpass123')
    await user.type(screen.getByLabelText('Passwort bestätigen'), 'neuesadminpass123')
    await user.click(screen.getByRole('button', { name: 'Passwort setzen' }))

    const button = screen.getByRole('button', { name: 'Wird gesendet...' })
    expect(button).toBeDisabled()
    expect(screen.getByLabelText('Einmal-Token')).toBeDisabled()
    expect(screen.getByLabelText('Neues Passwort')).toBeDisabled()
    expect(screen.getByLabelText('Passwort bestätigen')).toBeDisabled()

    await act(async () => {
      resolveFetch({ ok: true, status: 200, json: async () => ({ message: 'Passwort gesetzt.' }) })
    })
    expect(screen.getByText('Passwort gesetzt.')).toBeInTheDocument()
  })
})
