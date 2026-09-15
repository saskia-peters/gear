// @vitest-environment jsdom
import { render, screen, cleanup, fireEvent, act, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { InspectionPage } from './InspectionPage.tsx'
import { ThemeProvider } from '../context/ThemeContext.tsx'
import type { ToolTypeChecklistItem } from '../auth/tools.ts'
import styles from './InspectionPage.module.css'

type InspectionEntry = string | { pathname: string; state?: Record<string, unknown> }

// checklistItemsFixture is the ordered checklist of a checklist-mode tool type
// (Story 5.2 mode-aware surface): three points the mode-aware surface renders
// as ONE PassFailChips group per item.
function checklistItemsFixture(): ToolTypeChecklistItem[] {
  return [
    { id: 'item-1', position: 1, label: 'Kabel' },
    { id: 'item-2', position: 2, label: 'Bohrfutter' },
    { id: 'item-3', position: 3, label: 'Sicherheitsschalter' },
  ]
}

// The page has no prop seam since Story 5.4 (the submit calls the real client);
// tests render it directly and stub fetch to exercise the submit.
function renderPage(initialEntry: InspectionEntry) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/" element={<div>Dashboard</div>} />
          <Route path="/inspection/:toolId" element={<InspectionPage />} />
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
    checklist_items: [],
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

// submitOkResponse is a server-authoritative 200 pass_fail submit response
// (Story 5.3 contract, consumed by Story 5.4): the persisted record + the
// derived status. overall_result (what was persisted) and status.status (the
// derived state) are INDEPENDENT — a pass can be persisted on an already-OOS
// tool (a passing inspection does NOT clear OOS, FR-15), so the confirmation
// must follow the response, not the local chip.
function submitOkResponse(overallResult: 'pass' | 'fail' = 'pass', status: 'oos' | 'red' | 'orange' | 'green' = 'green') {
  return {
    ok: true,
    status: 200,
    json: async () => ({
      inspection: {
        id: 'insp-1',
        tool_id: 'id-w1',
        inspector_id: 'user-1',
        mode: 'pass_fail',
        overall_result: overallResult,
        notes: '',
        submitted_at: '2026-09-14T10:00:00Z',
        items: [],
      },
      status: { status, next_due: status === 'oos' || status === 'red' ? null : '2027-09-14T10:00:00Z' },
    }),
  }
}

// submitErrorResponse is a non-2xx submit response in the uniform envelope.
function submitErrorResponse(status: number, message: string) {
  return { ok: false, status, json: async () => ({ error: { code: 'error', message } }) }
}

// checklistSubmitOkResponse is a server-authoritative 200 CHECKLIST submit
// response (Story 5.5 contract, consumed by the confirmation): the persisted
// record carries the per-item snapshot (label/position/results) plus the
// overall result, and the derived status drives the OOS consequence.
// overall_result and status.status are INDEPENDENT — the confirmation must
// follow the response, not the local chips.
function checklistSubmitOkResponse(
  overallResult: 'pass' | 'fail',
  status: 'oos' | 'red' | 'orange' | 'green',
  items: Array<{ item_id: string; label: string; result: 'pass' | 'fail' }>,
) {
  return {
    ok: true,
    status: 200,
    json: async () => ({
      inspection: {
        id: 'insp-1',
        tool_id: 'id-w1',
        inspector_id: 'user-1',
        mode: 'checklist',
        overall_result: overallResult,
        notes: '',
        submitted_at: '2026-09-14T10:00:00Z',
        items: items.map((item, index) => ({
          id: `insp-item-${index + 1}`,
          item_id: item.item_id,
          label: item.label,
          position: index + 1,
          result: item.result,
        })),
      },
      status: { status, next_due: status === 'oos' || status === 'red' ? null : '2027-09-14T10:00:00Z' },
    }),
  }
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

  it('STATE: renders the tool name + identifier + type name + mode from router state without re-fetching', async () => {
    const mock = vi.fn()
    vi.stubGlobal('fetch', mock)
    renderPage({
      pathname: '/inspection/id-w1',
      state: {
        tool_name: 'Bohrmaschine-01',
        tool_type_name: 'Bohrmaschine',
        inventory_number: 'GEAR000001',
        inspection_mode: 'checklist',
        checklist_items: [],
      },
    })

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    // Story 5.2 mode-aware header: the Gerätetyp row shows the type name.
    expect(screen.getByText('Gerätetyp')).toBeInTheDocument()
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    // Router state is authoritative: no server call for the display data.
    expect(mock).not.toHaveBeenCalled()
  })

  it('REFETCH: without router state it re-fetches the tool + mode via startInspection', async () => {
    stubFetch({ ok: true, status: 200, json: async () => eligibleStart('id-w1') })
    renderPage('/inspection/id-w1')

    expect((await screen.findAllByText('Bohrmaschine-01')).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    // The fetched /start payload carries the type name → the Gerätetyp row.
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
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

  it('PASS_FAIL: a pass_fail mode maps to "Pass/Fail" and renders the single pass/fail toggle', async () => {
    renderPage({ pathname: '/inspection/id-w2', state: { tool_name: 'Schleifmaschine-01', inspection_mode: 'pass_fail' } })

    expect((await screen.findAllByText('Schleifmaschine-01')).length).toBe(2)
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
    // pass_fail renders the SINGLE chips group (one overall result).
    expect(screen.getByRole('group', { name: 'Ergebnis' })).toBeInTheDocument()
  })

  it('MODE_DEFAULT_UNKNOWN: a missing or unknown inspection_mode defaults to pass_fail (the single toggle)', async () => {
    // Missing mode → the safe default pass_fail.
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01' } })

    expect(await screen.findByRole('group', { name: 'Ergebnis' })).toBeInTheDocument()
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()

    cleanup()

    // Unknown mode value → pass_fail too (never an empty/unrenderable surface).
    renderPage({ pathname: '/inspection/id-w1', state: { tool_name: 'Bohrmaschine-01', inspection_mode: 'matrix' } })

    expect(await screen.findByRole('group', { name: 'Ergebnis' })).toBeInTheDocument()
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
    expect(screen.queryByText('Checkliste')).not.toBeInTheDocument()
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

  // The pass_fail default entry: the SINGLE-chip surface the Story 5.2
  // foundation tests exercise (a pass_fail fixture carries an empty checklist).
  function renderLoaded(
    entry: InspectionEntry = {
      pathname: '/inspection/id-w1',
      state: {
        tool_name: 'Bohrmaschine-01',
        tool_type_name: 'Bohrmaschine',
        inventory_number: 'GEAR000001',
        inspection_mode: 'pass_fail',
      },
    },
  ) {
    renderPage(entry)
  }

  // The checklist entry: a checklist-mode type WITH its ordered items.
  const CHECKLIST_ENTRY = {
    pathname: '/inspection/id-w1',
    state: {
      tool_name: 'Bohrmaschine-01',
      tool_type_name: 'Bohrmaschine',
      inventory_number: 'GEAR000001',
      inspection_mode: 'checklist',
      checklist_items: checklistItemsFixture(),
    },
  }

  // answerAllPass marks every checklist item as passed via the per-item chips
  // (the fixture is the 3-item checklist; state-path render is synchronous, so
  // getByRole works without awaiting).
  function answerAllPass() {
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(schalter.getByRole('radio', { name: 'OK/BESTANDEN' }))
  }

  it('RENDER: shows the single-screen content — header (name + identifier + type + mode), chips and submit', async () => {
    renderLoaded()

    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('GEAR000001')).toBeInTheDocument()
    expect(screen.getByText('Gerätetyp')).toBeInTheDocument()
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('Pass/Fail')).toBeInTheDocument()
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

  it('COMMENT: the optional Anmerkung textarea is present, editable, and does NOT gate the submit', async () => {
    renderLoaded()

    // The comment field exists with a German label and placeholder.
    const comment = screen.getByLabelText('Anmerkung (optional)')
    expect(comment).toBeInTheDocument()

    // It is OPTIONAL: with no result selected the submit is still disabled
    // (gated by the result, not the comment), and typing a comment does not
    // enable it.
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    expect(button).toBeDisabled()
    fireEvent.change(comment, { target: { value: 'Ölstand geprüft, auffällige Geräusche.' } })
    expect(comment).toHaveValue('Ölstand geprüft, auffällige Geräusche.')
    expect(button).toBeDisabled()

    // Once a result is selected the submit enables — the comment stays
    // optional and editable alongside it.
    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    expect(button).toBeEnabled()
    fireEvent.change(comment, { target: { value: 'Alles in Ordnung.' } })
    expect(comment).toHaveValue('Alles in Ordnung.')
  })

  it('SUBMIT: one submit shows the inline confirmation (names the tool + the outcome, SR role=status), disables the controls, then auto-returns to / after ~2s', async () => {
    vi.useFakeTimers()
    // Story 5.4: the pass_fail submit posts the real payload to the real
    // endpoint (server-derived status drives the confirmation).
    const fetchMock = stubFetch(submitOkResponse('pass', 'green'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    // The real client POSTs { mode: 'pass_fail', result, notes, items: [] } to
    // /api/v1/tools/{id}/inspection (FR-13: identity/timestamp/result/notes).
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ mode: 'pass_fail', result: 'pass', notes: '', items: [] })

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
    stubFetch(submitOkResponse('pass', 'green'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    expect(screen.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeDisabled()
    expect(screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })).toBeDisabled()
  })

  it('DOUBLE_SUBMIT_GUARD: a second click while the submit fetch is HELD in flight does not re-fire', async () => {
    type FetchResponse = { ok: boolean; status: number; json: () => Promise<unknown> }
    let resolveSubmit: ((value: FetchResponse) => void) | null = null
    const held = new Promise<FetchResponse>((resolve) => {
      resolveSubmit = resolve
    })
    const fetchMock = vi.fn().mockImplementation(() => held)
    vi.stubGlobal('fetch', fetchMock)
    renderLoaded()

    // A result is required before submit is possible.
    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    fireEvent.click(button)
    await act(async () => {})

    // Only ONE submit fired and the fetch is still HELD in flight (the real
    // client): no confirmation yet, the button stays disabled AND shows the
    // in-flight label (patch 12: aria-busy + "Wird gespeichert...").
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(button).toBeDisabled()
    const busyButton = screen.getByRole('button', { name: 'Wird gespeichert...' })
    expect(busyButton).toBeDisabled()
    expect(busyButton).toHaveAttribute('aria-busy', 'true')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    // Release the held response → the confirmation renders.
    resolveSubmit!(submitOkResponse('pass', 'green'))
    await act(async () => {})
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('REDUCE_MOTION: under prefers-reduced-motion the auto-return redirects immediately (no ~2s delay)', async () => {
    vi.useFakeTimers()
    stubMatchMedia(true)
    stubFetch(submitOkResponse('pass', 'green'))
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

  it('CHECKLIST_HEADER: a checklist-mode type shows the tool type name in the Gerätetyp row (from state or the fetched /start payload)', async () => {
    // From router state (the dashboard forwards it).
    renderLoaded(CHECKLIST_ENTRY)
    expect(await screen.findByText('Bohrmaschine-01')).toBeInTheDocument()
    expect(screen.getByText('Gerätetyp')).toBeInTheDocument()
    expect(screen.getByText('Bohrmaschine')).toBeInTheDocument()

    // From the fetched /start response (deep-link refresh — the /start payload
    // carries the type name + the checklist items).
    cleanup()
    stubFetch({
      ok: true,
      status: 200,
      json: async () => eligibleStart('id-w1', { checklist_items: checklistItemsFixture() }),
    })
    renderPage('/inspection/id-w1')

    expect(await screen.findByText('Bohrmaschine')).toBeInTheDocument()
    expect(screen.getByText('Checkliste')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Kabel' })).toBeInTheDocument()
  })

  it('CHECKLIST_RENDERS_PER_ITEM: checklist mode renders ONE chips group PER item with the item labels', async () => {
    renderLoaded(CHECKLIST_ENTRY)

    expect(await screen.findByRole('group', { name: 'Kabel' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Bohrfutter' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Sicherheitsschalter' })).toBeInTheDocument()
    // Checklist mode has NO single "Ergebnis" group (that is the pass_fail
    // toggle); every item's group carries its own pass + fail radio.
    expect(screen.queryByRole('group', { name: 'Ergebnis' })).not.toBeInTheDocument()
    expect(screen.getAllByRole('radio', { name: 'OK/BESTANDEN' })).toHaveLength(3)
    expect(screen.getAllByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })).toHaveLength(3)
  })

  it('CHECKLIST_PER_ITEM_TOGGLE: toggling one item is independent of the others (a unique radio group per item)', async () => {
    const user = userEvent.setup()
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })

    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const kabelPass = kabel.getByRole('radio', { name: 'OK/BESTANDEN' })
    const bohrfutterPass = bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' })

    // The radio groups are UNIQUE per item (each group's radios share a
    // distinct name attribute) — selecting Kabel must not affect Bohrfutter.
    expect(kabelPass).toHaveAttribute('name', 'inspection-item-item-1')
    expect(bohrfutterPass).toHaveAttribute('name', 'inspection-item-item-2')

    await user.click(kabel.getByText('OK/BESTANDEN').closest('label') as HTMLLabelElement)
    expect(kabelPass).toBeChecked()
    expect(bohrfutterPass).not.toBeChecked()

    await user.click(bohrfutter.getByText('FEHLER/NICHT BESTANDEN').closest('label') as HTMLLabelElement)
    expect(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })).toBeChecked()
    expect(kabelPass).toBeChecked()
  })

  it('CHECKLIST_SUBMIT_REQUIRES_ALL: the submit stays disabled until EVERY item has a result (FR-12)', async () => {
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })

    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))

    // No item answered → an outcome-less save is impossible.
    expect(button).toBeDisabled()

    // 2 of 3 answered → still disabled (all-items-required, FR-12).
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    expect(button).toBeDisabled()

    // All 3 answered → enabled.
    fireEvent.click(schalter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    expect(button).toBeEnabled()

    // Deselecting one item re-disables the submit (a mis-click must not save
    // a partial checklist).
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    expect(button).toBeDisabled()
  })

  it('CHECKLIST_SUBMIT_ALL_PASS: one submit with every item passed posts the real checklist payload and confirms "BESTANDEN" from the server, then auto-returns', async () => {
    vi.useFakeTimers()
    // Story 5.5: the checklist submit POSTs the per-item results + the derived
    // overall result to the real endpoint; the server returns the derived
    // status that drives the confirmation.
    const fetchMock = stubFetch(
      checklistSubmitOkResponse('pass', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'pass' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    // State-path render is synchronous — the per-item groups exist immediately
    // (findBy* would poll on a fake timer and hang, so getBy* is used here).
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(schalter.getByRole('radio', { name: 'OK/BESTANDEN' }))

    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    // The real client POSTs { mode: 'checklist', result, notes, items: the
    // type's checklist with each item's result } to
    // /api/v1/tools/{id}/inspection (FR-12: per-item + overall result persisted).
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      mode: 'checklist',
      result: 'pass',
      notes: '',
      items: [
        { item_id: 'item-1', result: 'pass' },
        { item_id: 'item-2', result: 'pass' },
        { item_id: 'item-3', result: 'pass' },
      ],
    })

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/Bohrmaschine-01/)
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).not.toHaveTextContent(/NICHT BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
    expect(status).toHaveTextContent(/gespeichert/)

    await act(async () => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('CHECKLIST_SUBMIT_SOME_FAILED: a checklist with failed items posts result fail and confirms the SERVER failure count + the OOS consequence', async () => {
    vi.useFakeTimers()
    // Story 5.5: the server persists the per-item results, derives oos from the
    // failed items (FR-14/AD-4) and returns it — the confirmation names the
    // SERVER count + consequence, not the local chips.
    const fetchMock = stubFetch(
      checklistSubmitOkResponse('fail', 'oos', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'fail' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'fail' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    // State-path render is synchronous — see CHECKLIST_SUBMIT_ALL_PASS.
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    fireEvent.click(schalter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))

    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    const body = JSON.parse(init.body as string)
    expect(body.mode).toBe('checklist')
    expect(body.result).toBe('fail')
    expect(body.items).toEqual([
      { item_id: 'item-1', result: 'pass' },
      { item_id: 'item-2', result: 'fail' },
      { item_id: 'item-3', result: 'fail' },
    ])

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/2 von 3 Punkten NICHT BESTANDEN/)
    expect(status).toHaveTextContent(/⛔ Wird als Außer Betrieb gesperrt/)
    expect(status).toHaveTextContent(/Bohrmaschine-01/)
    expect(status).toHaveTextContent(/gespeichert/)
  })

  it('CHECK_SUBMIT_INCOMPLETE: an unanswered item blocks the submit — the real client is never called (FR-12)', async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => ({}) })
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    // 2 of 3 answered → the button is disabled, a click is a no-op.
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    expect(button).toBeDisabled()
    fireEvent.click(button)
    await act(async () => {})
    // Nothing is persisted: the real endpoint was never hit.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('CHECK_ALLE_BESTANDEN: the "Alle bestanden" shortcut sets EVERY item to pass, enables the submit, stays per-item editable, and submits an all-pass payload (FR-12/UX-DR7)', async () => {
    const fetchMock = stubFetch(
      checklistSubmitOkResponse('pass', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'pass' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    expect(button).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Alle bestanden' }))

    // Every item is now marked passed → the submit enables (all-items rule).
    expect(kabel.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeChecked()
    expect(bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeChecked()
    expect(schalter.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeChecked()
    expect(button).toBeEnabled()

    // It is a convenience, not a gate: per-item answers stay editable.
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    expect(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' })).toBeChecked()
    expect(kabel.getByRole('radio', { name: 'OK/BESTANDEN' })).toBeChecked()

    // Re-mark everything passed and submit → the posted payload is the
    // all-pass checklist (mode checklist, result pass, every item pass).
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(button)
    await act(async () => {})

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    expect(JSON.parse(init.body as string)).toEqual({
      mode: 'checklist',
      result: 'pass',
      notes: '',
      items: [
        { item_id: 'item-1', result: 'pass' },
        { item_id: 'item-2', result: 'pass' },
        { item_id: 'item-3', result: 'pass' },
      ],
    })
    expect(screen.getByRole('status')).toHaveTextContent(/BESTANDEN/)
  })

  it('CHECK_ALLE_BESTANDEN_HIDDEN: the "Alle bestanden" shortcut renders ONLY in checklist mode', async () => {
    renderLoaded() // pass_fail entry
    await screen.findByRole('group', { name: 'Ergebnis' })
    expect(screen.queryByRole('button', { name: 'Alle bestanden' })).not.toBeInTheDocument()
  })

  it('CHECK_SUBMIT_400: a checklist validation 400 shows the German reason inline (role=alert) with NO confirmation and NO navigation', async () => {
    stubFetch({
      ok: false,
      status: 400,
      json: async () => ({ error: { code: 'invalid_request', message: 'Die Prüfpunkte stimmen nicht mit dem Gerätetyp überein.' } }),
    })
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Die Prüfpunkte stimmen nicht mit dem Gerätetyp überein.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('CHECK_SUBMIT_401: a 401 on a checklist submit clears auth state and redirects to /login', async () => {
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('CHECK_SUBMIT_403_OOS_TOOL: a checklist submit on an OOS tool answers 403 with nothing persisted (Story 5.6)', async () => {
    stubFetch({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Das Werkzeug ist außer Betrieb und kann nicht geprüft werden.' } }),
    })
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Das Werkzeug ist außer Betrieb und kann nicht geprüft werden.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('CHECK_SUBMIT_500: a 500 on a checklist submit shows the German inline error with no confirmation, and the controls re-enable', async () => {
    stubFetch(submitErrorResponse(500, 'Ein interner Fehler ist aufgetreten.'))
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('CHECKLIST_OUTCOME_FROM_SERVER_RECORD: the checklist confirmation names the SERVER-persisted count, not the local chips (Story 5.5)', async () => {
    vi.useFakeTimers()
    // The user tapped ALL PASS locally, but the server persisted a FAIL (the
    // record is authoritative) → the confirmation must name the server count.
    stubFetch(
      checklistSubmitOkResponse('fail', 'oos', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'fail' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'fail' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    // State-path render is synchronous — see CHECKLIST_SUBMIT_ALL_PASS.
    answerAllPass()
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/Ergebnis: 2 von 3 Punkten NICHT BESTANDEN/)
    expect(status).not.toHaveTextContent(/Ergebnis: BESTANDEN/)
  })

  it('OOS_CONSEQUENCE_CHECKLIST_SERVER_DRIVEN: the checklist consequence follows the SERVER status, never the local chips — a failed checklist whose response says green names the failure count but NO OOS copy (AD-4/AD-5)', async () => {
    vi.useFakeTimers()
    // The user marked an item FAIL locally, but the response status is green
    // (the server is authoritative — a naive client guess must never gate the
    // consequence). The confirmation still names the server failure count but
    // omits the OOS sentence.
    stubFetch(
      checklistSubmitOkResponse('fail', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'fail' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    // State-path render is synchronous — see CHECKLIST_SUBMIT_ALL_PASS.
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    fireEvent.click(schalter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/1 von 3 Punkten NICHT BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('CHECKLIST_CONTROLS_DISABLED: after a successful checklist submit every per-item chip and the "Alle bestanden" button are disabled, so the recorded results cannot diverge from the confirmation', async () => {
    stubFetch(
      checklistSubmitOkResponse('pass', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'pass' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    // All per-item radios (pass + fail for each of the 3 items) are locked.
    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(6)
    for (const radio of radios) {
      expect(radio).toBeDisabled()
    }
    // The "Alle bestanden" shortcut locks too (the submit button lock is
    // already asserted by the checklist submit tests).
    expect(screen.getByRole('button', { name: 'Alle bestanden' })).toBeDisabled()
  })

  it('CHECK_SUBMIT_NOTES: a non-empty Anmerkung travels in the checklist POST body as notes', async () => {
    const fetchMock = stubFetch(
      checklistSubmitOkResponse('pass', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'pass' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    fireEvent.change(screen.getByLabelText('Anmerkung (optional)'), {
      target: { value: 'Ölstand geprüft, auffällige Geräusche.' },
    })
    answerAllPass()
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    const body = JSON.parse(init.body as string)
    expect(body.mode).toBe('checklist')
    expect(body.notes).toBe('Ölstand geprüft, auffällige Geräusche.')
    // The submit succeeded → the confirmation renders.
    expect(screen.getByRole('status')).toHaveTextContent(/gespeichert/)
  })

  it('CHECK_SUBMIT_INVALID_SERVER_RESPONSE: a 200 whose record lacks the consumed fields (no items) shows "Ungültige Serverantwort." instead of confirming', async () => {
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ inspection: { id: 'x', overall_result: 'fail' }, status: { status: 'oos' } }),
    })
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })
    answerAllPass()
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Ungültige Serverantwort.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('OOS_CONSEQUENCE_FAIL: a failing pass_fail submit names the OOS consequence from the SERVER-derived status (Story 5.4, UX-DR6/DR8)', async () => {
    vi.useFakeTimers()
    // Story 5.4: the confirmation follows the RESPONSE status (the server
    // derives OOS from the full inspection + reinstatement history) — the
    // 200 body carries status oos.
    stubFetch(submitOkResponse('fail', 'oos'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/NICHT BESTANDEN/)
    expect(status).toHaveTextContent(/⛔ Wird als Außer Betrieb gesperrt/)
  })

  it('OOS_CONSEQUENCE_PASS: a passing pass_fail submit names NO OOS consequence', async () => {
    vi.useFakeTimers()
    stubFetch(submitOkResponse('pass', 'green'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('OOS_CONSEQUENCE_SERVER_DRIVEN: the consequence follows the SERVER status, never the local result — a fail submit whose response says green shows NO OOS copy (AD-4/AD-5)', async () => {
    vi.useFakeTimers()
    // The local result is FAIL, but the response status is green (the server
    // is authoritative — a naive client guess must never gate the
    // consequence). The confirmation still names NICHT BESTANDEN (the outcome
    // comes from the server-persisted overall_result) but omits the OOS
    // sentence.
    stubFetch(submitOkResponse('fail', 'green'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/NICHT BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('SUBMIT_400: a server validation 400 shows the German reason inline (role=alert) with NO confirmation and NO navigation, and the controls re-enable', async () => {
    stubFetch({
      ok: false,
      status: 400,
      json: async () => ({ error: { code: 'invalid_request', message: 'Bitte wähle ein gültiges Prüfergebnis.' } }),
    })
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    // Inline German role=alert, no navigation, no confirmation.
    expect(screen.getByRole('alert')).toHaveTextContent('Bitte wähle ein gültiges Prüfergebnis.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    // The submit re-enables for a retry.
    expect(button).toBeEnabled()
  })

  it('SUBMIT_403: a gating 403 shows the German reason inline with no navigation or confirmation', async () => {
    stubFetch({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Erforderliche Qualifikation fehlt.' } }),
    })
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Erforderliche Qualifikation fehlt.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('SUBMIT_404: an unknown/archived tool shows the German not-found message inline, no navigation or confirmation', async () => {
    stubFetch({
      ok: false,
      status: 404,
      json: async () => ({ error: { code: 'not_found', message: 'Das Werkzeug wurde nicht gefunden.' } }),
    })
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Das Werkzeug wurde nicht gefunden.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('SUBMIT_401: a 401 on the submit clears auth state and redirects to /login', async () => {
    stubFetch({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'unauthorized', message: 'Authentifizierung erforderlich.' } }),
    })
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    expect(await screen.findByText('Anmeldung')).toBeInTheDocument()
    expect(localStorage.getItem('gear.session_token')).toBeNull()
  })

  it('SUBMIT_NETWORK: a connection failure shows the German inline error with no navigation or confirmation', async () => {
    const mock = vi.fn().mockRejectedValue(new TypeError('fetch failed'))
    vi.stubGlobal('fetch', mock)
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent(/Verbindung zum Server fehlgeschlagen/)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('SUBMIT_500: a 500 submit shows the German inline error with no navigation or confirmation, and the controls re-enable', async () => {
    stubFetch(submitErrorResponse(500, 'Ein interner Fehler ist aufgetreten.'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Ein interner Fehler ist aufgetreten.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('SUBMIT_NOTES: a non-empty Anmerkung travels in the POST body as notes', async () => {
    const fetchMock = stubFetch(submitOkResponse('pass', 'green'))
    renderLoaded()

    fireEvent.change(screen.getByLabelText('Anmerkung (optional)'), {
      target: { value: 'Ölstand geprüft, auffällige Geräusche.' },
    })
    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/tools/id-w1/inspection')
    const body = JSON.parse(init.body as string)
    expect(body.mode).toBe('pass_fail')
    expect(body.result).toBe('pass')
    expect(body.notes).toBe('Ölstand geprüft, auffällige Geräusche.')
    expect(body.items).toEqual([])
    // The submit succeeded → the confirmation renders.
    expect(screen.getByRole('status')).toHaveTextContent(/gespeichert/)
  })

  it('SUBMIT_RETRY_AFTER_ERROR: after a 400 the submitError clears and a subsequent successful submit renders the confirmation', async () => {
    // First submit answers 400, the retry answers 200 (the server was
    // temporarily rejecting a stale payload).
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(submitErrorResponse(400, 'Bitte wähle ein gültiges Prüfergebnis.'))
      .mockResolvedValueOnce(submitOkResponse('pass', 'green'))
    vi.stubGlobal('fetch', fetchMock)
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})
    expect(screen.getByRole('alert')).toHaveTextContent('Bitte wähle ein gültiges Prüfergebnis.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    // Retry: the same payload now succeeds — the stale alert clears and the
    // confirmation renders (the submit is NOT wedged).
    fireEvent.click(button)
    await act(async () => {})
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(/gespeichert/)
  })

  it('OUTCOME_FROM_SERVER_RECORD: the confirmation names the SERVER-persisted overall_result, not the local chip (Story 5.4)', async () => {
    vi.useFakeTimers()
    // The user tapped BESTANDEN locally, but the server persisted a FAIL (the
    // record is authoritative) → the confirmation must name NICHT BESTANDEN.
    stubFetch(submitOkResponse('fail', 'oos'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/Ergebnis: NICHT BESTANDEN/)
    expect(status).not.toHaveTextContent(/Ergebnis: BESTANDEN/)
  })

  it('OOS_CONSEQUENCE_ORANGE: a response status orange names NO OOS consequence', async () => {
    vi.useFakeTimers()
    stubFetch(submitOkResponse('pass', 'orange'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('OOS_CONSEQUENCE_RED: a response status red names NO OOS consequence', async () => {
    vi.useFakeTimers()
    stubFetch(submitOkResponse('pass', 'red'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('OOS_CONSEQUENCE_PASS_ON_OOS_TOOL: a PASSING inspection on an already-OOS tool keeps it OOS (FR-15) — the server status oos drives the consequence even for a pass result', async () => {
    vi.useFakeTimers()
    // The server derives oos from the full history: this tool was already OOS
    // (an earlier fail, no reinstatement) and this PASS does NOT clear it —
    // the confirmation follows the server, naming BESTANDEN + the OOS
    // consequence.
    stubFetch(submitOkResponse('pass', 'oos'))
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/BESTANDEN/)
    expect(status).toHaveTextContent(/⛔ Wird als Außer Betrieb gesperrt/)
  })

  it('SUBMIT_INVALID_SERVER_RESPONSE: a 200 body without the record/status shows "Ungültige Serverantwort." instead of confirming (patch 14)', async () => {
    stubFetch({ ok: true, status: 200, json: async () => ({}) })
    renderLoaded()

    fireEvent.click(screen.getByRole('radio', { name: 'OK/BESTANDEN' }))
    const button = screen.getByRole('button', { name: 'Prüfung speichern' })
    fireEvent.click(button)
    await act(async () => {})

    expect(screen.getByRole('alert')).toHaveTextContent('Ungültige Serverantwort.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(button).toBeEnabled()
  })

  it('OOS_CONSEQUENCE_CHECKLIST: the checklist consequence follows the SERVER-derived status — a failed submit names "⛔ Wird als Außer Betrieb gesperrt.", an all-pass submit does not (FR-14/AD-4)', async () => {
    vi.useFakeTimers()
    // Story 5.5: the consequence comes from the RESPONSE status (the server
    // derives OOS from the failed items + the full history), never a local
    // guess.
    stubFetch(
      checklistSubmitOkResponse('fail', 'oos', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'fail' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    // State-path render is synchronous — see CHECKLIST_SUBMIT_ALL_PASS.
    const kabel = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))

    // One failed item → the server derives oos and the consequence is named
    // (FR-14: any failed item flips the tool OOS).
    fireEvent.click(kabel.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter.getByRole('radio', { name: 'FEHLER/NICHT BESTANDEN' }))
    fireEvent.click(schalter.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})
    expect(screen.getByRole('status')).toHaveTextContent(/1 von 3 Punkten NICHT BESTANDEN/)
    expect(screen.getByRole('status')).toHaveTextContent(/⛔ Wird als Außer Betrieb gesperrt/)

    // All items pass → the server returns green → no consequence.
    cleanup()
    vi.clearAllTimers()
    stubFetch(
      checklistSubmitOkResponse('pass', 'green', [
        { item_id: 'item-1', label: 'Kabel', result: 'pass' },
        { item_id: 'item-2', label: 'Bohrfutter', result: 'pass' },
        { item_id: 'item-3', label: 'Sicherheitsschalter', result: 'pass' },
      ]),
    )
    renderLoaded(CHECKLIST_ENTRY)
    const kabel2 = within(screen.getByRole('group', { name: 'Kabel' }))
    const bohrfutter2 = within(screen.getByRole('group', { name: 'Bohrfutter' }))
    const schalter2 = within(screen.getByRole('group', { name: 'Sicherheitsschalter' }))
    fireEvent.click(kabel2.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(bohrfutter2.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(schalter2.getByRole('radio', { name: 'OK/BESTANDEN' }))
    fireEvent.click(screen.getByRole('button', { name: 'Prüfung speichern' }))
    await act(async () => {})
    expect(screen.getByRole('status')).toHaveTextContent(/BESTANDEN/)
    expect(screen.getByRole('status')).not.toHaveTextContent(/Außer Betrieb/)
  })

  it('CHECKLIST_EMPTY: checklist mode with missing/empty items falls back to an empty-checklist note — no chips, submit disabled', async () => {
    renderLoaded({
      pathname: '/inspection/id-w1',
      state: { tool_name: 'Bohrmaschine-01', inspection_mode: 'checklist' },
    })

    expect(await screen.findByText('Für diesen Gerätetyp sind keine Prüfpunkte hinterlegt.')).toBeInTheDocument()
    // No chips groups render, and an empty checklist must not be savable as a
    // meaningless BESTANDEN (safety-critical).
    expect(screen.queryByRole('group', { name: 'Ergebnis' })).not.toBeInTheDocument()
    expect(screen.queryByRole('radio')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prüfung speichern' })).toBeDisabled()
  })

  it('LAYOUT_SINGLE_COLUMN_CHECKLIST: in checklist mode the header, every item group and the submit stay SIBLINGS in one section (never split)', async () => {
    renderLoaded(CHECKLIST_ENTRY)
    await screen.findByRole('group', { name: 'Kabel' })

    const header = screen.getByText('Bohrmaschine-01').closest('header') as HTMLElement
    const kabelGroup = screen.getByRole('group', { name: 'Kabel' })
    const submit = screen.getByRole('button', { name: 'Prüfung speichern' })
    const submitRow = submit.closest(`.${styles.submitRow}`) as HTMLElement
    const section = submit.closest(`.${styles.section}`) as HTMLElement

    expect(section).not.toBeNull()
    expect(header.parentElement).toBe(section)
    expect(kabelGroup.parentElement).toBe(section)
    expect(screen.getByRole('group', { name: 'Bohrfutter' }).parentElement).toBe(section)
    expect(submitRow.parentElement).toBe(section)
  })
})