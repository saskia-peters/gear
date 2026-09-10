// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminEinstellungenPage } from './AdminEinstellungenPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const SMTP_URL = '/api/v1/admin/settings/smtp'
const SMTP_TEST_URL = '/api/v1/admin/settings/smtp/test'
const BACKUP_URL = '/api/v1/admin/settings/backup'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'

function scheduleFixture() {
  return {
    id: 'id-s1',
    name: '1 year',
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

    expect(await screen.findByLabelText('Name')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Zeitpläne' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'E-Mail' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Backup' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('SMTP-Host')).not.toBeInTheDocument()
  })

  it('TAB_GATING_EMAIL: with only admin.settings.email the Zeitpläne tab is hidden', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['admin.settings.email']))
    stubFetchRoutes([stubGet(settingsFixture())])
    renderPage()

    expect(await screen.findByLabelText('SMTP-Host')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Zeitpläne' })).not.toBeInTheDocument()
  })

  it('EMPTY_LIST: no schedules renders an empty list without error', async () => {
    stubFetchRoutes([stubSchedulesList([])])
    renderPage()
    expect(await screen.findByRole('button', { name: 'Speichern' })).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Zeitpläne' })).not.toBeInTheDocument()
  })

  it('LIST: schedules render with the "Jährlich − 1 Jahr" interval display', async () => {
    stubFetchRoutes([
      stubSchedulesList([
        scheduleFixture(),
        { ...scheduleFixture(), id: 'id-s2', name: '2 weeks', interval_unit: 'week', interval_magnitude: 2 },
      ]),
    ])
    renderPage()

    expect(await screen.findByText('1 year')).toBeInTheDocument()
    expect(screen.getByText('Jährlich − 1 Jahr')).toBeInTheDocument()
    expect(screen.getByText('Wöchentlich − 2 Wochen')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Archivieren' })).toHaveLength(2)
  })

  it('CREATE: the form POSTs and adds the row inline', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedulesList([]),
      {
        matcher: (url, init) => url === SCHEDULES_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { ...scheduleFixture(), message: 'Zeitplan gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('button', { name: 'Speichern' })
    await user.type(screen.getByLabelText('Name'), '1 year')
    await user.selectOptions(screen.getByLabelText('Zeiteinheit'), 'year')
    await user.clear(screen.getByLabelText('Intervallgröße'))
    await user.type(screen.getByLabelText('Intervallgröße'), '1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Zeitplan gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === SCHEDULES_URL && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall![1] as RequestInit).body as string)
    expect(body.name).toBe('1 year')
    expect(body.interval_unit).toBe('year')
    expect(body.interval_magnitude).toBe(1)
    expect(await screen.findByText('1 year')).toBeInTheDocument()
  })

  it('EDIT: Bearbeiten loads the row, PUT persists the changed interval', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedulesList([scheduleFixture()]),
      {
        matcher: (url, init) => url === `${SCHEDULES_URL}/id-s1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...scheduleFixture(), name: '2 years', interval_magnitude: 2, message: 'Zeitplan gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('1 year')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(screen.getByRole('heading', { name: 'Zeitplan bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('1 year')
    expect(screen.getByLabelText('Zeiteinheit')).toHaveValue('year')

    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), '2 years')
    await user.clear(screen.getByLabelText('Intervallgröße'))
    await user.type(screen.getByLabelText('Intervallgröße'), '2')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('Zeitplan gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${SCHEDULES_URL}/id-s1` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.name).toBe('2 years')
    expect(body.interval_magnitude).toBe(2)
    expect(await screen.findByText('2 years')).toBeInTheDocument()
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

    await screen.findByText('1 year')
    await user.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(await screen.findByText('Zeitplan archiviert.')).toBeInTheDocument()
    expect(screen.queryByText('1 year')).not.toBeInTheDocument()
    const archiveCall = fetchMock.mock.calls.find(([url, init]) => url === `${SCHEDULES_URL}/id-s1/archive` && init?.method === 'POST')
    expect(archiveCall).toBeTruthy()
  })

  it('ARCHIVE_CANCEL: declining the prompt does not call the archive endpoint', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = stubFetchRoutes([stubSchedulesList([scheduleFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('1 year')
    await user.click(screen.getByRole('button', { name: 'Archivieren' }))

    const archiveCall = fetchMock.mock.calls.find(([url, init]) => url === `${SCHEDULES_URL}/id-s1/archive` && init?.method === 'POST')
    expect(archiveCall).toBeUndefined()
    expect(screen.getByText('1 year')).toBeInTheDocument()
  })

  it('FORM_ERROR: a 400 shows the server German message inline', async () => {
    stubFetchRoutes([
      stubSchedulesList([]),
      {
        matcher: (url, init) => url === SCHEDULES_URL && init?.method === 'POST',
        response: { ok: false, status: 400, body: { error: { code: 'invalid_request', message: 'Bitte gib einen Namen für den Zeitplan an.' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('button', { name: 'Speichern' })
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib einen Namen für den Zeitplan an.')
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
})