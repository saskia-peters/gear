// @vitest-environment jsdom
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminBenutzerPage } from './AdminBenutzerPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const USERS_URL = '/api/v1/admin/users'
const USER_GROUPS_URL = '/api/v1/admin/user-groups'
const GROUPS_URL = '/api/v1/admin/groups'

// The user list defaults to the aktiv filter, so listUsers is called with
// ?status=active unless the caller switches to Alle.

function usersFixture() {
  return {
    users: [
      { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active', user_groups: ['Gruppe Ost'] },
      { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local', status: 'pending_approval', user_groups: [] },
      { id: 'u-gone', vorname: 'Max', nachname: 'Gone', email: 'max@gear.local', status: 'deactivated', user_groups: [] },
    ],
  }
}

function detailFixture() {
  return {
    id: 'u-tim',
    vorname: 'Tim',
    nachname: 'Müller',
    email: 'tim@gear.local',
    status: 'active',
    roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }],
    user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }],
    direct_grants: [{ permission_id: 'p-1', code: 'report.export', granted_at: '' }],
    qualifications: [{ id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', assigned_at: '', status: 'valid' }],
    resolved_permissions: [
      { code: 'report.export', source_kind: 'direct', source_name: 'direct' },
    ],
  }
}

function groupsFixture() {
  return { user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost', description: '', created_at: '' }] }
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/benutzer']}>
        <Routes>
          <Route path="/admin/benutzer" element={<AdminBenutzerPage />} />
          <Route path="/admin/benutzer/pending" element={<div>PendingPage</div>} />
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/login" element={<div>LoginPage</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function stubFetchRoutes(routes: Array<{
  matcher: (url: string, init?: RequestInit) => boolean
  response:
    | { ok: boolean; status: number; body: unknown }
    | (() => { ok: boolean; status: number; body: unknown })
}>) {
  const mock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    const hit = routes.find((r) => r.matcher(url, init))
    const res = typeof hit?.response === 'function' ? hit.response() : hit?.response
    const final = res ?? { ok: false, status: 404, body: { error: { code: 'not_found', message: 'nope' } } }
    return { ok: final.ok, status: final.status, json: async () => final.body }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

// The default list fetch (aktiv filter). Matches the query string too.
const stubList = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === `${USERS_URL}?status=active` && !init?.method,
  response: { ok: true, status: 200, body },
})

// The "Alle" list fetch (no status query).
const stubListAll = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === USERS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubGroups = () => ({
  matcher: (url: string) => url === USER_GROUPS_URL,
  response: { ok: true, status: 200, body: groupsFixture() },
})

const stubDetail = (id: string, response: { ok: boolean; status: number; body: unknown }) => ({
  matcher: (url: string) => url === `${USERS_URL}/${id}`,
  response,
})

describe('AdminBenutzerPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    // Admin with users.manage + user_groups.manage + qualifications (the full
    // Benutzer surface).
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage', 'user_groups.manage', 'users.qualifications.manage', 'qualifications.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders a sortable spreadsheet table with the default aktiv filter and inline group tags', async () => {
    stubFetchRoutes([stubList(usersFixture()), stubGroups()])
    renderPage()

    // Default filter = aktiv: the list fetch carries ?status=active (the server
    // filters; the client renders what it gets).
    expect(await screen.findByText('Tim')).toBeInTheDocument()
    expect(screen.getByText('Müller')).toBeInTheDocument()
    expect(screen.getByText('aktiv')).toBeInTheDocument()
    // Inline group tag for the active user.
    expect(screen.getAllByText('Gruppe Ost').length).toBeGreaterThan(0)
    // Table header (real table semantics).
    expect(screen.getByRole('columnheader', { name: /Vorname/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /E-Mail/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Benutzer' })).toBeInTheDocument()
  })

  it('LIST_FETCH: the default fetch uses the authenticated admin headers and the aktiv status query', async () => {
    const fetchMock = stubFetchRoutes([stubList(usersFixture()), stubGroups()])
    renderPage()
    await screen.findByText('Tim')

    expect(fetchMock).toHaveBeenCalledWith(`${USERS_URL}?status=active`, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('FILTER_ALL: switching to Alle shows every user (no status query)', async () => {
    stubFetchRoutes([
      stubList(usersFixture()),
      stubListAll(usersFixture()),
      stubGroups(),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim')
    await user.click(screen.getByRole('button', { name: 'Alle' }))

    expect(await screen.findByText('Lena')).toBeInTheDocument()
    expect(screen.getByText('Max')).toBeInTheDocument()
  })

  it('SORT_EMAIL: clicking the E-Mail header sorts the rows', async () => {
    stubFetchRoutes([stubList(usersFixture()), stubGroups()])
    renderPage()
    await screen.findByText('Tim')

    const rows = screen.getAllByRole('row')
    const firstRow = within(rows[1])
    expect(firstRow.getByText('tim@gear.local')).toBeInTheDocument()
  })

  it('DETAIL: opening a user shows roles, user groups, direct grants, qualification status and the provenance view', async () => {
    stubFetchRoutes([stubList(usersFixture()), stubGroups(), stubDetail('u-tim', { ok: true, status: 200, body: detailFixture() })])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim')
    await user.click(screen.getByText('Tim').closest('tr')!)

    expect(await screen.findByText('helfende')).toBeInTheDocument()
    expect(screen.getByText('Gruppe Ost')).toBeInTheDocument()
    expect(screen.getAllByText('Berichte exportieren').length).toBeGreaterThan(0)
    expect(screen.getByText('Kettensäge')).toBeInTheDocument()
    expect(screen.getByText('Gültig')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Deaktivieren' })).toBeInTheDocument()
    // Collapsed provenance view with its source.
    expect(screen.getByText('Alle Berechtigungen')).toBeInTheDocument()
  })

  it('DEACTIVATE: the confirmed deactivate flow posts and refreshes the list', async () => {
    stubFetchRoutes([
      stubList(usersFixture()),
      stubGroups(),
      stubDetail('u-tim', { ok: true, status: 200, body: detailFixture() }),
      { matcher: (url, init) => url === `${USERS_URL}/u-tim/deactivate` && init?.method === 'POST', response: { ok: true, status: 200, body: { message: 'Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.', user_id: 'u-tim', email: 'tim@gear.local' } } },
      stubList({ users: usersFixture().users.map((u) => (u.id === 'u-tim' ? { ...u, status: 'deactivated' } : u)) }),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim')
    await user.click(screen.getByText('Tim').closest('tr')!)
    await screen.findByText('helfende')
    await user.click(screen.getByRole('button', { name: 'Deaktivieren' }))
    await user.click(screen.getByRole('button', { name: 'Ja, deaktivieren' }))

    expect(await screen.findByText('Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.')).toBeInTheDocument()
  })

  it('NEW_USER: creating a user refreshes the list with the server success message', async () => {
    let listCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === `${USERS_URL}?status=active` && !init?.method && (listCalls++, true),
        response: {
          ok: true, status: 200,
          body: listCalls === 1
            ? usersFixture()
            : { users: [...usersFixture().users, { id: 'u-neu', vorname: 'Anna', nachname: 'Neu', email: 'anna@gear.local', status: 'active', user_groups: [] }] },
        },
      },
      stubGroups(),
      { matcher: (url, init) => url === USERS_URL && init?.method === 'POST', response: { ok: true, status: 201, body: { message: 'Benutzer angelegt. Zugangsdaten werden separat vergeben.', user: detailFixture() } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim')
    await user.click(screen.getByRole('button', { name: 'Neuer Benutzer' }))
    await screen.findByRole('heading', { name: 'Neuer Benutzer' })
    await user.type(screen.getByLabelText('Vorname'), 'Anna')
    await user.type(screen.getByLabelText('Nachname'), 'Neu')
    await user.type(screen.getByLabelText('E-Mail'), 'anna@gear.local')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Benutzer angelegt. Zugangsdaten werden separat vergeben.')).toBeInTheDocument()
    expect(await screen.findByText('Anna')).toBeInTheDocument()
  })

  it('PENDING_BUTTON: a users.approve holder sees the "Ausstehende Anträge" button and it opens the pending page', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage', 'users.approve']))
    stubFetchRoutes([stubList(usersFixture()), stubGroups()])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim')
    const pendingButton = screen.getByRole('button', { name: 'Ausstehende Anträge' })
    await user.click(pendingButton)

    expect(await screen.findByText('PendingPage')).toBeInTheDocument()
  })

  it('PENDING_BUTTON_HIDDEN: without users.approve the pending button is not rendered', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage']))
    stubFetchRoutes([stubList(usersFixture()), stubGroups()])
    renderPage()

    await screen.findByText('Tim')
    expect(screen.queryByRole('button', { name: 'Ausstehende Anträge' })).not.toBeInTheDocument()
  })

  it('VIEW_ONLY: a users.view-only caller sees the list but no create CTA', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view']))
    stubFetchRoutes([stubList(usersFixture())])
    renderPage()

    await screen.findByText('Tim')
    expect(screen.queryByRole('button', { name: 'Neuer Benutzer' })).not.toBeInTheDocument()
  })

  it('NO_GROUPS_FETCH: a caller without user_groups.manage never fetches the group list', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage']))
    const fetchMock = stubFetchRoutes([stubList(usersFixture())])
    renderPage()

    await screen.findByText('Tim')
    const groupFetches = fetchMock.mock.calls.filter(([url]) => url === USER_GROUPS_URL)
    expect(groupFetches).toHaveLength(0)
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === `${USERS_URL}?status=active`, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed list fetch shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === `${USERS_URL}?status=active`, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Benutzer konnten nicht geladen werden.')
  })

  it('401_EXPIRED: a 401 on load clears auth state and redirects to /login', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === `${USERS_URL}?status=active`, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('LoginPage')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('VIEW_ONLY_NO_ROLES: a users.view holder without roles.* sees the list and no editor data fetch is attempted', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view']))
    const fetchMock = stubFetchRoutes([stubList(usersFixture())])
    renderPage()

    expect(await screen.findByText('Tim')).toBeInTheDocument()
    const roleFetches = fetchMock.mock.calls.filter(([url]) => url === GROUPS_URL)
    expect(roleFetches).toHaveLength(0)
    const groupFetches = fetchMock.mock.calls.filter(([url]) => url === USER_GROUPS_URL)
    expect(groupFetches).toHaveLength(0)
  })

  it('UNKNOWN_STATUS: an unrecognized status renders the raw value, never "undefined"', async () => {
    stubFetchRoutes([
      {
        matcher: (url) => url === `${USERS_URL}?status=active`,
        response: { ok: true, status: 200, body: { users: [{ id: 'u-x', vorname: 'X', nachname: 'Y', email: 'x@gear.local', status: 'weird_state', user_groups: [] }] } },
      },
      stubGroups(),
    ])
    renderPage()

    expect(await screen.findByText('X')).toBeInTheDocument()
    expect(screen.queryByText('undefined')).not.toBeInTheDocument()
    expect(screen.getByText('weird_state')).toBeInTheDocument()
  })
})