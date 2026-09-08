// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminBenutzergruppenPage } from './AdminBenutzergruppenPage.tsx'
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
      { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active', user_groups: ['Gruppe Ost'] },
      { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local', status: 'pending_approval', user_groups: [] },
      { id: 'u-gone', vorname: 'Max', nachname: 'Gone', email: 'max@gear.local', status: 'deactivated', user_groups: [] },
    ],
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
      <MemoryRouter initialEntries={['/admin/benutzergruppen']}>
        <Routes>
          <Route path="/admin/benutzergruppen" element={<AdminBenutzergruppenPage />} />
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

const stubGroups = (body: unknown = groupsFixture()) => ({
  matcher: (url: string, init?: RequestInit) => url === USER_GROUPS_URL && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubUsers = () => ({
  matcher: (url: string, init?: RequestInit) => url === USERS_URL && !init?.method,
  response: { ok: true, status: 200, body: usersFixture() },
})

const stubRoles = () => ({
  matcher: (url: string) => url === GROUPS_URL,
  response: { ok: true, status: 200, body: rolesFixture() },
})

describe('AdminBenutzergruppenPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    // A user_groups.manage holder reaches the page.
    localStorage.setItem('gear.permissions', JSON.stringify(['user_groups.manage']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders the group list with member/role/delete actions', async () => {
    stubFetchRoutes([stubGroups(), stubUsers(), stubRoles()])
    renderPage()

    expect(await screen.findByText('Gruppe Ost')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mitglieder' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rollen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Löschen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Erstellen' })).toBeInTheDocument()
  })

  it('EMPTY: no groups renders the empty state', async () => {
    stubFetchRoutes([stubGroups({ user_groups: [] }), stubUsers(), stubRoles()])
    renderPage()

    expect(await screen.findByText('Noch keine Benutzergruppen.')).toBeInTheDocument()
  })

  it('GROUP_CREATE: creating a user group posts and shows the new group', async () => {
    let groupCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === USER_GROUPS_URL && !init?.method && (groupCalls++, true),
        response: () => ({
          ok: true, status: 200,
          body: groupCalls === 1 ? groupsFixture() : { user_groups: [...groupsFixture().user_groups, { id: 'ug-neu', name: 'Gruppe West', description: '', created_at: '' }] },
        }),
      },
      stubUsers(),
      stubRoles(),
      { matcher: (url, init) => url === USER_GROUPS_URL && init?.method === 'POST', response: { ok: true, status: 201, body: { id: 'ug-neu', name: 'Gruppe West', description: '', created_at: '' } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Gruppe Ost')
    await user.type(screen.getByLabelText('Neue Benutzergruppe'), 'Gruppe West')
    await user.click(screen.getByRole('button', { name: 'Erstellen' }))

    expect(await screen.findByText('Benutzergruppe „Gruppe West“ erstellt.')).toBeInTheDocument()
    expect(await screen.findByText('Gruppe West')).toBeInTheDocument()
  })

  it('GROUP_DELETE: deleting a group requires confirmation and posts DELETE', async () => {
    let groupCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === USER_GROUPS_URL && !init?.method && (groupCalls++, true),
        response: () => ({
          ok: true, status: 200,
          body: groupCalls === 1 ? groupsFixture() : { user_groups: [] },
        }),
      },
      stubUsers(),
      stubRoles(),
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost` && init?.method === 'DELETE', response: { ok: true, status: 200, body: { message: 'Benutzergruppe gelöscht.' } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Gruppe Ost')
    await user.click(screen.getByRole('button', { name: 'Löschen' }))
    expect(screen.getByText(/löschen\?/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Ja, löschen' }))

    expect(await screen.findByText('Benutzergruppe „Gruppe Ost“ gelöscht.')).toBeInTheDocument()
  })

  it('GROUP_MEMBERS: assigning members pre-checks current members and posts the set', async () => {
    stubFetchRoutes([
      stubGroups(),
      stubUsers(),
      stubRoles(),
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/members` && !init?.method, response: { ok: true, status: 200, body: { user_ids: ['u-tim'] } } },
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/members` && init?.method === 'POST', response: { ok: true, status: 200, body: { message: 'Mitglieder der Benutzergruppe aktualisiert.', group: groupsFixture().user_groups[0] } } },
      stubGroups(),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Gruppe Ost')
    await user.click(screen.getByRole('button', { name: 'Mitglieder' }))
    expect(await screen.findByText(/Mitglieder von „Gruppe Ost“/)).toBeInTheDocument()
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

  it('GROUP_ROLES: a user_groups.manage holder assigns roles to a group', async () => {
    stubFetchRoutes([
      stubGroups(),
      stubUsers(),
      stubRoles(),
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/roles` && !init?.method, response: { ok: true, status: 200, body: { roles: [] } } },
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/roles` && init?.method === 'POST', response: { ok: true, status: 200, body: { roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }] } } },
      stubGroups(),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Gruppe Ost')
    await user.click(screen.getByRole('button', { name: 'Rollen' }))
    expect(await screen.findByText(/Rollen von „Gruppe Ost“/)).toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: /helfende/ }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText(/Rollen von „Gruppe Ost“ aktualisiert/)).toBeInTheDocument()
    const postCalls = (vi.mocked(fetch).mock.calls as Array<[string, RequestInit?]>).filter(([u, init]) =>
      u === `${USER_GROUPS_URL}/ug-ost/roles` && init?.method === 'POST')
    expect(postCalls).toHaveLength(1)
    const body = JSON.parse(postCalls[0][1]!.body as string)
    expect(body.role_ids).toEqual(['g-helfende'])
  })

  it('GROUP_ROLES_WITHOUT_DIRECTORY: a user_groups.manage holder without users.view still sees groups and can open the roles editor (no users for members)', async () => {
    stubFetchRoutes([
      stubGroups(),
      { matcher: (url, init) => url === USERS_URL && !init?.method, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
      stubRoles(),
      { matcher: (url, init) => url === `${USER_GROUPS_URL}/ug-ost/roles` && !init?.method, response: { ok: true, status: 200, body: { roles: [] } } },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Gruppe Ost')
    await user.click(screen.getByRole('button', { name: 'Rollen' }))
    expect(await screen.findByText(/Rollen von „Gruppe Ost“/)).toBeInTheDocument()
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === USER_GROUPS_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed group fetch shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === USER_GROUPS_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Benutzergruppen konnten nicht geladen werden.')
  })

  it('401_EXPIRED: a 401 on load clears auth state and redirects to /login', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === USER_GROUPS_URL, response: { ok: false, status: 401, body: { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('LoginPage')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })
})