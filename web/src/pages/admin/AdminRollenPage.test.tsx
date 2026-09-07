// @vitest-environment jsdom
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminRollenPage } from './AdminRollenPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const GROUPS_URL = '/api/v1/admin/groups'

const CATALOG = [
  { code: 'dashboard.view', label: 'Dashboard ansehen' },
  { code: 'tools.manage', label: 'Geräte verwalten' },
]

function groupsFixture() {
  return {
    groups: [
      { id: 'g-helfende', name: 'helfende', description: 'Prüft Geräte', is_base_role: true, permissions: ['dashboard.view'] },
      { id: 'g-gerat', name: 'gerätewart', description: '', is_base_role: false, permissions: ['tools.manage'] },
    ],
    available_permissions: CATALOG,
  }
}

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/rollen']}>
        <Routes>
          <Route path="/admin/rollen" element={<AdminRollenPage />} />
          <Route path="/" element={<div>Dashboard</div>} />
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

const stubList = (body: unknown) => ({
  matcher: (url: string) => url === GROUPS_URL,
  response: { ok: true, status: 200, body },
})

const stubPut = (id: string, response: { ok: boolean; status: number; body: unknown }) => ({
  matcher: (url: string, init?: RequestInit) => init?.method === 'PUT' && url === `${GROUPS_URL}/${id}`,
  response,
})

describe('AdminRollenPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    // Admin with create + edit + assign (the full Rollen surface).
    localStorage.setItem('gear.permissions', JSON.stringify(['roles.create', 'roles.edit', 'roles.assign']))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders each role with a base-role badge, permission count and edit CTA', async () => {
    stubFetchRoutes([stubList(groupsFixture())])
    renderPage()

    expect(await screen.findByText('helfende')).toBeInTheDocument()
    expect(screen.getByText('Basisrolle')).toBeInTheDocument()
    expect(screen.getAllByText('1 Berechtigung')).toHaveLength(2)
    expect(screen.getByText('gerätewart')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Bearbeiten' })).toHaveLength(2)
    // "Neue Rolle" is present for a roles.create holder.
    expect(screen.getByRole('button', { name: 'Neue Rolle' })).toBeInTheDocument()
  })

  it('LIST_FETCH: fetch uses the authenticated admin headers', async () => {
    const fetchMock = stubFetchRoutes([stubList(groupsFixture())])
    renderPage()
    await screen.findByText('helfende')

    expect(fetchMock).toHaveBeenCalledWith(GROUPS_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('NEW_ROLE: clicking Neue Rolle opens the editor, creating a role refreshes the list', async () => {
    let listCalls = 0
    stubFetchRoutes([
      {
        matcher: (url, init) => url === GROUPS_URL && !init?.method && (listCalls++, true),
        response: {
          ok: true, status: 200,
          body: listCalls === 1
            ? groupsFixture()
            : { ...groupsFixture(), groups: [...groupsFixture().groups, { id: 'g-neu', name: 'kassierer', description: '', is_base_role: false, permissions: ['tools.manage'] }] },
        },
      },
      {
        matcher: (url, init) => url === GROUPS_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { id: 'g-neu', name: 'kassierer', description: '', is_base_role: false, permissions: ['tools.manage'] } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('helfende')
    await user.click(screen.getByRole('button', { name: 'Neue Rolle' }))

    expect(await screen.findByRole('heading', { name: 'Neue Rolle' })).toBeInTheDocument()
    await user.type(screen.getByLabelText('Name'), 'kassierer')
    await user.click(screen.getByRole('checkbox', { name: 'Geräte verwalten' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    // Success feedback and the refreshed list include the new role.
    expect(await screen.findByText('Rolle erstellt.')).toBeInTheDocument()
    expect(await screen.findByText('kassierer')).toBeInTheDocument()
  })

  it('EDIT: editing a role saves via PUT and shows success feedback', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(groupsFixture()),
      stubPut('g-helfende', { ok: true, status: 200, body: { id: 'g-helfende', name: 'helfende', description: 'Basis', is_base_role: true, permissions: ['tools.manage'] } }),
      stubList(groupsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('helfende')
    const helfendeRow = screen.getByText('helfende').closest('li')
    if (!helfendeRow) throw new Error('helfende row not found')
    await user.click(within(helfendeRow as HTMLElement).getByRole('button', { name: 'Bearbeiten' }))

    expect(await screen.findByRole('heading', { name: 'Rolle „helfende“ bearbeiten' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Rolle gespeichert.')).toBeInTheDocument()
    const putCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${GROUPS_URL}/g-helfende` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
  })

  it('ASSIGN_ONLY: a caller holding only roles.assign can view the list but sees no Neue Rolle / Bearbeiten', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['roles.assign']))
    stubFetchRoutes([stubList(groupsFixture())])
    renderPage()

    await screen.findByText('helfende')
    expect(screen.queryByRole('button', { name: 'Neue Rolle' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bearbeiten' })).not.toBeInTheDocument()
  })

  it('CREATE_ONLY: a roles.create-only holder sees Neue Rolle but NOT Bearbeiten', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['roles.create']))
    stubFetchRoutes([stubList(groupsFixture())])
    renderPage()

    await screen.findByText('helfende')
    expect(screen.getByRole('button', { name: 'Neue Rolle' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bearbeiten' })).not.toBeInTheDocument()
  })

  it('EDIT_ONLY: a roles.edit-only holder sees Bearbeiten but NOT Neue Rolle', async () => {
    localStorage.setItem('gear.permissions', JSON.stringify(['roles.edit']))
    stubFetchRoutes([stubList(groupsFixture())])
    renderPage()

    await screen.findByText('helfende')
    expect(screen.getAllByRole('button', { name: 'Bearbeiten' })).toHaveLength(2)
    expect(screen.queryByRole('button', { name: 'Neue Rolle' })).not.toBeInTheDocument()
  })

  it('CATALOG_FALLBACK: an empty server catalog falls back to the shipped 21-code grid', async () => {
    stubFetchRoutes([
      {
        matcher: (url) => url === GROUPS_URL,
        response: { ok: true, status: 200, body: { groups: groupsFixture().groups, available_permissions: [] } },
      },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('helfende')
    await user.click(screen.getByRole('button', { name: 'Neue Rolle' }))

    // A label that exists only in the shipped fallback (not the 2-entry test
    // catalog), plus the full 21-code grid.
    expect(await screen.findByRole('checkbox', { name: 'Rollen zuweisen' })).toBeInTheDocument()
    expect(screen.getAllByRole('checkbox')).toHaveLength(21)
  })

  it('FORBIDDEN: a 403 on load clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      { matcher: (url) => url === GROUPS_URL, response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } } },
    ])
    renderPage()

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('SAVE_FORBIDDEN: a 403 on save clears the admin flag and leaves the admin module', async () => {
    localStorage.setItem('gear.is_admin', 'true')
    stubFetchRoutes([
      {
        matcher: (url, init) => url === GROUPS_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
      stubList(groupsFixture()),
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('helfende')
    await user.click(screen.getByRole('button', { name: 'Neue Rolle' }))
    await screen.findByRole('heading', { name: 'Neue Rolle' })
    await user.type(screen.getByLabelText('Name'), 'kassierer')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('LOAD_ERROR: a failed list fetch shows a German inline error', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === GROUPS_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Rollen konnten nicht geladen werden.')
  })
})
