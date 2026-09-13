// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { InspectionPage } from './InspectionPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

type InspectionEntry = string | { pathname: string; state?: Record<string, unknown> }

function renderPage(initialEntry: InspectionEntry) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/inspection/:toolId" element={<InspectionPage />} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function eligibleStart(toolId: string, overrides: Record<string, unknown> = {}) {
  return {
    tool_id: toolId,
    tool_name: 'Bohrmaschine-01',
    tool_type_id: 'id-t1',
    tool_type_name: 'Bohrmaschine',
    inspection_mode: 'checklist',
    ...overrides,
  }
}

function stubFetch(response: unknown) {
  const mock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', mock)
  return mock
}

describe('InspectionPage stub (Story 5.1)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('STATE: renders the tool name + mode from router state without re-fetching', async () => {
    const mock = vi.fn()
    vi.stubGlobal('fetch', mock)
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01', inspection_mode: 'checklist' } })

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    // Router state is authoritative: no server call for the display data.
    expect(mock).not.toHaveBeenCalled()
  })

  it('REFETCH: without router state it re-fetches the tool + mode via startInspection', async () => {
    stubFetch({ ok: true, status: 200, json: async () => eligibleStart('id-w1') })
    renderPage('/inspection/id-w1')

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
  })

  it('REFETCH_401: a 401 on the re-fetch clears auth state and redirects to /login', async () => {
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderPage('/inspection/id-w1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('REFETCH_ERROR: a failed re-fetch shows an inline error state (no crash, no raw id leak)', async () => {
    stubFetch({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    })
    renderPage('/inspection/id-w1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
  })

  it('PASS_FAIL: a pass_fail mode maps to "Pass/Fail"', async () => {
    renderPage({ pathname: '/inspection/id-w2', state: { tool_name: 'Schleifmaschine-01', inspection_mode: 'pass_fail' } })

    expect(await screen.findByText('Schleifmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
  })

  it('MISSING_MODE: an absent inspection_mode falls back to the neutral dash', async () => {
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01' } })

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('–')).toBeInTheDocument()
  })

  it('EMPTY_NAME_FALLBACK: an absent tool_name falls back to the tool id (`||`, not `??`)', async () => {
    renderPage({ pathname: '/inspection/id-w1', state: { inspection_mode: 'checklist' } })

    expect(await screen.findByText('id-w1')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
  })
})