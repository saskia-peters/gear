// @vitest-environment jsdom
import { render, screen, waitFor, within, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { PendingApprovals } from './PendingApprovals.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

const PENDING_URL = '/api/v1/admin/users/pending'

function renderWidget() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin']}>
        <Routes>
          <Route path="/admin" element={<PendingApprovals />} />
          <Route path="/" element={<div>Dashboard</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

// stubFetchRoutes dispatches per URL/method so a single widget can serve the
// initial list fetch AND the approve/reject action with different responses
// (vi.stubGlobal replaces the whole global fetch, so a second call would
// overwrite the first).
interface StubRoute {
  matcher: (url: string, init?: RequestInit) => boolean
  response: { ok: boolean; status: number; body: unknown }
}

function stubFetchRoutes(routes: StubRoute[]) {
  const mock = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    const hit = routes.find((r) => r.matcher(url, init))
    const res = hit?.response ?? { ok: false, status: 404, body: { error: { code: 'not_found', message: 'nope' } } }
    return { ok: res.ok, status: res.status, json: async () => res.body }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

const stubList = (users: unknown[]): StubRoute => ({
  matcher: (url) => url === PENDING_URL,
  response: { ok: true, status: 200, body: { users } },
})

const stubPost = (path: string, response: { ok: boolean; status: number; body: unknown }): StubRoute => ({
  matcher: (url, init) => init?.method === 'POST' && url.endsWith(path),
  response,
})

const PENDING_USERS = [
  { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local' },
  { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local' },
]

// rowFor locates the pending-request row for the given person (by name) so a
// click can be scoped to that row's own CTAs (rows share identical button
// labels).
function rowFor(name: string) {
  const nameEl = screen.getByText(name)
  const li = nameEl.closest('li')
  if (!li) throw new Error(`row for ${name} not found`)
  return within(li as HTMLElement)
}

describe('PendingApprovals', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('LIST: renders each pending request as a row (Vorname, Nachname, E-Mail) with Freigeben/Ablehnen', async () => {
    const fetchMock = stubFetchRoutes([stubList(PENDING_USERS)])
    renderWidget()

    expect(await screen.findByText('Tim Müller')).toBeInTheDocument()
    expect(screen.getByText('tim@gear.local')).toBeInTheDocument()
    expect(screen.getByText('Lena Schmidt')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Freigeben' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: 'Ablehnen' })).toHaveLength(2)

    // The list is fetched with the authenticated admin headers.
    expect(fetchMock).toHaveBeenCalledWith(PENDING_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('EMPTY: an empty list shows "Keine ausstehenden Anträge"', async () => {
    stubFetchRoutes([stubList([])])
    renderWidget()

    expect(
      await screen.findByRole('heading', { name: 'Keine ausstehenden Anträge' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/Neue Freigaben von Mitgliedern erscheinen hier/)).toBeInTheDocument()
  })

  it('APPROVE_HAPPY: approving posts, removes the row and announces success', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-tim/approve', {
        ok: true,
        status: 200,
        body: { message: 'Antrag freigegeben. Die Person kann sich jetzt anmelden.', user_id: 'u-tim', email: 'tim@gear.local' },
      }),
    ])
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Tim Müller')
    await user.click(rowFor('Tim Müller').getByRole('button', { name: 'Freigeben' }))

    await waitFor(() => {
      expect(screen.queryByText('Tim Müller')).not.toBeInTheDocument()
    })
    // The SUCCESS message comes from the server (single source of truth), not a
    // duplicated client string.
    expect(
      screen.getByText('Antrag freigegeben. Die Person kann sich jetzt anmelden.'),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-tim/approve', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('APPROVE_FALLBACK: when the server omits a message, a generic German string is used', async () => {
    stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-tim/approve', { ok: true, status: 200, body: { user_id: 'u-tim', email: 'tim@gear.local' } }),
    ])
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Tim Müller')
    await user.click(rowFor('Tim Müller').getByRole('button', { name: 'Freigeben' }))

    expect(await screen.findByText('Antrag freigegeben.')).toBeInTheDocument()
  })

  it('REJECT_CONFIRM: a mis-click on Ablehnen does NOT post; the confirm button posts', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-lena/reject', {
        ok: true,
        status: 200,
        body: { message: 'Antrag abgelehnt.', user_id: 'u-lena', email: 'lena@gear.local' },
      }),
    ])
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Lena Schmidt')
    const row = rowFor('Lena Schmidt')
    await user.click(row.getByRole('button', { name: 'Ablehnen' }))

    // First click only arms the confirm — nothing is posted yet.
    expect(row.getByText('Wirklich ablehnen?')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/admin/users/u-lena/reject', expect.anything())

    // The confirm button (not "Ablehnen") posts the reject.
    await user.click(row.getByRole('button', { name: 'Ja, ablehnen' }))

    await waitFor(() => {
      expect(screen.queryByText('Lena Schmidt')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Antrag abgelehnt.')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/users/u-lena/reject', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('REJECT_CANCEL: Abbrechen dismisses the confirm without posting', async () => {
    const fetchMock = stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-lena/reject', { ok: true, status: 200, body: { message: 'Antrag abgelehnt.', user_id: 'u-lena', email: 'lena@gear.local' } }),
    ])
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Lena Schmidt')
    const row = rowFor('Lena Schmidt')
    await user.click(row.getByRole('button', { name: 'Ablehnen' }))
    expect(row.getByText('Wirklich ablehnen?')).toBeInTheDocument()

    await user.click(row.getByRole('button', { name: 'Abbrechen' }))

    // Confirm dismissed, row still present, nothing posted.
    expect(screen.queryByText('Wirklich ablehnen?')).not.toBeInTheDocument()
    expect(screen.getByText('Lena Schmidt')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/admin/users/u-lena/reject', expect.anything())
  })

  it('LOAD_ANNOUNCEMENT: a visually-hidden live region announces completion', async () => {
    stubFetchRoutes([stubList([])])
    renderWidget()

    // The live region (role="status") is always present; once loading finishes
    // it announces the empty state to assistive technology.
    const liveRegion = await screen.findByRole('status')
    await waitFor(() => {
      expect(liveRegion).toHaveTextContent('Keine ausstehenden Anträge.')
    })
  })

  it('LOAD_ERROR: a failed list fetch shows a German inline error, no crash', async () => {
    stubFetchRoutes([
      { matcher: (url) => url === PENDING_URL, response: { ok: false, status: 500, body: { error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } } } },
    ])
    renderWidget()

    expect(
      await screen.findByRole('alert'),
    ).toHaveTextContent('Ausstehende Anträge konnten nicht geladen werden.')
  })

  it('NETWORK_ERROR: a thrown fetch shows the inline error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('down')))
    renderWidget()

    expect(
      await screen.findByRole('alert'),
    ).toHaveTextContent('Ausstehende Anträge konnten nicht geladen werden.')
  })

  it('ACTION_ERROR: a failed approve surfaces the server message inline', async () => {
    stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-tim/approve', {
        ok: false,
        status: 404,
        body: { error: { code: 'not_found', message: 'Der Antrag wurde nicht gefunden oder ist nicht mehr ausstehend.' } },
      }),
    ])
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Tim Müller')
    await user.click(rowFor('Tim Müller').getByRole('button', { name: 'Freigeben' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Der Antrag wurde nicht gefunden oder ist nicht mehr ausstehend.',
    )
  })

  it('FORBIDDEN: a 403 clears the admin flag and leaves the admin module', async () => {
    stubFetchRoutes([
      stubList(PENDING_USERS),
      stubPost('/users/u-tim/approve', { ok: false, status: 403, body: { error: { code: 'forbidden', message: 'Keine Berechtigung.' } } }),
    ])
    localStorage.setItem('gear.is_admin', 'true')
    const user = userEvent.setup()
    renderWidget()

    await screen.findByText('Tim Müller')
    await user.click(rowFor('Tim Müller').getByRole('button', { name: 'Freigeben' }))

    expect(await screen.findByText('Dashboard')).toBeInTheDocument()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })
})