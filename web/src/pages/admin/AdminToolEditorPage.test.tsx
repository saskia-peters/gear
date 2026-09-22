// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminToolEditorPage } from './AdminToolEditorPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const TOOLS_URL = '/api/v1/admin/tools'
const TOOL_TYPES_URL = '/api/v1/admin/tool-types'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'

function toolItemFixture() {
  return {
    id: 'id-w1',
    name: 'Bohrmaschine-01',
    tool_type_id: 'id-t1',
    tool_type_name: 'Bohrmaschine',
    schedule_id: 'id-s1',
    inventory_number: 'GEAR000001',
    attributes: {},
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

function toolTypeFixture() {
  return {
    id: 'id-t1',
    name: 'Bohrmaschine',
    default_schedule_id: 'id-s1',
    required_qualification_id: '',
    inspection_mode: 'pass_fail',
    attributes: {},
    checklist_items: [],
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

function renderEditorPage(initialEntry: string) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/admin/werkzeuge/tools/neu" element={<AdminToolEditorPage />} />
          <Route path="/admin/werkzeuge/tools/:id" element={<AdminToolEditorPage />} />
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

const stubToolTypes = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubSchedules = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubTools = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

describe('AdminToolEditorPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('CREATE_RENDER: create mode renders the empty form with the first type auto-selected and a readonly inventory', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    expect(await screen.findByRole('heading', { name: 'Neues Werkzeug' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('')
    // Fresh create form: the first tool type is auto-selected so the save is
    // immediately valid; the override select starts on the inherit default.
    expect(screen.getByLabelText('Gerätetyp')).toHaveValue('id-t1')
    expect(screen.getByLabelText('Zeitplan')).toHaveValue('')
    expect(screen.getByRole('option', { name: 'Standard für diesen Typ' })).toBeInTheDocument()
    // The inventory number is READ-ONLY on create (the server auto-assigns).
    const createInv = screen.getByLabelText('Gerätenummer')
    expect(createInv).toHaveValue('wird automatisch vergeben')
    expect(createInv).toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '← Zurück zur Liste' })).toBeInTheDocument()
  })

  it('CREATE_SUBMIT: creating a tool POSTs name + type + override and navigates back to the list (no inventory in the body)', async () => {
    const fetchMock = stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolItemFixture(), id: 'id-w2', name: 'Bohrmaschine-02', message: 'Werkzeug gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.selectOptions(screen.getByLabelText('Zeitplan'), 'id-s1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOLS_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.name).toBe('Bohrmaschine-02')
    expect(body.tool_type_id).toBe('id-t1')
    expect(body.schedule_id).toBe('id-s1')
    // CREATE_IGNORE_CLIENT: the create body never carries the inventory number.
    expect(body.inventory_number).toBeUndefined()
  })

  it('CREATE_ATTRIBUTES: a touched "Eigene Felder" section submits the attributes object', async () => {
    const fetchMock = stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolItemFixture(), id: 'id-w2', message: 'Werkzeug gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'standort')
    await user.type(screen.getByLabelText('Wert 1'), 'Werkstatt')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOLS_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.attributes).toEqual({ standort: 'Werkstatt' })
  })

  it('CREATE_BLOCKED_NO_MANAGE: a tool.edit-only holder reaching the create route sees a no-permission note, no form', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.edit']))
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    expect(await screen.findByText('Keine Berechtigung zum Anlegen neuer Werkzeuge.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
  })

  it('EDIT_PREFILL: edit mode pre-fills the stored tool (incl. the editable inventory) and PUTs the changes', async () => {
    const fetchMock = stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...toolItemFixture(), name: 'Bohrmaschine-01 neu', message: 'Werkzeug gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/id-w1')

    expect(await screen.findByRole('heading', { name: 'Werkzeug bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Bohrmaschine-01')
    expect(screen.getByLabelText('Gerätetyp')).toHaveValue('id-t1')
    expect(screen.getByLabelText('Zeitplan')).toHaveValue('id-s1')
    // On edit the inventory number is an EDITABLE input starting at the stored
    // value (unlike the read-only create display).
    const invInput = screen.getByLabelText('Gerätenummer')
    expect(invInput).toHaveValue('GEAR000001')
    expect(invInput).not.toHaveAttribute('readonly')

    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-01 neu')
    await user.clear(invInput)
    await user.type(invInput, 'GEAR0042')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.name).toBe('Bohrmaschine-01 neu')
    expect(body.inventory_number).toBe('GEAR0042')
  })

  it('EDIT_STALE_OVERRIDE_CLEARED: a stale schedule override clears to the inherit default', async () => {
    const stale = { ...toolItemFixture(), schedule_id: 'id-stale' }
    const fetchMock = stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      stubTools([stale]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...stale, schedule_id: '', message: 'Werkzeug gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/id-w1')

    expect(await screen.findByRole('heading', { name: 'Werkzeug bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Zeitplan')).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('WerkzeugListe')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.schedule_id).toBe('')
  })

  it('EDIT_UNKNOWN: an unknown :id renders the German not-found and no form', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      stubTools([toolItemFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/id-unbekannt')

    expect(await screen.findByText('Werkzeug nicht gefunden.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
  })

  it('EDIT_401: a 401 on the tool load clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/tools/id-w1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('EDIT_403: a 403 on the tool load leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/tools/id-w1')

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('SAVE_400: a 400 on create shows the server German message inline (no navigation)', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: {
          ok: false,
          status: 400,
          body: { error: { code: 'invalid_request', message: 'Es gibt bereits ein Werkzeug mit dieser Gerätenummer.' } },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Es gibt bereits ein Werkzeug mit dieser Gerätenummer.')
    expect(screen.queryByText('WerkzeugListe')).not.toBeInTheDocument()
  })

  it('CREATE_DEGRADE_TYPES_403: a 403 on the tool-types catalog must NOT eject — empty option, Save disabled with the hint', async () => {
    // A holder of tools.manage WITHOUT tool_types.manage gets a 403 on the type
    // catalog. The form still renders (no eject), the Gerätetyp dropdown
    // degrades to the German empty option and Save is disabled.
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubSchedules([scheduleFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    expect(await screen.findByRole('heading', { name: 'Neues Werkzeug' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(screen.queryByText('Anmeldung')).not.toBeInTheDocument()
    const typeSelect = screen.getByLabelText('Gerätetyp')
    expect(typeSelect).toHaveTextContent('Keine Gerätetypen vorhanden')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(
      screen.getByText('Wähle einen Gerätetyp aus, um zu speichern.'),
    ).toBeInTheDocument()
  })

  it('CREATE_DEGRADE_SCHEDULES_403: a 403 on the schedules catalog degrades to the inherit-default option without ejecting', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    expect(await screen.findByRole('heading', { name: 'Neues Werkzeug' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    const overrideSelect = screen.getByLabelText('Zeitplan')
    const options = Array.from(overrideSelect.querySelectorAll('option'))
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toBe('Standard für diesen Typ')
    expect(overrideSelect).toHaveValue('')
    // The override is optional — Save stays enabled.
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeEnabled()
  })

  it('CREATE_EMPTY_CATALOG_SAVE_DISABLED: an empty tool-types catalog disables Save with the hint', async () => {
    stubFetchRoutes([
      stubToolTypes([]),
      stubSchedules([scheduleFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    expect(await screen.findByRole('heading', { name: 'Neues Werkzeug' })).toBeInTheDocument()
    expect(screen.getByLabelText('Gerätetyp')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
  })

  it('CREATE_SECONDARY_401: a 401 on a secondary catalog fetch is authoritative — redirects to /login (Spec 4-6 review 6)', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
      stubSchedules([scheduleFixture()]),
    ])
    renderEditorPage('/admin/werkzeuge/tools/neu')

    // Create mode has no primary list load — the expired session must not
    // silently render a full form.
    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('EDIT_LOAD_500: a 500 on the edit load shows the German load error', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Interner Fehler.' } } },
      },
    ])
    renderEditorPage('/admin/werkzeuge/tools/id-w1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Das Werkzeug konnte nicht geladen werden.')
  })

  it('SAVE_401: a 401 during save clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('SAVE_403: a 403 during save leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/werkzeuge/tools/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })
})
