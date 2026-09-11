// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { DashboardPage } from './DashboardPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const DASHBOARD_TOOLS_URL = '/api/v1/tools'

function dashboardToolFixture(id: string, name: string, toolTypeName = 'Bohrmaschine', inventoryNumber = 'GEAR00000X') {
  return {
    id,
    name,
    tool_type_id: 'id-t1',
    tool_type_name: toolTypeName,
    inventory_number: inventoryNumber,
  }
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/login" element={<div>Anmeldung</div>} />
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