// @vitest-environment jsdom
import { render, screen, within, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminEinstellungenPage } from './AdminEinstellungenPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const SMTP_URL = '/api/v1/admin/settings/smtp'
const SMTP_TEST_URL = '/api/v1/admin/settings/smtp/test'
const BACKUP_URL = '/api/v1/admin/settings/backup'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'
const SYSTEM_URL = '/api/v1/admin/settings/system'

function scheduleFixture() {
  return {
    id: 'id-s1',
    name: '1 Jahr',
    interval_unit: 'year',
    interval_magnitude: 1,
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

function backupFixture() {
  return {
    id: 'id-a',
    name: 'S3 Ziel',
    mechanism: 's3',
    endpoint: 's3.example.com',
    bucket_or_path: 'bucket',
    username: 'svc',
    credential_configured: true,
    schedule: '0 2 * * *',
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

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

function renderPage(initialEntry: string | { pathname: string; state?: unknown } = '/admin/einstellungen') {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/admin/einstellungen" element={<AdminEinstellungenPage />} />
          <Route path="/admin/einstellungen/zeitplaene/neu" element={<div>ZeitplanNeuSeite</div>} />
          <Route path="/admin/einstellungen/zeitplaene/:id" element={<div>ZeitplanEditSeite</div>} />
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

  it('TEST_OK: Sendetest-E-Mail asks for the receiver in a dialog and shows the inline success', async () => {
    const fetchMock = stubFetchRoutes([
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

    // The receiver prompt opens before any request goes out.
    const dialog = await screen.findByRole('dialog', { name: 'Test-E-Mail senden' })
    expect(dialog).toBeInTheDocument()
    await user.type(screen.getByLabelText('Empfängeradresse'), 'ziel@example.com')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByText('Test-E-Mail erfolgreich gesendet.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    // The receiver travelled in the POST body.
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === SMTP_TEST_URL && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall![1] as RequestInit).body as string)
    expect(body.to).toBe('ziel@example.com')
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
    await user.type(await screen.findByLabelText('Empfängeradresse'), 'ziel@example.com')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Die Test-E-Mail konnte nicht gesendet werden.')
  })

  it('TEST_PROMPT_EMPTY: sending with an empty receiver is blocked inline, no POST', async () => {
    const fetchMock = stubFetchRoutes([stubGet(settingsFixture())])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Sendetest-E-Mail' }))
    await screen.findByLabelText('Empfängeradresse')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib eine Empfängeradresse ein.')
    const postCalls = fetchMock.mock.calls.filter(([url, init]) => url === SMTP_TEST_URL && init?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })

  it('TEST_PROMPT_INVALID: a malformed receiver is rejected inline with the German message, no POST', async () => {
    const fetchMock = stubFetchRoutes([stubGet(settingsFixture())])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Sendetest-E-Mail' }))
    await user.type(await screen.findByLabelText('Empfängeradresse'), 'kein-at')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib eine gültige Empfängeradresse an.')
    const postCalls = fetchMock.mock.calls.filter(([url, init]) => url === SMTP_TEST_URL && init?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })

  it('TEST_PROMPT_CANCEL: Abbrechen closes the receiver prompt without sending', async () => {
    const fetchMock = stubFetchRoutes([stubGet(settingsFixture())])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('SMTP-Host')
    await user.click(screen.getByRole('button', { name: 'Sendetest-E-Mail' }))
    expect(await screen.findByRole('dialog', { name: 'Test-E-Mail senden' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Abbrechen' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    const postCalls = fetchMock.mock.calls.filter(([url, init]) => url === SMTP_TEST_URL && init?.method === 'POST')
    expect(postCalls).toHaveLength(0)
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

describe('AdminEinstellungenPage Backup tab', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.backup']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  const stubBackupList = (body: unknown) => ({
    matcher: (url: string, init?: RequestInit) => url === BACKUP_URL && !init?.method,
    response: { ok: true, status: 200, body },
  })

  it('TAB_GATING_BACKUP: with only admin.settings.backup the E-Mail tab is hidden and Backup renders', async () => {
    stubFetchRoutes([stubBackupList([])])
    renderPage()

    expect(await screen.findByText('Mindestens ein Backup-Ziel ist erforderlich.')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Backup' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'E-Mail' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('SMTP-Host')).not.toBeInTheDocument()
  })

  it('TAB_GATING_EMAIL: with only admin.settings.email the Backup tab is hidden and E-Mail renders', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.email']))
    stubFetchRoutes([stubGet(settingsFixture())])
    renderPage()

    expect(await screen.findByLabelText('SMTP-Host')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'E-Mail' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Backup' })).not.toBeInTheDocument()
  })

  it('EMPTY_LIST: the ≥1 destination warning is shown when none exist', async () => {
    stubFetchRoutes([stubBackupList([])])
    renderPage()
    expect(await screen.findByText('Mindestens ein Backup-Ziel ist erforderlich.')).toBeInTheDocument()
  })

  it('LIST: destinations render with masked credential state', async () => {
    stubFetchRoutes([stubBackupList([backupFixture()])])
    renderPage()

    expect(await screen.findByText('S3 Ziel')).toBeInTheDocument()
    expect(screen.getByText(/S3-kompatibel · s3\.example\.com · bucket · svc · 0 2 \* \* \*/)).toBeInTheDocument()
    expect(screen.getByText('Zugangsberechtigung konfiguriert')).toBeInTheDocument()
    // The credential value is never rendered (write-only, NFR-S4).
    expect(screen.queryByText('geheim')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Verbindung testen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Löschen' })).toBeInTheDocument()
  })

  it('CREATE: the form POSTs and adds the row inline', async () => {
    const fetchMock = stubFetchRoutes([
      stubBackupList([]),
      {
        matcher: (url, init) => url === BACKUP_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { ...backupFixture(), message: 'Backup-Ziel gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Mindestens ein Backup-Ziel ist erforderlich.')
    await user.type(screen.getByLabelText('Name'), 'S3 Ziel')
    await user.selectOptions(screen.getByLabelText('Mechanismus'), 's3')
    await user.type(screen.getByLabelText(/Endpunkt \/ Host/), 's3.example.com')
    await user.type(screen.getByLabelText('Bucket'), 'bucket')
    await user.type(screen.getByLabelText(/Benutzername/), 'svc')
    await user.type(screen.getByLabelText(/Zugangsberechtigung/), 'geheim123')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Backup-Ziel gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === BACKUP_URL && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall![1] as RequestInit).body as string)
    expect(body.mechanism).toBe('s3')
    expect(body.bucket_or_path).toBe('bucket')
    expect(body.password).toBe('geheim123')
    expect(await screen.findByText('S3 Ziel')).toBeInTheDocument()
  })

  it('EDIT: Bearbeiten loads the row, PUT omits a blank credential (keeps existing)', async () => {
    const fetchMock = stubFetchRoutes([
      stubBackupList([backupFixture()]),
      {
        matcher: (url, init) => url === `${BACKUP_URL}/id-a` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...backupFixture(), name: 'S3 Ziel v2', message: 'Backup-Ziel gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('S3 Ziel')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(screen.getByRole('heading', { name: 'Backup-Ziel bearbeiten' })).toBeInTheDocument()
    // The stored credential is masked, not filled in.
    const passwordInput = screen.getByLabelText(/^Zugangsberechtigung/) as HTMLInputElement
    expect(passwordInput.value).toBe('')
    expect(passwordInput).toHaveAttribute('placeholder', '••••••••')

    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), 'S3 Ziel v2')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('Backup-Ziel gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${BACKUP_URL}/id-a` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.name).toBe('S3 Ziel v2')
    expect('password' in body).toBe(false)
    expect(await screen.findByText('S3 Ziel v2')).toBeInTheDocument()
  })

  it('EDIT_REMOVE_CREDENTIAL: checking Anmeldedaten entfernen sends clear_credential:true and no password', async () => {
    const fetchMock = stubFetchRoutes([
      stubBackupList([backupFixture()]),
      {
        matcher: (url, init) => url === `${BACKUP_URL}/id-a` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...backupFixture(), credential_configured: false, message: 'Backup-Ziel gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('S3 Ziel')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    // The remove affordance appears only when a stored credential exists.
    const checkbox = screen.getByLabelText(/Anmeldedaten entfernen/)
    expect(checkbox).not.toBeChecked()
    // The masked password field is present but empty.
    const passwordInput = screen.getByLabelText(/^Zugangsberechtigung/) as HTMLInputElement
    await user.type(passwordInput, 'darf-nicht-reisen')
    await user.click(checkbox)
    // Typing is disabled while removal is armed, so the value is cleared.
    expect(passwordInput).toBeDisabled()
    expect(passwordInput).toHaveValue('')

    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('Backup-Ziel gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${BACKUP_URL}/id-a` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.clear_credential).toBe(true)
    expect('password' in body).toBe(false)
    expect(await screen.findByText('Ohne Zugangsberechtigung')).toBeInTheDocument()
  })

  it('TEST_OK: Verbindung testen shows the inline success', async () => {
    stubFetchRoutes([
      stubBackupList([backupFixture()]),
      {
        matcher: (url, init) => url === `${BACKUP_URL}/id-a/test` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ok: true, message: 'Verbindung erfolgreich getestet.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('S3 Ziel')
    await user.click(screen.getByRole('button', { name: 'Verbindung testen' }))

    expect(await screen.findByText('Verbindung erfolgreich getestet.')).toBeInTheDocument()
  })

  it('TEST_FAIL: a failed connection shows the inline German error', async () => {
    stubFetchRoutes([
      stubBackupList([backupFixture()]),
      {
        matcher: (url, init) => url === `${BACKUP_URL}/id-a/test` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ok: false, message: 'Die Verbindung zum Backup-Ziel konnte nicht hergestellt werden.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('S3 Ziel')
    await user.click(screen.getByRole('button', { name: 'Verbindung testen' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Die Verbindung zum Backup-Ziel konnte nicht hergestellt werden.')
  })

  it('DELETE: Löschen removes the row and shows the confirmation', async () => {
    const fetchMock = stubFetchRoutes([
      stubBackupList([backupFixture()]),
      {
        matcher: (url, init) => url === `${BACKUP_URL}/id-a` && init?.method === 'DELETE',
        response: { ok: true, status: 200, body: { message: 'Backup-Ziel gelöscht.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('S3 Ziel')
    await user.click(screen.getByRole('button', { name: 'Löschen' }))

    expect(await screen.findByText('Backup-Ziel gelöscht.')).toBeInTheDocument()
    expect(screen.queryByText('S3 Ziel')).not.toBeInTheDocument()
    expect(await screen.findByText('Mindestens ein Backup-Ziel ist erforderlich.')).toBeInTheDocument()
    const delCall = fetchMock.mock.calls.find(([url, init]) => url === `${BACKUP_URL}/id-a` && init?.method === 'DELETE')
    expect(delCall).toBeTruthy()
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === BACKUP_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('UNAUTHORIZED: a 401 on load clears auth state and redirects to /login', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === BACKUP_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })
})

describe('AdminEinstellungenPage Zeitpläne tab', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['schedules.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
    vi.restoreAllMocks()
  })

  const stubSchedulesList = (body: unknown) => ({
    matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
    response: { ok: true, status: 200, body },
  })

  it('TAB_GATING_SCHEDULES: with only schedules.manage the E-Mail/Backup tabs are hidden and Zeitpläne renders', async () => {
    stubFetchRoutes([stubSchedulesList([])])
    renderPage()

    expect(await screen.findByText('Keine Zeitpläne vorhanden. Lege den ersten Zeitplan an.')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Zeitpläne' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'E-Mail' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Backup' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('SMTP-Host')).not.toBeInTheDocument()
    // No inline create/edit form on the list.
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Intervallgröße')).not.toBeInTheDocument()
  })

  it('TAB_GATING_EMAIL: with only admin.settings.email the Zeitpläne tab is hidden', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.email']))
    stubFetchRoutes([stubGet(settingsFixture())])
    renderPage()

    expect(await screen.findByLabelText('SMTP-Host')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Zeitpläne' })).not.toBeInTheDocument()
  })

  it('EMPTY_LIST: no schedules renders the German empty note and the create button stays visible', async () => {
    stubFetchRoutes([stubSchedulesList([])])
    renderPage()
    expect(await screen.findByText('Keine Zeitpläne vorhanden. Lege den ersten Zeitplan an.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Zeitplan' })).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Zeitpläne' })).not.toBeInTheDocument()
  })

  it('LIST: schedules render as compact one-line rows with the interval display + sort headers', async () => {
    stubFetchRoutes([
      stubSchedulesList([
        scheduleFixture(),
        { ...scheduleFixture(), id: 'id-s2', name: '2 Wochen', interval_unit: 'week', interval_magnitude: 2 },
      ]),
    ])
    renderPage()

    expect(await screen.findByText('1 Jahr')).toBeInTheDocument()
    expect(screen.getByText('Jährlich − 1 Jahr')).toBeInTheDocument()
    expect(screen.getByText('Wöchentlich − 2 Wochen')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Archivieren' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: 'Bearbeiten' })).toHaveLength(2)
    // The Name column is the default sort, so it shows the next-action hint.
    expect(screen.getByRole('button', { name: 'Sortieren nach Name (absteigend)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sortieren nach Intervall' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Zeitplan' })).toBeInTheDocument()
  })

  it('SORT: clicking the Intervall column re-orders by CHRONOLOGICAL duration (Spec 4-6 review 2)', async () => {
    stubFetchRoutes([
      stubSchedulesList([
        scheduleFixture(), // Jährlich − 1 Jahr (365 days)
        { ...scheduleFixture(), id: 'id-s2', name: 'Monatlich', interval_unit: 'month', interval_magnitude: 1 }, // 30 days
        { ...scheduleFixture(), id: 'id-s3', name: '2 Wochen', interval_unit: 'week', interval_magnitude: 2 }, // 14 days
      ]),
    ])
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('1 Jahr')).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'Zeitpläne' })
    const names = () =>
      Array.from(table.querySelectorAll('tbody tr')).map((tr) => tr.querySelector('td')?.textContent?.trim() ?? '')
    // Default: Name asc → "1 Jahr" before "2 Wochen" before "Monatlich".
    expect(names()).toEqual(['1 Jahr', '2 Wochen', 'Monatlich'])
    // Intervall asc → shortest first: 2 Wochen (14d), Monatlich (30d), 1 Jahr (365d).
    await user.click(screen.getByRole('button', { name: 'Sortieren nach Intervall' }))
    expect(names()).toEqual(['2 Wochen', 'Monatlich', '1 Jahr'])
    // Intervall desc → longest first.
    await user.click(screen.getByRole('button', { name: 'Sortieren nach Intervall (absteigend)' }))
    expect(names()).toEqual(['1 Jahr', 'Monatlich', '2 Wochen'])
  })

  it('CREATE_NAV: "Neuer Zeitplan" navigates to the dedicated create page', async () => {
    stubFetchRoutes([stubSchedulesList([])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Keine Zeitpläne vorhanden. Lege den ersten Zeitplan an.')
    await user.click(screen.getByRole('button', { name: 'Neuer Zeitplan' }))
    expect(await screen.findByText('ZeitplanNeuSeite')).toBeInTheDocument()
  })

  it('EDIT_NAV: "Bearbeiten" navigates to the dedicated :id edit page', async () => {
    stubFetchRoutes([stubSchedulesList([scheduleFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('1 Jahr')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(await screen.findByText('ZeitplanEditSeite')).toBeInTheDocument()
  })

  it('ARCHIVE: confirming the prompt archives the row and removes it inline', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = stubFetchRoutes([
      stubSchedulesList([scheduleFixture()]),
      {
        matcher: (url, init) => url === `${SCHEDULES_URL}/id-s1/archive` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ...scheduleFixture(), message: 'Zeitplan archiviert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('1 Jahr')
    await user.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(await screen.findByText('Zeitplan archiviert.')).toBeInTheDocument()
    expect(screen.queryByText('1 Jahr')).not.toBeInTheDocument()
    const archiveCall = fetchMock.mock.calls.find(([url, init]) => url === `${SCHEDULES_URL}/id-s1/archive` && init?.method === 'POST')
    expect(archiveCall).toBeTruthy()
  })

  it('ARCHIVE_CANCEL: declining the prompt does not call the archive endpoint', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = stubFetchRoutes([stubSchedulesList([scheduleFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('1 Jahr')
    await user.click(screen.getByRole('button', { name: 'Archivieren' }))

    const archiveCall = fetchMock.mock.calls.find(([url, init]) => url === `${SCHEDULES_URL}/id-s1/archive` && init?.method === 'POST')
    expect(archiveCall).toBeUndefined()
    expect(screen.getByText('1 Jahr')).toBeInTheDocument()
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SCHEDULES_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('UNAUTHORIZED: a 401 on load clears auth state and redirects to /login', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SCHEDULES_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('NOTICE_TAB_RETURN: a multi-tab holder returns from the schedule editor to the Zeitpläne tab with the success notice', async () => {
    // The holder owns BOTH E-Mail and Zeitpläne: the editor returns with
    // { tab: 'schedules', message } so the Zeitpläne tab (not the E-Mail
    // default) is active and the server confirmation shows once (Spec 4-6
    // review 1/4).
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.email', 'schedules.manage']))
    stubFetchRoutes([
      stubGet(settingsFixture()),
      stubSchedulesList([scheduleFixture()]),
    ])
    renderPage({ pathname: '/admin/einstellungen', state: { tab: 'schedules', message: 'Zeitplan gespeichert.' } })

    expect(await screen.findByText('1 Jahr')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Zeitpläne' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'E-Mail' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('status')).toHaveTextContent('Zeitplan gespeichert.')
  })
})

describe('AdminEinstellungenPage System tab', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.system']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  // The 21 seeded atomic settings exactly as the server GET answers them
  // (durations as whole seconds; integer/text rows carry the display unit).
  function systemSettingsFixture() {
    return [
      { key: 'smtp_dial_timeout', value_type: 'duration', value: 10 },
      { key: 'smtp_protocol_timeout', value_type: 'duration', value: 30 },
      { key: 'backup_dial_timeout', value_type: 'duration', value: 10 },
      { key: 'backup_protocol_timeout', value_type: 'duration', value: 10 },
      { key: 'password_reset_ttl', value_type: 'duration', value: 1800 },
      { key: 'admin_recovery_ttl', value_type: 'duration', value: 1800 },
      { key: 'forgot_throttle_interval', value_type: 'duration', value: 60 },
      { key: 'otp_ttl', value_type: 'duration', value: 900 },
      { key: 'otp_length', value_type: 'integer', unit: 'Zeichen', value: 10 },
      { key: 'mfa_enrollment_window', value_type: 'duration', value: 600 },
      { key: 'lockout_threshold_short', value_type: 'integer', unit: 'Fehlversuche', value: 3 },
      { key: 'lockout_threshold_long', value_type: 'integer', unit: 'Fehlversuche', value: 4 },
      { key: 'lockout_duration_short', value_type: 'duration', value: 30 },
      { key: 'lockout_duration_long', value_type: 'duration', value: 60 },
      { key: 'lockout_max_failed_count', value_type: 'integer', unit: 'Fehlversuche', value: 10 },
      { key: 'attribute_key_max_runes', value_type: 'integer', unit: 'Zeichen', value: 64 },
      { key: 'attributes_max_size', value_type: 'integer', unit: 'Bytes', value: 16384 },
      { key: 'inventory_prefix', value_type: 'text', value: 'GEAR' },
      { key: 'inventory_width', value_type: 'integer', unit: 'Ziffern', value: 6 },
      { key: 'inspection_orange_window_days', value_type: 'integer', unit: 'Tage', value: 14 },
      { key: 'qualification_expiring_soon_window', value_type: 'duration', value: 2592000 },
    ]
  }

  const stubSystemList = (body: unknown) => ({
    matcher: (url: string, init?: RequestInit) => url === SYSTEM_URL && !init?.method,
    response: { ok: true, status: 200, body },
  })

  it('TAB_GATING_SYSTEM: with only admin.settings.system the other tabs are hidden and the System table renders', async () => {
    stubFetchRoutes([stubSystemList(systemSettingsFixture())])
    renderPage()

    // The settings render under category headings; the first table is the
    // E-Mail-Versand group.
    expect(await screen.findByRole('table', { name: 'E-Mail-Versand' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'E-Mail-Versand' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'System' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'E-Mail' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Backup' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Zeitpläne' })).not.toBeInTheDocument()
  })

  it('SPA_CATEGORIES: the 21 settings are grouped under the 8 German category headings', async () => {
    stubFetchRoutes([stubSystemList(systemSettingsFixture())])
    renderPage()

    await screen.findByRole('table', { name: 'E-Mail-Versand' })
    const headings = [
      'E-Mail-Versand',
      'Backup',
      'Passwort & Kontowiederherstellung',
      'Zwei-Faktor-Authentifizierung (MFA)',
      'Anmeldesperre',
      'Attribute',
      'Inventarnummern',
      'Prüfung & Qualifikation',
    ]
    for (const heading of headings) {
      expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument()
    }
    // Every one of the 21 settings still renders (one save button each).
    expect(screen.getAllByRole('button', { name: 'Speichern' })).toHaveLength(21)
  })

  it('SPA_TABLE: every setting row renders German name, formatted value, typed input and a "?" button', async () => {
    stubFetchRoutes([stubSystemList(systemSettingsFixture())])
    renderPage()

    expect(await screen.findByText('SMTP-Verbindungsaufbau-Timeout')).toBeInTheDocument()
    expect(screen.getByText('OTP-Länge')).toBeInTheDocument()
    expect(screen.getByText('Präfix Inventarnummer')).toBeInTheDocument()
    // Durations render a friendly German label (10s, 30min, 30 days).
    expect(screen.getAllByText('10 Sekunden').length).toBeGreaterThanOrEqual(3)
    expect(screen.getAllByText('30 Minuten').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('30 Tage')).toBeInTheDocument()
    // Integer/text values render raw with the server's display unit so days and
    // seconds never look alike.
    expect(screen.getByText('GEAR')).toBeInTheDocument()
    expect(screen.getByText('16384 Bytes')).toBeInTheDocument()
    expect(screen.getByText('14 Tage')).toBeInTheDocument()
    expect(screen.getByText('10 Zeichen')).toBeInTheDocument()
    expect(screen.getByText('6 Ziffern')).toBeInTheDocument()
    // 21 rows: one input + one save + one "?" per row.
    expect(screen.getAllByRole('spinbutton')).toHaveLength(20)
    expect(screen.getAllByRole('textbox')).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: 'Speichern' })).toHaveLength(21)
    expect(screen.getAllByRole('button', { name: /Erklärung zu/ })).toHaveLength(21)
  })

  it('SPA_POPUP: clicking the "?" opens a labelled help popup; Escape closes it', async () => {
    const user = userEvent.setup()
    stubFetchRoutes([stubSystemList(systemSettingsFixture())])
    renderPage()

    const trigger = await screen.findByRole('button', { name: 'Erklärung zu OTP-Länge' })
    await user.click(trigger)

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleName('OTP-Länge')
    expect(dialog).toHaveAccessibleDescription(expect.stringContaining('Einmalpasswort'))
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('SPA_EDIT: editing a value and saving PUTs the typed value and shows the inline confirmation', async () => {
    const fetchMock = stubFetchRoutes([
      stubSystemList(systemSettingsFixture()),
      {
        matcher: (url, init) => url === `${SYSTEM_URL}/otp_length` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { key: 'otp_length', value_type: 'integer', value: 8, message: 'System-Einstellung gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('OTP-Länge')).closest('tr')!
    const input = within(row).getByLabelText('OTP-Länge bearbeiten') as HTMLInputElement
    await user.clear(input)
    await user.type(input, '8')
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('System-Einstellung gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${SYSTEM_URL}/otp_length` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.value).toBe(8)
  })

  it('SPA_EDIT_TEXT: a text setting PUTs the typed value and the row value updates (the server trims)', async () => {
    const fetchMock = stubFetchRoutes([
      stubSystemList(systemSettingsFixture()),
      {
        matcher: (url, init) => url === `${SYSTEM_URL}/inventory_prefix` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { key: 'inventory_prefix', value_type: 'text', value: 'GKW', message: 'System-Einstellung gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('Präfix Inventarnummer')).closest('tr')!
    const input = within(row).getByLabelText('Präfix Inventarnummer bearbeiten') as HTMLInputElement
    await user.clear(input)
    await user.type(input, 'GKW')
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('System-Einstellung gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${SYSTEM_URL}/inventory_prefix` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.value).toBe('GKW')
    expect(await screen.findByText('GKW')).toBeInTheDocument()
  })

  it('SPA_EDIT_ERROR: a server 400 surfaces its German message inline', async () => {
    stubFetchRoutes([
      stubSystemList(systemSettingsFixture()),
      {
        matcher: (url, init) => url === `${SYSTEM_URL}/otp_length` && init?.method === 'PUT',
        response: { ok: false, status: 400, body: { error: { code: 'invalid_request', message: 'Der Wert darf nicht negativ sein.' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('OTP-Länge')).closest('tr')!
    const input = within(row).getByLabelText('OTP-Länge bearbeiten') as HTMLInputElement
    await user.clear(input)
    await user.type(input, '-1')
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Der Wert darf nicht negativ sein.')
  })

  it('SPA_EDIT_DURATION: saving a duration row PUTs whole seconds and the value column shows the friendly label', async () => {
    const fetchMock = stubFetchRoutes([
      stubSystemList(systemSettingsFixture()),
      {
        matcher: (url, init) => url === `${SYSTEM_URL}/otp_ttl` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { key: 'otp_ttl', value_type: 'duration', value: 1200, message: 'System-Einstellung gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('Gültigkeit Einmalpasswort (OTP)')).closest('tr')!
    const input = within(row).getByLabelText('Gültigkeit Einmalpasswort (OTP) bearbeiten') as HTMLInputElement
    await user.clear(input)
    await user.type(input, '1200')
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('System-Einstellung gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${SYSTEM_URL}/otp_ttl` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.value).toBe(1200)
    // 1200 seconds renders the friendly label "20 Minuten".
    expect(await screen.findByText('20 Minuten')).toBeInTheDocument()
  })

  it('SPA_EDIT_EMPTY: clearing a numeric field and saving is blocked client-side with an inline error and never issues a PUT', async () => {
    const fetchMock = stubFetchRoutes([stubSystemList(systemSettingsFixture())])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('OTP-Länge')).closest('tr')!
    const input = within(row).getByLabelText('OTP-Länge bearbeiten') as HTMLInputElement
    await user.clear(input)
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Bitte gib einen Wert für diese Einstellung ein.')).toBeInTheDocument()
    const putCalls = fetchMock.mock.calls.filter(([url, init]) => url.startsWith(`${SYSTEM_URL}/`) && init?.method === 'PUT')
    expect(putCalls).toHaveLength(0)
  })

  it('SPA_EDIT_NONFINITE: a non-finite server value is rejected inline, never re-sent', async () => {
    // JSON cannot carry Infinity, but a huge literal like 1e309 parses to it —
    // the fixture models that drifted server value, and saving it must be
    // blocked inline (never serialized as JSON null).
    const fixture = systemSettingsFixture().map((s) => (s.key === 'otp_length' ? { ...s, value: Infinity } : s))
    const fetchMock = stubFetchRoutes([stubSystemList(fixture)])
    const user = userEvent.setup()
    renderPage()

    const row = (await screen.findByText('OTP-Länge')).closest('tr')!
    await user.click(within(row).getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Ungültiger Wert.')).toBeInTheDocument()
    const putCalls = fetchMock.mock.calls.filter(([url, init]) => url.startsWith(`${SYSTEM_URL}/`) && init?.method === 'PUT')
    expect(putCalls).toHaveLength(0)
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SYSTEM_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('UNAUTHORIZED: a 401 on load clears auth state and redirects to /login', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === SYSTEM_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })
})
