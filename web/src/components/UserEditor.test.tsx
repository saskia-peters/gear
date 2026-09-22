// @vitest-environment jsdom
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { UserEditor } from './UserEditor.tsx'
import type { AdminUserDetail } from '../auth/users.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../auth/roles.ts'

const ROLES: RoleGroup[] = [
  { id: 'g-helfende', name: 'helfende', description: 'Basis', is_base_role: true, permissions: ['dashboard.view'] },
  { id: 'g-gerat', name: 'gerätewart', description: '', is_base_role: false, permissions: ['tools.manage'] },
]

const GROUPS = [
  { id: 'ug-ost', name: 'Gruppe Ost', description: '', created_at: '' },
  { id: 'ug-west', name: 'Gruppe West', description: '', created_at: '' },
]

const CATALOG: PermissionCatalogEntry[] = [
  { code: 'dashboard.view', label: 'Dashboard ansehen' },
  { code: 'report.export', label: 'Berichte exportieren' },
]

function renderEditor(opts?: {
  user?: AdminUserDetail | null
  onSaved?: () => void
  onCancel?: () => void
  onForbidden?: () => void
}) {
  const user = opts?.user === undefined ? null : opts.user
  return render(
    <UserEditor
      user={user}
      roles={ROLES}
      userGroups={GROUPS}
      availablePermissions={CATALOG}
      onSaved={opts?.onSaved ?? (() => {})}
      onCancel={opts?.onCancel ?? (() => {})}
      onForbidden={opts?.onForbidden ?? (() => {})}
    />,
  )
}

describe('UserEditor', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('NEW: renders the create form with profile fields, status, roles, groups and grants', () => {
    renderEditor()
    expect(screen.getByRole('heading', { name: 'Neuer Benutzer' })).toBeInTheDocument()
    expect(screen.getByLabelText('Vorname')).toBeInTheDocument()
    expect(screen.getByLabelText('Nachname')).toBeInTheDocument()
    expect(screen.getByLabelText('E-Mail')).toBeInTheDocument()
    expect(screen.getByLabelText('Status')).toHaveValue('active')
    expect(screen.getByRole('checkbox', { name: /helfende/ })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Gruppe Ost' })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Dashboard ansehen' })).toBeInTheDocument()
  })

  it('EDIT: pre-fills the profile fields and the existing assignment sets', () => {
    renderEditor({
      user: {
        id: 'u-tim',
        vorname: 'Tim',
        nachname: 'Müller',
        email: 'tim@gear.local',
        status: 'active',
        roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }],
        user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }],
        direct_grants: [{ permission_id: 'p-1', code: 'report.export', granted_at: '' }],
        qualifications: [],
      },
    })
    expect(screen.getByRole('heading', { name: 'Benutzer „Tim Müller“ bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Vorname')).toHaveValue('Tim')
    expect(screen.getByLabelText('Nachname')).toHaveValue('Müller')
    expect(screen.getByLabelText('E-Mail')).toHaveValue('tim@gear.local')
    expect((screen.getByRole('checkbox', { name: /helfende/ }) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByRole('checkbox', { name: 'Gruppe Ost' }) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByRole('checkbox', { name: 'Berichte exportieren' }) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByRole('checkbox', { name: /gerätewart/ }) as HTMLInputElement).checked).toBe(false)
  })

  it('CREATE: posting the assignment sets calls createUser and reports the server success message', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return { ok: true, status: 201, json: async () => ({ message: 'Benutzer angelegt. Zugangsdaten werden separat vergeben.', user: { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active' } }) }
      }
      return { ok: true, status: 200, json: async () => ({ groups: [], available_permissions: CATALOG }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({ onSaved })

    await user.type(screen.getByLabelText('Vorname'), 'Tim')
    await user.type(screen.getByLabelText('Nachname'), 'Müller')
    await user.type(screen.getByLabelText('E-Mail'), 'tim@gear.local')
    await user.click(screen.getByRole('checkbox', { name: /helfende/ }))
    await user.click(screen.getByRole('checkbox', { name: 'Gruppe Ost' }))
    await user.click(screen.getByRole('checkbox', { name: 'Berichte exportieren' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(
        expect.objectContaining({ email: 'tim@gear.local' }),
        'Benutzer angelegt. Zugangsdaten werden separat vergeben.',
      )
    })
    const createCall = fetchMock.mock.calls.find(([u, init]) =>
      u === '/api/v1/admin/users' && init?.method === 'POST',
    )
    expect(createCall).toBeTruthy()
    const body = JSON.parse(createCall![1].body)
    expect(body).toEqual({
      vorname: 'Tim',
      nachname: 'Müller',
      email: 'tim@gear.local',
      status: 'active',
      role_ids: ['g-helfende'],
      user_group_ids: ['ug-ost'],
      direct_grant_codes: ['report.export'],
    })
  })

  it('VALIDATION: an empty required field shows inline German error and does not submit', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({ onSaved })

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Bitte fülle alle Pflichtfelder aus.',
    )
    expect(onSaved).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('EDIT: saving calls updateUser with the id and the changed sets', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, json: async () => ({ message: 'Benutzer gespeichert. Änderungen gelten ab der nächsten Anfrage.', user: { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active' } }) }
      }
      return { ok: true, status: 200, json: async () => ({ groups: [], available_permissions: CATALOG }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({
      user: {
        id: 'u-tim',
        vorname: 'Tim',
        nachname: 'Müller',
        email: 'tim@gear.local',
        status: 'active',
        roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }],
        user_groups: [{ id: 'ug-ost', name: 'Gruppe Ost' }],
        direct_grants: [],
        qualifications: [],
      },
      onSaved,
    })

    // Toggle OFF the helfende role, then save.
    await user.click(screen.getByRole('checkbox', { name: /helfende/ }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(
        expect.objectContaining({ email: 'tim@gear.local' }),
        'Benutzer gespeichert. Änderungen gelten ab der nächsten Anfrage.',
      )
    })
    const updateCall = fetchMock.mock.calls.find(([u, init]) =>
      u === '/api/v1/admin/users/u-tim' && init?.method === 'PUT',
    )
    expect(updateCall).toBeTruthy()
    const body = JSON.parse(updateCall![1].body)
    expect(body.role_ids).toEqual([])
  })

  it('CANCEL: clicking Abbrechen calls onCancel without submitting', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onCancel = vi.fn()
    renderEditor({ onCancel })

    await user.click(screen.getByRole('button', { name: 'Abbrechen' }))

    expect(onCancel).toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('ERROR: a failed save surfaces the server’s German message inline', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: { code: 'conflict', message: 'Es gibt bereits ein Konto mit dieser E-Mail-Adresse.' } }),
    }))
    const user = userEvent.setup()
    renderEditor()

    await user.type(screen.getByLabelText('Vorname'), 'Tim')
    await user.type(screen.getByLabelText('Nachname'), 'Müller')
    await user.type(screen.getByLabelText('E-Mail'), 'admin.1@gear.local')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Es gibt bereits ein Konto mit dieser E-Mail-Adresse.',
    )
  })

  it('FORBIDDEN: a 403 on save invokes onForbidden (parent leaves the module) instead of inline feedback', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Keine Berechtigung.' } }),
    }))
    const user = userEvent.setup()
    const onForbidden = vi.fn()
    renderEditor({ onForbidden })

    await user.type(screen.getByLabelText('Vorname'), 'Tim')
    await user.type(screen.getByLabelText('Nachname'), 'Müller')
    await user.type(screen.getByLabelText('E-Mail'), 'tim@gear.local')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onForbidden).toHaveBeenCalled()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})