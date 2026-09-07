// @vitest-environment jsdom
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { UserDetail } from './UserDetail.tsx'
import type { AdminUserDetail, QualificationAssignment } from '../auth/users.ts'

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
  onEdit?: () => void
  onBack?: () => void
  onDeactivated?: (message: string) => void
  onForbidden?: () => void
}) {
  return render(
    <UserDetail
      user={opts?.user ?? activeUser()}
      canManage={opts?.canManage ?? true}
      onEdit={opts?.onEdit ?? (() => {})}
      onBack={opts?.onBack ?? (() => {})}
      onDeactivated={opts?.onDeactivated ?? (() => {})}
      onForbidden={opts?.onForbidden ?? (() => {})}
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
})