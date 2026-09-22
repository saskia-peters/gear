// @vitest-environment jsdom
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { GroupRolesEditor } from './GroupRolesEditor.tsx'
import type { RoleGroupRef } from '../auth/users.ts'

const ROLES: RoleGroupRef[] = [
  { id: 'g-helfende', name: 'helfende', is_base_role: true },
  { id: 'g-gerat', name: 'gerätewart', is_base_role: false },
]

function renderEditor(opts?: {
  roles?: RoleGroupRef[]
  onSaved?: (message: string) => void
  onForbidden?: () => void
  onUnauthorized?: () => void
  onCancel?: () => void
}) {
  return render(
    <GroupRolesEditor
      groupId="ug-ost"
      groupName="Gruppe Ost"
      roles={opts?.roles ?? ROLES}
      onSaved={opts?.onSaved ?? (() => {})}
      onForbidden={opts?.onForbidden ?? (() => {})}
      onUnauthorized={opts?.onUnauthorized ?? (() => {})}
      onCancel={opts?.onCancel ?? (() => {})}
    />,
  )
}

function fetchOnce(ok: boolean, status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok, status, json: async () => body }))
}

describe('GroupRolesEditor', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: loads the group current roles and renders the checkbox list', async () => {
    fetchOnce(true, 200, { roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }] })
    renderEditor()

    expect(await screen.findByText(/Rollen von „Gruppe Ost“/)).toBeInTheDocument()
    const helfende = screen.getByRole('checkbox', { name: /helfende/ }) as HTMLInputElement
    const gerat = screen.getByRole('checkbox', { name: /gerätewart/ }) as HTMLInputElement
    await waitFor(() => expect(helfende.checked).toBe(true))
    expect(gerat.checked).toBe(false)
  })

  it('BASIS_TAG: a base role shows the "Basis" badge like the user editor (a custom role does not)', async () => {
    fetchOnce(true, 200, { roles: ROLES })
    renderEditor()

    await screen.findByText(/Rollen von „Gruppe Ost“/)
    expect(screen.getByText('Basis')).toBeInTheDocument()
    // Only the one base role (helfende) carries the badge.
    expect(screen.getAllByText('Basis')).toHaveLength(1)
  })

  it('ASSIGN: saving posts the full role set atomically', async () => {
    const mock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return { ok: true, status: 200, json: async () => ({ roles: ROLES }) }
      }
      return { ok: true, status: 200, json: async () => ({ roles: [{ id: 'g-helfende', name: 'helfende', is_base_role: true }] }) }
    })
    vi.stubGlobal('fetch', mock)
    const onSaved = vi.fn()
    const user = userEvent.setup()
    renderEditor({ onSaved })

    await screen.findByText(/Rollen von „Gruppe Ost“/)
    await user.click(screen.getByRole('checkbox', { name: /gerätewart/ }))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    const postCall = mock.mock.calls.find(([u, init]) => u === '/api/v1/admin/user-groups/ug-ost/roles' && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall as [string, RequestInit])[1].body as string)
    expect(body.role_ids).toEqual(expect.arrayContaining(['g-helfende', 'g-gerat']))
  })

  it('REMOVE: unchecking a role drops it from the saved set', async () => {
    const mock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return { ok: true, status: 200, json: async () => ({ roles: [{ id: 'g-gerat', name: 'gerätewart', is_base_role: false }] }) }
      }
      return { ok: true, status: 200, json: async () => ({ roles: ROLES }) }
    })
    vi.stubGlobal('fetch', mock)
    const user = userEvent.setup()
    renderEditor()

    await screen.findByText(/Rollen von „Gruppe Ost“/)
    const helfende = screen.getByRole('checkbox', { name: /helfende/ }) as HTMLInputElement
    await waitFor(() => expect(helfende.checked).toBe(true))
    await user.click(helfende)
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      const postCall = mock.mock.calls.find(([u, init]) => u === '/api/v1/admin/user-groups/ug-ost/roles' && init?.method === 'POST')
      expect(postCall).toBeTruthy()
      const body = JSON.parse((postCall as [string, RequestInit])[1].body as string)
      expect(body.role_ids).not.toContain('g-helfende')
    })
  })

  it('COMPACT: in compact mode the redundant title and Abbrechen button are hidden', async () => {
    fetchOnce(true, 200, { roles: ROLES })
    render(
      <GroupRolesEditor
        groupId="ug-ost"
        groupName="Gruppe Ost"
        roles={ROLES}
        onSaved={() => {}}
        onForbidden={() => {}}
        onUnauthorized={() => {}}
        onCancel={() => {}}
        compact
      />,
    )

    await screen.findByRole('checkbox', { name: /helfende/ })
    expect(screen.queryByText(/Rollen von „Gruppe Ost“/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Abbrechen' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeInTheDocument()
  })

  it('FORBIDDEN: a 403 on load invokes onForbidden (parent leaves the module)', async () => {
    fetchOnce(false, 403, { error: { code: 'forbidden', message: 'Keine Berechtigung.' } })
    const onForbidden = vi.fn()
    renderEditor({ onForbidden })

    await waitFor(() => expect(onForbidden).toHaveBeenCalled())
  })

  it('UNAUTHORIZED: a 401 on load invokes onUnauthorized (clear auth + login redirect)', async () => {
    fetchOnce(false, 401, { error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } })
    const onUnauthorized = vi.fn()
    renderEditor({ onUnauthorized })

    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
  })
})
