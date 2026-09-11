// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminWerkzeugePage } from './AdminWerkzeugePage.tsx'
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

const stubToolTypes = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOL_TYPES_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubSchedules = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && !init?.method,
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

  it('TAB_WERKZEUGE: switching to the Werkzeuge tab shows the empty state', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes(baseRoutes())
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('tab', { name: 'Werkzeuge' }))
    expect(screen.getByText('Keine Werkzeuge vorhanden')).toBeInTheDocument()
    expect(screen.getByText('Die Verwaltung einzelner Werkzeuge folgt in einem späteren Schritt.')).toBeInTheDocument()
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

  it('SAVE_DISABLED_EMPTY_CATALOGS: save is disabled with a hint when no schedule/qualification is selectable (Fix 9)', async () => {
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

    // Empty catalogs → no auto-selection → the save button is disabled.
    expect(await screen.findByLabelText('Name')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(
      screen.getByText('Wähle einen Standard-Zeitplan und eine erforderliche Qualifikation aus, um zu speichern.'),
    ).toBeInTheDocument()
  })
})