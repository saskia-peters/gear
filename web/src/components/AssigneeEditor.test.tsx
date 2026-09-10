// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { AssigneeEditor } from './AssigneeEditor.tsx'
import type { Qualification, QualificationAssignee, QualificationRosterUser } from '../auth/qualifications.ts'

const QUALS_URL = '/api/v1/admin/qualifications'

const QUALIFICATION: Qualification = {
  id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'unlimited', status: 'unlimited',
}

const USERS: QualificationRosterUser[] = [
  { id: 'u-1', name: 'Frei Willig' },
  { id: 'u-2', name: 'Vera Waltung' },
]

const CURRENT: QualificationAssignee[] = [{ id: 'u-1', name: 'Frei Willig' }]

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

function renderEditor(users: QualificationRosterUser[] = USERS, assignees: QualificationAssignee[] = CURRENT, onSaved = vi.fn()) {
  const onCancel = vi.fn()
  const onForbidden = vi.fn()
  const utils = render(
    <AssigneeEditor
      qualification={QUALIFICATION}
      users={users}
      assignees={assignees}
      onSaved={onSaved}
      onCancel={onCancel}
      onForbidden={onForbidden}
    />,
  )
  return { onSaved, onCancel, onForbidden, ...utils }
}

describe('AssigneeEditor', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST_CHECKED: the current assignees are pre-checked from the server set', () => {
    renderEditor()

    expect(screen.getByRole('heading', { name: 'Zugewiesen an „Kettensäge“' })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Frei Willig' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Vera Waltung' })).not.toBeChecked()
  })

  it('ASSIGN: adding a volunteer POSTs the full replacement set', async () => {
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { message: 'Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.', assignees: [...CURRENT, { id: 'u-2', name: 'Vera Waltung' }] } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('checkbox', { name: 'Vera Waltung' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(onSaved).toHaveBeenCalled()
    const assignCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1/assignees` && init?.method === 'POST')
    expect(assignCall).toBeTruthy()
    expect(JSON.parse((assignCall![1] as RequestInit).body as string)).toEqual({ user_ids: ['u-1', 'u-2'] })
  })

  it('ASSIGN_REMOVE: unchecking the only volunteer POSTs an empty set (immediate revocation, AD-7/FR-22)', async () => {
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { message: 'Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.', assignees: [] } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('checkbox', { name: 'Frei Willig' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    const assignCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1/assignees` && init?.method === 'POST')
    expect(assignCall).toBeTruthy()
    expect(JSON.parse((assignCall![1] as RequestInit).body as string)).toEqual({ user_ids: [] })
    expect(onSaved).toHaveBeenCalled()
  })

  it('EMPTY_ROSTER: an empty roster shows the German empty state and no checkboxes', () => {
    renderEditor([], [])

    expect(screen.getByText('Es sind noch keine Personen vorhanden.')).toBeInTheDocument()
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
  })

  it('ASSIGNEE_NOT_IN_ROSTER: an assignee absent from the roster is still visible and can be unchecked (finding 4)', async () => {
    // u-2 is a current assignee but the (page-load-time) roster does not
    // contain her — she must still render as a checked checkbox so the admin
    // can see and remove her assignment instead of it being silently hidden.
    const rosterOnlyU1: QualificationRosterUser[] = [{ id: 'u-1', name: 'Frei Willig' }]
    const assigneesWithLate = [...CURRENT, { id: 'u-2', name: 'Vera Waltung' }]
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: true, status: 200, body: { message: 'Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort.', assignees: [] } },
      },
    ])
    const { onSaved } = renderEditor(rosterOnlyU1, assigneesWithLate)
    const user = userEvent.setup()

    // Both assignees render; the roster-only row and the late assignee row.
    expect(screen.getByRole('checkbox', { name: 'Frei Willig' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Vera Waltung' })).toBeChecked()

    // The late assignee can be unchecked and removed on save.
    await user.click(screen.getByRole('checkbox', { name: 'Vera Waltung' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    const assignCall = fetchMock.mock.calls.find(([url, init]) =>
      url === `${QUALS_URL}/q-1/assignees` && init?.method === 'POST')
    expect(assignCall).toBeTruthy()
    expect(JSON.parse((assignCall![1] as RequestInit).body as string)).toEqual({ user_ids: ['u-1'] })
    expect(onSaved).toHaveBeenCalled()
  })

  it('ERROR: a server 400 shows the German invalid-person message inline', async () => {
    stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: false, status: 400, body: { error: { code: 'invalid', message: 'Eine ausgewählte Person ist ungültig.' } } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Eine ausgewählte Person ist ungültig.')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('FORBIDDEN: a 403 on save invokes onForbidden', async () => {
    stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'POST' && url === `${QUALS_URL}/q-1/assignees`,
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const { onForbidden, onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(onForbidden).toHaveBeenCalled()
    expect(onSaved).not.toHaveBeenCalled()
  })
})