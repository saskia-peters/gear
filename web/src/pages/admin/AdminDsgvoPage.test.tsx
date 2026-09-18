// @vitest-environment jsdom
import { render, screen, within, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminDsgvoPage } from './AdminDsgvoPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const PERMISSIONS_KEY = 'gear.permissions'
const USERS_URL = '/api/v1/admin/users'
const DSGVO_REPORT_URL = '/api/v1/admin/dsgvo/reports'
const DSGVO_DELETE_URL = '/api/v1/admin/dsgvo/users'
const DSGVO_DELETED_URL = '/api/v1/admin/dsgvo/users/deleted'

const DSGVO_ACCESS_REPORT = 'dsgvo.access_report'
const DSGVO_DELETE = 'dsgvo.delete'

function seedPermissions(codes: string[]) {
  localStorage.setItem(PERMISSIONS_KEY, JSON.stringify(codes))
}

function usersFixture() {
  return {
    users: [
      { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active', user_groups: [] },
      { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local', status: 'deactivated', user_groups: [] },
    ],
  }
}

function reportFixture(overrides?: Partial<{ sessions: unknown[]; loginAttempts: unknown; inspections: unknown[]; summary: unknown[] }>) {
  return {
    user: {
      profile: {
        id: 'u-tim',
        email: 'tim@gear.local',
        pending_email: 'neu@gear.local',
        display_name: 'Tim Müller',
        first_name: 'Tim',
        last_name: 'Müller',
        state: 'active',
        is_mfa_enabled: true,
        attributes: { geburtsjahr: '1990' },
        created_at: '2024-01-01T10:00:00Z',
        updated_at: '2026-01-01T10:00:00Z',
      },
      roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }],
      user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }],
      direct_grants: [{ permission_id: 'p-1', code: 'report.export', granted_at: '2024-01-01T10:00:00Z' }],
      qualifications: [
        { id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', assigned_at: '', status: 'valid' },
      ],
      sessions: overrides?.sessions ?? [
        { id: 'sess-1', created_at: '2026-09-01T10:00:00Z', expires_at: '2026-09-01T18:00:00Z' },
      ],
      login_attempts: overrides?.loginAttempts ?? {
        email: 'tim@gear.local',
        failed_count: 2,
        lockout_until: null,
        updated_at: '2026-09-01T09:00:00Z',
      },
    },
    tools: {
      inspections: overrides?.inspections ?? [
        {
          id: 'insp-1',
          tool_id: 'id-tool-a',
          tool_name: 'Bohrmaschine-01',
          mode: 'checklist',
          overall_result: 'fail',
          notes: 'Bohrfutter locker',
          submitted_at: '2026-09-01T08:00:00Z',
          items: [{ id: 'item-1', item_id: 'c-1', label: 'Kabel', position: 0, result: 'pass' }],
        },
      ],
      reinstatements: [{ id: 're-1', tool_id: 'id-tool-b', tool_name: 'Kettensäge-02', reason: 'Ersatzteil eingetroffen', created_at: '2026-09-02T08:00:00Z' }],
      summary: overrides?.summary ?? [
        { tool_id: 'id-tool-a', tool_name: 'Bohrmaschine-01', inspection_count: 1, fail_count: 1 },
      ],
    },
    generated_at: '2026-09-18T12:00:00Z',
  }
}

function emptyReportFixture() {
  return {
    user: {
      profile: {
        id: 'u-lena',
        email: 'lena@gear.local',
        display_name: 'Lena Schmidt',
        first_name: 'Lena',
        last_name: 'Schmidt',
        state: 'deactivated',
        is_mfa_enabled: false,
        attributes: {},
        created_at: '2024-01-01T10:00:00Z',
        updated_at: '2026-01-01T10:00:00Z',
      },
      roles: [],
      user_groups: [],
      direct_grants: [],
      qualifications: [],
      sessions: [],
      login_attempts: null,
    },
    tools: {
      inspections: [],
      reinstatements: [],
      summary: [],
    },
    generated_at: '2026-09-18T12:00:00Z',
  }
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

const stubUsers = () => ({
  matcher: (url: string, init?: RequestInit) => url === USERS_URL && !init?.method,
  response: { ok: true, status: 200, body: usersFixture() },
})

const stubReport = (id: string, body: unknown) => ({
  matcher: (url: string, init?: RequestInit) => url === `${DSGVO_REPORT_URL}/${id}` && !init?.method,
  response: { ok: true, status: 200, body },
})

const stubReportError = (id: string, status: number) => ({
  matcher: (url: string) => url === `${DSGVO_REPORT_URL}/${id}`,
  response: { ok: false, status, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
})

const stubDeletedList = (rows: unknown[]) => ({
  matcher: (url: string, init?: RequestInit) => url === DSGVO_DELETED_URL && !init?.method,
  response: { ok: true, status: 200, body: { accounts: rows } },
})

const stubDeleteUser = (id: string, body: unknown, status = 200) => ({
  matcher: (url: string, init?: RequestInit) =>
    url === `${DSGVO_DELETE_URL}/${id}/delete` && init?.method === 'POST',
  response: { ok: status === 200, status, body },
})

const stubPurge = (id: string, body: unknown, status = 200) => ({
  matcher: (url: string, init?: RequestInit) =>
    url === `${DSGVO_DELETED_URL}/${id}` && init?.method === 'DELETE',
  response: { ok: status === 200, status, body },
})

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/dsgvo']}>
        <Routes>
          <Route path="/admin/dsgvo" element={<AdminDsgvoPage />} />
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/login" element={<div>LoginPage</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

async function generateReportForTim() {
  const user = userEvent.setup()
  await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
  await user.click(screen.getByRole('button', { name: 'Bericht erstellen' }))
}

describe('AdminDsgvoPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('TABS_GATED: only the tabs the resolved set allows render', async () => {
    // A dsgvo.access_report-only holder sees Datenauskunft, NOT Konto löschen.
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers()])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })
    expect(screen.getByRole('tab', { name: 'Datenauskunft' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Konto löschen' })).not.toBeInTheDocument()

    cleanup()
    vi.unstubAllGlobals()

    // A dsgvo.delete-only holder sees Konto löschen (the two-step delete flow)
    // and NOT Datenauskunft.
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([stubUsers(), stubDeletedList([])])
    renderPage()
    await screen.findByRole('tab', { name: 'Konto löschen' })
    expect(screen.queryByRole('tab', { name: 'Datenauskunft' })).not.toBeInTheDocument()
    expect(screen.getByText('Unumkehrbar · Audit-Pflicht')).toBeInTheDocument()
    expect(screen.getByText('Keine gelöschten Konten.')).toBeInTheDocument()
  })

  it('REPORT_RENDER: a report holder picks a user, generates and renders all sections', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers(), stubReport('u-tim', reportFixture())])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    await generateReportForTim()

    const report = await screen.findByRole('region', { name: 'Datenauskunft' })
    expect(within(report).getByText('Datenauskunft für Tim Müller')).toBeInTheDocument()
    // Report header: the generation timestamp is rendered.
    expect(within(report).getByText(/Erstellt am/)).toBeInTheDocument()
    // Profile section.
    expect(within(report).getByText('tim@gear.local')).toBeInTheDocument()
    // A staged email change is data the org holds → rendered when present.
    expect(within(report).getByText('neu@gear.local')).toBeInTheDocument()
    expect(within(report).getByText('MFA aktiv')).toBeInTheDocument()
    // Roles / groups / grants.
    expect(within(report).getByText(/helfende/)).toBeInTheDocument()
    expect(within(report).getByText(/Gruppe Ost/)).toBeInTheDocument()
    expect(within(report).getByText(/report\.export/)).toBeInTheDocument()
    // Qualifications.
    expect(within(report).getByText('Kettensäge')).toBeInTheDocument()
    // Auth history (the session row + the qualification row both carry "gültig
    // bis" — assert at least one renders).
    expect(within(report).getAllByText(/gültig bis/).length).toBeGreaterThan(0)
    expect(within(report).getByText(/2 fehlgeschlagen/)).toBeInTheDocument()
    // Inspection summary + per-inspection rows.
    expect(within(report).getByText('Bohrmaschine-01 · fehlgeschlagen')).toBeInTheDocument()
    expect(within(report).getByText(/1 Prüfungen · 1 fehlgeschlagen/)).toBeInTheDocument()
    expect(within(report).getByText(/Kettensäge-02/)).toBeInTheDocument()
  })

  it('EMPTY_SECTIONS: no sessions/attempts/inspections render German empty notes, never an error', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers(), stubReport('u-lena', emptyReportFixture())])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-lena')
    await user.click(screen.getByRole('button', { name: 'Bericht erstellen' }))

    const report = await screen.findByRole('region', { name: 'Datenauskunft' })
    expect(within(report).getByText('Keine Sitzungen.')).toBeInTheDocument()
    expect(within(report).getByText('Keine Anmeldeversuche.')).toBeInTheDocument()
    expect(within(report).getByText('Keine Prüfungen.')).toBeInTheDocument()
    expect(within(report).getByText('Keine Prüfungen in der Zusammenfassung.')).toBeInTheDocument()
    expect(within(report).getByText('Keine Wiederherstellungen.')).toBeInTheDocument()
    expect(within(report).getByText('Keine Qualifikationen.')).toBeInTheDocument()
  })

  it('DOWNLOAD: the JSON download action produces the rendered report as a file', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers(), stubReport('u-tim', reportFixture())])

    // Stub the object-URL + anchor-click so the download is observable in jsdom.
    const createUrl = vi.fn(() => 'blob:dsgvo-report')
    const revokeUrl = vi.fn()
    vi.stubGlobal('URL', { ...URL, createObjectURL: createUrl, revokeObjectURL: revokeUrl })
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })
    await generateReportForTim()
    await screen.findByRole('region', { name: 'Datenauskunft' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Herunterladen (JSON)' }))

    expect(createUrl).toHaveBeenCalledTimes(1)
    expect(clickSpy).toHaveBeenCalledTimes(1)
    expect(revokeUrl).toHaveBeenCalledWith('blob:dsgvo-report')
    const anchor = clickSpy.mock.instances[0] as HTMLAnchorElement
    expect(anchor.download).toContain('dsgvo-datenauskunft')
  })

  it('403: a 403 report response leaves the admin module (no personal data)', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers(), stubReportError('u-tim', 403)])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    await generateReportForTim()
    await screen.findByText('Dashboard')
  })

  it('401: an expired session redirects to /login', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([stubUsers(), stubReportError('u-tim', 401)])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    await generateReportForTim()
    await screen.findByText('LoginPage')
  })

  it('NO_PERMISSION: a caller with neither DSGVO code sees a German notice, never a blank body', async () => {
    // Only reachable if the route gating drifts (the route already requires one
    // DSGVO code) — the page must still render a notice.
    seedPermissions(['dashboard.view'])
    stubFetchRoutes([stubUsers()])
    renderPage()

    expect(await screen.findByText('Keine Berechtigung für die DSGVO-Funktionen.')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Datenauskunft' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Konto löschen' })).not.toBeInTheDocument()
  })

  it('NULL_SECTIONS: a defensive report with null user/tools renders a German notice, never crashes', async () => {
    seedPermissions([DSGVO_ACCESS_REPORT])
    stubFetchRoutes([
      stubUsers(),
      stubReport('u-tim', { user: null, tools: null, generated_at: '2026-09-18T12:00:00Z' }),
    ])
    renderPage()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    await generateReportForTim()

    expect(
      await screen.findByText('Der Bericht ist unvollständig und kann nicht angezeigt werden.'),
    ).toBeInTheDocument()
  })

  // --- Story 3.4: the two-step delete flow + purge list ---------------------

  async function openDeleteTab() {
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Konto löschen' }))
    return user
  }

  it('PICKER_OMITS_TOMBSTONE: a deleted tombstone in the payload is never offered as a delete target', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([
      {
        matcher: (url: string, init?: RequestInit) => url === USERS_URL && !init?.method,
        response: {
          ok: true,
          status: 200,
          body: {
            users: [
              { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active', user_groups: [] },
              // A tombstone leaks into the payload (e.g. a stale cache): the
              // picker must defensively omit it (Story 3.4 Never rule).
              { id: 'u-dead', vorname: 'Gelöscht', nachname: 'Person', email: 'deleted.u-dead@deleted.local', status: 'deleted', user_groups: [] },
            ],
          },
        },
      },
      stubDeletedList([]),
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    expect(screen.queryByRole('option', { name: /Gelöscht Person/ })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Tim Müller/ })).toBeInTheDocument()
  })

  it('SPA_TWO_STEP: the delete button stays disabled until the name matches AND the reason is non-empty', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([stubUsers(), stubDeletedList([])])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    // No user selected yet → the delete button is not even rendered.
    expect(screen.queryByRole('button', { name: 'Endgültig löschen' })).not.toBeInTheDocument()

    // Pick Tim → the exact name is revealed; the button stays disabled until
    // BOTH the name AND the reason are valid.
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    expect(screen.getByText('Tim Müller')).toBeInTheDocument()
    const deleteBtn = screen.getByRole('button', { name: 'Endgültig löschen' })
    expect(deleteBtn).toBeDisabled()

    // Only the reason filled → still disabled.
    await user.type(screen.getByLabelText('Begründung'), 'Doppelkonto')
    expect(deleteBtn).toBeDisabled()

    // Name typed EXACTLY + reason → enabled.
    await user.type(screen.getByLabelText('Name bestätigen'), 'Tim Müller')
    expect(deleteBtn).toBeEnabled()
  })

  it('SPA_NAME_MISMATCH: a wrong typed name shows an inline German error and blocks deletion', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([stubUsers(), stubDeletedList([])])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    const nameInput = screen.getByLabelText('Name bestätigen')
    await user.type(nameInput, 'Tim Falsch')
    expect(screen.getByRole('alert')).toHaveTextContent('Der Name stimmt nicht überein.')
    expect(screen.getByRole('button', { name: 'Endgültig löschen' })).toBeDisabled()
  })

  it('SPA_EMPTY_REASON: an empty Begründung keeps the button disabled (mandatory)', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([stubUsers(), stubDeletedList([])])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    await user.type(screen.getByLabelText('Name bestätigen'), 'Tim Müller')
    expect(screen.getByRole('button', { name: 'Endgültig löschen' })).toBeDisabled()
  })

  it('DELETE_CONFIRM: a valid delete call removes the user from the picker, shows the server confirmation and the archived list refreshes', async () => {
    seedPermissions([DSGVO_DELETE])
    // The archived list is stateful: the FIRST GET is empty, and the delete
    // request pushes the archived row so the post-delete refresh GET returns it
    // (stubFetchRoutes matches the first route, so the list must be a function).
    const archivedRows: unknown[] = []
    const listStub = {
      matcher: (url: string, init?: RequestInit) => url === DSGVO_DELETED_URL && !init?.method,
      response: () => ({ ok: true, status: 200, body: { accounts: archivedRows } }),
    }
    const fetchMock = stubFetchRoutes([
      stubUsers(),
      listStub,
      {
        matcher: (url: string, init?: RequestInit) =>
          url === `${DSGVO_DELETE_URL}/u-tim/delete` && init?.method === 'POST',
        response: () => {
          archivedRows.push({ id: 'a-tim', email: 'tim@gear.local', display_name: 'Tim Müller', deleted_at: '2026-09-18T12:00:00Z', reason: 'Doppelkonto' })
          return { ok: true, status: 200, body: { message: 'Konto u-tim wurde gelöscht.' } }
        },
      },
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    await user.type(screen.getByLabelText('Name bestätigen'), 'Tim Müller')
    await user.type(screen.getByLabelText('Begründung'), 'Doppelkonto')
    await user.click(screen.getByRole('button', { name: 'Endgültig löschen' }))

    // The server confirmation renders and the archived account appears.
    expect(await screen.findByText('Konto u-tim wurde gelöscht.')).toBeInTheDocument()
    expect(await screen.findByText(/tim@gear\.local · gelöscht am/)).toBeInTheDocument()
    // The picker no longer offers Tim (option u-tim gone).
    expect(screen.queryByRole('option', { name: /Tim Müller/ })).not.toBeInTheDocument()
    // The delete request carried the trimmed reason via POST (the reason travels
    // in the body — never a DELETE with a stripped body).
    const deleteCall = fetchMock.mock.calls.find((c) => String(c[0]).includes('/users/u-tim/delete'))
    expect(deleteCall).toBeTruthy()
    expect(deleteCall?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(deleteCall?.[1]?.body))).toEqual({ reason: 'Doppelkonto' })
  })

  it('SPA_PURGE: "Jetzt endgültig löschen" confirms, purges, removes the row and shows the confirmation', async () => {
    seedPermissions([DSGVO_DELETE])
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    stubFetchRoutes([
      stubUsers(),
      stubDeletedList([{ id: 'a-tim', email: 'tim@gear.local', display_name: 'Tim Müller', deleted_at: '2026-09-18T12:00:00Z', reason: 'Doppelkonto' }]),
      stubPurge('a-tim', { message: 'Der gelöschte Account wurde endgültig gelöscht.' }),
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Jetzt endgültig löschen' }))

    expect(confirmSpy).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('Der gelöschte Account wurde endgültig gelöscht.')).toBeInTheDocument()
    expect(screen.getByText('Keine gelöschten Konten.')).toBeInTheDocument()
  })

  it('PURGE_CANCEL: a cancelled window.confirm performs no purge and keeps the row', async () => {
    seedPermissions([DSGVO_DELETE])
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    stubFetchRoutes([
      stubUsers(),
      stubDeletedList([{ id: 'a-tim', email: 'tim@gear.local', display_name: 'Tim Müller', deleted_at: '2026-09-18T12:00:00Z', reason: 'Doppelkonto' }]),
      stubPurge('a-tim', { message: 'x' }),
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Jetzt endgültig löschen' }))

    expect(confirmSpy).toHaveBeenCalledTimes(1)
    // The archived row is still listed (the option AND the row both carry the
    // email, so match all).
    expect(screen.getAllByText(/tim@gear\.local/).length).toBeGreaterThan(0)
  })

  it('DELETE_403: a 403 delete response leaves the admin module (no data exposed)', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([
      stubUsers(),
      stubDeletedList([]),
      stubDeleteUser('u-tim', { error: { code: 'forbidden', message: 'Keine Berechtigung.' } }, 403),
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    await user.type(screen.getByLabelText('Name bestätigen'), 'Tim Müller')
    await user.type(screen.getByLabelText('Begründung'), 'Doppelkonto')
    await user.click(screen.getByRole('button', { name: 'Endgültig löschen' }))

    await screen.findByText('Dashboard')
  })

  it('DELETE_401: an expired session on the delete flow redirects to /login', async () => {
    seedPermissions([DSGVO_DELETE])
    stubFetchRoutes([
      stubUsers(),
      stubDeletedList([]),
      stubDeleteUser('u-tim', { error: { code: 'unauthorized', message: 'x' } }, 401),
    ])
    renderPage()
    await openDeleteTab()
    await screen.findByRole('combobox', { name: 'Benutzer' })

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Benutzer'), 'u-tim')
    await user.type(screen.getByLabelText('Name bestätigen'), 'Tim Müller')
    await user.type(screen.getByLabelText('Begründung'), 'Doppelkonto')
    await user.click(screen.getByRole('button', { name: 'Endgültig löschen' }))

    await screen.findByText('LoginPage')
  })
})