// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminBenutzerPage } from './AdminBenutzerPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const USERS_URL = '/api/v1/admin/users'
const USER_GROUPS_URL = '/api/v1/admin/user-groups'
const GROUPS_URL = '/api/v1/admin/groups'

const CATALOG = [
  { code: 'dashboard.view', label: 'Dashboard ansehen' },
  { code: 'report.export', label: 'Berichte exportieren' },
]

function usersFixture() {
  return {
    users: [
      { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active' },
      { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local', status: 'pending_approval' },
      { id: 'u-gone', vorname: 'Max', nachname: 'Gone', email: 'max@gear.local', status: 'deactivated' },
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
  }
}

function rolesFixture() {
  return {
    groups: [
      { id: 'g-helfende', name: 'helfende', description: 'Basis', is_base_role: true, permissions: ['dashboard.view'] },
    ],
    available_permissions: CATALOG,
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
          <Route path="/" element={<div>Dashboard</div>} />
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

const stubList = (body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === USERS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubRoles = () => ({
  matcher: (url: string) => url === GROUPS_URL,
  response: { ok: true, status: 200, body: rolesFixture() },
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
    // Admin with users.manage + user_groups.manage (the full Benutzer surface).
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage', 'user_groups.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders each user with a status badge and the create CTA', async () => {
    stubFetchRoutes([stubList(usersFixture()), stubRoles(), stubGroups()])
    renderPage()

    expect(await screen.findByText('Tim Müller')).toBeInTheDocument()
    expect(screen.getByText('Lena Schmidt')).toBeInTheDocument()
    expect(screen.getByText('Max Gone')).toBeInTheDocument()
    expect(screen.getAllByText('aktiv')).toHaveLength(1)
    expect(screen.getByText('pending')).toBeInTheDocument()
    expect(screen.getByText('deaktiviert')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Neuer Benutzer' })).toBeInTheDocument()
  })

  it('LIST_FETCH: fetch uses the authenticated admin headers', async () => {
    const fetchMock = stubFetchRoutes([stubList(usersFixture()), stubRoles(), stubGroups()])
    renderPage()
    await screen.findByText('Tim Müller')

    expect(fetchMock).toHaveBeenCalledWith(USERS_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('DETAIL: opening a user shows roles, user groups, direct grants and qualification status', async () => {
    stubFetchRoutes([stubList(usersFixture()), stubRoles(), stubGroups(), stubDetail('u-tim', { ok: true, status: 200, body: detailFixture() })])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.click(screen.getAllByRole('button', { name: 'Öffnen' })[0])

    expect(await screen.findByText('helfende')).toBeInTheDocument()
    expect(screen.getByText('Gruppe Ost')).toBeInTheDocument()
    expect(screen.getByText('Berichte exportieren')).toBeInTheDocument()
    expect(screen.getByText('Kettensäge')).toBeInTheDocument()
    expect(screen.getByText('Gültig')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Deaktivieren' })).toBeInTheDocument()
  })

  it('DEACTIVATE: the confirmed deactivate flow posts and refreshes the list', async () => {
    stubFetchRoutes([
      stubList(usersFixture()),
      stubRoles(),
      stubGroups(),
      stubDetail('u-tim', { ok: true, status: 200, body: detailFixture() }),
      { matcher: (url, init) => url === `${USERS_URL}/u-tim/deactivate` && init?.method === 'POST', response: { ok: true, status: 200, body: { message: 'Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.', user_id: 'u-tim', email: 'tim@gear.local' } } },
      stubList({ users: usersFixture().users.map((u) => (u.id === 'u-tim' ? { ...u, status: 'deactivated' } : u)) }),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.click(screen.getAllByRole('button', { name: 'Öffnen' })[0])
    await screen.findByText('helfende')
    await user.click(screen.getByRole('button', { name: 'Deaktivieren' }))
    await user.click(screen.getByRole('button', { name: 'Ja, deaktivieren' }))

    expect(await screen.findByText('Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.')).toBeInTheDocument()
    expect(await screen.findByText('deaktiviert')).toBeInTheDocument()
  })

  it('NEW_USER: creating a user refreshes the list with the server success message', async () => {
    let listCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === USERS_URL && !init?.method && (listCalls++, true),
        response: {
          ok: true, status: 200,
          body: listCalls === 1
            ? usersFixture()
            : { users: [...usersFixture().users, { id: 'u-neu', vorname: 'Anna', nachname: 'Neu', email: 'anna@gear.local', status: 'active' }] },
        },
      },
      stubRoles(),
      stubGroups(),
      { matcher: (url, init) => url === USERS_URL && init?.method === 'POST', response: { ok: true, status: 201, body: { message: 'Benutzer angelegt. Zugangsdaten werden separat vergeben.', user: detailFixture() } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.click(screen.getByRole('button', { name: 'Neuer Benutzer' }))
    await screen.findByRole('heading', { name: 'Neuer Benutzer' })
    await user.type(screen.getByLabelText('Vorname'), 'Anna')
    await user.type(screen.getByLabelText('Nachname'), 'Neu')
    await user.type(screen.getByLabelText('E-Mail'), 'anna@gear.local')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    // The server message is what the page shows (finding 8).
    expect(await screen.findByText('Benutzer angelegt. Zugangsdaten werden separat vergeben.')).toBeInTheDocument()
    expect(await screen.findByText('Anna Neu')).toBeInTheDocument()
  })

  it('VIEW_ONLY: a users.view-only caller sees the list but no create CTA', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view']))
    stubFetchRoutes([stubList(usersFixture()), stubRoles(), stubGroups()])
    renderPage()

    await screen.findByText('Tim Müller')
    expect(screen.queryByRole('button', { name: 'Neuer Benutzer' })).not.toBeInTheDocument()
  })

  it('NO_GROUPS: a caller without user_groups.manage sees no group-create section', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage']))
    stubFetchRoutes([stubList(usersFixture()), stubRoles(), stubGroups()])
    renderPage()

    await screen.findByText('Tim Müller')
    expect(screen.queryByRole('heading', { name: 'Benutzergruppen' })).not.toBeInTheDocument()
  })

  it('GROUP_CREATE: creating a user group posts and shows the new group', async () => {
    let groupCalls = 0
    stubFetchRoutes([
      stubList(usersFixture()),
      stubRoles(),
      {
        matcher: (url, init) => url === USER_GROUPS_URL && !init?.method && (groupCalls++, true),
        response: () => ({
          ok: true, status: 200,
          body: groupCalls === 1 ? groupsFixture() : { user_groups: [...groupsFixture().user_groups, { id: 'ug-neu', name: 'Gruppe West', description: '', created_at: '' }] },
        }),
      },
      { matcher: (url, init) => url === USER_GROUPS_URL && init?.method === 'POST', response: { ok: true, status: 201, body: { id: 'ug-neu', name: 'Gruppe West', description: '', created_at: '' } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.type(screen.getByLabelText('Neue Benutzergruppe'), 'Gruppe West')
    await user.click(screen.getByRole('button', { name: 'Erstellen' }))

    expect(await screen.findByText('Benutzergruppe „Gruppe West“ erstellt.')).toBeInTheDocument()
    expect(await screen.findByText('Gruppe West')).toBeInTheDocument()
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === USERS_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed list fetch shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === USERS_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Benutzer konnten nicht geladen werden.')
  })

  it('MANAGE_NO_GROUPS: a users.manage holder without user_groups.manage still sees the user list', async () => {
    // finding 2: the group fetch is gated on user_groups.manage — a users.manage
    // holder without it must NOT have its whole page aborted by a group 403.
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view', 'users.manage']))
    const fetchMock = stubFetchRoutes([stubList(usersFixture())])
    renderPage()

    expect(await screen.findByText('Tim Müller')).toBeInTheDocument()
    expect(screen.getByText('Lena Schmidt')).toBeInTheDocument()
    // No group section, no error alert, and no group fetch was attempted.
    expect(screen.queryByRole('heading', { name: 'Benutzergruppen' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    const groupFetches = fetchMock.mock.calls.filter(([url]) => url === USER_GROUPS_URL)
    expect(groupFetches).toHaveLength(0)
  })

  it('VIEW_ONLY_NO_ROLES: a users.view holder without roles.* sees the list and no editor data fetch is attempted', async () => {
    // finding 2: listRoles is gated on roles.* — a caller without any roles.*
    // code must not have the page aborted by a roles 403.
    localStorage.setItem('gear.permissions', JSON.stringify(['users.view']))
    const fetchMock = stubFetchRoutes([stubList(usersFixture())])
    renderPage()

    expect(await screen.findByText('Tim Müller')).toBeInTheDocument()
    const roleFetches = fetchMock.mock.calls.filter(([url]) => url === GROUPS_URL)
    expect(roleFetches).toHaveLength(0)
    const groupFetches = fetchMock.mock.calls.filter(([url]) => url === USER_GROUPS_URL)
    expect(groupFetches).toHaveLength(0)
  })

it('GROUP_DELETE: deleting a group requires confirmation and posts DELETE', async () => {
    let groupCalls = 0
    stubFetchRoutes([
      stubList(usersFixture()),
      stubRoles(),
      {
        matcher: (url, init) => url === USER_GROUPS_URL && !init?.method && (groupCalls++, true),
        response: () => ({
          ok: true, status: 200,
          body: groupCalls === 1 ? groupsFixture() : { user_groups: [] },
        }),
      },
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost` && init?.method === 'DELETE', response: { ok: true, status: 200, body: { message: 'Benutzergruppe gelöscht.' } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.click(screen.getByRole('button', { name: 'Löschen' }))
    expect(screen.getByText(/löschen\?/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Ja, löschen' }))

    expect(await screen.findByText('Benutzergruppe „Gruppe Ost“ gelöscht.')).toBeInTheDocument()
    expect(screen.queryByText('Gruppe Ost')).not.toBeInTheDocument()
  })

  it('GROUP_MEMBERS: assigning members pre-checks current members and posts the set', async () => {
    stubFetchRoutes([
      stubList(usersFixture()),
      stubRoles(),
      stubGroups(),
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/members` && !init?.method, response: { ok: true, status: 200, body: { user_ids: ['u-tim'] } } },
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/members` && init?.method === 'POST', response: { ok: true, status: 200, body: { message: 'Mitglieder der Benutzergruppe aktualisiert.', group: groupsFixture().user_groups[0] } } },
      stubGroups(),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Tim Müller')
    await user.click(screen.getByRole('button', { name: 'Mitglieder' }))
    expect(await screen.findByText(/Mitglieder von „Gruppe Ost“/)).toBeInTheDocument()
    // u-tim is pre-checked (current member); toggle Lena on.
    expect((screen.getByRole('checkbox', { name: /Tim Müller/ }) as HTMLInputElement).checked).toBe(true)
    await user.click(screen.getByRole('checkbox', { name: /Lena Schmidt/ }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Mitglieder von „Gruppe Ost“ aktualisiert.')).toBeInTheDocument()
    const postCalls = (vi.mocked(fetch).mock.calls as Array<[string, RequestInit?]>).filter(([u, init]) =>
      u === `${USER_GROUPS_URL}/ug-ost/members` && init?.method === 'POST')
    expect(postCalls).toHaveLength(1)
    const body = JSON.parse(postCalls[0][1]!.body as string)
    expect(body.user_ids).toEqual(expect.arrayContaining(['u-tim', 'u-lena']))
  })

  it('UNKNOWN_STATUS: an unrecognized status renders the raw value, never "undefined"', async () => {
    // finding 12: label lookups fall back to the raw value.
    stubFetchRoutes([
      {
        matcher: (url) => url === USERS_URL,
        response: { ok: true, status: 200, body: { users: [{ id: 'u-x', vorname: 'X', nachname: 'Y', email: 'x@gear.local', status: 'weird_state' }] } },
      },
      stubRoles(),
      stubGroups(),
    ])
    renderPage()

    expect(await screen.findByText('X Y')).toBeInTheDocument()
    expect(screen.queryByText('undefined')).not.toBeInTheDocument()
    expect(screen.getByText('weird_state')).toBeInTheDocument()
  })
})