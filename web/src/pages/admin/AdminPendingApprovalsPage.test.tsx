// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminPendingApprovalsPage } from './AdminPendingApprovalsPage.tsx'
import { ThemeProvider } from '../../context/ThemeContext.tsx'

const PENDING_URL = '/api/v1/admin/users/pending'

function renderPage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/admin/benutzer/pending']}>
        <Routes>
          <Route path="/admin/benutzer/pending" element={<AdminPendingApprovalsPage />} />
          <Route path="/admin/benutzer" element={<div>BenutzerPage</div>} />
          <Route path="/" element={<div>Dashboard</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

describe('AdminPendingApprovalsPage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    localStorage.setItem('gear.permissions', JSON.stringify(['users.approve', 'users.view', 'users.manage']))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ users: [] }),
    }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('PAGE: renders the dedicated pending-approvals surface with title and empty state', async () => {
    renderPage()

    expect(screen.getByRole('heading', { level: 2, name: 'Ausstehende Anträge' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Keine ausstehenden Anträge' })).toBeInTheDocument()
  })

  it('PAGE_LIST: renders pending requests fetched from the admin API', async () => {
    vi.unstubAllGlobals()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        users: [
          { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local' },
          { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local' },
        ],
      }),
    }))
    renderPage()

    expect(await screen.findByText('Tim Müller')).toBeInTheDocument()
    expect(screen.getByText('Lena Schmidt')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Freigeben' }).length).toBeGreaterThan(0)
  })

  it('BACK: the back button returns to the Benutzer surface', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Keine ausstehenden Anträge' })

    await user.click(screen.getByRole('button', { name: /Zurück zu Benutzer/ }))

    expect(await screen.findByText('BenutzerPage')).toBeInTheDocument()
  })

  it('FETCH: the pending list is fetched from /api/v1/admin/users/pending', async () => {
    const fetchMock = vi.mocked(fetch)
    renderPage()
    await screen.findByRole('heading', { name: 'Keine ausstehenden Anträge' })

    expect(fetchMock).toHaveBeenCalledWith(PENDING_URL, expect.objectContaining({ headers: expect.any(Object) }))
  })
})