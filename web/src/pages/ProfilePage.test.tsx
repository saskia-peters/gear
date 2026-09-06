// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { ProfilePage } from './ProfilePage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname}</span>
}

function renderProfilePage() {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={['/profil']}>
        <LocationProbe />
        <ProfilePage />
      </MemoryRouter>
    </ThemeProvider>,
  )
}

interface ProfileFixture {
  id: string
  email: string
  first_name: string
  last_name: string
  display_name: string
  pending_email?: string
  attributes?: Record<string, unknown>
}

const baseProfile: ProfileFixture = {
  id: 'u-1',
  email: 'max@example.com',
  first_name: 'Max',
  last_name: 'Mustermann',
  display_name: 'Max Mustermann',
}

// postResponse is a minimal fetch Response stand-in used by the mocks.
type postResponse = {
  ok: boolean
  status: number
  json: () => Promise<unknown>
}

// profileFetch returns a fetch stub that routes GET /api/v1/auth/profile to a
// profile payload and lets the caller handle POST calls.
function profileFetch(
  profile: ProfileFixture = baseProfile,
  postHandler?: (url: string, init?: RequestInit) => postResponse | Promise<postResponse>,
) {
  return vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') {
      if (postHandler) {
        return postHandler(url, init)
      }
      if (url === '/api/v1/auth/profile/email') {
        return {
          ok: true,
          status: 200,
          json: async () => ({ message: 'E-Mail-Änderung wartet auf Admin-Freigabe.', pending_email: 'neu@example.com' }),
        }
      }
      return { ok: true, status: 200, json: async () => ({ ...profile, display_name: 'Max' }) }
    }
    return { ok: true, status: 200, json: async () => profile }
  })
}

describe('ProfilePage', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('HAPPY_PATH: loads and shows the profile base data plus the MFA/Passwort entry points', async () => {
    vi.stubGlobal('fetch', profileFetch())

    renderProfilePage()

    expect(await screen.findByRole('heading', { level: 2, name: 'Profil' })).toBeInTheDocument()
    expect(await screen.findByLabelText('Vorname')).toHaveValue('Max')
    expect(screen.getByLabelText('Nachname')).toHaveValue('Mustermann')
    expect(screen.getByLabelText('Anzeigename')).toHaveValue('Max Mustermann')
    expect(screen.getByLabelText('E-Mail-Adresse')).toHaveValue('max@example.com')
    expect(screen.getByRole('button', { name: 'Speichern' })).toBeInTheDocument()

    // Navigation cards (Story 2.1): the MFA and password surfaces are
    // discoverable from the profile page.
    expect(
      screen.getByRole('link', { name: /Zwei-Faktor-Authentifizierung verwalten/ }),
    ).toHaveAttribute('href', '/mfa')
    expect(screen.getByRole('link', { name: 'Passwort ändern' })).toHaveAttribute('href', '/password')
    expect(screen.getByRole('link', { name: 'Zurück zur Übersicht' })).toBeInTheDocument()
  })

  it('HAPPY_PATH: saving base data posts the trimmed payload with the bearer token and shows "Profil aktualisiert."', async () => {
    const user = userEvent.setup()
    const postMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ...baseProfile, first_name: 'Erika', last_name: 'Musterfrau', display_name: 'Erika' }),
    })
    vi.stubGlobal('fetch', profileFetch(baseProfile, (_url, init) => postMock(init)))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.clear(screen.getByLabelText('Vorname'))
    await user.type(screen.getByLabelText('Vorname'), 'Erika')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByText('Profil aktualisiert.')).toBeInTheDocument()
    })
    expect(postMock).toHaveBeenCalledWith({
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
      body: JSON.stringify({ first_name: 'Erika', last_name: 'Mustermann', display_name: 'Max Mustermann' }),
    })
  })

  it('ROUND_TRIP: editing base data preserves loaded attributes in the save payload', async () => {
    // Story 1.9, verification gap: a profile loaded WITH attributes must
    // round-trip them unchanged on a base-data save (never wiped).
    const user = userEvent.setup()
    const withAttrs: ProfileFixture = { ...baseProfile, attributes: { note: 'Interne Notiz' } }
    const postMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ...withAttrs, first_name: 'Erika' }),
    })
    vi.stubGlobal('fetch', profileFetch(withAttrs, (_url, init) => postMock(init)))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.clear(screen.getByLabelText('Vorname'))
    await user.type(screen.getByLabelText('Vorname'), 'Erika')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByText('Profil aktualisiert.')).toBeInTheDocument()
    })
    const init = postMock.mock.calls[0][0] as RequestInit
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(body.attributes).toEqual({ note: 'Interne Notiz' })
  })

  it('NEVER_WIPE: when attributes were not loaded, the save body omits the field entirely', async () => {
    // Story 1.9, review finding: a profile without a server-reported attributes
    // object must NOT send `attributes` on save — the field is omitted so the
    // server's leave-unchanged semantics apply instead of wiping stored values.
    const user = userEvent.setup()
    const postMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ...baseProfile, first_name: 'Erika' }),
    })
    vi.stubGlobal('fetch', profileFetch(baseProfile, (_url, init) => postMock(init)))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.clear(screen.getByLabelText('Vorname'))
    await user.type(screen.getByLabelText('Vorname'), 'Erika')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByText('Profil aktualisiert.')).toBeInTheDocument()
    })
    const init = postMock.mock.calls[0][0] as RequestInit
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(body).not.toHaveProperty('attributes')
  })

  it('EMAIL_STAGE: a changed email is staged via POST /api/v1/auth/profile/email and the German confirmation is shown', async () => {
    const user = userEvent.setup()
    const emailPost = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ message: 'E-Mail-Änderung wartet auf Admin-Freigabe.', pending_email: 'neu@example.com' }),
    })
    const postHandler = (url: string) => {
      if (url === '/api/v1/auth/profile/email') {
        return emailPost()
      }
      return { ok: true, status: 200, json: async () => baseProfile }
    }
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'neu@example.com')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByText('E-Mail-Änderung wartet auf Admin-Freigabe.')).toBeInTheDocument()
    })
    // The email field is reset to the active login email; the account stays on
    // it until admin approval.
    expect(screen.getByLabelText('E-Mail-Adresse')).toHaveValue('max@example.com')
  })

  it('EMAIL_STAGE_SAME: an unchanged email is not submitted to the email endpoint', async () => {
    const user = userEvent.setup()
    const emailPost = vi.fn()
    vi.stubGlobal('fetch', profileFetch(baseProfile, (url, init) => {
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
        return { ok: true, status: 200, json: async () => ({}) }
      }
      return postProfileResponse(init)
    }))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByText('Profil aktualisiert.')).toBeInTheDocument()
    })
    expect(emailPost).not.toHaveBeenCalled()
  })

  it('EMAIL_STAGE_INVALID: an inline validation error is shown and the server is not called for the email', async () => {
    const user = userEvent.setup()
    const emailPost = vi.fn()
    vi.stubGlobal('fetch', profileFetch(baseProfile, (url, init) => {
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
      }
      return postProfileResponse(init)
    }))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'keine-mail')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Bitte gib eine gültige E-Mail-Adresse ein.')).toBeInTheDocument()
    expect(emailPost).not.toHaveBeenCalled()
  })

  it('EMAIL_STAGE_DUPLICATE: the German server error is shown when the staged email is already in use', async () => {
    const user = userEvent.setup()
    const postHandler = (url: string) => {
      if (url === '/api/v1/auth/profile/email') {
        return {
          ok: false,
          status: 400,
          json: async () => ({
            error: { code: 'invalid_request', message: 'Diese E-Mail-Adresse wird bereits verwendet.' },
          }),
        }
      }
      return postProfileResponse()
    }
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'taken@example.com')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(
      await screen.findByText('Diese E-Mail-Adresse wird bereits verwendet.'),
    ).toBeInTheDocument()
  })

  it('UNAUTHENTICATED: a 401 on load clears the stale auth state and redirects to /login', async () => {
    localStorage.setItem('gear.display_name', 'Max Mustermann')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
      }),
    )

    renderProfilePage()

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/login')
    })
    expect(localStorage.getItem('gear.session_token')).toBeNull()
    expect(localStorage.getItem('gear.display_name')).toBeNull()
  })

  it('UNAUTHENTICATED: a 401 on save clears the stale auth state and redirects to /login', async () => {
    const user = userEvent.setup()
    const postHandler = () => ({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/login')
    })
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('NETWORK_ERROR: shows the load-failure state when the profile cannot be fetched', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))

    renderProfilePage()

    expect(
      await screen.findByText('Profil konnte nicht geladen werden. Bitte versuche es erneut.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Vorname')).not.toBeInTheDocument()
  })

  it('PENDING_EMAIL: shows the staged-email notice when the loaded profile has a pending_email', async () => {
    vi.stubGlobal(
      'fetch',
      profileFetch({ ...baseProfile, pending_email: 'neu@example.com' }),
    )

    renderProfilePage()

    expect(
      await screen.findByText('E-Mail-Änderung auf neu@example.com wartet auf Admin-Freigabe.'),
    ).toBeInTheDocument()
  })

  it('EMPTY_EMAIL: a cleared email field is rejected with a field error and nothing is submitted', async () => {
    const user = userEvent.setup()
    const emailPost = vi.fn()
    vi.stubGlobal('fetch', profileFetch(baseProfile, (url, init) => {
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
      }
      return postProfileResponse(init)
    }))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Bitte gib eine E-Mail-Adresse an.')).toBeInTheDocument()
    expect(emailPost).not.toHaveBeenCalled()
  })

  it('EMPTY_NAME: an empty Vorname is rejected with an inline field error and nothing is submitted', async () => {
    const user = userEvent.setup()
    const emailPost = vi.fn()
    vi.stubGlobal('fetch', profileFetch(baseProfile, (url, init) => {
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
      }
      return postProfileResponse(init)
    }))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.clear(screen.getByLabelText('Vorname'))
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Bitte gib deinen Vornamen ein.')).toBeInTheDocument()
    expect(emailPost).not.toHaveBeenCalled()
  })

  it('BASE_SAVE_FAILED: a failed base-data save (500) prevents the email POST', async () => {
    // Review finding: the email POST must be gated on the ACTUAL base-save
    // result of this submission, not the render-closure errors state.
    const user = userEvent.setup()
    const emailPost = vi.fn()
    const postHandler = (url: string, init?: RequestInit) => {
      if (url === '/api/v1/auth/profile') {
        return {
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
        }
      }
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
      }
      return postProfileResponse(init)
    }
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'neu@example.com')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    expect(await screen.findByText('Ein interner Fehler ist aufgetreten.')).toBeInTheDocument()
    expect(emailPost).not.toHaveBeenCalled()
  })

  it('RESUBMIT_AFTER_ERROR: a valid resubmit after a prior general error does send the email POST', async () => {
    // Review finding: a stale general error must not suppress a later valid
    // resubmit. The base save fails once, then succeeds; the email POST fires
    // only on the second (successful) submission.
    const user = userEvent.setup()
    const emailPost = vi.fn()
    let baseCalls = 0
    const postHandler = (url: string, init?: RequestInit) => {
      if (url === '/api/v1/auth/profile') {
        baseCalls++
        if (baseCalls === 1) {
          return {
            ok: false,
            status: 500,
            json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
          }
        }
        return postProfileResponse(init)
      }
      if (url === '/api/v1/auth/profile/email') {
        emailPost()
        return { ok: true, status: 200, json: async () => ({ message: 'E-Mail-Änderung wartet auf Admin-Freigabe.', pending_email: 'neu@example.com' }) }
      }
      return postProfileResponse(init)
    }
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('E-Mail-Adresse')

    await user.clear(screen.getByLabelText('E-Mail-Adresse'))
    await user.type(screen.getByLabelText('E-Mail-Adresse'), 'neu@example.com')

    // First submit: base save fails → general error, no email POST.
    await user.click(screen.getByRole('button', { name: 'Speichern' }))
    await screen.findByText('Ein interner Fehler ist aufgetreten.')
    expect(emailPost).not.toHaveBeenCalled()

    // Second submit: base save succeeds → email POST fires.
    await user.click(screen.getByRole('button', { name: 'Speichern' }))
    await waitFor(() => {
      expect(emailPost).toHaveBeenCalledTimes(1)
    })
    expect(screen.getByText('E-Mail-Änderung wartet auf Admin-Freigabe.')).toBeInTheDocument()
  })

  it('RE_SYNC_FROM_RESPONSE: the form fields are re-synced from the server response after a save', async () => {
    // Review finding: trimmed/server-normalized values are shown immediately
    // (not after a reload). The user types an untrimmed name; the server
    // returns the trimmed value and the field must reflect it.
    const user = userEvent.setup()
    const postHandler = () => ({
      ok: true,
      status: 200,
      json: async () => ({
        ...baseProfile,
        first_name: 'Erika',
        last_name: 'Musterfrau',
        display_name: 'Erika M',
      }),
    })
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('Vorname')

    await user.clear(screen.getByLabelText('Vorname'))
    await user.type(screen.getByLabelText('Vorname'), '  Erika  ')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(screen.getByLabelText('Vorname')).toHaveValue('Erika')
    })
    expect(screen.getByLabelText('Nachname')).toHaveValue('Musterfrau')
    expect(screen.getByLabelText('Anzeigename')).toHaveValue('Erika M')
  })

  it('HEADER_CACHE: a successful base-data save updates gear.display_name in localStorage', async () => {
    // Review finding: the header greeting reads the cached display name; after
    // a save it must reflect the saved value (setDisplayName).
    const user = userEvent.setup()
    const postHandler = () => ({
      ok: true,
      status: 200,
      json: async () => ({
        ...baseProfile,
        first_name: 'Erika',
        last_name: 'Musterfrau',
        display_name: 'Erika Musterfrau',
      }),
    })
    vi.stubGlobal('fetch', profileFetch(baseProfile, postHandler))

    renderProfilePage()
    await screen.findByLabelText('Anzeigename')

    await user.clear(screen.getByLabelText('Anzeigename'))
    await user.type(screen.getByLabelText('Anzeigename'), 'Erika Musterfrau')
    await user.click(screen.getByRole('button', { name: 'Speichern' }))

    await waitFor(() => {
      expect(localStorage.getItem('gear.display_name')).toBe('Erika Musterfrau')
    })
  })
})

// postProfileResponse is the default success response for a base-data save.
function postProfileResponse(init?: RequestInit) {
  void init
  return { ok: true, status: 200, json: async () => baseProfile }
}
