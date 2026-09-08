// @vitest-environment jsdom
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { UserDetail } from './UserDetail.tsx'
import type { AdminUserDetail, QualificationAssignment, UserGroup } from '../auth/users.ts'

function activeUser(): AdminUserDetail {
  return {
    id: 'u-tim',
    vorname: 'Tim',
    nachname: 'Müller',
    email: 'tim@gear.local',
    status: 'active',
    roles: [
      { id: 'g-helfende', name: 'helfende', is_base_role: true },
      { id: 'g-gerat', name: 'gerätewart', is_base_role: false },
    ],
    user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }],
    direct_grants: [{ permission_id: 'p-1', code: 'report.export', granted_at: '' }],
    qualifications: [
      { id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', assigned_at: '', status: 'valid' },
      { id: 'q-2', name: 'Generator', description: '', expiry_kind: 'unlimited', expires_at: null, assigned_at: '', status: 'unlimited' },
    ],
  }
}

function renderDetail(opts?: {
  user?: AdminUserDetail
  canManage?: boolean
  canManageQualifications?: boolean
  canManageGroups?: boolean
  userGroups?: UserGroup[]
  onEdit?: () => void
  onBack?: () => void
  onRefreshDetail?: () => void
  onDeactivated?: (message: string) => void
  onForbidden?: () => void
  onUnauthorized?: () => void
}) {
  return render(
    <UserDetail
      user={opts?.user ?? activeUser()}
      canManage={opts?.canManage ?? true}
      canManageQualifications={opts?.canManageQualifications ?? false}
      canManageGroups={opts?.canManageGroups ?? false}
      userGroups={opts?.userGroups ?? []}
      onEdit={opts?.onEdit ?? (() => {})}
      onBack={opts?.onBack ?? (() => {})}
      onRefreshDetail={opts?.onRefreshDetail ?? (() => {})}
      onDeactivated={opts?.onDeactivated ?? (() => {})}
      onForbidden={opts?.onForbidden ?? (() => {})}
      onUnauthorized={opts?.onUnauthorized ?? (() => {})}
    />,
  )
}

describe('UserDetail', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('READ: renders the profile, roles, user groups, direct grants and qualification statuses', () => {
    renderDetail()
    expect(screen.getByText('Tim Müller')).toBeInTheDocument()
    expect(screen.getByText('aktiv')).toBeInTheDocument()
    expect(screen.getByText('tim@gear.local')).toBeInTheDocument()
    expect(screen.getByText('helfende')).toBeInTheDocument()
    expect(screen.getByText('gerätewart')).toBeInTheDocument()
    expect(screen.getByText('Gruppe Ost')).toBeInTheDocument()
    // Direct grant label comes from the shared permission-label fallback.
    expect(screen.getByText('Berichte exportieren')).toBeInTheDocument()
    expect(screen.getByText('Kettensäge')).toBeInTheDocument()
    expect(screen.getByText('Gültig')).toBeInTheDocument()
    expect(screen.getByText('Unbegrenzt')).toBeInTheDocument()
  })

  it('DEACTIVATE: the flow requires an explicit confirmation step ("Sofort kein Login")', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ message: 'Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.', user_id: 'u-tim', email: 'tim@gear.local' }),
    }))
    const user = userEvent.setup()
    const onDeactivated = vi.fn()
    renderDetail({ onDeactivated })

    // The deactivation only happens after the explicit confirm step.
    await user.click(screen.getByRole('button', { name: 'Deaktivieren' }))
    expect(screen.getByText(/Sofort kein Login/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Ja, deaktivieren' }))

    await waitFor(() => {
      expect(onDeactivated).toHaveBeenCalledWith(
        'Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich.',
      )
    })
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-tim/deactivate', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ confirmed: true }),
    }))
  })

  it('DEACTIVATE_CANCEL: cancelling the confirmation step does not deactivate', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onDeactivated = vi.fn()
    renderDetail({ onDeactivated })

    await user.click(screen.getByRole('button', { name: 'Deaktivieren' }))
    await user.click(screen.getByRole('button', { name: 'Abbrechen' }))

    expect(fetchMock).not.toHaveBeenCalled()
    expect(onDeactivated).not.toHaveBeenCalled()
  })

  it('NO_MANAGE: without users.manage the edit and deactivate actions are hidden', () => {
    renderDetail({ canManage: false })
    expect(screen.queryByRole('button', { name: 'Bearbeiten' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Deaktivieren' })).not.toBeInTheDocument()
  })

  it('INACTIVE: a deactivated user offers no deactivate action', () => {
    const user = activeUser()
    user.status = 'deactivated'
    renderDetail({ user })
    expect(screen.getByText('deaktiviert')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Deaktivieren' })).not.toBeInTheDocument()
  })

  it('DEACTIVATE_FORBIDDEN: a 403 on deactivate invokes onForbidden (parent leaves the module)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Keine Berechtigung.' } }),
    }))
    const user = userEvent.setup()
    const onForbidden = vi.fn()
    renderDetail({ onForbidden })

    await user.click(screen.getByRole('button', { name: 'Deaktivieren' }))
    await user.click(screen.getByRole('button', { name: 'Ja, deaktivieren' }))

    await waitFor(() => {
      expect(onForbidden).toHaveBeenCalled()
    })
  })

  it('BACK: clicking the back button returns to the list', async () => {
    const user = userEvent.setup()
    const onBack = vi.fn()
    renderDetail({ onBack })

    await user.click(screen.getByRole('button', { name: /Zurück zur Liste/ }))

    expect(onBack).toHaveBeenCalled()
  })

  it('UNKNOWN_QUAL_STATUS: an unrecognized qualification status renders the raw value, never "undefined"', () => {
    // finding 12: the label helper falls back to the raw status code. The value
    // is deliberately out-of-band (cast) to simulate an unknown status.
    const user = activeUser()
    user.qualifications = [
      { id: 'q-9', name: 'Unbekannt', description: '', expiry_kind: 'fixed', expires_at: null, assigned_at: '', status: 'mystery_status' as unknown as QualificationAssignment['status'] },
    ]
    renderDetail({ user })

    expect(screen.getByText('Unbekannt')).toBeInTheDocument()
    expect(screen.getByText('mystery_status')).toBeInTheDocument()
    expect(screen.queryByText('undefined')).not.toBeInTheDocument()
  })

  it('UNKNOWN_USER_STATUS: an unrecognized account state renders the raw value, never "undefined"', () => {
    // finding 12: the user-status label helper falls back to the raw value.
    const user = activeUser()
    user.status = 'mystery_state' as unknown as AdminUserDetail['status']
    renderDetail({ user })

    expect(screen.getByText('mystery_state')).toBeInTheDocument()
    expect(screen.queryByText('undefined')).not.toBeInTheDocument()
  })

  it('PROVENANCE: the collapsed "Alle Berechtigungen" view shows each permission with its source', async () => {
    const user = activeUser()
    user.resolved_permissions = [
      { code: 'report.export', source_kind: 'direct', source_name: 'direct' },
      { code: 'tools.manage', source_kind: 'group', source_name: 'Gruppe Ost' },
      { code: 'report.export', source_kind: 'role', source_name: 'helfende' },
    ]
    renderDetail({ user, canManageQualifications: false })

    expect(screen.getByText('Alle Berechtigungen')).toBeInTheDocument()
    await userEvent.click(screen.getByText('Alle Berechtigungen'))
    expect(screen.getByText('(Direkt)')).toBeInTheDocument()
    expect(screen.getByText('(Benutzergruppe: Gruppe Ost)')).toBeInTheDocument()
    expect(screen.getByText('(Rolle: helfende)')).toBeInTheDocument()
  })

  it('QUAL_READONLY: without users.qualifications.manage the section is read-only (no add/revoke UI)', () => {
    renderDetail({ canManageQualifications: false })
    expect(screen.queryByRole('button', { name: 'Zuweisen' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Entziehen' })).not.toBeInTheDocument()
    expect(screen.getByText('Gültig')).toBeInTheDocument()
  })

  it('QUAL_ASSIGN: a users.qualifications.manage holder can assign a qualification with valid-until', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ qualifications: [{ id: 'q-3', name: 'Generator', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', status: 'valid' }] }),
    }))
    const user = userEvent.setup()
    const onRefreshDetail = vi.fn()
    renderDetail({ canManageQualifications: true, onRefreshDetail })

    await screen.findByText('Qualifikation zuweisen')
    await user.selectOptions(screen.getByLabelText('Qualifikation'), 'q-3')
    await user.type(screen.getByLabelText('Gültig bis'), '2099-01-01')
    await user.click(screen.getByRole('button', { name: 'Zuweisen' }))

    await waitFor(() => expect(onRefreshDetail).toHaveBeenCalled())
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-tim/qualifications/q-3', expect.objectContaining({
      method: 'POST',
    }))
  })

  it('QUAL_HIDES_ASSIGNED: an already-assigned qualification is not offered for assignment', async () => {
    // The user already holds Kettensäge (q-1) and Generator (q-2). The
    // vocabulary also contains them (plus a free one) — the dropdown must
    // exclude the assigned ones so re-assignment cannot renew an existing
    // assignment from the "assign" surface.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        qualifications: [
          { id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', status: 'valid' },
          { id: 'q-2', name: 'Generator', description: '', expiry_kind: 'unlimited', expires_at: null, status: 'unlimited' },
          { id: 'q-3', name: 'Kran', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', status: 'valid' },
        ],
      }),
    }))
    renderDetail({ canManageQualifications: true })

    await screen.findByText('Qualifikation zuweisen')
    const options = screen.getAllByRole('option').map((o) => o.textContent)
    // The placeholder plus only the unassigned qualification.
    expect(options).toEqual(['Auswählen…', 'Kran'])
  })

  it('QUAL_ALL_ASSIGNED: when every qualification is assigned, no dropdown and a hint are shown', async () => {
    // The vocabulary contains exactly the two qualifications the user holds.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        qualifications: [
          { id: 'q-1', name: 'Kettensäge', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', status: 'valid' },
          { id: 'q-2', name: 'Generator', description: '', expiry_kind: 'unlimited', expires_at: null, status: 'unlimited' },
        ],
      }),
    }))
    renderDetail({ canManageQualifications: true })

    expect(await screen.findByText('Alle Qualifikationen sind diesem Benutzer bereits zugewiesen.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Qualifikation')).not.toBeInTheDocument()
  })

  it('QUAL_REVOKE: a holder can revoke an assigned qualification', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ qualifications: [] }),
    }))
    const user = userEvent.setup()
    const onRefreshDetail = vi.fn()
    renderDetail({ canManageQualifications: true, onRefreshDetail })

    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Entziehen' }).length).toBeGreaterThan(0))
    await user.click(screen.getAllByRole('button', { name: 'Entziehen' })[0])

    await waitFor(() => expect(onRefreshDetail).toHaveBeenCalled())
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-tim/qualifications/q-1', expect.objectContaining({
      method: 'DELETE',
    }))
  })

  it('QUAL_ASSIGN_FIXED_NO_EXPIRY: a fixed qualification requires a valid-until (client-side guard)', async () => {
    // Fix 2: selecting a fixed qualification with an empty Gültig-bis disables
    // "Zuweisen" and shows a German error — the server would 400 a null expiry.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ qualifications: [{ id: 'q-3', name: 'Generator', description: '', expiry_kind: 'fixed', expires_at: '2099-01-01T00:00:00Z', status: 'valid' }] }),
    }))
    const user = userEvent.setup()
    const onRefreshDetail = vi.fn()
    renderDetail({ canManageQualifications: true, onRefreshDetail })

    await screen.findByText('Qualifikation zuweisen')
    await user.selectOptions(screen.getByLabelText('Qualifikation'), 'q-3')

    expect(screen.getByText('Gültig bis ist erforderlich.')).toBeInTheDocument()
    const assignButton = screen.getByRole('button', { name: 'Zuweisen' }) as HTMLButtonElement
    expect(assignButton.disabled).toBe(true)
    expect(onRefreshDetail).not.toHaveBeenCalled()
    const fetchMock = vi.mocked(fetch)
    const postCalls = fetchMock.mock.calls.filter(([u, init]) => u === '/api/v1/admin/users/u-tim/qualifications/q-3' && init?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })

  it('QUAL_VOCAB_FAILURE: a genuine vocabulary fetch failure shows the fallback note (no crash)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    }))
    renderDetail({ canManageQualifications: true })

    expect(await screen.findByText(/Die Auswahlliste ist nicht verfügbar/)).toBeInTheDocument()
  })

  it('GROUPS_EDIT: a user_groups.manage holder can change the user\'s group memberships and save', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ message: 'Benutzergruppen aktualisiert. Änderungen gelten ab sofort.', user: { id: 'u-tim', user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }] } }),
    }))
    const user = userEvent.setup()
    const onRefreshDetail = vi.fn()
    renderDetail({ canManageGroups: true, userGroups: [{ id: 'ug-ost', name: 'Gruppe Ost' }, { id: 'ug-west', name: 'Gruppe West' }], onRefreshDetail })

    // The editable checkbox list appears (not just badges).
    await user.click(screen.getByRole('button', { name: 'Benutzergruppen speichern' }))
    await waitFor(() => expect(onRefreshDetail).toHaveBeenCalled())
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-tim/groups', expect.objectContaining({ method: 'PUT' }))
  })

  it('GROUPS_READONLY: without user_groups.manage the section is read-only badges', () => {
    renderDetail({ canManageGroups: false, userGroups: [{ id: 'ug-ost', name: 'Gruppe Ost' }] })
    expect(screen.queryByRole('button', { name: 'Benutzergruppen speichern' })).not.toBeInTheDocument()
    expect(screen.getByText('Gruppe Ost')).toBeInTheDocument()
  })
})