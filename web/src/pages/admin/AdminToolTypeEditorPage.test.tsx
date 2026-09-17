// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminToolTypeEditorPage } from './AdminToolTypeEditorPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const TOOL_TYPES_URL = '/api/v1/admin/tool-types'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'
const QUALIFICATIONS_URL = '/api/v1/admin/qualifications'

function toolTypeFixture() {
  return {
    id: 'id-t1',
    name: 'Bohrmaschine',
    default_schedule_id: 'id-s1',
    required_qualification_id: 'id-q1',
    inspection_mode: 'checklist',
    attributes: {},
    checklist_items: [
      { id: 'ci-1', position: 0, label: 'Bohrfutter' },
      { id: 'ci-2', position: 1, label: 'Kabel' },
    ],
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

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

function qualificationsFixture() {
  return {
    qualifications: [
      { id: 'id-q1', name: 'Kettensäge', description: '', expiry_kind: 'unlimited', status: 'unlimited' },
    ],
    users: [],
  }
}

function renderEditorPage(initialEntry: string) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/admin/werkzeuge/typen/neu" element={<AdminToolTypeEditorPage />} />
          <Route path="/admin/werkzeuge/typen/:id" element={<AdminToolTypeEditorPage />} />
          <Route path="/admin/werkzeuge" element={<div>WerkzeugListe</div>} />
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

const stubSchedules = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubQualifications = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === QUALIFICATIONS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubToolTypes = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

describe('AdminToolTypeEditorPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('CREATE_RENDER: create mode renders the empty form with the first schedule auto-selected', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
    ])
    renderEditorPage('/admin/werkzeuge/typen/neu')

    expect(await screen.findByRole('heading', { name: 'Neuer Gerätetyp' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('')
    // Fresh create form: the first schedule is auto-selected so the save is
    // immediately valid; the required qualification stays unselected.
    expect(screen.getByLabelText('Standard-Zeitplan')).toHaveValue('id-s1')
    expect(screen.getByLabelText('Erforderliche Qualifikation')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeEnabled()
    // Back button links to the list.
    expect(screen.getByRole('button', { name: '← Zurück zur Liste' })).toBeInTheDocument()
  })

  it('CREATE_SUBMIT: creating a checklist-mode type POSTs the ordered items and navigates back to the list', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolTypeFixture(), id: 'id-new', name: 'Schleifmaschine', message: 'Gerätetyp gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Schleifmaschine')
    await user.click(screen.getByLabelText('Checkliste'))
    const addInput = screen.getByPlaceholderText('Prüfpunkt hinzufügen')
    await user.type(addInput, 'Scheibe')
    await user.click(screen.getByRole('button', { name: 'Hinzufügen' }))
    await user.type(addInput, 'Schutzhaube')
    await user.click(screen.getByRole('button', { name: 'Hinzufügen' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    // Successful create navigates back to the list.
    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOL_TYPES_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.name).toBe('Schleifmaschine')
    expect(body.inspection_mode).toBe('checklist')
    expect(body.items).toEqual([{ label: 'Scheibe' }, { label: 'Schutzhaube' }])
  })

  it('CREATE_ATTRIBUTES: a touched "Eigene Felder" section submits the attributes object', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolTypeFixture(), id: 'id-new', name: 'Schleifmaschine', message: 'Gerätetyp gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Schleifmaschine')
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'standort')
    await user.type(screen.getByLabelText('Wert 1'), 'Werkstatt')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOL_TYPES_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.attributes).toEqual({ standort: 'Werkstatt' })
  })

  it('EDIT_PREFILL: edit mode pre-fills the stored type and PUTs the changes', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      stubToolTypes([toolTypeFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...toolTypeFixture(), name: 'Bohrmaschine neu', message: 'Gerätetyp gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/id-t1')

    expect(await screen.findByRole('heading', { name: 'Gerätetyp bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Bohrmaschine')
    expect(screen.getByLabelText('Standard-Zeitplan')).toHaveValue('id-s1')
    expect(screen.getByLabelText('Erforderliche Qualifikation')).toHaveValue('id-q1')
    // Checklist-mode type: the stored items render in the item editor.
    expect(screen.getByText('Bohrfutter')).toBeInTheDocument()
    expect(screen.getByText('Kabel')).toBeInTheDocument()

    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine neu')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.name).toBe('Bohrmaschine neu')
    expect(body.inspection_mode).toBe('checklist')
  })

  it('EDIT_UNKNOWN: an unknown :id renders the German not-found and no form', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      stubToolTypes([toolTypeFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/typen/id-unbekannt')

    expect(await screen.findByText('Gerätetyp nicht gefunden.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
  })

  it('EDIT_401: a 401 on the type load clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string) => url === TOOL_TYPES_URL,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/typen/id-t1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('EDIT_403: a 403 on the type load leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string) => url === TOOL_TYPES_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/typen/id-t1')

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('SAVE_400: a 400 on create shows the server German message inline (no navigation)', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: {
          ok: false,
          status: 400,
          body: { error: { code: 'invalid_request', message: 'Es gibt bereits einen Gerätetyp mit diesem Namen.' } },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Es gibt bereits einen Gerätetyp mit diesem Namen.')
    expect(screen.queryByText('WerkzeugListe')).not.toBeInTheDocument()
  })

  it('CREATE_DEGRADE_SCHEDULES_403: a 403 on the schedules catalog must NOT eject — empty option, Save disabled with the hint', async () => {
    // A holder of tool_types.manage WITHOUT schedules.manage gets a 403 on the
    // schedule catalog. The form still renders (no eject), the schedule
    // dropdown degrades to the German empty option and Save is disabled.
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubQualifications(qualificationsFixture()),
    ])
    renderEditorPage('/admin/werkzeuge/typen/neu')

    expect(await screen.findByRole('heading', { name: 'Neuer Gerätetyp' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(screen.queryByText('Anmeldung')).not.toBeInTheDocument()
    const scheduleSelect = screen.getByLabelText('Standard-Zeitplan')
    expect(scheduleSelect).toHaveTextContent('Keine Zeitpläne vorhanden')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(
      screen.getByText('Wähle einen Standard-Zeitplan aus, um zu speichern.'),
    ).toBeInTheDocument()
  })

  it('CREATE_EMPTY_CATALOG_SAVE_DISABLED: an empty schedules catalog disables Save with the hint', async () => {
    stubFetchRoutes([
      stubSchedules([]),
      stubQualifications(qualificationsFixture()),
    ])
    renderEditorPage('/admin/werkzeuge/typen/neu')

    expect(await screen.findByRole('heading', { name: 'Neuer Gerätetyp' })).toBeInTheDocument()
    expect(screen.getByLabelText('Standard-Zeitplan')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
  })

  it('CREATE_SECONDARY_401: a 401 on a secondary catalog fetch is authoritative — redirects to /login (Spec 4-6 review 6)', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
      stubQualifications(qualificationsFixture()),
    ])
    renderEditorPage('/admin/werkzeuge/typen/neu')

    // Create mode has no primary list load — the expired session must not
    // silently render a full form.
    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('EDIT_LOAD_500: a 500 on the edit load shows the German load error', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string) => url === TOOL_TYPES_URL,
        response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Interner Fehler.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/typen/id-t1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Der Gerätetyp konnte nicht geladen werden.')
  })

  it('SAVE_401: a 401 during save clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('SAVE_403: a 403 during save leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      stubQualifications(qualificationsFixture()),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/typen/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })
})
