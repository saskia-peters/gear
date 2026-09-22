// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { QualificationEditor } from './QualificationEditor.tsx'
import type { Qualification } from '../auth/qualifications.ts'

const QUALS_URL = '/api/v1/admin/qualifications'

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

function renderEditor(qualification: Qualification | null = null, onSaved = vi.fn()) {
  const onCancel = vi.fn()
  const onForbidden = vi.fn()
  const utils = render(
    <QualificationEditor
      qualification={qualification}
      onSaved={onSaved}
      onCancel={onCancel}
      onForbidden={onForbidden}
    />,
  )
  return { onSaved, onCancel, onForbidden, ...utils }
}

describe('QualificationEditor', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('CREATE_UNLIMITED: a qualification with the "Unbegrenzt gültig" checkbox POSTs expiry_kind unlimited (default)', async () => {
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { message: 'Qualifikation erstellt.', qualification: { id: 'q-9', name: 'Erste Hilfe', description: '', expiry_kind: 'unlimited', status: 'unlimited' } } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Name'), 'Erste Hilfe')
    // Unlimited is the default (checkbox checked).
    const checkbox = screen.getByLabelText(/Unbegrenzt gültig/) as HTMLInputElement
    expect(checkbox.checked).toBe(true)
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    // Finding 5: the success microcopy comes from the SERVER response.
    expect(onSaved).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Erste Hilfe' }),
      'Qualifikation erstellt.',
    )
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === QUALS_URL && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(JSON.parse((postCall![1] as RequestInit).body as string)).toEqual({
      name: 'Erste Hilfe',
      description: '',
      expiry_kind: 'unlimited',
    })
  })

  it('CREATE_FIXED: unchecking the checkbox POSTs expiry_kind fixed (no date — the per-user valid-until is set at assignment)', async () => {
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: true, status: 201, body: { message: 'Qualifikation erstellt.', qualification: { id: 'q-9', name: 'Kettensäge', description: '', expiry_kind: 'fixed', status: 'fixed' } } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Name'), 'Kettensäge')
    await user.click(screen.getByLabelText(/Unbegrenzt gültig/))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(onSaved).toHaveBeenCalled()
    const postCall = fetchMock.mock.calls.find(([url, init]) => url === QUALS_URL && init?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall![1] as RequestInit).body as string)
    expect(body.expiry_kind).toBe('fixed')
    // No vocabulary-level date is sent (2026-09-08 rework).
    expect('expires_at' in body).toBe(false)
  })

  it('CREATE_INVALID: an empty name blocks the save with German feedback, no fetch', async () => {
    const fetchMock = stubFetchRoutes([])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib einen Namen für die Qualifikation an.')
    expect(onSaved).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('EDIT: editing a qualification PUTs the replaced row with the preserved expiry kind', async () => {
    const editing: Qualification = {
      id: 'q-1', name: 'Kettensäge', description: 'Führerschein',
      expiry_kind: 'fixed', status: 'fixed',
    }
    const fetchMock = stubFetchRoutes([
      {
        matcher: (url, init) => init?.method === 'PUT' && url === `${QUALS_URL}/q-1`,
        response: { ok: true, status: 200, body: { message: 'Qualifikation gespeichert.', qualification: { ...editing, name: 'Kettensäge neu' } } },
      },
    ])
    const { onSaved } = renderEditor(editing)
    const user = userEvent.setup()

    // The form pre-fills the current values; a fixed qualification has the
    // checkbox unchecked; renaming stays legal.
    expect(screen.getByLabelText('Name')).toHaveValue('Kettensäge')
    const checkbox = screen.getByLabelText(/Unbegrenzt gültig/) as HTMLInputElement
    expect(checkbox.checked).toBe(false)
    await user.clear(screen.getByLabelText('Name'))
    await user.type(screen.getByLabelText('Name'), 'Kettensäge neu')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    // Finding 5: the update success text comes from the server response.
    expect(onSaved).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Kettensäge neu' }),
      'Qualifikation gespeichert.',
    )
    const putCall = fetchMock.mock.calls.find(([url, init]) => url === `${QUALS_URL}/q-1` && init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(body.expiry_kind).toBe('fixed')
  })

  it('EDIT_UNLIMITED: an unlimited qualification has the checkbox checked', () => {
    renderEditor({
      id: 'q-2', name: 'Erste Hilfe', description: '', expiry_kind: 'unlimited', status: 'unlimited',
    })
    const checkbox = screen.getByLabelText(/Unbegrenzt gültig/) as HTMLInputElement
    expect(checkbox.checked).toBe(true)
  })

  it('ERROR: a server 409 shows the German conflict message inline', async () => {
    stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: false, status: 409, body: { error: { code: 'conflict', message: 'Es gibt bereits eine Qualifikation mit diesem Namen.' } } },
      },
    ])
    const { onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Name'), 'Kettensäge')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Es gibt bereits eine Qualifikation mit diesem Namen.')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('FORBIDDEN: a 403 on save invokes onForbidden', async () => {
    stubFetchRoutes([
      {
        matcher: (url, init) => url === QUALS_URL && init?.method === 'POST',
        response: { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } },
      },
    ])
    const { onForbidden, onSaved } = renderEditor()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Name'), 'Seilwinde')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(onForbidden).toHaveBeenCalled()
    expect(onSaved).not.toHaveBeenCalled()
  })
})