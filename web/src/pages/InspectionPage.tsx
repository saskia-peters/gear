import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { PassFailChips } from '../components/PassFailChips.tsx'
import type { PassFailValue } from '../components/PassFailChips.tsx'
import { clearAuthState } from '../auth/authState.ts'
import { startInspection, submitInspection, submitInspectionPlaceholder } from '../auth/tools.ts'
import type {
  InspectionResult,
  InspectionStart,
  InspectionSubmitStatus,
  ToolTypeChecklistItem,
} from '../auth/tools.ts'
import { OOS_STATUS } from '../types/filters.ts'
import styles from './InspectionPage.module.css'

// AUTO_RETURN_MS is the post-submit delay before the page auto-returns to the
// refreshed Dashboard (UX-DR7/DR8). prefers-reduced-motion skips it (0ms).
const AUTO_RETURN_MS = 2000

// EffectiveMode is the rendering mode after the safe-default resolution: an
// unknown/missing wire mode is treated as pass_fail (the single toggle).
type EffectiveMode = 'pass_fail' | 'checklist'

// effectiveMode resolves the wire inspection_mode to the rendering mode (Story
// 5.2 mode-aware decision): 'checklist' renders ONE PassFailChips group per
// checklist item; anything else — pass_fail, a missing OR an unknown mode —
// renders the SINGLE pass/fail toggle as the safe default.
function effectiveMode(mode: string | undefined): EffectiveMode {
  return mode === 'checklist' ? 'checklist' : 'pass_fail'
}

// modeLabel maps the resolved inspection_mode to its German display label.
function modeLabel(mode: string | undefined): string {
  if (mode === 'checklist') return 'Checkliste'
  return 'Pass/Fail'
}

// prefersReducedMotion reports the OS "reduce motion" preference (UX-DR9). The
// guard covers jsdom/tests where window.matchMedia is absent → normal motion.
function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// InspectionState is the router state the dashboard navigates here with (Story
// 5.1 + 5.2): the tool + its mode from the eligible /start payload, the type's
// ordered checklist items (forwarded for checklist-mode types so the mode-aware
// surface renders without a re-fetch), plus the inventory number carried from
// the tool LIST so the header can show the identifier (it falls back to the
// tool name).
interface InspectionState {
  tool_name?: string
  tool_type_name?: string
  inspection_mode?: string
  inventory_number?: string
  checklist_items?: ToolTypeChecklistItem[]
}

// InspectionPage is the SINGLE-COLUMN inspection screen (Story 5.2 + 5.4,
// FR-8/UX-DR3/DR5/DR6/DR7/DR8/DR9/DR10): a tool header (name + identifier +
// type + mode), large green/red Pass/Fail chips and ONE submit → inline
// confirmation → ~2s auto-return to a refreshed Dashboard (immediate under
// Reduce Motion). The inspection content NEVER splits into a two-column layout
// at any width (safety-critical input, UX-DR10). A result MUST be chosen before
// the submit enables — an inspection can never be saved without an outcome.
//
// PASS_FAIL is REAL since Story 5.4 (FR-13): the submit posts
// { mode: 'pass_fail', result, notes, items: [] } to
// POST /api/v1/tools/{id}/inspection; the server persists identity/timestamp/
// result/notes (AD-4) and returns the derived status. The confirmation names
// the OUTCOME from the server-persisted record (inspection.overall_result) and
// the OOS consequence ONLY from the returned status (status.status === 'oos') —
// never from local state: a passing inspection does NOT clear OOS (reinstatement
// is the sole exit, FR-15), so the server's derived status is authoritative. A
// failed submit (400/403/404/500/network) shows an inline German role=alert
// with NO navigation/confirmation; 401 clears auth and redirects to /login.
//
// MODE-AWARE (user decision — the checklist surface ships NOW, not in 5.5): a
// checklist-mode type renders ONE PassFailChips group PER checklist item; the
// submit requires EVERY item answered (FR-12) and the confirmation names the
// count of failed items ("2 von 3 Punkten NICHT BESTANDEN") or overall
// BESTANDEN when all pass. In 5.4 the checklist submit stays on the UX
// PLACEHOLDER seam (no fetch — the consequence is locally derived); Story 5.5
// wires it to the same endpoint (+ "Alle bestanden"). Every other mode —
// pass_fail, a missing or unknown one — renders the SINGLE pass/fail toggle
// (the safe default).
//
// Data comes from the eligible /start response, which the dashboard navigates
// here with as router state. On a refresh / deep link the state is GONE, so the
// page re-fetches via the startInspection client (it re-resolves the tool + its
// mode server-side) and shows a loading/error state meanwhile. The display
// falls back with `||` (not `??`) so an EMPTY string in the state never bypasses
// the fallback.
export function InspectionPage() {
  const { toolId } = useParams<{ toolId: string }>()
  const location = useLocation()
  const navigate = useNavigate()
  const state = (location.state ?? {}) as InspectionState

  // Router state is authoritative when present; otherwise a server re-fetch
  // recovers the display data after a refresh/deep link.
  const hasStateData = Boolean(state.tool_name || state.inspection_mode)
  const [fetched, setFetched] = useState<InspectionStart | null>(null)
  const [loading, setLoading] = useState(!hasStateData)
  const [error, setError] = useState('')

  // The mode-aware UX-foundation local state (Story 5.2 + mode-aware decision):
  // a pass_fail inspection keeps ONE selected chip value; a checklist inspection
  // keeps one PassFailValue PER ITEM (keyed by item id) — the all-items-required
  // submit (FR-12) needs every item answered. Plus the submit lifecycle
  // (in-flight placeholder + submitted → confirmation + the auto-return delay).
  // submitPendingRef is the SYNCHRONOUS double-submit guard (mirrors the
  // DashboardPage start guard); the disabled button is the visible one.
  const [result, setResult] = useState<PassFailValue | null>(null)
  const [itemResults, setItemResults] = useState<Record<string, PassFailValue>>({})
  // comment is the OPTIONAL inspection note (FR-13 "optional notes" / the
  // inspection comment the user asked for): a free-form German textarea
  // surfaced between the result chips and the submit. It is optional — the
  // submit never depends on it — and is part of the inspection data 5.4/5.5
  // persist with the record.
  const [comment, setComment] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  // serverStatus is the SERVER-authoritative derived status of the last
  // successful pass_fail submit (Story 5.4, AD-4/AD-5): the confirmation names
  // the OOS consequence ONLY from status.status === 'oos'. A passing inspection
  // does NOT clear OOS — reinstatement is the sole exit (FR-15), so the
  // server's derived status (which sees the full history) is authoritative,
  // never a local result guess. Checklist mode keeps the local consequence
  // until Story 5.5.
  const [serverStatus, setServerStatus] = useState<InspectionSubmitStatus | null>(null)
  // serverOutcome is the server-PERSISTED overall result of the last successful
  // pass_fail submit (Story 5.4, FR-13): the confirmation names the outcome
  // from inspection.overall_result, not the local chip — the confirmation must
  // name what was actually recorded.
  const [serverOutcome, setServerOutcome] = useState<InspectionResult | null>(null)
  // submitError is the inline German role=alert for a failed submit (Story
  // 5.4): 400/403/404/500/network surface here with NO navigation and NO
  // confirmation — the controls stay enabled for a retry.
  const [submitError, setSubmitError] = useState('')
  const submitPendingRef = useRef(false)
  const [reduceMotion] = useState(() => prefersReducedMotion())

  useEffect(() => {
    if (!toolId) return
    if (hasStateData) return

    let cancelled = false
    async function load(id: string) {
      // The reset lands on a microtask boundary (not synchronously in the
      // effect) — the effect body itself performs no state writes — while still
      // clearing stale data when the effect re-runs for a DIFFERENT tool.
      await Promise.resolve()
      if (cancelled) return
      setLoading(true)
      setError('')
      try {
        const result = await startInspection(id)
        if (cancelled) return
        setFetched(result)
      } catch (err) {
        if (cancelled) return
        const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
        if (status === 401) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        setError(err instanceof Error && err.message !== '' ? err.message : 'Die Prüfungsdaten konnten nicht geladen werden.')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load(toolId)
    return () => {
      cancelled = true
    }
  }, [toolId, hasStateData, navigate])

  // Auto-return to a refreshed Dashboard after the submit confirmation (Story
  // 5.2, UX-DR7/DR8): ~2s after the confirmation renders, navigate('/') — which
  // REMOUNTS DashboardPage and re-fetches the tool list. Under
  // prefers-reduced-motion the delay is skipped (0ms → redirect immediately
  // after the confirmation renders, UX-DR9). Effect-driven so the return only
  // ever fires after the confirmation has committed; the timer is cleared on
  // unmount.
  useEffect(() => {
    if (!submitted) return
    const delay = reduceMotion ? 0 : AUTO_RETURN_MS
    const timer = window.setTimeout(() => navigate('/'), delay)
    return () => window.clearTimeout(timer)
  }, [submitted, reduceMotion, navigate])

  const toolName = fetched?.tool_name || state.tool_name || toolId
  // The identifier is the inventory number when available (carried from the
  // dashboard list); it falls back to the tool name on a refresh/deep link
  // (the /start payload has no inventory_number).
  const identifier = fetched?.inventory_number || state.inventory_number || toolName
  // The type display name (Story 5.2 mode-aware header): from the /start
  // response or the router state; a neutral dash when neither carries it.
  const toolTypeName = fetched?.tool_type_name || state.tool_type_name || '–'
  const mode = modeLabel(fetched?.inspection_mode || state.inspection_mode)
  const modeValue = effectiveMode(fetched?.inspection_mode || state.inspection_mode)
  // The checklist items come from the /start response (fetched) or the router
  // state (the dashboard forwards them); a deep-link refresh without state
  // re-fetches and gets them from /start. Missing items fall back to an EMPTY
  // checklist gracefully (the empty-checklist state renders, never a crash).
  const checklistItems = fetched?.checklist_items ?? state.checklist_items ?? []
  const isChecklistComplete =
    checklistItems.length > 0 && checklistItems.every((item) => itemResults[item.id] !== undefined)
  const failedCount = checklistItems.filter((item) => itemResults[item.id] === 'fail').length
  // canSubmit encodes the outcome-required rule (UX-DR7/FR-12): a pass_fail
  // inspection needs ONE selected chip; a checklist inspection needs EVERY item
  // answered (and at least one item exists — an empty checklist must not save
  // a meaningless BESTANDEN).
  const canSubmit = modeValue === 'checklist' ? isChecklistComplete : result !== null
  // The confirmation names the recorded outcome (UX-DR7) — always set by the
  // time submit is possible. A checklist inspection names the count of failed
  // items ("2 von 3 Punkten NICHT BESTANDEN") or overall BESTANDEN when all
  // items pass (FR-12). For pass_fail the outcome is the SERVER-PERSISTED
  // overall_result (Story 5.4): the confirmation names what was recorded, with
  // the local chip only as the pre-submit fallback.
  const outcomeLabel =
    modeValue === 'checklist'
      ? failedCount > 0
        ? `${failedCount} von ${checklistItems.length} Punkten NICHT BESTANDEN`
        : 'BESTANDEN'
      : serverOutcome === 'pass'
        ? 'BESTANDEN'
        : serverOutcome === 'fail'
          ? 'NICHT BESTANDEN'
          : result === 'pass'
            ? 'BESTANDEN'
            : result === 'fail'
              ? 'NICHT BESTANDEN'
              : null
  // isFailure is the Story 5.3 consequence trigger for CHECKLIST mode (the 5.4
  // placeholder seam): any failed checklist item makes the tool unsafe. For
  // PASS_FAIL the consequence is SERVER-driven (serverStatus), so isFailure is
  // checklist-only — a local result guess is never the OOS authority (AD-4).
  const isFailure = modeValue === 'checklist' ? failedCount > 0 : false
  // oosConsequence is the confirmation consequence. Checklist mode (5.4
  // placeholder) stays locally derived from isFailure; pass_fail uses the
  // SERVER-authoritative derived status from the submit response — the
  // confirmation names the OOS consequence ONLY when the server returned
  // status.status === 'oos'. A passing inspection does NOT clear OOS
  // (reinstatement is the sole exit, FR-15), so the server's derived status —
  // which sees the full inspection + reinstatement history — is authoritative,
  // never a local result guess (AD-4/AD-5).
  const oosConsequence =
    modeValue === 'checklist'
      ? isFailure
        ? `⛔ Wird als ${OOS_STATUS} gesperrt. `
        : ''
      : serverStatus?.status === 'oos'
        ? `⛔ Wird als ${OOS_STATUS} gesperrt. `
        : ''

  // handleItemSelect records one checklist item's per-item result (keyed by the
  // item id); re-tapping the selected chip deselects it (null → the key leaves
  // the map so the all-items-required submit disables again).
  const handleItemSelect = (itemId: string, value: PassFailValue | null): void => {
    setItemResults((prev) => {
      const next = { ...prev }
      if (value === null) {
        delete next[itemId]
      } else {
        next[itemId] = value
      }
      return next
    })
  }

  // handleSubmit executes the inspection submit (Story 5.4, UX-DR7/DR8):
  //   PASS_FAIL builds the real payload { mode: 'pass_fail', result, notes,
  //   items: [] } and POSTs it via the real client bound to the URL toolId. A
  //   200 stores the server-authoritative status + the server-persisted outcome
  //   and confirms (a malformed body answers an inline error instead); any
  //   error (400/403/404/500/network) shows an inline German role=alert and does
  //   NOT navigate or confirm — 401 clears auth + redirects to /login (the
  //   load-path pattern). CHECKLIST mode (5.4) stays on the UX placeholder: no
  //   fetch, local consequence — Story 5.5 wires it. The button is disabled
  //   while it runs AND during the auto-return delay (double-submit guard).
  const handleSubmit = async (): Promise<void> => {
    // Defense-in-depth: an incomplete result set can never be submitted (the
    // button is also disabled), keeping an outcome-less save impossible in a
    // safety-critical flow — pass_fail needs one selected chip, a checklist
    // needs EVERY item answered (FR-12).
    if (submitPendingRef.current || submitted) return
    if (modeValue === 'checklist') {
      if (!isChecklistComplete) return
    } else if (result === null) {
      return
    }
    submitPendingRef.current = true
    setSubmitting(true)
    setSubmitError('')
    try {
      if (modeValue === 'checklist') {
        // Story 5.4 checklist placeholder: no fetch, no server status — the
        // local isFailure consequence drives the confirmation (5.5 wires it).
        await submitInspectionPlaceholder()
      } else {
        // The route always carries a toolId; the guard is defensive (an
        // unmappable route never confirms).
        if (!toolId) {
          setSubmitError('Die Prüfung konnte nicht gespeichert werden.')
          return
        }
        // The guard above guarantees a selected result here; TS cannot narrow
        // `result` through the checklist disjunction, so assert it explicitly.
        const serverResult = await submitInspection(toolId, {
          mode: 'pass_fail',
          result: result!,
          notes: comment,
          items: [],
        })
        // Response-shape guard: a 200 whose body lacks the record/status must
        // NOT confirm a success — surface the inline error instead.
        if (!serverResult || !serverResult.status || !serverResult.inspection) {
          setSubmitError('Ungültige Serverantwort.')
          return
        }
        setServerStatus(serverResult.status)
        setServerOutcome(serverResult.inspection.overall_result)
      }
      setSubmitted(true)
    } catch (err) {
      // Defensive status extraction (patch 14): an ApiError-like rejection may
      // be any object shape, so branch on the property rather than instanceof.
      const status = err && typeof err === 'object' && 'status' in err ? (err as { status: number }).status : 0
      if (status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setSubmitError(
        err instanceof Error && err.message !== '' ? err.message : 'Die Prüfung konnte nicht gespeichert werden.',
      )
    } finally {
      submitPendingRef.current = false
      setSubmitting(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Prüfung</h2>
          <p className={styles.pageSubtitle}>
            Prüfe das Werkzeug und wähle das Prüfergebnis. Du speicherst die Prüfung in einem Schritt.
          </p>
        </section>

        <section className={styles.section} aria-label="Prüfungswerkzeug">
          {loading ? (
            <div className={styles.loading} role="status" aria-live="polite">
              Prüfungsdaten werden geladen...
            </div>
          ) : error ? (
            <p role="alert" className={styles.error}>
              {error}
            </p>
          ) : (
            <>
              <header className={styles.toolHeader}>
                <dl className={styles.detail}>
                  <div className={styles.detailRow}>
                    <dt className={styles.detailTerm}>Werkzeug</dt>
                    <dd className={styles.detailValue}>{toolName}</dd>
                  </div>
                  <div className={styles.detailRow}>
                    <dt className={styles.detailTerm}>Gerätenummer</dt>
                    <dd className={styles.detailValue}>{identifier}</dd>
                  </div>
                  <div className={styles.detailRow}>
                    <dt className={styles.detailTerm}>Gerätetyp</dt>
                    <dd className={styles.detailValue}>{toolTypeName}</dd>
                  </div>
                  <div className={styles.detailRow}>
                    <dt className={styles.detailTerm}>Prüfmodus</dt>
                    <dd className={styles.detailValue}>{mode}</dd>
                  </div>
                </dl>
              </header>

              {/* Mode-aware surface (Story 5.2 + mode-aware decision): a
                  checklist-mode type renders ONE PassFailChips group PER item
                  (each fieldset names the item as its legend; the radio groups
                  are unique per item); every other mode — pass_fail, missing or
                  unknown — renders the SINGLE pass/fail toggle (the safe
                  default). */}
              {modeValue === 'checklist' ? (
                checklistItems.length === 0 ? (
                  <p className={styles.emptyChecklist}>
                    Für diesen Gerätetyp sind keine Prüfpunkte hinterlegt.
                  </p>
                ) : (
                  checklistItems.map((item) => (
                    <PassFailChips
                      key={item.id}
                      name={`inspection-item-${item.id}`}
                      legend={item.label}
                      selected={itemResults[item.id] ?? null}
                      onSelect={(value) => handleItemSelect(item.id, value)}
                      disabled={submitting || submitted}
                    />
                  ))
                )
              ) : (
                <PassFailChips
                  name="inspection-result"
                  legend="Ergebnis"
                  selected={result}
                  onSelect={setResult}
                  disabled={submitting || submitted}
                />
              )}

              <div className={styles.commentField}>
                <label className={styles.commentLabel} htmlFor="inspection-comment">
                  Anmerkung (optional)
                </label>
                <textarea
                  id="inspection-comment"
                  className={styles.commentInput}
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  placeholder="z.B. Ölstand geprüft, auffällige Geräusche..."
                  rows={3}
                  maxLength={4000}
                  disabled={submitting || submitted}
                  autoComplete="off"
                />
              </div>

              <div className={styles.submitRow}>
                <button
                  type="button"
                  className={styles.submitButton}
                  disabled={!canSubmit || submitting || submitted}
                  aria-busy={submitting}
                  onClick={() => void handleSubmit()}
                >
                  {submitting ? 'Wird gespeichert...' : 'Prüfung speichern'}
                </button>
              </div>

              {submitError && (
                <p role="alert" className={styles.error}>
                  {submitError}
                </p>
              )}

              {submitted && (
                <p role="status" className={styles.confirmation}>
                  Die Prüfung für „{toolName}“ wurde gespeichert — Ergebnis: {outcomeLabel}. {oosConsequence}Du kehrst zur Werkzeugliste zurück.
                </p>
              )}
            </>
          )}
        </section>
      </main>
    </div>
  )
}