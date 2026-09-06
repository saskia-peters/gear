// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
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
})
