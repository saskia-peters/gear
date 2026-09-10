// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminEinstellungenPage } from './AdminEinstellungenPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const SMTP_URL = '/api/v1/admin/settings/smtp'
const SMTP_TEST_URL = '/api/v1/admin/settings/smtp/test'

function settingsFixture() {
  return {
    host: 'smtp.example.com',
    port: 587,
    security: 'starttls',
    sender_address: 'noreply@example.com',
    sender_name: 'G.E.A.R.',
    username: 'smtpuser',
    password_configured: true,
  }
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/einstellungen']}>
        <Routes>
          <Route path="/admin/einstellungen" element={<AdminEinstellungenPage />} />
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetchRoutes(routes: Array<{
  matcher: (url: string, init?: RequestInit) => boolean
  response: { ok: boolean; status: number; body: unknown }
}>) {
  const mock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    const hit = routes.find((r) => r.matcher(url, init))
    const res = hit?.response ?? { ok: false, status: 404, body: { error: { code: 'not_found', message: 'nope' } } }
    return { ok: res.ok, status: res.status, json: async () => res.body }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

const stubGet = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === SMTP_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

describe('AdminEinstellungenPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.email']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('RENDER: shows the SMTP settings with the masked password state', async () => {
    stubFetchRoutes([stubGet(settingsFixture())])
    renderPage()

    expect(await screen.findByLabelText('SMTP-Host')).toHaveValue('smtp.example.com')
    expect(screen.getByLabelText('Port')).toHaveValue(587)
    expect(screen.getByLabelText('Verschlüsselung')).toHaveValue('starttls')
    expect(screen.getByLabelText('Absenderadresse')).toHaveValue('noreply@example.com')
    expect(screen.getByLabelText('Absendername (optional)')).toHaveValue('G.E.A.R.')
    expect(screen.getByLabelText('Benutzername (optional)')).toHaveValue('smtpuser')
    // The password field is empty and masked, showing the stored state.
    const passwordInput = screen.getByLabelText(/Passwort/) as HTMLInputElement
    expect(passwordInput.value).toBe('')
    expect(passwordInput).toHaveAttribute('type', 'password')
    expect(passwordInput).toHaveAttribute('placeholder', '••••••••')
    expect(screen.getByText('Ein Passwort ist gespeichert. Nur ausfüllen, um es zu ändern.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sendetest-E-Mail' })).toBeInTheDocument()
  })

  it('GET_HEADERS: the load uses the authenticated admin headers', async () => {
    const fetchMock = stubFetchRoutes([stubGet(settingsFixture())])
    renderPage()
    await screen.findByLabelText('SMTP-Host')

    expect(fetchMock).toHaveBeenCalledWith(SMTP_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('SAVE: PUT persists the settings and omits the blank password (keeps existing)', async () => {
    const fetchMock = stubFetchRoutes([
      stubGet(settingsFixture()),
      {
        matcher: (url, init) => url === SMTP_URL && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...settingsFixture(), host: 'mail.example.com', message: 'SMTP-Einstellungen gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.clear(screen.getByLabelText('SMTP-Host'))
    await user.type(screen.getByLabelText('SMTP-Host'), 'mail.example.com')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('SMTP-Einstellungen gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === SMTP_URL && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.host).toBe('mail.example.com')
    expect(body.port).toBe(587)
    expect('password' in body).toBe(false)
  })

  it('SAVE_WITH_PASSWORD: a typed password is included in the PUT body', async () => {
    const fetchMock = stubFetchRoutes([
      stubGet({ ...settingsFixture(), password_configured: false }),
      {
        matcher: (url, init) => url === SMTP_URL && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...settingsFixture(), message: 'SMTP-Einstellungen gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.type(screen.getByLabelText(/Passwort/), 'neues-geheim')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('SMTP-Einstellungen gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === SMTP_URL && init?.method === 'PUT')
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.password).toBe('neues-geheim')
  })

  it('SAVE_ERROR: a 400 shows the server German message inline', async () => {
    stubFetchRoutes([
      stubGet({ ...settingsFixture(), password_configured: false }),
      {
        matcher: (url, init) => url === SMTP_URL && init?.method === 'PUT',
        response: { ok: false, status: 400, body: { error: { code: 'invalid_request', message: 'Bitte gib einen SMTP-Host an.' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.clear(screen.getByLabelText('SMTP-Host'))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib einen SMTP-Host an.')
  })

  it('TEST_OK: Sendetest-E-Mail shows the inline success', async () => {
    stubFetchRoutes([
      stubGet(settingsFixture()),
      {
        matcher: (url, init) => url === SMTP_TEST_URL && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ok: true, message: 'Test-E-Mail erfolgreich gesendet.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Sendetest-E-Mail' }))

    expect(await screen.findByText('Test-E-Mail erfolgreich gesendet.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('TEST_FAIL: an SMTP failure shows the inline German error (never a toast)', async () => {
    stubFetchRoutes([
      stubGet(settingsFixture()),
      {
        matcher: (url, init) => url === SMTP_TEST_URL && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ok: false, message: 'Die Test-E-Mail konnte nicht gesendet werden.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Sendetest-E-Mail' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Die Test-E-Mail konnte nicht gesendet werden.')
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SMTP_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('SAVE_FORBIDDEN: a 403 on save leaves the module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      stubGet(settingsFixture()),
      {
        matcher: (url, init) => url === SMTP_URL && init?.method === 'PUT',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('UNAUTHORIZED: a 401 on load clears auth state and redirects to /login', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SMTP_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed load shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === SMTP_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Die SMTP-Einstellungen konnten nicht geladen werden.')
  })
})