// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route, useParams, useLocation } from 'react-router-dom'
import { DashboardPage } from './DashboardPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const DASHBOARD_TOOLS_URL = '/api/v1/tools'

function dashboardToolFixture(id: string, name: string, toolTypeName = 'Bohrmaschine', inventoryNumber = 'GEAR000001') {
  return {
    id,
    name,
    tool_type_id: 'id-t1',
    tool_type_name: toolTypeName,
    inventory_number: inventoryNumber,
  }
}

function InspectionStubRoute() {
  const { toolId } = useParams()
  const location = useLocation()
  // The stub route captures the EXACT navigation-state object the dashboard
  // builds (Story 5.1/5.2), so the SPA_200 test can assert the real state.
  const state = (location.state ?? {}) as { tool_name?: string; inventory_number?: string; inspection_mode?: string }
  return (
    <div>
      InspectionStub <span data-testid="stub-tool-id">{toolId}</span>{' '}
      <span data-testid="stub-tool-name">{state.tool_name}</span>{' '}
      <span data-testid="stub-inventory-number">{state.inventory_number}</span>{' '}
      <span data-testid="stub-inspection-mode">{state.inspection_mode}</span>
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

  it('GET_LIST: renders each active tool as name + type name + inventory number + "verfügbar" (green)', async () => {
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
    // Every listed tool carries the static "verfügbar" label.
    expect(screen.getAllByText('verfügbar')).toHaveLength(2)
    // No status derivation (Story 6.1 owns it): no Green/Orange/Red chips.
    expect(screen.queryByText('Keine Werkzeuge vorhanden')).not.toBeInTheDocument()
  })

  it('GET_EMPTY: an empty list keeps the "Keine Werkzeuge vorhanden" EmptyState', async () => {
    stubFetchTools([])
    renderPage()

    expect(await screen.findByText('Keine Werkzeuge vorhanden')).toBeInTheDocument()
    expect(screen.queryAllByText('verfügbar')).toHaveLength(0)
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
      { tool_id: 'id-w1', tool_name: 'Bohrmaschine-01', tool_type_id: 'id-t1', tool_type_name: 'Bohrmaschine', inspection_mode: 'checklist' },
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
    // inspection header can show it.
    expect(screen.getByTestId('stub-tool-name')).toHaveTextContent('Bohrmaschine-01')
    expect(screen.getByTestId('stub-inspection-mode')).toHaveTextContent('checklist')
    expect(screen.getByTestId('stub-inventory-number')).toHaveTextContent('GEAR000001')
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