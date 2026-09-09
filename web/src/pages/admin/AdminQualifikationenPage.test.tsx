// @vitest-environment jsdom
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminQualifikationenPage } from './AdminQualifikationenPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const QUALS_URL = '/api/v1/admin/qualifications'

function qualsFixture() {
  return {
    qualifications: [
      { id: 'q-1', name: 'Kettensäge', description: 'Führerschein', expiry_kind: 'unlimited', status: 'unlimited' },
      { id: 'q-2', name: 'Erste Hilfe', description: '', expiry_kind: 'fixed', status: 'fixed' },
    ],
    users: [
      { id: 'u-1', name: 'Frei Willig' },
      { id: 'u-2', name: 'Vera Waltung' },
    ],
  }
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/qualifikationen']}>
        <Routes>
          <Route path="/admin/qualifikationen" element={<AdminQualifikationenPage />} />
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetchRoutes(routes: Array<{
  matcher: (url: string, init?: RequestInit) => boolean
  response:
    | { ok: boolean; status: number; body: unknown }
    | ((url: string, init?: RequestInit) => { ok: boolean; status: number; body: unknown })
}>) {
  const mock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    const hit = routes.find((r) => r.matcher(url, init))
    const resolved = typeof hit?.response === 'function' ? hit.response(url, init) : hit?.response
    const res = resolved ?? { ok: false, status: 404, body: { error: { code: 'not_found', message: 'nope' } } }
    return { ok: res.ok, status: res.status, json: async () => res.body }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

const stubList = (body: unknown) => ({
  matcher: (url: string) => url === QUALS_URL,
  response: { ok: true, status: 200, body },
})

describe('AdminQualifikationenPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['qualifications.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders each qualification with a status badge and the roster-fed assignment CTA', async () => {
    stubFetchRoutes([stubList(qualsFixture())])
    renderPage()

    expect(await screen.findByText('Kettensäge')).toBeInTheDocument()
    expect(screen.getByText('Unbegrenzt')).toBeInTheDocument()
    expect(screen.getByText('Befristet')).toBeInTheDocument()
    expect(screen.getByText('Erste Hilfe')).toBeInTheDocument()
    expect(screen.getByText('Führerschein')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Bearbeiten' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: 'Zuweisung' })).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Neue Qualifikation' })).toBeInTheDocument()
  })

  it('LIST_FETCH: fetch uses the authenticated admin headers', async () => {
    const fetchMock = stubFetchRoutes([stubList(qualsFixture())])
    renderPage()
    await screen.findByText('Kettensäge')

    expect(fetchMock).toHaveBeenCalledWith(QUALS_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('NEW_QUAL: clicking Neue Qualifikation opens the editor, creating refreshes the list', async () => {
    let listCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && !init?.method && (listCalls++, true),
        response: {
          ok: true, status: 200,
          body: listCalls === 1
            ? qualsFixture()
            : { ...qualsFixture(), qualifications: [...qualsFixture().qualifications, { id: 'q-9', name: 'Seilwinde', description: '', expiry_kind: 'unlimited', status: 'unlimited' }] },
        },
      },
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { message: 'Qualifikation erstellt.', qualification: { id: 'q-9', name: 'Seilwinde', description: '', expiry_kind: 'unlimited', status: 'unlimited' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Kettensäge')
    await user.click(screen.getByRole('button', { name: 'Neue Qualifikation' }))

    expect(await screen.findByRole('heading', { name: 'Neue Qualifikation' })).toBeInTheDocument()
    await user.type(screen.getByLabelText('Name'), 'Seilwinde')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Qualifikation erstellt.')).toBeInTheDocument()
    expect(await screen.findByText('Seilwinde')).toBeInTheDocument()
  })

  it('EDIT: editing a qualification saves via PUT and shows success feedback', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(qualsFixture()),
      {
        matcher: (url, init) => init?.method === 'PUT' && url === `${QUALS_URL}/q-1`,
        response: { ok: true, status: 200, body: { message: 'Qualifikation gespeichert.', qualification: { id: 'q-1', name: 'Kettensäge', description: 'Neu', expiry_kind: 'unlimited', status: 'unlimited' } } },
      },
      stubList(qualsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Kettensäge')
    const row = screen.getByText('Kettensäge').closest('li')
    if (!row) throw new Error('Kettensäge row not found')
    await user.click(within(row as HTMLElement).getByRole('button', { name: 'Bearbeiten' }))

    expect(await screen.findByRole('heading', { name: 'Qualifikation „Kettensäge“ bearbeiten' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Qualifikation gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
  })

  it('ASSIGN: opening Zuweisung pre-checks the current assignees; saving replaces the set', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(qualsFixture()),
      {
        matcher: (url, init) => !init?.method && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { assignees: [{ id: 'u-1', name: 'Frei Willig' }] } },
      },
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { message: 'Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.', assignees: [{ id: 'u-1', name: 'Frei Willig' }, { id: 'u-2', name: 'Vera Waltung' }] } },
      },
      stubList(qualsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Kettensäge')
    const row = screen.getByText('Kettensäge').closest('li')
    if (!row) throw new Error('Kettensäge row not found')
    await user.click(within(row as HTMLElement).getByRole('button', { name: 'Zuweisung' }))

    // The editor pre-checks the current assignee (Frei Willig).
    expect(await screen.findByRole('heading', { name: 'Zugewiesen an „Kettensäge“' })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Frei Willig' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Vera Waltung' })).not.toBeChecked()

    // Assign the second volunteer and save — the set is replaced atomically.
    await user.click(screen.getByRole('checkbox', { name: 'Vera Waltung' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.')).toBeInTheDocument()
    const assignCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1/assignees` && init?.method === 'POST')
    expect(assignCall).toBeTruthy()
    expect(JSON.parse((assignCall![1] as RequestInit).body as string)).toEqual({ user_ids: ['u-1', 'u-2'] })
  })

  it('ASSIGN_REMOVE: unchecking every volunteer sends an empty set (immediate revocation)', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(qualsFixture()),
      {
        matcher: (url, init) => !init?.method && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { assignees: [{ id: 'u-1', name: 'Frei Willig' }] } },
      },
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { message: 'Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.', assignees: [] } },
      },
      stubList(qualsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Kettensäge')
    const row = screen.getByText('Kettensäge').closest('li')
    if (!row) throw new Error('Kettensäge row not found')
    await user.click(within(row as HTMLElement).getByRole('button', { name: 'Zuweisung' }))
    await screen.findByRole('heading', { name: 'Zugewiesen an „Kettensäge“' })

    await user.click(screen.getByRole('checkbox', { name: 'Frei Willig' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    const assignCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1/assignees` && init?.method === 'POST')
    expect(assignCall).toBeTruthy()
    expect(JSON.parse((assignCall![1] as RequestInit).body as string)).toEqual({ user_ids: [] })
  })

  it('EMPTY: an empty vocabulary shows the German empty state', async () => {
    stubFetchRoutes([stubList({ qualifications: [], users: [] })])
    renderPage()

    expect(await screen.findByText('Noch keine Qualifikationen vorhanden.')).toBeInTheDocument()
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === QUALS_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('SAVE_FORBIDDEN: a 403 on save clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubList(qualsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Kettensäge')
    await user.click(screen.getByRole('button', { name: 'Neue Qualifikation' }))
    await screen.findByRole('heading', { name: 'Neue Qualifikation' })
    await user.type(screen.getByLabelText('Name'), 'Seilwinde')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed list fetch shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === QUALS_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Qualifikationen konnten nicht geladen werden.')
  })

  it('STALE_ERROR_CLEARED: a failed load followed by a successful reload clears the old error (finding 3)', async () => {
    let listCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && !init?.method && (listCalls++, true),
        response: () => (listCalls === 1
          ? { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } }
          : {
              ok: true, status: 200,
              body: { ...qualsFixture(), qualifications: [...qualsFixture().qualifications, { id: 'q-9', name: 'Seilwinde', description: '', expiry_kind: 'unlimited', status: 'unlimited' }] },
            }),
      },
      {
        matcher: (url, init) => init?.method === 'POST' && url === QUALS_URL,
        response: { ok: true, status: 201, body: { message: 'Qualifikation erstellt.', qualification: { id: 'q-9', name: 'Seilwinde', description: '', expiry_kind: 'unlimited', status: 'unlimited' } } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    // First load fails → the German error banner shows.
    expect(await screen.findByRole('alert')).toHaveTextContent('Qualifikationen konnten nicht geladen werden.')

    // Create a qualification → the save reloads the list successfully and the
    // stale error banner must disappear.
    await user.click(screen.getByRole('button', { name: 'Neue Qualifikation' }))
    await screen.findByRole('heading', { name: 'Neue Qualifikation' })
    await user.type(screen.getByLabelText('Name'), 'Seilwinde')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Seilwinde')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('UNAUTHORIZED: a 401 on load clears auth state and redirects to /login (finding 11)', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    localStorage.setItem('gear.permissions', JSON.stringify(['qualifications.manage']))
    stubFetchRoutes([
      { matcher: (url) => url === QUALS_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })
})