// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminScheduleEditorPage } from './AdminScheduleEditorPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const SCHEDULES_URL = '/api/v1/admin/settings/schedules'

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
          <Route path="/admin/einstellungen/zeitplaene/neu" element={<AdminScheduleEditorPage />} />
          <Route path="/admin/einstellungen/zeitplaene/:id" element={<AdminScheduleEditorPage />} />
          <Route path="/admin/einstellungen" element={<div>EinstellungenListe</div>} />
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

describe('AdminScheduleEditorPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['schedules.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('CREATE_RENDER: create mode renders the empty form with the default unit, save disabled until valid (Spec 4-6 review 3)', async () => {
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    expect(await screen.findByRole('heading', { name: 'Neuer Zeitplan' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('')
    expect(screen.getByLabelText('Zeiteinheit')).toHaveValue('year')
    expect(screen.getByLabelText('Intervallgröße')).toHaveValue(null)
    // An empty name + magnitude must never be submitted — Save is disabled
    // with the inline hint.
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    expect(
      screen.getByText('Gib einen Namen und eine gültige Intervallgröße an, um zu speichern.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '← Zurück zur Liste' })).toBeInTheDocument()
  })

  it('CREATE_SAVE_DISABLED: a name without a valid magnitude keeps Save disabled (never Number(""))', async () => {
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    await screen.findByLabelText('Name')
    // Name but no magnitude → still disabled.
    await user.type(screen.getByLabelText('Name'), 'Halbjährlich')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    // Magnitude 0 → invalid (must be ≥ 1).
    await user.type(screen.getByLabelText('Intervallgröße'), '0')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeDisabled()
    // A valid magnitude re-enables the save.
    await user.clear(screen.getByLabelText('Intervallgröße'))
    await user.type(screen.getByLabelText('Intervallgröße'), '6')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeEnabled()
  })

  it('CREATE_SUBMIT: creating a schedule POSTs name + unit + magnitude and navigates back to the list', async () => {
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && init?.method === 'POST',
        response: {
          ok: true,
          status: 201,
          body: { ...scheduleFixture(), name: '2 Wochen', interval_unit: 'week', interval_magnitude: 2, message: 'Zeitplan gespeichert.' },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), '2 Wochen')
    await user.selectOptions(screen.getByLabelText('Zeiteinheit'), 'week')
    await user.type(screen.getByLabelText('Intervallgröße'), '2')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('EinstellungenListe')).toBeInTheDocument()
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === SCHEDULES_URL && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse(String(postCall![1].body))
    expect(body.name).toBe('2 Wochen')
    expect(body.interval_unit).toBe('week')
    expect(body.interval_magnitude).toBe(2)
  })

  it('EDIT_PREFILL: edit mode pre-fills the stored schedule and PUTs the changes', async () => {
    const fetchMock = stubFetchRoutes([
      stubSchedules([scheduleFixture()]),
      {
        matcher: (url: string, init?: RequestInit) => url === `${SCHEDULES_URL}/id-s1` && init?.method === 'PUT',
        response: { ok: true, status: 200, body: { ...scheduleFixture(), name: '2 Jahre', interval_magnitude: 2, message: 'Zeitplan gespeichert.' } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/id-s1')

    expect(await screen.findByRole('heading', { name: 'Zeitplan bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('1 Jahr')
    expect(screen.getByLabelText('Zeiteinheit')).toHaveValue('year')
    expect(screen.getByLabelText('Intervallgröße')).toHaveValue(1)

    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), '2 Jahre')
    await user.clear(screen.getByLabelText('Intervallgröße'))
    await user.type(screen.getByLabelText('Intervallgröße'), '2')
    await user.click(screen.getByRole('button', { name: 'Änderungen speichern' }))

    expect(await screen.findByText('EinstellungenListe')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) => url === `${SCHEDULES_URL}/id-s1` && init?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    const body = JSON.parse(String(putCall![1].body))
    expect(body.name).toBe('2 Jahre')
    expect(body.interval_magnitude).toBe(2)
  })

  it('EDIT_UNKNOWN: an unknown :id renders the German not-found and no form', async () => {
    stubFetchRoutes([stubSchedules([scheduleFixture()])])
    renderEditorPage('/admin/einstellungen/zeitplaene/id-unbekannt')

    expect(await screen.findByText('Zeitplan nicht gefunden.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
  })

  it('EDIT_401: a 401 on the schedule load clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string) => url === SCHEDULES_URL,
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    renderEditorPage('/admin/einstellungen/zeitplaene/id-s1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('EDIT_403: a 403 on the schedule load leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string) => url === SCHEDULES_URL,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    renderEditorPage('/admin/einstellungen/zeitplaene/id-s1')

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })

  it('SAVE_400: a 400 on create shows the server German message inline (no navigation)', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && init?.method === 'POST',
        response: {
          ok: false,
          status: 400,
          body: { error: { code: 'invalid_request', message: 'Bitte gib einen Namen für den Zeitplan an.' } },
        },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    await screen.findByLabelText('Name')
    // The canSave gate needs a name + valid magnitude before a submit fires.
    await user.type(screen.getByLabelText('Name'), 'Quartal')
    await user.type(screen.getByLabelText('Intervallgröße'), '1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib einen Namen für den Zeitplan an.')
    expect(screen.queryByText('EinstellungenListe')).not.toBeInTheDocument()
  })

  it('EDIT_LOAD_500: a 500 on the edit load renders the German load error and NO form (Spec 4-6 review 8)', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string) => url === SCHEDULES_URL,
        response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Interner Fehler.' } } },
      },
    ])
    renderEditorPage('/admin/einstellungen/zeitplaene/id-s1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Der Zeitplan konnte nicht geladen werden.')
    // No form may render over the failure — an empty enabled form must never
    // overwrite the stored schedule with blank data.
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Speichern' })).not.toBeInTheDocument()
  })

  it('EDIT_UNKNOWN_UNIT: an unknown stored interval unit falls back to "year" (Spec 4-6 review 9)', async () => {
    stubFetchRoutes([
      stubSchedules([
        { ...scheduleFixture(), interval_unit: 'fortnight', interval_magnitude: 1 },
      ]),
    ])
    renderEditorPage('/admin/einstellungen/zeitplaene/id-s1')

    expect(await screen.findByRole('heading', { name: 'Zeitplan bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Zeiteinheit')).toHaveValue('year')
  })

  it('SAVE_401: a 401 during save clears auth and redirects to /login', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && init?.method === 'POST',
        response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Quartal')
    await user.type(screen.getByLabelText('Intervallgröße'), '1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
  })

  it('SAVE_403: a 403 during save leaves the admin module back to the dashboard', async () => {
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === SCHEDULES_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const user = userEvent.setup()
    renderEditorPage('/admin/einstellungen/zeitplaene/neu')

    await screen.findByLabelText('Name')
    await user.type(screen.getByLabelText('Name'), 'Quartal')
    await user.type(screen.getByLabelText('Intervallgröße'), '1')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
  })
})
