// @vitest-environment jsdom
import { render, screen, within, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route, useParams, useLocation } from 'react-router-dom'
import { DashboardPage } from './DashboardPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'
import type { ToolStatusInfo } from '../auth/tools.ts'
import styles from './DashboardPage.module.css'

const DASHBOARD_TOOLS_URL = '/api/v1/tools'

function dashboardToolFixture(
  id: string,
  name: string,
  toolTypeName = 'Bohrmaschine',
  inventoryNumber = 'GEAR000001',
  status: ToolStatusInfo = { status: 'green', next_due: null },
) {
  return {
    id,
    name,
    tool_type_id: 'id-t1',
    tool_type_name: toolTypeName,
    inventory_number: inventoryNumber,
    status,
  }
}

function InspectionStubRoute() {
  const { toolId } = useParams()
  const location = useLocation()
  // The stub route captures the EXACT navigation-state object the dashboard
  // builds (Story 5.1/5.2), so the SPA_200 test can assert the real state.
  const state = (location.state ?? {}) as {
    tool_name?: string
    tool_type_name?: string
    inventory_number?: string
    inspection_mode?: string
    checklist_items?: unknown[]
  }
  return (
    <div>
      InspectionStub <span data-testid="stub-tool-id">{toolId}</span>{' '}
      <span data-testid="stub-tool-name">{state.tool_name}</span>{' '}
      <span data-testid="stub-tool-type-name">{state.tool_type_name}</span>{' '}
      <span data-testid="stub-inventory-number">{state.inventory_number}</span>{' '}
      <span data-testid="stub-inspection-mode">{state.inspection_mode}</span>{' '}
      <span data-testid="stub-checklist-count">{state.checklist_items?.length ?? 0}</span>
    </div>
  )
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/login" element={<div>Anmeldung</div>} />
          <Route path="/inspection/:toolId" element={<InspectionStubRoute />} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetchTools(body: unknown, status = 200) {
  const ok = status >= 200 && status < 300
  const mock = vi.fn().mockImplementation(async (url: string) => {
    if (url === DASHBOARD_TOOLS_URL) {
      return { ok, status, json: async () => body }
    }
    return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

// stubFetchStart serves the Werkzeugliste list AND the Story 5.1 inspection
// start (POST /api/v1/tools/{id}/inspection/start). startStatus/startBody
// drive the start response; a non-2xx body falls back to the uniform envelope.
function stubFetchStart(listBody: unknown, startStatus: number, startBody: unknown = null) {
  const ok = startStatus >= 200 && startStatus < 300
  const mock = vi.fn().mockImplementation(async (url: string) => {
    if (url === DASHBOARD_TOOLS_URL) {
      return { ok: true, status: 200, json: async () => listBody }
    }
    if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/inspection/start')) {
      const body = startBody ?? { error: { code: 'forbidden', message: 'Erforderliche Qualifikation fehlt.' } }
      return { ok, status: startStatus, json: async () => body }
    }
    return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

describe('DashboardPage Werkzeugliste (Story 4-3b)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('GET_LIST: renders each active tool as name + type name + inventory number + derived status label (green)', async () => {
    stubFetchTools([
      dashboardToolFixture('id-w1', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001'),
      dashboardToolFixture('id-w2', 'Bohrmaschine-02', 'Schleifmaschine', 'GEAR000002'),
    ])
    renderPage()

    // The tool list renders the name and the JOINed type name per row.
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Bohrmaschine-02')).toBeInTheDocument()
    // Type names: one row is Bohrmaschine, the other Schleifmaschine.
    expect(screen.getByText('Schleifmaschine')).toBeInTheDocument()
    // DASHBOARD (Story 4-3b): the inventory number shows as row meta.
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('GEAR000002')).toBeInTheDocument()
    // Story 6.1: every listed tool renders its DERIVED German label (both
    // fixtures default green) — the static "verfügbar" is gone.
    const list = screen.getByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getAllByText('Einsatzbereit')).toHaveLength(2)
    expect(screen.queryByText('verfügbar')).not.toBeInTheDocument()
    expect(screen.queryByText('Keine Werkzeuge vorhanden')).not.toBeInTheDocument()
  })

  it('GET_EMPTY: an empty list keeps the "Keine Werkzeuge vorhanden" EmptyState', async () => {
    stubFetchTools([])
    renderPage()

    expect(await screen.findByText('Keine Werkzeuge vorhanden')).toBeInTheDocument()
  })

  it('GET_UNAUTHENTICATED: a 401 on load clears the stale auth state and redirects to /login', async () => {
    stubFetchTools({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }, 401)
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('GET_FORBIDDEN: a 403 is handled defensively (clears auth, redirects to /login)', async () => {
    // dashboard.view should never 403 for a logged-in user (all base roles hold
    // it), but the SPA handles it defensively without exposing tool data.
    stubFetchTools({ error: { code: 'forbidden', message: 'Keine Berechtigung.' } }, 403)
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('LOAD_ERROR: a non-auth failure shows an inline German error', async () => {
    stubFetchTools({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }, 500)
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Die Werkzeugliste konnte nicht geladen werden.')
    expect(localStorage.getItem('gear.session_token')).toBe('sesstoken123')
  })
})

describe('DashboardPage inspection start (Story 5.1, FR-11/AD-7)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('SPA_200: clicking "Prüfung starten" on an eligible row navigates to /inspection/:toolId', async () => {
    const user = userEvent.setup()
    stubFetchStart(
      [dashboardToolFixture('id-w1', 'Bohrmaschine-01')],
      200,
      { tool_id: 'id-w1', tool_name: 'Bohrmaschine-01', tool_type_id: 'id-t1', tool_type_name: 'Bohrmaschine', inspection_mode: 'checklist', checklist_items: [] },
    )
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    await user.click(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' }))

    // The stub inspection route is reached with the tool id.
    expect(await screen.findByText('InspectionStub')).toBeInTheDocument()
    expect(screen.getByTestId('stub-tool-id')).toHaveTextContent('id-w1')
    // Story 5.1/5.2: the navigation-state object the dashboard builds carries
    // the tool name, the inspection mode and the inventory number (the latter
    // from the tool LIST — the /start payload has no identifier), so the
    // inspection header can show it. The mode-aware surface additionally
    // receives the type name + the type's checklist items from the /start
    // payload (the checklist-mode surface renders without a re-fetch).
    expect(screen.getByTestId('stub-tool-name')).toHaveTextContent('Bohrmaschine-01')
    expect(screen.getByTestId('stub-tool-type-name')).toHaveTextContent('Bohrmaschine')
    expect(screen.getByTestId('stub-inspection-mode')).toHaveTextContent('checklist')
    expect(screen.getByTestId('stub-inventory-number')).toHaveTextContent('GEAR000001')
    expect(screen.getByTestId('stub-checklist-count')).toHaveTextContent('0')
  })

  it('SPA_403: an ineligible click shows the German reason inline and disables that row for the session (persists across list refetches)', async () => {
    const user = userEvent.setup()
    stubFetchStart(
      [dashboardToolFixture('id-w1', 'Bohrmaschine-01'), dashboardToolFixture('id-w2', 'Schleifmaschine-02')],
      403,
      { error: { code: 'forbidden', message: 'Erforderliche Qualifikation fehlt.' } },
    )
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    // Every start button carries an aria-label tying it to its tool (UX-DR8):
    // assistive-tech users can tell which row each control starts.
    const buttons = screen.getAllByRole('button', { name: /^Prüfung starten für / })
    expect(buttons).toHaveLength(2)

    await user.click(buttons[0])

    // The server's German reason shows inline (uniform envelope message).
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Erforderliche Qualifikation fehlt.')
    // The clicked row's button disables FOR THE SESSION (the id is kept in a
    // ref, so it survives a LIST REFETCH — only a full reload clears it); the
    // other row stays enabled (the 403 IS the gate — never a client-side
    // pre-query).
    expect(buttons[0]).toBeDisabled()
    expect(screen.getAllByRole('button', { name: /^Prüfung starten für / })[1]).not.toBeDisabled()
    // No navigation happened.
    expect(screen.queryByText('InspectionStub')).not.toBeInTheDocument()
  })

  it('SPA_401: a 401 on the start clears auth state and redirects to /login', async () => {
    const user = userEvent.setup()
    stubFetchStart(
      [dashboardToolFixture('id-w1', 'Bohrmaschine-01')],
      401,
      { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } },
    )
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    await user.click(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' }))

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('START_OTHER: a non-auth start failure shows an inline error and the button stays enabled for a retry', async () => {
    const user = userEvent.setup()
    stubFetchStart(
      [dashboardToolFixture('id-w1', 'Bohrmaschine-01')],
      500,
      { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } },
    )
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    const button = screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' })
    await user.click(button)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
    // Not a 403: the button stays enabled (a transient failure can be retried).
    expect(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' })).not.toBeDisabled()
    expect(localStorage.getItem('gear.session_token')).toBe('sesstoken123')
  })

  it('DOUBLE_CLICK: a second click while the start is in flight fires only ONE start request', async () => {
    const user = userEvent.setup()
    let resolveStart: (r: unknown) => void = () => {}
    const startPromise = new Promise<unknown>((resolve) => {
      resolveStart = resolve
    })
    const startCalls: string[] = []
    const mock = vi.fn().mockImplementation(async (url: string) => {
      if (url === DASHBOARD_TOOLS_URL) {
        return { ok: true, status: 200, json: async () => [dashboardToolFixture('id-w1', 'Bohrmaschine-01')] }
      }
      if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/inspection/start')) {
        startCalls.push(url)
        return startPromise
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', mock)
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    const button = screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' })
    await user.click(button)
    // The button is disabled while in flight (the pending ref guard also
    // makes the second click a no-op): only ONE start request fires.
    await user.click(button)

    expect(startCalls).toHaveLength(1)

    // Resolve the start so the pending state clears and the navigation happens.
    resolveStart({
      ok: true,
      status: 200,
      json: async () => ({
        tool_id: 'id-w1',
        tool_name: 'Bohrmaschine-01',
        tool_type_id: 'id-t1',
        tool_type_name: 'Bohrmaschine',
        inspection_mode: 'checklist',
        checklist_items: [],
      }),
    })
    expect(await screen.findByText('InspectionStub')).toBeInTheDocument()
    expect(screen.getByTestId('stub-tool-name')).toHaveTextContent('Bohrmaschine-01')
    expect(screen.getByTestId('stub-inspection-mode')).toHaveTextContent('checklist')
    expect(screen.getByTestId('stub-inventory-number')).toHaveTextContent('GEAR000001')
  })

  it('RELOAD_CLEARS_ERROR: a stale inline start error is cleared on a successful list reload', async () => {
    const user = userEvent.setup()
    // First render: the start 500s, leaving an inline error.
    stubFetchStart(
      [dashboardToolFixture('id-w1', 'Bohrmaschine-01')],
      500,
      { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } },
    )
    renderPage()
    await screen.findByText('Bohrmaschine-01')
    await user.click(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')

    // Simulate a successful list reload (Story 4-3b refetch): the tool list is
    // fetched again and the stale inline error is cleared.
    cleanup()
    vi.unstubAllGlobals()
    stubFetchTools([dashboardToolFixture('id-w1', 'Bohrmaschine-01')], 200)
    renderPage()
    await screen.findByText('Bohrmaschine-01')

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

describe('DashboardPage color-coded status (Story 6.1, FR-16/AD-4/AD-5)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  // mixedTools covers every derived code so the row/filter/count/tap matrix can
  // be exercised against real server-returned statuses.
  const mixedTools = [
    dashboardToolFixture('id-green', 'Grün', 'Bohrmaschine', 'GEAR000001', { status: 'green', next_due: '2027-01-01T00:00:00Z' }),
    dashboardToolFixture('id-orange', 'Orange', 'Bohrmaschine', 'GEAR000002', { status: 'orange', next_due: '2026-09-20T00:00:00Z' }),
    dashboardToolFixture('id-red', 'Rot', 'Bohrmaschine', 'GEAR000003', { status: 'red', next_due: '2026-09-01T00:00:00Z' }),
    dashboardToolFixture('id-oos', 'Oos', 'Bohrmaschine', 'GEAR000004', { status: 'oos', next_due: null }),
  ]

  it('SPA_ROW: every row shows the derived German label + color chip instead of the static "verfügbar"', async () => {
    stubFetchTools(mixedTools)
    renderPage()

    const list = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Einsatzbereit')).toBeInTheDocument()
    expect(within(list).getByText('Ausstehend')).toBeInTheDocument()
    expect(within(list).getByText('Überfällig')).toBeInTheDocument()
    expect(within(list).getByText('Außer Betrieb')).toBeInTheDocument()
    expect(within(list).queryByText('verfügbar')).not.toBeInTheDocument()
  })

  it('SPA_FILTER: selecting a status chip shows only the matching tools', async () => {
    const user = userEvent.setup()
    stubFetchTools(mixedTools)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Überfällig' }))

    const list = screen.getByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Rot')).toBeInTheDocument()
    expect(within(list).queryByText('Grün')).not.toBeInTheDocument()
    expect(within(list).queryByText('Orange')).not.toBeInTheDocument()
    expect(within(list).queryByText('Oos')).not.toBeInTheDocument()
    // The chip is active (aria-pressed).
    expect(screen.getByRole('button', { name: 'Überfällig' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('SPA_MULTI: several statuses active combine as the union', async () => {
    const user = userEvent.setup()
    stubFetchTools(mixedTools)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Überfällig' }))
    await user.click(screen.getByRole('button', { name: 'Ausstehend' }))

    const list = screen.getByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Rot')).toBeInTheDocument()
    expect(within(list).getByText('Orange')).toBeInTheDocument()
    expect(within(list).queryByText('Grün')).not.toBeInTheDocument()
    expect(within(list).queryByText('Oos')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Überfällig' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Ausstehend' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('SPA_ALL: no status selected (or "Alle") shows the full list', async () => {
    const user = userEvent.setup()
    stubFetchTools(mixedTools)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(screen.getAllByRole('listitem')).toHaveLength(4)

    // Select a filter, then tap "Alle" to clear it → the full list returns.
    await user.click(screen.getByRole('button', { name: 'Überfällig' }))
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Alle' }))
    expect(screen.getAllByRole('listitem')).toHaveLength(4)
    expect(screen.getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('SPA_COUNTS: the summary grid shows the real per-status totals', async () => {
    stubFetchTools(mixedTools)
    renderPage()

    const statusRegion = await screen.findByRole('region', { name: 'Statusübersicht' })
    expect(within(statusRegion).getByRole('button', { name: '1 Einsatzbereit' })).toBeInTheDocument()
    expect(within(statusRegion).getByRole('button', { name: '1 Ausstehend' })).toBeInTheDocument()
    expect(within(statusRegion).getByRole('button', { name: '1 Überfällig' })).toBeInTheDocument()
    expect(within(statusRegion).getByRole('button', { name: '1 Außer Betrieb' })).toBeInTheDocument()
  })

  it('SPA_TAP: tapping a summary count card activates its filter; tapping it again clears it', async () => {
    const user = userEvent.setup()
    stubFetchTools(mixedTools)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    // Tap the Überfällig count card → only the red tool remains visible.
    await user.click(screen.getByRole('button', { name: '1 Überfällig' }))
    const list = screen.getByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Rot')).toBeInTheDocument()
    expect(within(list).queryByText('Grün')).not.toBeInTheDocument()

    // Tap it again → the filter clears and the full list returns.
    await user.click(screen.getByRole('button', { name: '1 Überfällig' }))
    expect(screen.getAllByRole('listitem')).toHaveLength(4)
  })

  it('SPA_COLOR: each row chip carries the distinct module class for its code (OOS must never render green) — patch 14', async () => {
    stubFetchTools(mixedTools)
    renderPage()

    await screen.findByRole('list', { name: 'Werkzeuge' })
    const cases: Array<[string, string, string]> = [
      ['Grün', 'Einsatzbereit', styles.statusGreen],
      ['Orange', 'Ausstehend', styles.statusOrange],
      ['Rot', 'Überfällig', styles.statusRed],
      ['Oos', 'Außer Betrieb', styles.statusOos],
    ]
    for (const [toolName, label, classKey] of cases) {
      const row = screen.getByText(toolName).closest('li')
      expect(row).not.toBeNull()
      expect(within(row as HTMLElement).getByText(label)).toHaveClass(classKey)
    }
  })

  it('FILTERED_EMPTY: a non-empty fleet filtered to nothing shows the distinct message + "Alle anzeigen", never the fleet EmptyState — patch 2', async () => {
    const user = userEvent.setup()
    stubFetchTools([
      dashboardToolFixture('id-green', 'Grün', 'Bohrmaschine', 'GEAR000001', { status: 'green', next_due: '2027-01-01T00:00:00Z' }),
    ])
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Überfällig' }))

    expect(screen.getByText('Keine Werkzeuge mit dem ausgewählten Status.')).toBeInTheDocument()
    expect(screen.queryByText('Keine Werkzeuge vorhanden')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Alle anzeigen' }))
    expect(screen.getByRole('list', { name: 'Werkzeuge' })).toBeInTheDocument()
  })

  it('REMOUNT_REFRESH: a remount refetches and renders updated statuses, counts and colors (UX-DR6) — patch 8', async () => {
    stubFetchTools([
      dashboardToolFixture('id-w1', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001', { status: 'green', next_due: '2027-01-01T00:00:00Z' }),
    ])
    renderPage()
    const firstList = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(within(firstList).getByText('Einsatzbereit')).toBeInTheDocument()

    // Simulate "returning to the dashboard" after an inspection: the remount
    // fetches a FRESH list with the tool now out of service.
    cleanup()
    vi.unstubAllGlobals()
    stubFetchTools([
      dashboardToolFixture('id-w1', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001', { status: 'oos', next_due: null }),
    ])
    renderPage()

    const secondList = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(within(secondList).getByText('Außer Betrieb')).toBeInTheDocument()
    expect(within(secondList).queryByText('Einsatzbereit')).not.toBeInTheDocument()
    // The counts refresh too: green drops to 0, OOS rises to 1.
    const statusRegion = screen.getByRole('region', { name: 'Statusübersicht' })
    expect(within(statusRegion).getByRole('button', { name: '1 Außer Betrieb' })).toBeInTheDocument()
    expect(within(statusRegion).queryByRole('button', { name: '1 Einsatzbereit' })).not.toBeInTheDocument()
  })
})

describe('DashboardPage out-of-service reinstatement (Story 5.6, FR-14/AD-4 + FR-15/AD-9)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  // stubFetchReinstate serves the Werkzeugliste list AND the Story 5.6
  // reinstatement POST. It records the request bodies so the SPA_DIALOG test
  // can assert the exact { reason } the dialog collects.
  function stubFetchReinstate(listBody: unknown, reinstateStatus: number, reinstateBody: unknown = null) {
    const ok = reinstateStatus >= 200 && reinstateStatus < 300
    const bodies: Array<{ url: string; body?: Record<string, unknown> }> = []
    const mock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === DASHBOARD_TOOLS_URL) {
        return { ok: true, status: 200, json: async () => listBody }
      }
      if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/reinstatement')) {
        bodies.push({ url, body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined })
        return { ok, status: reinstateStatus, json: async () => reinstateBody }
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', mock)
    return { mock, bodies }
  }

  const oosTool = dashboardToolFixture('id-oos', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001', {
    status: 'oos',
    next_due: null,
  })

  it('SPA_START_DISABLED: an OOS row shows "Außer Betrieb" and its "Prüfung starten" button is DISABLED (OOS is not inspectable)', async () => {
    stubFetchReinstate([oosTool], 200)
    renderPage()

    const list = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Außer Betrieb')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' })).toBeDisabled()
  })

  it('SPA_REINSTATE_HOLDER: a tool.reinstate holder sees "Wiederherstellen" on the OOS row', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['dashboard.view', 'inspection.submit', 'tool.reinstate']))
    stubFetchReinstate([oosTool], 200)
    renderPage()

    await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' })).toBeInTheDocument()
    // The start button is still disabled on the OOS row.
    expect(screen.getByRole('button', { name: 'Prüfung starten für Bohrmaschine-01' })).toBeDisabled()
  })

  it('SPA_REINSTATE_NONHOLDER: a caller without tool.reinstate never sees the button (the 403 IS the gate, AD-6)', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['dashboard.view', 'inspection.submit']))
    stubFetchReinstate([oosTool], 200)
    renderPage()

    const list = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(screen.queryByRole('button', { name: /Wiederherstellen/ })).not.toBeInTheDocument()
    // The row still shows the OOS state chip.
    expect(within(list).getByText('Außer Betrieb')).toBeInTheDocument()
  })

  it('SPA_DIALOG: clicking "Wiederherstellen" opens the PromptDialog and the submit carries the TRIMMED reason in the POST body', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    const { bodies } = stubFetchReinstate(
      [oosTool],
      200,
      { status: { status: 'green', next_due: '2027-01-01T00:00:00Z' }, message: 'Das Gerät wurde wiederhergestellt.' },
    )
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))

    // The PromptDialog asks for the mandatory reason.
    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByLabelText('Grund für die Wiederherstellung von Bohrmaschine-01')
    await user.type(input, '  Ersatzteil eingetroffen  ')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    // The POST body carries the trimmed { reason } to the reinstate endpoint.
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0].url).toBe(`${DASHBOARD_TOOLS_URL}/id-oos/reinstatement`)
    expect(bodies[0].body).toEqual({ reason: 'Ersatzteil eingetroffen' })
  })

  it('SPA_DIALOG_EMPTY: submitting the dialog without a reason shows the empty-reason message and sends nothing', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    const { bodies } = stubFetchReinstate([oosTool], 200)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    expect(within(dialog).getByRole('alert')).toHaveTextContent('Bitte gib einen Grund für die Wiederherstellung an.')
    expect(bodies).toHaveLength(0)
  })

  it('SPA_REFRESH: after a successful reinstatement the list refetches and the tool leaves OOS (confirmation shows)', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    // The first list fetch shows the tool OOS; the refetch after the reinstate
    // shows it green (the server derived the new status).
    let listCall = 0
    const mock = vi.fn().mockImplementation(async (url: string) => {
      if (url === DASHBOARD_TOOLS_URL) {
        listCall += 1
        if (listCall === 1) {
          return { ok: true, status: 200, json: async () => [oosTool] }
        }
        return {
          ok: true,
          status: 200,
          json: async () => [
            dashboardToolFixture('id-oos', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001', {
              status: 'green',
              next_due: '2027-01-01T00:00:00Z',
            }),
          ],
        }
      }
      if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/reinstatement')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ status: { status: 'green', next_due: '2027-01-01T00:00:00Z' }, message: 'Das Gerät wurde wiederhergestellt.' }),
        }
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', mock)
    renderPage()
    const list = await screen.findByRole('list', { name: 'Werkzeuge' })
    expect(within(list).getByText('Außer Betrieb')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Grund für die Wiederherstellung von Bohrmaschine-01'), 'Ersatzteil eingetroffen')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    // The list refetches: the tool leaves OOS, the confirmation shows and the
    // reinstate action is gone (the row is serviceable again).
    expect(await screen.findByText('Das Gerät wurde wiederhergestellt.')).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Werkzeuge' })).queryByText('Außer Betrieb')).not.toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Werkzeuge' })).getByText('Einsatzbereit')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Wiederherstellen/ })).not.toBeInTheDocument()
  })

  it('SPA_ERROR: a failed reinstate closes the dialog and shows the server German message inline', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    stubFetchReinstate([oosTool], 400, { error: { code: 'invalid_request', message: 'Der Grund ist zu lang (maximal 2000 Zeichen).' } })
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Grund für die Wiederherstellung von Bohrmaschine-01'), 'x'.repeat(2001))
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    // The dialog closes and the server's German message shows inline on the row.
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Der Grund ist zu lang (maximal 2000 Zeichen).')
    // The tool stays OOS (nothing was persisted client-side).
    expect(within(screen.getByRole('list', { name: 'Werkzeuge' })).getByText('Außer Betrieb')).toBeInTheDocument()
  })

  it('SPA_DIALOG_CAPTURED: the dialog target is captured at open time — the label and the POST use the captured tool, not a live list lookup', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    const { bodies } = stubFetchReinstate(
      [oosTool],
      200,
      { status: { status: 'green', next_due: '2027-01-01T00:00:00Z' }, message: 'Das Gerät wurde wiederhergestellt.' },
    )
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))

    // The dialog is driven by the CAPTURED target (tool id + name at open
    // time) — a later list change can never resolve it to null mid-input.
    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByLabelText('Grund für die Wiederherstellung von Bohrmaschine-01')
    expect(input).toBeInTheDocument()
    await user.type(input, 'Ersatzteil')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    // The POST goes to the captured tool id.
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0].url).toBe(`${DASHBOARD_TOOLS_URL}/id-oos/reinstatement`)
  })

  it('SPA_REINSTATE_BUSY: while a reinstatement is in flight the row reinstate buttons are disabled (no second dialog mid-flight)', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    const oosTool2 = dashboardToolFixture('id-oos-2', 'Schleifmaschine-02', 'Schleifmaschine', 'GEAR000002', {
      status: 'oos',
      next_due: null,
    })
    let resolveReinstate: (r: unknown) => void = () => {}
    const reinstatePromise = new Promise<unknown>((resolve) => {
      resolveReinstate = resolve
    })
    const mock = vi.fn().mockImplementation(async (url: string) => {
      if (url === DASHBOARD_TOOLS_URL) {
        return { ok: true, status: 200, json: async () => [oosTool, oosTool2] }
      }
      if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/reinstatement')) {
        return reinstatePromise
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', mock)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    const buttons = screen.getAllByRole('button', { name: /^Wiederherstellen für / })
    expect(buttons).toHaveLength(2)
    expect(buttons[0]).not.toBeDisabled()
    expect(buttons[1]).not.toBeDisabled()

    await user.click(buttons[0])
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText(/Grund für die Wiederherstellung von/), 'Ersatzteil')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    // The POST is in flight: every row reinstate button is disabled so a second
    // dialog can't be opened mid-flight via another row.
    const busyButtons = screen.getAllByRole('button', { name: /^Wiederherstellen für / })
    expect(busyButtons[0]).toBeDisabled()
    expect(busyButtons[1]).toBeDisabled()

    resolveReinstate({
      ok: true,
      status: 200,
      json: async () => ({ status: { status: 'green', next_due: null }, message: 'Das Gerät wurde wiederhergestellt.' }),
    })
    expect(await screen.findByText('Das Gerät wurde wiederhergestellt.')).toBeInTheDocument()
  })

  it('SPA_CONFIRM_CLEARED: a stale success confirmation is cleared when a newer reinstate attempt errors (no stale success next to a newer error)', async () => {
    const user = userEvent.setup()
    localStorage.setItem('gear.permissions', JSON.stringify(['tool.reinstate']))
    const oosTool2 = dashboardToolFixture('id-oos-2', 'Schleifmaschine-02', 'Schleifmaschine', 'GEAR000002', {
      status: 'oos',
      next_due: null,
    })
    // Two full dialog flows (a success + a 2001-rune typed failure) exceed the
    // default 5s budget — run with a generous timeout.
    let listCall = 0
    let reinstateCall = 0
    const mock = vi.fn().mockImplementation(async (url: string) => {
      if (url === DASHBOARD_TOOLS_URL) {
        listCall += 1
        if (listCall === 1) {
          return { ok: true, status: 200, json: async () => [oosTool, oosTool2] }
        }
        return {
          ok: true,
          status: 200,
          json: async () => [
            dashboardToolFixture('id-oos', 'Bohrmaschine-01', 'Bohrmaschine', 'GEAR000001', {
              status: 'green',
              next_due: '2027-01-01T00:00:00Z',
            }),
            oosTool2,
          ],
        }
      }
      if (url.startsWith(`${DASHBOARD_TOOLS_URL}/`) && url.endsWith('/reinstatement')) {
        reinstateCall += 1
        if (reinstateCall === 1) {
          return {
            ok: true,
            status: 200,
            json: async () => ({ status: { status: 'green', next_due: null }, message: 'Das Gerät wurde wiederhergestellt.' }),
          }
        }
        return { ok: false, status: 400, json: async () => ({ error: { code: 'invalid_request', message: 'Der Grund ist zu lang (maximal 2000 Zeichen).' } }) }
      }
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found', message: 'nope' } }) }
    })
    vi.stubGlobal('fetch', mock)
    renderPage()
    await screen.findByRole('list', { name: 'Werkzeuge' })

    // 1. Reinstate tool 1 → confirmation shows.
    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Bohrmaschine-01' }))
    let dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Grund für die Wiederherstellung von Bohrmaschine-01'), 'Ersatzteil')
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))
    expect(await screen.findByText('Das Gerät wurde wiederhergestellt.')).toBeInTheDocument()

    // 2. A NEW reinstate attempt (tool 2) fails → the stale success is gone,
    //    the newer error shows inline.
    await user.click(screen.getByRole('button', { name: 'Wiederherstellen für Schleifmaschine-02' }))
    dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Grund für die Wiederherstellung von Schleifmaschine-02'), 'x'.repeat(2001))
    await user.click(within(dialog).getByRole('button', { name: 'Wiederherstellen' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Der Grund ist zu lang (maximal 2000 Zeichen).')
    expect(screen.queryByText('Das Gerät wurde wiederhergestellt.')).not.toBeInTheDocument()
  }, 20000)
})
