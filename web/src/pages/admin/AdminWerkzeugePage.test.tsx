// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminWerkzeugePage } from './AdminWerkzeugePage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const TOOL_TYPES_URL = '/api/v1/admin/tool-types'
const TOOLS_URL = '/api/v1/admin/tools'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'
const QUALIFICATIONS_URL = '/api/v1/admin/qualifications'

function toolTypeFixture() {
  return {
    id: 'id-t1',
    name: 'Bohrmaschine',
    default_schedule_id: 'id-s1',
    required_qualification_id: 'id-q1',
    inspection_mode: 'checklist',
    checklist_items: [
      { id: 'ci-1', position: 0, label: 'Bohrfutter' },
      { id: 'ci-2', position: 1, label: 'Kabel' },
    ],
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

function toolItemFixture() {
  return {
    id: 'id-w1',
    name: 'Bohrmaschine-01',
    tool_type_id: 'id-t1',
    tool_type_name: 'Bohrmaschine',
    schedule_id: '',
    attributes: {},
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

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/werkzeuge']}>
        <Routes>
          <Route path="/admin/werkzeuge" element={<AdminWerkzeugePage />} />
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

const stubToolTypes = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubTools = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubQualifications = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === QUALIFICATIONS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

function baseRoutes() {
  return [
    stubToolTypes([toolTypeFixture()]),
    stubSchedules([scheduleFixture()]),
    stubQualifications(qualificationsFixture()),
  ]
}

describe('AdminWerkzeugePage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('TAB_TYPEN: shows the Typen tab with the active list + editor for a tool_types.manage holder', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes(baseRoutes())
    renderPage()

    expect(await screen.findByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('Checkliste · 2 Punkte')).toBeInTheDocument()
    expect(screen.getByText('Bohrfutter · Kabel')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Typen' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
    expect(screen.getByLabelText('Standard-Zeitplan')).toBeInTheDocument()
    expect(screen.getByLabelText('Erforderliche Qualifikation')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeInTheDocument()
  })

  it('TAB_WERKZEUGE_GATED: a tool_types.manage-only holder does NOT see the Werkzeuge tab (AD-6)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes(baseRoutes())
    renderPage()

    await screen.findByText('Bohrmaschine')
    expect(screen.getByRole('tab', { name: 'Typen' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Werkzeuge' })).not.toBeInTheDocument()
  })

  it('TOOLS_TAB: a tools.manage holder sees the Werkzeuge tab with the tool list + editor + type dropdown + override select', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
    ])
    renderPage()

    // The active tab is "Werkzeuge" (the only surface this holder reaches).
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    // The JOINed type name appears in the tool row; the same name also fills
    // the type-dropdown option, so at least one match is expected.
    expect(screen.getAllByText('Bohrmaschine').length).toBeGreaterThan(0)
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Typen' })).not.toBeInTheDocument()
    // Editor fields.
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
    expect(screen.getByLabelText('Gerätetyp')).toBeInTheDocument()
    // The override select offers the EXACT default label (AD-5).
    const overrideSelect = screen.getByLabelText('Zeitplan')
    expect(screen.getByRole('option', { name: 'Standard für diesen Typ' })).toBeInTheDocument()
    expect(overrideSelect).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeInTheDocument()
  })

  it('TOOLS_CREATE: creates a tool with name + type + override via POST /tools', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const fetchMock = stubFetchRoutes([
      stubTools([]),
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolItemFixture(), id: 'id-w2', name: 'Bohrmaschine-02', schedule_id: 'id-s1', message: 'Werkzeug gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('Name')
    // The type dropdown auto-selects the only available type.
    expect(screen.getByLabelText('Gerätetyp')).toHaveValue('id-t1')

    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    // Pick a schedule override (the default empty option inherits, AD-5).
    await user.selectOptions(screen.getByLabelText('Zeitplan'), 'id-s1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Werkzeug gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOLS_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.name).toBe('Bohrmaschine-02')
    expect(body.tool_type_id).toBe('id-t1')
    expect(body.schedule_id).toBe('id-s1')
  })

  it('TOOLS_DEFAULT_OVERRIDE: leaving the override on "Standard für diesen Typ" submits an empty schedule_id', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const fetchMock = stubFetchRoutes([
      stubTools([]),
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
    renderPage()

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Bohrmaschine-02')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Werkzeug gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOLS_URL && init?.method === 'POST',
    )
    const body = JSON.parse(String(postCall![1].body))
    expect(body.schedule_id).toBe('')
  })

  it('TOOLS_EDIT_CLEAR_OVERRIDE: editing a tool and clearing its override PUTs an empty schedule_id', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const overridden = { ...toolItemFixture(), schedule_id: 'id-s1' }
    const fetchMock = stubFetchRoutes([
      stubTools([overridden]),
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
        response: {
          ok: true,
          status: 200,
          body: { ...overridden, schedule_id: '', message: 'Werkzeug gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    expect(screen.getByText('Zeitplan überschrieben')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    // The override select starts on the stored override...
    expect(screen.getByLabelText('Zeitplan')).toHaveValue('id-s1')
    // ...and clearing it back to the default submits an empty schedule_id.
    await user.selectOptions(screen.getByLabelText('Zeitplan'), '')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    await screen.findByText('Werkzeug gespeichert.')
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.schedule_id).toBe('')
  })

  it('TOOLS_ARCHIVE: confirm + archive removes the tool from the active list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/id-w1/archive` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ...toolItemFixture(), message: 'Werkzeug archiviert.' } },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await userEvent.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(await screen.findByText('Werkzeug archiviert.')).toBeInTheDocument()
    expect(screen.queryByText('Bohrmaschine-01')).not.toBeInTheDocument()
    confirmSpy.mockRestore()
  })

  it('TOOLS_TYPE_CATALOG_403_DEGRADES: a 403 on the tool-type catalog must NOT eject — the tool list renders and save is disabled', async () => {
    // A holder of tools.manage WITHOUT tool_types.manage gets a 403 on the
    // type catalog fetch. The tool list (primary content) is authoritative and
    // renders; the catalog fetch degrades to an EMPTY type dropdown and the
    // page does NOT navigate away.
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const fetchMock = stubFetchRoutes([
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubSchedules([scheduleFixture()]),
    ])
    renderPage()

    // The tool list still renders (the page did NOT navigate to /dashboard).
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    // The degraded type catalog shows the empty-state option and disables save.
    expect(screen.getByRole('combobox', { name: 'Gerätetyp' })).toHaveTextContent('Keine Gerätetypen vorhanden')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(screen.getByText('Wähle einen Gerätetyp aus, um zu speichern.')).toBeInTheDocument()
    // No 403-eject call on the type-catalog request itself (the fetch was made,
    // but no adminForbiddenHandled navigation fired).
    expect(fetchMock).toHaveBeenCalled()
  })

  it('TOOLS_SCHEDULES_403_DEGRADES: a 403 on the schedules catalog must NOT eject — the tool list renders, save stays enabled, and the override select shows only the default option', async () => {
    // A holder of tools.manage WITHOUT schedules.manage gets a 403 on the
    // schedules fetch. The tool list (primary content) is authoritative and
    // renders; the override dropdown degrades to ONLY the inherit-default
    // option ("Standard für diesen Typ", AD-5) and the save is NOT disabled
    // (the override is optional — the inherit default is always valid).
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const fetchMock = stubFetchRoutes([
      stubTools([toolItemFixture()]),
      stubToolTypes([toolTypeFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    // The override select has the single valid empty-value option only.
    const overrideSelect = screen.getByLabelText('Zeitplan')
    const options = Array.from(overrideSelect.querySelectorAll('option'))
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toBe('Standard für diesen Typ')
    expect(overrideSelect).toHaveValue('')
    // Save is NOT disabled (the type catalog loaded fine; the override is
    // optional).
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeEnabled()
    // No 403-eject call on the schedules request itself (the fetch was made,
    // but no adminForbiddenHandled navigation fired).
    expect(fetchMock).toHaveBeenCalled()
  })

  it('TOOLS_EDIT_MISSING_TYPE_FALLBACK: editing a tool whose type is absent from the loaded catalog falls back to an empty type (save disabled)', async () => {
    // The freshly-loaded type catalog does NOT contain the tool's stored type
    // (e.g. a degraded/empty catalog). startEdit must fall back to '' so the
    // save turns off instead of showing a blank select with an enabled save.
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([toolItemFixture()]), // tool_type_id 'id-t1'
      stubToolTypes([]), // catalog does not contain id-t1
      stubSchedules([scheduleFixture()]),
    ])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await userEvent.click(screen.getByRole('button', { name: 'Bearbeiten' }))

    expect(screen.getByLabelText('Gerätetyp')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Änderungen speichern' })).toBeDisabled()
    expect(screen.getByText('Wähle einen Gerätetyp aus, um zu speichern.')).toBeInTheDocument()
  })

  it('TOOLS_EDIT_STALE_OVERRIDE_CLEARED: editing a tool whose override is no longer an active schedule clears the selection to the inherit default', async () => {
    // The tool's stored override (id-stale) was archived since it was saved and
    // is NOT in the loaded schedules catalog. startEdit must clear it to ''
    // ("Standard für diesen Typ") instead of resubmitting a stale override that
    // would 400 with an unclear cause.
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const stale = { ...toolItemFixture(), schedule_id: 'id-stale' }
    const fetchMock = stubFetchRoutes([
      stubTools([stale]),
      stubToolTypes([toolTypeFixture()]),
      stubSchedules([scheduleFixture()]), // only id-s1; id-stale is gone
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
        response: {
          ok: true,
          status: 200,
          body: { ...stale, schedule_id: '', message: 'Werkzeug gespeichert.' },
        },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await userEvent.click(screen.getByRole('button', { name: 'Bearbeiten' }))

    // The stale override is cleared back to the inherit default.
    expect(screen.getByLabelText('Zeitplan')).toHaveValue('')
    await userEvent.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('Werkzeug gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOLS_URL}/id-w1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.schedule_id).toBe('')
  })

  it('TOOLS_401: a 401 on the tool list clears auth and redirects to /login', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('TOOLS_403: a 403 on the tool list leaves the admin module back to the dashboard', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('EDITOR_CREATE: creates a checklist-mode type with the submitted ordered items', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...toolTypeFixture(), id: 'id-new', name: 'Schleifmaschine', message: 'Gerätetyp gespeichert.' },
        },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')

    await userEvent.type(screen.getByLabelText('Name'), 'Schleifmaschine')
    // Prüfmodus → Checkliste reveals the item editor.
    await userEvent.click(screen.getByLabelText('Checkliste'))
    const addInput = screen.getByPlaceholderText('Prüfpunkt hinzufügen')
    await userEvent.type(addInput, 'Scheibe')
    await userEvent.click(screen.getByRole('button', { name: 'Hinzufügen' }))
    await userEvent.type(addInput, 'Schutzhaube')
    await userEvent.click(screen.getByRole('button', { name: 'Hinzufügen' }))

    await userEvent.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Gerätetyp gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOL_TYPES_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.name).toBe('Schleifmaschine')
    expect(body.inspection_mode).toBe('checklist')
    expect(body.items).toEqual([{ label: 'Scheibe' }, { label: 'Schutzhaube' }])
  })

  it('EDITOR_PASS_FAIL: a pass_fail type submits an empty checklist', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { ...toolTypeFixture(), id: 'id-new', name: 'Hammer', message: 'Gerätetyp gespeichert.' } },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')

    await userEvent.type(screen.getByLabelText('Name'), 'Hammer')
    // pass_fail is the default → the checklist editor is NOT shown.
    expect(screen.queryByPlaceholderText('Prüfpunkt hinzufügen')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Speichern' }))

    await screen.findByText('Gerätetyp gespeichert.')
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === TOOL_TYPES_URL && init?.method === 'POST',
    )
    const body = JSON.parse(String(postCall![1].body))
    expect(body.inspection_mode).toBe('pass_fail')
    expect(body.items).toEqual([])
  })

  it('CHECKLIST_ORDERING: up/down reorders the checklist items before save', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...toolTypeFixture(), name: 'Bohrmaschine', message: 'Gerätetyp gespeichert.' } },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    await userEvent.click(screen.getByLabelText('Checkliste'))

    // The fixture items render in the editor.
    expect(screen.getByText('Bohrfutter')).toBeInTheDocument()
    expect(screen.getByText('Kabel')).toBeInTheDocument()

    // Move "Kabel" up above "Bohrfutter" → [Kabel, Bohrfutter].
    await userEvent.click(screen.getByRole('button', { name: 'Nach oben: Kabel' }))

    await userEvent.click(screen.getByRole('button', { name: 'Änderungen speichern' }))
    await screen.findByText('Gerätetyp gespeichert.')
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
    )
    const body = JSON.parse(String(putCall![1].body))
    expect(body.items).toEqual([{ label: 'Kabel' }, { label: 'Bohrfutter' }])
  })

  it('CHECKLIST_REMOVE: an item can be removed before save', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...toolTypeFixture(), name: 'Bohrmaschine', message: 'Gerätetyp gespeichert.' } },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    await userEvent.click(screen.getByLabelText('Checkliste'))
    await userEvent.click(screen.getByRole('button', { name: 'Entfernen: Bohrfutter' }))

    await userEvent.click(screen.getByRole('button', { name: 'Änderungen speichern' }))
    await screen.findByText('Gerätetyp gespeichert.')
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${TOOL_TYPES_URL}/id-t1` && init?.method === 'PUT',
    )
    const body = JSON.parse(String(putCall![1].body))
    expect(body.items).toEqual([{ label: 'Kabel' }])
  })

  it('ARCHIVE: confirm + archive removes the row from the active list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOL_TYPES_URL}/id-t1/archive` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { ...toolTypeFixture(), message: 'Gerätetyp archiviert.' } },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(await screen.findByText('Gerätetyp archiviert.')).toBeInTheDocument()
    expect(screen.queryByText('Bohrmaschine')).not.toBeInTheDocument()
    confirmSpy.mockRestore()
  })

  it('ARCHIVE_CANCEL: declining the confirm does not archive', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = stubFetchRoutes(baseRoutes())
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    const archiveCall = fetchMock.mock.calls.find(([url]) => url.includes('/archive'))
    expect(archiveCall).toBeUndefined()
    confirmSpy.mockRestore()
  })

  it('FEEDBACK_400: a duplicate-name error renders inline', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: {
          ok: false,
          status: 400,
          body: { error: { code: 'invalid_request', message: 'Es gibt bereits einen Gerätetyp mit diesem Namen.' } },
        },
      },
    ])
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.type(screen.getByLabelText('Name'), 'Nochmal')
    await userEvent.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Es gibt bereits einen Gerätetyp mit diesem Namen.')
  })

  it('ERROR_401: a 401 clears auth and redirects to /login', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      {
        matcher: (url: string) => url === TOOL_TYPES_URL,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('ERROR_403: a 403 leaves the admin module back to the dashboard', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      {
        matcher: (url: string) => url === TOOL_TYPES_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('CATALOG_403_DEGRADES: a 403 on the schedules catalog must NOT eject — the type list still renders (Fix 1)', async () => {
    // A Schirrmeister/Führende holder of tool_types.manage WITHOUT
    // schedules.manage gets a 403 on the schedules fetch. The tool-type list
    // (primary content) is authoritative and renders; the catalog fetch
    // degrades to an empty dropdown and the page does NOT navigate away.
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubQualifications(qualificationsFixture()),
    ])
    renderPage()

    // The type list still renders (the page did NOT navigate to /dashboard).
    expect(await screen.findByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('Checkliste · 2 Punkte')).toBeInTheDocument()
    // No navigation happened: the Werkzeuge page is still mounted.
    expect(screen.getByRole('tab', { name: 'Typen' })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    // The degraded schedule catalog shows the empty-state option.
    expect(screen.getByRole('combobox', { name: 'Standard-Zeitplan' })).toHaveTextContent('Keine Zeitpläne vorhanden')
    // No 403-eject call on the schedules request itself (the fetch was made,
    // but no adminForbiddenHandled navigation fired).
    expect(fetchMock).toHaveBeenCalled()
  })

  it('SAVE_DISABLED_EMPTY_CATALOGS: save is disabled with a hint when no schedule is selectable (Fix 9)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      stubToolTypes([]),
      stubSchedules([]),
      {
        matcher: (url: string, init?: RequestInit) => url === QUALIFICATIONS_URL && !init?.method,
        response: { ok: true, status: 200, body: { qualifications: [], users: [] } },
      },
    ])
    renderPage()

    // Empty schedule catalog → no auto-selection → the save button is disabled.
    // The required qualification is OPTIONAL (000023) and starts unselected.
    expect(await screen.findByLabelText('Name')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(
      screen.getByText('Wähle einen Standard-Zeitplan aus, um zu speichern.'),
    ).toBeInTheDocument()
  })

  it('QUALIFICATION_OPTIONAL: a fresh create form starts with "Keine Qualifikation erforderlich" and submits an empty qualification', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const fetchMock = stubFetchRoutes([
      stubToolTypes([]),
      ...baseRoutes(),
      {
        matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { ...toolTypeFixture(), id: 'id-new', name: 'Säge', message: 'Gerätetyp gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('Name')
    // The qualification select offers the no-qualification option.
    const qualSelect = screen.getByLabelText('Erforderliche Qualifikation')
    expect(screen.getByRole('option', { name: 'Keine Qualifikation erforderlich' })).toBeInTheDocument()
    expect(qualSelect).toHaveValue('')

    await user.type(screen.getByLabelText('Name'), 'Säge')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Gerätetyp gespeichert.')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === TOOL_TYPES_URL && init?.method === 'POST')
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.required_qualification_id).toBe('')
  })
})