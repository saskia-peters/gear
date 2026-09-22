// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminWerkzeugePage } from './AdminWerkzeugePage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'
import { TOOL_IMPORT_TEMPLATE_HEADER, TOOL_IMPORT_TEMPLATE_ROW } from '../../auth/tools.ts'

const TOOL_TYPES_URL = '/api/v1/admin/tool-types'
const TOOLS_URL = '/api/v1/admin/tools'

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

function toolItemFixture() {
  return {
    id: 'id-w1',
    name: 'Bohrmaschine-01',
    tool_type_id: 'id-t1',
    tool_type_name: 'Bohrmaschine',
    schedule_id: '',
    inventory_number: 'GEAR000001',
    attributes: {},
    created_at: '2026-09-10T10:00:00Z',
    updated_at: '2026-09-10T10:00:00Z',
  }
}

function renderPage(initialEntry: string | { pathname: string; state?: unknown } = '/admin/werkzeuge') {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/admin/werkzeuge" element={<AdminWerkzeugePage />} />
          <Route path="/admin/werkzeuge/typen/neu" element={<div>TypNeuSeite</div>} />
          <Route path="/admin/werkzeuge/typen/:id" element={<div>TypEditSeite</div>} />
          <Route path="/admin/werkzeuge/tools/neu" element={<div>WerkzeugNeuSeite</div>} />
          <Route path="/admin/werkzeuge/tools/:id" element={<div>WerkzeugEditSeite</div>} />
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

const stubTools = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === TOOLS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

// tableColumn reads the given <td> index (0-based) of every tbody row of the
// catalogue table, in render order.
function tableColumn(tableLabel: string, index: number): string[] {
  const table = screen.getByRole('table', { name: tableLabel })
  return Array.from(table.querySelectorAll('tbody tr')).map(
    (tr) => tr.querySelectorAll('td')[index]?.textContent?.trim() ?? '',
  )
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

  it('TAB_TYPEN: shows the compact one-line sortable list + create button, NO inline form', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    renderPage()

    expect(await screen.findByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Typen' })).toBeInTheDocument()
    // One line: name · mode ("Checkliste · N Punkte") · actions — NO checklist
    // label dump, NO inline create/edit form.
    expect(screen.getByText('Checkliste · 2 Punkte')).toBeInTheDocument()
    expect(screen.queryByText('Bohrfutter · Kabel')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Gerätetyp' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Bearbeiten' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Archivieren' })).toBeInTheDocument()
    // Sortable headers carry the German accessible names (the Name column is
    // the default sort, so it shows the next-action hint like UserTable).
    expect(screen.getByRole('button', { name: 'Sortieren nach Name (absteigend)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sortieren nach Modus' })).toBeInTheDocument()
    // No inline form: no submit button, no editor fields.
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Standard-Zeitplan')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Erforderliche Qualifikation')).not.toBeInTheDocument()
  })

  it('TAB_WERKZEUGE_GATED: a tool_types.manage-only holder does NOT see the Werkzeuge tab (AD-6)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    renderPage()

    await screen.findByText('Bohrmaschine')
    expect(screen.getByRole('tab', { name: 'Typen' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Werkzeuge' })).not.toBeInTheDocument()
  })

  it('NO_SURFACE_CODE: a holder with NEITHER tool_types.manage NOR tools.manage/tool.edit sees no tabs and the EmptyState fallback (finding 15)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['dashboard.view']))
    const fetchMock = stubFetchRoutes([])
    renderPage()

    expect(screen.queryByRole('tab', { name: 'Typen' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Werkzeuge' })).not.toBeInTheDocument()
    expect(
      screen.getByText('Werkzeuge und Gerätetypen sind für dein Konto nicht verfügbar.'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('SORT_TYPEN_NAME: clicking the Name column toggles asc/desc ordering (German locale)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      stubToolTypes([
        { ...toolTypeFixture(), id: 'id-b', name: 'Bohrmaschine' },
        { ...toolTypeFixture(), id: 'id-a', name: 'Axt', inspection_mode: 'pass_fail', checklist_items: [] },
      ]),
    ])
    const user = userEvent.setup()
    renderPage()

    // Default sort: Name asc → Axt before Bohrmaschine.
    expect(await screen.findByText('Axt')).toBeInTheDocument()
    expect(tableColumn('Gerätetypen', 0)).toEqual(['Axt', 'Bohrmaschine'])

    // Toggle to desc.
    await user.click(screen.getByRole('button', { name: /Sortieren nach Name/ }))
    expect(tableColumn('Gerätetypen', 0)).toEqual(['Bohrmaschine', 'Axt'])

    // Toggle back to asc.
    await user.click(screen.getByRole('button', { name: /Sortieren nach Name/ }))
    expect(tableColumn('Gerätetypen', 0)).toEqual(['Axt', 'Bohrmaschine'])
  })

  it('SORT_TYPEN_MODUS: clicking the Modus column sorts by the German mode label', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([
      stubToolTypes([
        { ...toolTypeFixture(), id: 'id-pf', name: 'Hammer', inspection_mode: 'pass_fail', checklist_items: [] },
        { ...toolTypeFixture(), id: 'id-cl', name: 'Bohrmaschine' },
      ]),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Hammer')
    // "Checkliste · 2 Punkte" sorts before "Pass/Fail" (localeCompare de).
    await user.click(screen.getByRole('button', { name: 'Sortieren nach Modus' }))
    expect(tableColumn('Gerätetypen', 0)).toEqual(['Bohrmaschine', 'Hammer'])
  })

  it('EMPTY_TYPEN: an empty list shows the German empty note while the create button stays visible', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([])])
    renderPage()

    expect(await screen.findByText('Keine Gerätetypen vorhanden. Lege den ersten Typ an.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Gerätetyp' })).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Gerätetypen' })).not.toBeInTheDocument()
  })

  it('CREATE_NAV_TYPEN: "Neuer Gerätetyp" navigates to the dedicated create page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine')
    await user.click(screen.getByRole('button', { name: 'Neuer Gerätetyp' }))
    expect(await screen.findByText('TypNeuSeite')).toBeInTheDocument()
  })

  it('EDIT_NAV_TYPEN: "Bearbeiten" navigates to the dedicated :id edit page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(await screen.findByText('TypEditSeite')).toBeInTheDocument()
  })

  it('ARCHIVE_TYPEN: confirm + archive removes the row from the active list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    stubFetchRoutes([
      stubToolTypes([toolTypeFixture()]),
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

  it('ARCHIVE_CANCEL_TYPEN: declining the confirm does not archive', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    renderPage()

    await screen.findByText('Bohrmaschine')
    await userEvent.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    const archiveCall = fetchMock.mock.calls.find(([url]) => url.includes('/archive'))
    expect(archiveCall).toBeUndefined()
    confirmSpy.mockRestore()
  })

  it('ERROR_401_TYPEN: a 401 on the type list clears auth and redirects to /login', async () => {
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

  it('ERROR_403_TYPEN: a 403 on the type list leaves the admin module back to the dashboard', async () => {
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

  it('TOOLS_TAB: a tools.manage holder sees the one-line tool list + create button, NO inline form', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    renderPage()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Typen' })).not.toBeInTheDocument()
    // One line per row: name · type · inventory · actions.
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neues Werkzeug' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sortieren nach Typ' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sortieren nach Gerätenummer' })).toBeInTheDocument()
    // No inline form.
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Gerätetyp')).not.toBeInTheDocument()
  })

  it('TOOLS_SORT: sorting by Typ re-orders the one-line rows', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([
        { ...toolItemFixture(), id: 'id-w1', name: 'Bohrmaschine-01', tool_type_name: 'Bohrmaschine' },
        { ...toolItemFixture(), id: 'id-w2', name: 'Axt-01', tool_type_name: 'Axt', inventory_number: 'GEAR000002' },
      ]),
    ])
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    // Default: Name asc → Axt-01 before Bohrmaschine-01.
    expect(tableColumn('Werkzeuge', 0)).toEqual(['Axt-01', 'Bohrmaschine-01'])
    // Sort by Typ asc → Axt before Bohrmaschine.
    await user.click(screen.getByRole('button', { name: 'Sortieren nach Typ' }))
    expect(tableColumn('Werkzeuge', 0)).toEqual(['Axt-01', 'Bohrmaschine-01'])
    // Toggle Typ desc → Bohrmaschine before Axt.
    await user.click(screen.getByRole('button', { name: 'Sortieren nach Typ (absteigend)' }))
    expect(tableColumn('Werkzeuge', 0)).toEqual(['Bohrmaschine-01', 'Axt-01'])
  })

  it('TOOLS_CREATE_NAV: "Neues Werkzeug" navigates to the dedicated create page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.click(screen.getByRole('button', { name: 'Neues Werkzeug' }))
    expect(await screen.findByText('WerkzeugNeuSeite')).toBeInTheDocument()
  })

  it('TOOLS_EDIT_NAV: "Bearbeiten" navigates to the dedicated :id edit page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(await screen.findByText('WerkzeugEditSeite')).toBeInTheDocument()
  })

  it('TOOLS_ARCHIVE: confirm + archive removes the tool from the active list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
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

  it('TOOLS_ARCHIVE_CANCEL: declining the confirm does not archive', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = stubFetchRoutes([stubTools([toolItemFixture()])])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await userEvent.click(screen.getByRole('button', { name: 'Archivieren' }))

    expect(screen.getByText('Bohrmaschine-01')).toBeInTheDocument()
    const archiveCall = fetchMock.mock.calls.find(([url]) => url.includes('/archive'))
    expect(archiveCall).toBeUndefined()
    confirmSpy.mockRestore()
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

  it('TOOLS_403_TOOL_EDIT: a 403 on the tool list for a tool.edit holder still leaves the module (defensive)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.edit']))
    stubFetchRoutes([
      {
        matcher: (url: string) => url === TOOLS_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('TOOLS_EDIT_ONLY: a tool.edit-only holder sees the list but NO create button and NO archive; Bearbeiten navigates to the edit page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.edit']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    const user = userEvent.setup()
    renderPage()

    // The tool.edit-only holder reaches the Werkzeuge tab (any-of gate) and
    // sees the list with the inventory number.
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    // NO create button (tools.manage-only, Story 4-3b) and NO archive buttons.
    expect(screen.queryByRole('button', { name: 'Neues Werkzeug' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Archivieren' })).not.toBeInTheDocument()

    // Bearbeiten navigates to the dedicated edit page.
    await user.click(screen.getByRole('button', { name: 'Bearbeiten' }))
    expect(await screen.findByText('WerkzeugEditSeite')).toBeInTheDocument()
  })

  it('NOTICE_TAB_RETURN: a return with { tab, message } restores the originating tab and shows the success notice once', async () => {
    // A holder of both surface codes returns from the Werkzeuge editor with the
    // Werkzeuge tab + the server confirmation (Spec 4-6 review 1/4).
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage', 'tools.manage']))
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      stubToolTypes([toolTypeFixture()]),
    ])
    renderPage({ pathname: '/admin/werkzeuge', state: { tab: 'werkzeuge', message: 'Werkzeug gespeichert.' } })

    // The carried tab wins over the permission-default 'typen'.
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Werkzeuge' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Typen' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('status')).toHaveTextContent('Werkzeug gespeichert.')
  })

  it('NOTICE_CLEARED_ON_REMOUNT: the carried success message does not reappear on a later remount', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool_types.manage']))
    stubFetchRoutes([stubToolTypes([toolTypeFixture()])])
    const { unmount } = renderPage({ pathname: '/admin/werkzeuge', state: { tab: 'typen', message: 'Gerätetyp gespeichert.' } })

    expect(await screen.findByRole('status')).toHaveTextContent('Gerätetyp gespeichert.')
    // Unmount + remount WITHOUT carried state: the notice must NOT reappear.
    unmount()
    cleanup()
    renderPage()
    expect(await screen.findByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.queryByText('Gerätetyp gespeichert.')).not.toBeInTheDocument()
  })

  it('TOOLS_OVERRIDE_BADGE: a tool with a schedule override shows the "Zeitplan überschrieben" meta', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([{ ...toolItemFixture(), schedule_id: 'id-s1' }]),
    ])
    renderPage()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Zeitplan überschrieben')).toBeInTheDocument()
  })

  it('IMPORT_UI_TOOLS_MANAGE: a tools.manage holder sees CSV import + template buttons and the format guide', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    expect(screen.getByRole('button', { name: 'CSV importieren' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Vorlage herunterladen' })).toBeInTheDocument()
    expect(screen.getByText(/CSV-Format: name \(Pflicht\)/)).toBeInTheDocument()
  })

  it('IMPORT_UI_TOOL_EDIT_ONLY: a tool.edit-only holder sees NO import controls (AD-6, tools.manage-only)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.edit']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    expect(screen.queryByRole('button', { name: 'CSV importieren' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Vorlage herunterladen' })).not.toBeInTheDocument()
    expect(screen.queryByText(/CSV-Format: name \(Pflicht\)/)).not.toBeInTheDocument()
  })

  it('IMPORT_UPLOAD_SUCCESS: uploading a CSV imports and RELOADS the tool list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const fetchMock = stubFetchRoutes([
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/import` && init?.method === 'POST',
        response: { ok: true, status: 200, body: { imported: 1, errors: [] } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    const listCallsBefore = fetchMock.mock.calls.filter(([url, init]) => url === TOOLS_URL && !init?.method).length
    expect(listCallsBefore).toBe(1)

    await user.upload(
      screen.getByLabelText('CSV-Datei für den Import wählen'),
      new File(['name,tool_type\nNeu,Bohrmaschine\n'], 'tools.csv', { type: 'text/csv' }),
    )

    expect(await screen.findByText('1 Werkzeuge importiert, 0 Fehler.')).toBeInTheDocument()
    // The list is reloaded after a successful import.
    const listCalls = fetchMock.mock.calls.filter(([url, init]) => url === TOOLS_URL && !init?.method).length
    expect(listCalls).toBe(2)
  })

  it('IMPORT_ERRORS: a mixed result lists per-row errors and offers error-report + retry', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/import` && init?.method === 'POST',
        response: {
          ok: true,
          status: 200,
          body: { imported: 1, errors: [{ row: 3, reason: "Tool Type 'GibtEsNicht' nicht gefunden" }] },
        },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.upload(
      screen.getByLabelText('CSV-Datei für den Import wählen'),
      new File(['name,tool_type\nA,B\nC,X\n'], 'tools.csv', { type: 'text/csv' }),
    )

    expect(await screen.findByText('1 Werkzeuge importiert, 1 Fehler.')).toBeInTheDocument()
    expect(screen.getByText("Zeile 3: Tool Type 'GibtEsNicht' nicht gefunden")).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Fehlerreport herunterladen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Erneut importieren' })).toBeInTheDocument()

    // "Erneut importieren" clears the result so the user can pick a new file.
    await user.click(screen.getByRole('button', { name: 'Erneut importieren' }))
    expect(screen.queryByText('1 Werkzeuge importiert, 1 Fehler.')).not.toBeInTheDocument()
  })

  it('IMPORT_TEMPLATE_DOWNLOAD: "Vorlage herunterladen" downloads the exact template CSV blob with the right filename', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([stubTools([toolItemFixture()])])
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => 'blob:test-template')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
    // Capture the anchor's download attributes so the filename is observable.
    let clickedDownload: string | null = null
    let clickedHref: string | null = null
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clickedDownload = this.getAttribute('download')
      clickedHref = this.href
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.click(screen.getByRole('button', { name: 'Vorlage herunterladen' }))

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    // Observe the Blob content: the EXACT header + example row the parser
    // accepts (the template and the parser can never drift).
    const blob = createObjectURL.mock.calls[0]![0]
    expect(await blob.text()).toBe(`${TOOL_IMPORT_TEMPLATE_HEADER}\n${TOOL_IMPORT_TEMPLATE_ROW}\n`)
    // The anchor download carries the template filename.
    expect(clickedDownload).toBe('werkzeuge-import-vorlage.csv')
    expect(clickedHref).toContain('blob:test-template')
    clickSpy.mockRestore()
  })

  it('IMPORT_ERROR_REPORT_DOWNLOAD: "Fehlerreport herunterladen" downloads the JSON result blob', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    const errorReport = { imported: 0, errors: [{ row: 2, reason: 'Zeitplan fehlt.' }] }
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/import` && init?.method === 'POST',
        response: { ok: true, status: 200, body: errorReport },
      },
    ])
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => 'blob:test-error-report')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
    // Capture the anchor's download attributes so the filename is observable.
    let clickedDownload: string | null = null
    let clickedHref: string | null = null
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clickedDownload = this.getAttribute('download')
      clickedHref = this.href
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.upload(
      screen.getByLabelText('CSV-Datei für den Import wählen'),
      new File(['name,tool_type\nA,B\n'], 'tools.csv', { type: 'text/csv' }),
    )
    await screen.findByText('0 Werkzeuge importiert, 1 Fehler.')
    await user.click(screen.getByRole('button', { name: 'Fehlerreport herunterladen' }))

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    // Observe the Blob content: the exact JSON result (client-side report, no
    // server-side stored file).
    const blob = createObjectURL.mock.calls[0]![0]
    expect(await blob.text()).toBe(JSON.stringify(errorReport, null, 2))
    expect(clickedDownload).toBe('werkzeug-import-fehler.json')
    expect(clickedHref).toContain('blob:test-error-report')
    clickSpy.mockRestore()
  })

  it('IMPORT_HARD_ERROR: a 400 German reason surfaces inline (missing header)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    stubFetchRoutes([
      stubTools([toolItemFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${TOOLS_URL}/import` && init?.method === 'POST',
        response: {
          ok: false,
          status: 400,
          body: { error: { code: 'invalid_request', message: "Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten." } },
        },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.upload(
      screen.getByLabelText('CSV-Datei für den Import wählen'),
      new File(['foo,bar\nx,y\n'], 'tools.csv', { type: 'text/csv' }),
    )

    expect(await screen.findByText("Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten.")).toBeInTheDocument()
    expect(screen.queryByText(/Werkzeuge importiert/)).not.toBeInTheDocument()
  })

  it('IMPORT_RELOAD_FAILURE: a reload failure after a successful import shows success + a stale-list note, not an import error', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['tools.manage']))
    // The initial list load succeeds; the POST-IMPORT reload (2nd list GET)
    // fails — the import itself already persisted, so the success must stay
    // visible and the reload failure surfaces as a non-fatal stale-list note.
    let listCalls = 0
    const fetchMock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === `${TOOLS_URL}/import` && init?.method === 'POST') {
        return { ok: true, status: 200, json: async () => ({ imported: 1, errors: [] }) }
      }
      if (url === TOOLS_URL && !init?.method) {
        listCalls++
        if (listCalls > 1) {
          return {
            ok: false,
            status: 500,
            json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
          }
        }
        return { ok: true, status: 200, json: async () => [toolItemFixture()] }
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Bohrmaschine-01')
    await user.upload(
      screen.getByLabelText('CSV-Datei für den Import wählen'),
      new File(['name,tool_type\nNeu,Bohrmaschine\n'], 'tools.csv', { type: 'text/csv' }),
    )

    // The import SUCCESS is still shown (imported count), never an import error.
    expect(await screen.findByText('1 Werkzeuge importiert, 0 Fehler.')).toBeInTheDocument()
    expect(screen.queryByText(/Der CSV-Import ist fehlgeschlagen/)).not.toBeInTheDocument()
    // The reload failure is a non-fatal stale-list note (the existing feedback
    // pattern, German).
    expect(
      screen.getByText('Die Liste konnte nach dem Import nicht aktualisiert werden. Bitte lade die Seite neu.'),
    ).toBeInTheDocument()
    expect(listCalls).toBe(2)
  })
})
