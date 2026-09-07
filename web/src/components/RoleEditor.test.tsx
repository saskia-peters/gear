// @vitest-environment jsdom
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { RoleEditor } from './RoleEditor.tsx'
import type { PermissionCatalogEntry, RoleGroup } from '../auth/roles.ts'

const CATALOG: PermissionCatalogEntry[] = [
  { code: 'dashboard.view', label: 'Dashboard ansehen' },
  { code: 'tools.manage', label: 'Geräte verwalten' },
  { code: 'roles.edit', label: 'Rollen bearbeiten' },
]

function renderEditor(opts?: {
  role?: RoleGroup | null
  onSaved?: () => void
  onCancel?: () => void
  onForbidden?: () => void
}) {
  const role = opts?.role === undefined ? null : opts.role
  return render(
    <RoleEditor
      role={role}
      availablePermissions={CATALOG}
      onSaved={opts?.onSaved ?? (() => {})}
      onCancel={opts?.onCancel ?? (() => {})}
      onForbidden={opts?.onForbidden ?? (() => {})}
    />,
  )
}

describe('RoleEditor', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('NEW: renders the 21-code additive grid as unchecked checkboxes and a name field', () => {
    renderEditor()
    expect(screen.getByRole('heading', { name: 'Neue Rolle' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
    // Each code is an additive checkbox (checked = granted).
    for (const perm of CATALOG) {
      expect(screen.getByRole('checkbox', { name: perm.label })).toBeInTheDocument()
    }
    // None checked for a new role.
    expect(screen.getAllByRole('checkbox').every((cb) => !(cb as HTMLInputElement).checked)).toBe(true)
  })

  it('EDIT: pre-checks the role’s existing permission set and pre-fills the name', () => {
    renderEditor({
      role: {
        id: 'g-helfende',
        name: 'helfende',
        description: 'Basis',
        is_base_role: true,
        permissions: ['dashboard.view', 'tools.manage'],
      },
    })
    expect(screen.getByRole('heading', { name: 'Rolle „helfende“ bearbeiten' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('helfende')
    const dash = screen.getByRole('checkbox', { name: 'Dashboard ansehen' }) as HTMLInputElement
    expect(dash.checked).toBe(true)
    const tools = screen.getByRole('checkbox', { name: 'Geräte verwalten' }) as HTMLInputElement
    expect(tools.checked).toBe(true)
    const edit = screen.getByRole('checkbox', { name: 'Rollen bearbeiten' }) as HTMLInputElement
    expect(edit.checked).toBe(false)
  })

  it('CREATE: posting the additive set calls createRole and reports success', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return { ok: true, status: 201, json: async () => ({ id: 'g-gerat', name: 'gerätewart', description: '', is_base_role: false, permissions: ['tools.manage'] }) }
      }
      return { ok: true, status: 200, json: async () => ({ groups: [], available_permissions: CATALOG }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({ onSaved })

    await user.type(screen.getByLabelText('Name'), 'gerätewart')
    await user.click(screen.getByRole('checkbox', { name: 'Geräte verwalten' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'gerätewart' }),
        'Rolle erstellt.',
      )
    })
    // POST /groups with the additive checked set.
    const createCall = fetchMock.mock.calls.find(([u, init]) =>
      u === '/api/v1/admin/groups' && init?.method === 'POST',
    )
    expect(createCall).toBeTruthy()
    const body = JSON.parse(createCall![1].body)
    expect(body).toEqual({ name: 'gerätewart', description: '', permissions: ['tools.manage'] })
  })

  it('VALIDATION: an empty name shows inline German error and does not submit', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({ onSaved })

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Bitte gib einen Namen für die Rolle an.',
    )
    expect(onSaved).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('EDIT: saving calls updateRole with the id and the checked set', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, json: async () => ({ id: 'g-x', name: 'x', description: '', is_base_role: false, permissions: ['tools.manage'] }) }
      }
      return { ok: true, status: 200, json: async () => ({ groups: [], available_permissions: CATALOG }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    const onSaved = vi.fn()
    renderEditor({
      role: {
        id: 'g-x', name: 'x', description: '', is_base_role: false,
        permissions: ['dashboard.view'],
      },
      onSaved,
    })

    // Toggle OFF dashboard.view and toggle ON tools.manage, then save.
    await user.click(screen.getByRole('checkbox', { name: 'Dashboard ansehen' }))
    await user.click(screen.getByRole('checkbox', { name: 'Geräte verwalten' }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalled()
    })
    const updateCall = fetchMock.mock.calls.find(([u, init]) =>
      u === '/api/v1/admin/groups/g-x' && init?.method === 'PUT',
    )
    expect(updateCall).toBeTruthy()
    const body = JSON.parse(updateCall![1].body)
    expect(body.permissions).toEqual(['tools.manage'])
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
      json: async () => ({ error: { code: 'conflict', message: 'Es gibt bereits eine Rolle mit diesem Namen.' } }),
    }))
    const user = userEvent.setup()
    renderEditor()

    await user.type(screen.getByLabelText('Name'), 'helfende')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Es gibt bereits eine Rolle mit diesem Namen.',
    )
  })

  it('NETWORK_ERROR: a thrown fetch shows the German connection error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('down')))
    const user = userEvent.setup()
    renderEditor()

    await user.type(screen.getByLabelText('Name'), 'gerätewart')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Verbindung zum Server fehlgeschlagen.',
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

    await user.type(screen.getByLabelText('Name'), 'gerätewart')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(onForbidden).toHaveBeenCalled()
    })
    // No inline feedback is rendered for the revocation path.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
