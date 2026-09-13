// @vitest-environment jsdom
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { InspectionPage } from './InspectionPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'
import styles from './InspectionPage.module.css'

type InspectionEntry = string | { pathname: string; state?: Record<string, unknown> }

// submitInspection is forwarded to InspectionPage so the tests can HOLD the
// Story 5.2 placeholder submit in flight — the double-submit guard needs a real
// in-flight window to exercise.
function renderPage(initialEntry: InspectionEntry, submitInspection?: () => Promise<void>) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/inspection/:toolId" element={<InspectionPage submitInspection={submitInspection} />} />
          <Route path="/login" element={<div>Anmeldung</div>} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  )
}

function eligibleStart(toolId: string, overrides: Record<string, unknown> = {}) {
  return {
    tool_id: toolId,
    tool_name: 'Bohrmaschine-01',
    tool_type_id: 'id-t1',
    tool_type_name: 'Bohrmaschine',
    inspection_mode: 'checklist',
    ...overrides,
  }
}

function stubFetch(response: unknown) {
  const mock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', mock)
  return mock
}

// stubMatchMedia installs a matchMedia returning `reduced` for the reduce query
// (jsdom has no matchMedia by default — the page guards that, but the
// REDUCE_MOTION path needs a real stub BEFORE render because the preference is
// read once at mount).
function stubMatchMedia(reduced: boolean) {
  const mock = vi.fn().mockImplementation((query: string) => ({
    matches: reduced,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
    onchange: null,
  }))
  vi.stubGlobal('matchMedia', mock)
  return mock
}

describe('InspectionPage data loading (Story 5.1)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    cleanup()
  })

  it('STATE: renders the tool name + mode from router state without re-fetching', async () => {
    const mock = vi.fn()
    vi.stubGlobal('fetch', mock)
    renderPage({
      pathname: '/inspection/id-w1',
      state: { tool_name: 'Bohrmaschine-01', inventory_number: 'GEAR000001', inspection_mode: 'checklist' },
    })

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    // Router state is authoritative: no server call for the display data.
    expect(mock).not.toHaveBeenCalled()
  })

  it('REFETCH: without router state it re-fetches the tool + mode via startInspection', async () => {
    stubFetch({ ok: true, status: 200, json: async () => eligibleStart('id-w1') })
    renderPage('/inspection/id-w1')

    expect((await screen.findAllByText('Bohrmaschine-01')).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
  })

  it('IDENTIFIER_FALLBACK_FETCHED: on a refresh/deep link the /start payload has no inventory_number, so the identifier row falls back to the tool name', async () => {
    // Decision (Story 5.2 review): the Gerätenummer row stays visible and
    // falls back to the tool name when no inventory number exists — both the
    // "Werkzeug" name and the "Gerätenummer" identifier show the tool name.
    stubFetch({ ok: true, status: 200, json: async () => eligibleStart('id-w1') })
    renderPage('/inspection/id-w1')

    expect((await screen.findAllByText('Bohrmaschine-01')).length).toBe(2)
  })

  it('REFETCH_401: a 401 on the re-fetch clears auth state and redirects to /login', async () => {
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderPage('/inspection/id-w1')

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('REFETCH_ERROR: a failed re-fetch shows an inline error state (no crash, no raw id leak)', async () => {
    stubFetch({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    })
    renderPage('/inspection/id-w1')

    expect(await screen.findByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
  })

  it('PASS_FAIL: a pass_fail mode maps to "Pass/Fail"', async () => {
    renderPage({ pathname: '/inspection/id-w2', state: { tool_name: 'Schleifmaschine-01', inspection_mode: 'pass_fail' } })

    expect((await screen.findAllByText('Schleifmaschine-01')).length).toBe(2)
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
  })

  it('MISSING_MODE: an absent inspection_mode falls back to the neutral dash', async () => {
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01' } })

    expect((await screen.findAllByText('Bohrmaschine-01')).length).toBe(2)
    expect(screen.getByText('–')).toBeInTheDocument()
  })

  it('EMPTY_NAME_FALLBACK: an absent tool_name falls back to the tool id (`||`, not `??`)', async () => {
    renderPage({ pathname: '/inspection/id-w1', state: { inspection_mode: 'checklist' } })

    // EXACT: the id shows as BOTH the tool name and the identifier fallback —
    // mirroring the sibling identifier-fallback tests (name + identifier = 2).
    expect((await screen.findAllByText('id-w1')).length).toBe(2)
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
  })

  it('IDENTIFIER_FALLBACK: without an inventory number the header falls back to the tool name', async () => {
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01', inspection_mode: 'checklist' } })

    // name + identifier fallback → the name appears twice.
    expect((await screen.findAllByText('Bohrmaschine-01')).length).toBe(2)
  })
})

describe('InspectionPage UX foundation (Story 5.2)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    cleanup()
  })

  function renderLoaded(entry: InspectionEntry = {
    pathname: '/inspection/id-w1',
    state: { tool_name: 'Bohrmaschine-01', inventory_number: 'GEAR000001', inspection_mode: 'checklist' },
  }, submitInspection?: () => Promise<void>) {
    renderPage(entry, submitInspection)
  }

  it('RENDER: shows the single-screen content — header (name + identifier + mode), chips and submit', async () => {
    renderLoaded()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Ergebnis' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prüfung speichern' })).toBeInTheDocument()
  })

  it('CHIP_TOGGLE: tapping a chip selects it (radio), others clear, and the active class lands on the label', async () => {
    const user = userEvent.setup()
    renderLoaded()

    const passLabel = screen.getByText('OK/BESTANDEN').closest('label') as HTMLLabelElement
    const failLabel = screen.getByText('FEHLER/NICHT BESTANDEN').closest('label') as HTMLLabelElement
    const passRadio = screen.getByRole('radio', { name: 'OK/BESTANDEN' })
    const failRadio = screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })

    expect(passRadio).not.toBeChecked()
    expect(failRadio).not.toBeChecked()

    await user.click(failLabel)
    expect(failRadio).toBeChecked()
    expect(passRadio).not.toBeChecked()
    // The active class fills the tapped chip only.
    expect(failLabel.className).toContain('chipSelectedFail')
    expect(passLabel.className).not.toContain('chipSelectedPass')

    await user.click(passLabel)
    expect(passRadio).toBeChecked()
    expect(failRadio).not.toBeChecked()
    expect(passLabel.className).toContain('chipSelectedPass')
  })

  it('CHIP_KEYBOARD: the chips are native radios in one group — arrow keys move the selection', async () => {
    const user = userEvent.setup()
    renderLoaded()

    const passRadio = screen.getByRole('radio', { name: 'OK/BESTANDEN' })
    const failRadio = screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })

    passRadio.focus()
    await user.keyboard('{ArrowRight}')
    expect(failRadio).toBeChecked()

    await user.keyboard('{ArrowLeft}')
    expect(passRadio).toBeChecked()
  })

  it('SUBMIT_REQUIRES_RESULT: the submit button stays disabled until a result chip is selected, and re-disables on deselect (safety-critical)', async () => {
    renderLoaded()

    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    const passRadio = screen.getByRole('radio', { name: 'OK/BESTANDEN' })

    // No outcome selected → an outcome-less save is impossible.
    expect(button).toBeDisabled()

    fireEvent.click(passRadio)
    expect(passRadio).toBeChecked()
    expect(button).toBeEnabled()

    // Deselect by tapping the selected chip again (a mis-click must not trap
    // the user) → the submit disables again.
    fireEvent.click(passRadio)
    expect(screen.getByRole('radio', { name: 'OK/BESTANDEN' })).not.toBeChecked()
    expect(button).toBeDisabled()
  })

  it('SUBMIT: one submit shows the inline confirmation (names the tool + the outcome, SR role=status), disables the controls, then auto-returns to / after ~2s', async () => {
    vi.useFakeTimers()
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    // Inline confirmation, announced via role=status, names the tool + the
    // recorded outcome + a generic saved consequence (UX-DR7).
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/Bohrmaschine-01/)
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).toHaveTextContent(/gespeichert/)
    // Double-submit guard: the button stays disabled during the auto-return
    // delay.
    expect(button).toBeDisabled()

    // ~2s later the page navigates to '/' (Dashboard remount + refetch).
    await act(async () => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('CHIPS_DISABLED: the chips are disabled during the submit and after it, so the recorded result cannot diverge from the confirmation', async () => {
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    expect(screen.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeDisabled()
    expect(screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })).toBeDisabled()
  })

  it('DOUBLE_SUBMIT_GUARD: a second click while the placeholder submit is HELD in flight does not re-fire', async () => {
    let resolveSubmit: (() => void) | null = null
    const held = new Promise<void>((resolve) => {
      resolveSubmit = resolve
    })
    const submitSpy = vi.fn().mockReturnValue(held)
    renderLoaded(undefined, submitSpy)

    // A result is required before submit is possible.
    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    fireEvent.click(button)
    await act(async () => {})

    // Only ONE submit fired and it is still in flight (the placeholder is
    // held): no confirmation yet, and the button stays disabled.
    expect(submitSpy).toHaveBeenCalledTimes(1)
    expect(button).toBeDisabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    // Release the held placeholder → the confirmation renders.
    resolveSubmit!()
    await act(async () => {})
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('REDUCE_MOTION: under prefers-reduced-motion the auto-return redirects immediately (no ~2s delay)', async () => {
    vi.useFakeTimers()
    stubMatchMedia(true)
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    // The confirmation renders before the (immediate) redirect.
    expect(screen.getByRole('status')).toHaveTextContent(/gespeichert/)
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()

    // The 0ms reduced-motion timer fires without advancing the ~2s delay.
    await act(async () => {
      vi.advanceTimersByTime(0)
    })
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('UNSUBMITTED: leaving without submitting shows no confirmation and no navigation', async () => {
    renderLoaded()

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prüfung speichern' })).toBeDisabled()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
  })

  it('LAYOUT_SINGLE_COLUMN: the inspection content stays one column at all widths — header, chips and submit are SIBLINGS in the same section, never split side-by-side', async () => {
    renderLoaded()

    const header = screen.getByText('Bohrmaschine-01').closest('header') as HTMLElement
    const chips = screen.getByRole('group', { name: 'Ergebnis' })
    const submit = screen.getByRole('button', { name: 'Prüfung speichern' })
    const submitRow = submit.closest(`.${styles.submitRow}`) as HTMLElement
    const section = submit.closest(`.${styles.section}`) as HTMLElement

    // The tool header, the chips and the submit row are SIBLINGS — DIRECT
    // children of the SAME single-column section (UX-DR3/DR10). Wrapping any
    // block into a side-by-side (two-column) container at any width breaks this
    // assertion.
    //
    // LIMITATION: jsdom does not evaluate layout — the CSS modules are not
    // processed here, so getComputedStyle cannot prove `flex-direction: column`
    // rendering. This test structurally guards the single-column DOM contract
    // only; the genuine single-column-at-all-widths check is a browser/e2e
    // inspection (noted in the spec change log). No e2e suite is built in this
    // story.
    expect(section).not.toBeNull()
    expect(header.parentElement).toBe(section)
    expect(chips.parentElement).toBe(section)
    expect(submitRow.parentElement).toBe(section)
  })
})