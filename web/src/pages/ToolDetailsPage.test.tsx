// @vitest-environment jsdom
import { render, screen, cleanup, act } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route, useNavigate } from 'react-router-dom'
import { ToolDetailsPage } from './ToolDetailsPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'
import type { ToolHistory } from '../auth/tools.ts'

type ToolDetailsEntry = string | { pathname: string; state?: Record<string, unknown> }

function renderPage(initialEntry: ToolDetailsEntry) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/tools/:toolId" element={<ToolDetailsPage />} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

// dashboardListFixture is the GET /api/v1/tools payload (Story 6.1) the header
// deep-link refetch resolves the tool from.
function dashboardListFixture() {
  return [
    {
      id: 'id-w1',
      name: 'Bohrmaschine-01',
      tool_type_id: 'id-t1',
      tool_type_name: 'Bohrmaschine',
      inventory_number: 'GEAR000001',
      status: { status: 'green', next_due: '2027-09-14T10:00:00Z' },
    },
  ]
}

// historyFixture is the GET /api/v1/tools/{id}/history payload (Story 6.3): a
// NEWER checklist inspection with its per-item snapshot results + an OLDER
// pass_fail + one reinstatement, all by the known inspector.
function historyFixture(): ToolHistory {
  return {
    inspections: [
      {
        id: 'insp-new',
        inspector_id: 'user-1',
        inspector_name: 'Anna Muster',
        mode: 'checklist',
        overall_result: 'fail',
        notes: 'Bohrfutter locker',
        submitted_at: '2026-09-15T10:00:00Z',
        items: [
          { id: 'it-1', item_id: 'item-1', label: 'Kabel', position: 0, result: 'pass' },
          { id: 'it-2', item_id: 'item-2', label: 'Bohrfutter', position: 1, result: 'fail' },
        ],
      },
      {
        id: 'insp-old',
        inspector_id: 'user-1',
        inspector_name: 'Anna Muster',
        mode: 'pass_fail',
        overall_result: 'pass',
        notes: 'Alles ok',
        submitted_at: '2026-09-01T09:00:00Z',
        items: [],
      },
    ],
    reinstatements: [
      {
        id: 'rein-1',
        actor_id: 'user-1',
        actor_name: 'Anna Muster',
        reason: 'Ersatzteil eingetroffen',
        created_at: '2026-09-16T08:00:00Z',
      },
    ],
  }
}

function stubFetch(response: unknown) {
  const mock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', mock)
  return mock
}

// setPermissions seeds the cached resolved permission set (the SPA's
// hasPermission source, loaded server-side by RequireAuth).
function setPermissions(perms: string[]) {
  localStorage.setItem('gear.permissions', JSON.stringify(perms))
}

const HEADER_STATE = {
  pathname: '/tools/id-w1',
  state: {
    tool_name: 'Bohrmaschine-01',
    inventory_number: 'GEAR000001',
    tool_type_name: 'Bohrmaschine',
    status: { status: 'green', next_due: '2027-09-14T10:00:00Z' },
  },
}

describe('ToolDetailsPage header (Story 6.3)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    setPermissions([])
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('HEADER_STATE: renders the header (Werkzeug, Gerätenummer, Gerätetyp, status chip) from router state without re-fetching', async () => {
    const mock = vi.fn()
    vi.stubGlobal('fetch', mock)
    renderPage(HEADER_STATE)

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('Gerätetyp')).toBeInTheDocument()
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    // The green status renders as its German label chip.
    expect(screen.getByText('Einsatzbereit')).toBeInTheDocument()
    // Router state is authoritative: no server call at all.
    expect(mock).not.toHaveBeenCalled()
  })

  it('HEADER_DEEPLINK: without router state it re-resolves the header via listDashboardTools find-by-id', async () => {
    const mock = stubFetch({ ok: true, status: 200, json: async () => dashboardListFixture() })
    renderPage('/tools/id-w1')

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('Einsatzbereit')).toBeInTheDocument()
    const [url] = mock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/tools')
  })

  it('HEADER_DEEPLINK_NOT_FOUND: a tool missing from the dashboard list shows the German not-found note', async () => {
    stubFetch({ ok: true, status: 200, json: async () => [] })
    renderPage('/tools/id-missing')

    expect(await screen.findByRole('alert')).toHaveTextContent('Das Werkzeug wurde nicht gefunden.')
  })

  it('HEADER_401: a 401 on the deep-link refetch clears auth state and redirects to /login', async () => {
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderPage('/tools/id-w1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('HEADER_ERROR: a failed deep-link refetch shows an inline error state (no crash, no raw id leak)', async () => {
    stubFetch({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    })
    renderPage('/tools/id-w1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
  })
})

describe('ToolDetailsPage history (Story 6.3, FR-18)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('HISTORY_RENDER: a holder sees the reverse-chronological inspections (inspector, date, outcome, notes, mode, per-item results) and the reinstatements', async () => {
    setPermissions(['inspection.history.view'])
    const mock = stubFetch({ ok: true, status: 200, json: async () => historyFixture() })
    renderPage(HEADER_STATE)

    // The history fetch hits the real endpoint for this tool.
    expect(await screen.findByText('Bohrfutter locker')).toBeInTheDocument()
    const [url] = mock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/tools/id-w1/history')

    // Inspector display names (server-resolved) render.
    expect(screen.getAllByText('Anna Muster').length).toBeGreaterThanOrEqual(3)

    // Newest checklist inspection: mode + outcome + notes + per-item results.
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    expect(screen.getByText('Bohrfutter locker')).toBeInTheDocument()
    expect(screen.getByText('Kabel')).toBeInTheDocument()
    expect(screen.getByText('Bohrfutter')).toBeInTheDocument()
    // BESTANDEN appears for the Kabel item + the older pass outcome; NICHT
    // BESTANDEN for the Bohrfutter item + the newest fail outcome.
    expect(screen.getAllByText('BESTANDEN').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('NICHT BESTANDEN').length).toBeGreaterThanOrEqual(2)
    // The timestamps render in the German locale (newest first).
    expect(screen.getByText(new Date('2026-09-15T10:00:00Z').toLocaleString('de-DE'))).toBeInTheDocument()
    expect(screen.getByText(new Date('2026-09-01T09:00:00Z').toLocaleString('de-DE'))).toBeInTheDocument()

    // Older pass_fail inspection: mode + notes.
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
    expect(screen.getByText('Alles ok')).toBeInTheDocument()

    // Reinstatement: actor + reason + date.
    expect(screen.getByText('Ersatzteil eingetroffen')).toBeInTheDocument()
    expect(screen.getByText('Durchgeführt von')).toBeInTheDocument()
    expect(screen.getByText(new Date('2026-09-16T08:00:00Z').toLocaleString('de-DE'))).toBeInTheDocument()
  })

  it('HISTORY_EMPTY: a tool with no records shows the German empty notes for both lists', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({ ok: true, status: 200, json: async () => ({ inspections: [], reinstatements: [] }) })
    renderPage(HEADER_STATE)

    expect(await screen.findByText('Keine Prüfungen vorhanden.')).toBeInTheDocument()
    expect(screen.getByText('Keine Wiederherstellungen vorhanden.')).toBeInTheDocument()
  })

  it('HISTORY_NOPERMISSION: a non-holder sees the German no-permission note in BOTH history sections and the history is NEVER fetched', async () => {
    setPermissions([])
    const mock = stubFetch({ ok: true, status: 200, json: async () => historyFixture() })
    renderPage(HEADER_STATE)

    // Both the Prüfhistorie AND the Wiederherstellungen sections carry the
    // note (a bare heading would leak nothing but read as broken UX).
    expect(
      (await screen.findAllByText('Du hast keine Berechtigung, die Prüfhistorie anzuzeigen.')).length,
    ).toBe(2)
    // The header renders (from router state) but the history fetch is skipped —
    // no doomed 403 round-trip for a non-holder.
    expect(screen.getByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(mock).not.toHaveBeenCalled()
  })

  it('HISTORY_401: a 401 on the history fetch clears auth state and redirects to /login', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderPage(HEADER_STATE)

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('HISTORY_403: a server-side 403 (the permission cache went stale — the SERVER is the gate) shows the German message inline in both sections', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Keine Berechtigung.' } }),
    })
    renderPage(HEADER_STATE)

    const alerts = await screen.findAllByRole('alert')
    expect(alerts.length).toBe(2)
    for (const alert of alerts) {
      expect(alert).toHaveTextContent('Keine Berechtigung.')
    }
    // The header still renders (the details page is open to all).
    expect(screen.getByText('Bohrmaschine-01')).toBeInTheDocument()
  })

  it('HISTORY_404: a server-side 404 on the history fetch shows the German not-found message inline and the page stays', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: false,
      status: 404,
      json: async () => ({ error: { code: 'not_found', message: 'Das Werkzeug wurde nicht gefunden.' } }),
    })
    renderPage(HEADER_STATE)

    const alerts = await screen.findAllByRole('alert')
    expect(alerts.length).toBe(2)
    for (const alert of alerts) {
      expect(alert).toHaveTextContent('Das Werkzeug wurde nicht gefunden.')
    }
    // No header leak: the header still renders from router state and the page
    // never navigates away.
    expect(screen.getByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.queryByText('Anmeldung')).not.toBeInTheDocument()
  })

  it('HISTORY_DELETED_USER: a missing inspector/actor account renders the literal "Deleted User"', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        inspections: [
          {
            id: 'insp-1',
            inspector_id: 'u-gone',
            inspector_name: 'Deleted User',
            mode: 'pass_fail',
            overall_result: 'pass',
            notes: '',
            submitted_at: '2026-09-15T10:00:00Z',
            items: [],
          },
        ],
        reinstatements: [
          {
            id: 'rein-1',
            actor_id: 'u-gone',
            actor_name: 'Deleted User',
            reason: 'Ersatzteil eingetroffen',
            created_at: '2026-09-16T08:00:00Z',
          },
        ],
      }),
    })
    renderPage(HEADER_STATE)

    // The literal renders for BOTH the inspection inspector and the
    // reinstatement actor (Story 3.4 forward-compat).
    expect((await screen.findAllByText('Deleted User')).length).toBe(2)
  })

  it('HISTORY_MALFORMED: a malformed history body (null lists/elements/items) never crashes the render', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        inspections: [
          null,
          {
            id: 'insp-1',
            inspector_id: 'u-1',
            inspector_name: 'Anna Muster',
            mode: 'checklist',
            overall_result: 'pass',
            notes: '',
            submitted_at: '2026-09-15T10:00:00Z',
            items: null,
          },
          {
            id: 'insp-2',
            inspector_id: 'u-1',
            inspector_name: 'Bernd Beispiel',
            mode: 'checklist',
            overall_result: 'pass',
            notes: '',
            submitted_at: '2026-09-14T10:00:00Z',
            items: 'kein-array',
          },
        ],
        reinstatements: null,
      }),
    })
    renderPage(HEADER_STATE)

    // The null element is dropped and the null/non-array items are coerced to
    // [] — the page renders the surviving inspections without crashing.
    expect(await screen.findByText('Anna Muster')).toBeInTheDocument()
    expect(screen.getByText('Bernd Beispiel')).toBeInTheDocument()
    // The coerced empty lists render the German empty notes instead of a crash.
    expect(screen.getByText('Keine Wiederherstellungen vorhanden.')).toBeInTheDocument()
  })

  it('HISTORY_ERROR: a failed history fetch shows the German inline error in both sections (no crash, no confirmation)', async () => {
    setPermissions(['inspection.history.view'])
    stubFetch({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    })
    renderPage(HEADER_STATE)

    const alerts = await screen.findAllByRole('alert')
    expect(alerts.length).toBe(2)
    for (const alert of alerts) {
      expect(alert).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
    }
    expect(screen.getByText('Bohrmaschine-01')).toBeInTheDocument()
  })

  it('DEEPLINK_FULL: on a deep link the header resolves from the dashboard list AND the history renders', async () => {
    setPermissions(['inspection.history.view'])
    const mock = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/tools') {
        return Promise.resolve({ ok: true, status: 200, json: async () => dashboardListFixture() })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => historyFixture() })
    })
    vi.stubGlobal('fetch', mock)
    renderPage('/tools/id-w1')

    // Header from the dashboard-list refetch.
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    // History from the tool-history fetch.
    expect(await screen.findByText('Bohrfutter locker')).toBeInTheDocument()
    expect(screen.getByText('Ersatzteil eingetroffen')).toBeInTheDocument()
    // Both endpoints were called.
    const urls = mock.mock.calls.map((c) => c[0])
    expect(urls).toContain('/api/v1/tools')
    expect(urls).toContain('/api/v1/tools/id-w1/history')
  })

  it('DEEPLINK_NO_HISTORY_PERMISSION: on a deep link the header resolves but no history fetch fires for a non-holder', async () => {
    setPermissions([])
    const mock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => dashboardListFixture() })
    vi.stubGlobal('fetch', mock)
    renderPage('/tools/id-w1')

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(
      screen.getAllByText('Du hast keine Berechtigung, die Prüfhistorie anzuzeigen.').length,
    ).toBe(2)
    const urls = mock.mock.calls.map((c) => c[0])
    expect(urls).toEqual(['/api/v1/tools'])
  })

  it('TOOL_SWITCH: navigating between /tools/:id URLs never flashes the previous tool header/history (identity guard)', async () => {
    setPermissions(['inspection.history.view'])
    // Hold the SECOND tool's dashboard-list AND history fetches so the render
    // under /tools/id-w2 is observed BEFORE its data resolves — the previous
    // tool's header/history must not appear (no stale flash under the new URL).
    let resolveW2List: ((v: unknown) => void) | null = null
    const heldW2List = new Promise((resolve) => {
      resolveW2List = resolve
    })
    let resolveW2History: ((v: unknown) => void) | null = null
    const heldW2History = new Promise((resolve) => {
      resolveW2History = resolve
    })
    let dashboardCalls = 0
    const mock = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/tools') {
        dashboardCalls++
        if (dashboardCalls === 1) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => [
              ...dashboardListFixture(),
              {
                id: 'id-w2',
                name: 'Schleifmaschine-01',
                tool_type_id: 'id-t1',
                tool_type_name: 'Schleifmaschine',
                inventory_number: 'GEAR000002',
                status: { status: 'green', next_due: '2027-09-14T10:00:00Z' },
              },
            ],
          })
        }
        return heldW2List
      }
      if (url === '/api/v1/tools/id-w1/history') {
        return Promise.resolve({ ok: true, status: 200, json: async () => historyFixture() })
      }
      if (url === '/api/v1/tools/id-w2/history') {
        return heldW2History
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ inspections: [], reinstatements: [] }) })
    })
    vi.stubGlobal('fetch', mock)

    function Harness() {
      const navigate = useNavigate()
      return (
        <div>
          <ToolDetailsPage />
          <button type="button" onClick={() => navigate('/tools/id-w2')}>
            zu Werkzeug 2
          </button>
        </div>
      )
    }
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/tools/id-w1']}>
          <Routes>
            <Route path="/tools/:toolId" element={<Harness />} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    )

    // id-w1 deep link: header + history resolve.
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(await screen.findByText('Bohrfutter locker')).toBeInTheDocument()

    // Navigate to id-w2 while its data is HELD: the previous tool's header and
    // history must NOT render under the new URL — the loading states show.
    await act(async () => {
      screen.getByRole('button', { name: 'zu Werkzeug 2' }).click()
    })
    expect(screen.queryByText('Bohrmaschine-01')).not.toBeInTheDocument()
    expect(screen.queryByText('Bohrfutter locker')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Werkzeugdetails werden geladen')).toBeInTheDocument()
    expect(screen.getAllByText('Prüfhistorie wird geladen...').length).toBe(2)

    // Release the held fetches → the id-w2 header + empty history render.
    resolveW2List!({
      ok: true,
      status: 200,
      json: async () => [
        {
          id: 'id-w2',
          name: 'Schleifmaschine-01',
          tool_type_id: 'id-t1',
          tool_type_name: 'Schleifmaschine',
          inventory_number: 'GEAR000002',
          status: { status: 'green', next_due: '2027-09-14T10:00:00Z' },
        },
      ],
    })
    resolveW2History!({ ok: true, status: 200, json: async () => ({ inspections: [], reinstatements: [] }) })
    await act(async () => {})
    expect(await screen.findByText('Schleifmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Keine Prüfungen vorhanden.')).toBeInTheDocument()
  })
})