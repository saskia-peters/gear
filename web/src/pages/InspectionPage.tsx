import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { PassFailChips } from '../components/PassFailChips.tsx'
import type { PassFailValue } from '../components/PassFailChips.tsx'
import { clearAuthState } from '../auth/authState.ts'
import { startInspection, submitInspectionPlaceholder } from '../auth/tools.ts'
import type { InspectionStart } from '../auth/tools.ts'
import styles from './InspectionPage.module.css'

// AUTO_RETURN_MS is the post-submit delay before the page auto-returns to the
// refreshed Dashboard (UX-DR7/DR8). prefers-reduced-motion skips it (0ms).
const AUTO_RETURN_MS = 2000

// modeLabel maps a wire inspection_mode to its German display label. An empty
// or unknown mode falls back to the neutral "–".
function modeLabel(mode: string | undefined): string {
  if (mode === 'checklist') return 'Checkliste'
  if (mode === 'pass_fail') return 'Pass/Fail'
  return '–'
}

// prefersReducedMotion reports the OS "reduce motion" preference (UX-DR9). The
// guard covers jsdom/tests where window.matchMedia is absent → normal motion.
function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// InspectionState is the router state the dashboard navigates here with (Story
// 5.1 + 5.2): the tool + its mode from the eligible /start payload, plus the
// inventory number carried from the tool LIST so the header can show the
// identifier (it falls back to the tool name).
interface InspectionState {
  tool_name?: string
  inspection_mode?: string
  inventory_number?: string
}

// InspectionPageProps carries ONE optional seam (Story 5.2): submitInspection is
// the injectable stand-in for the real record call (defaults to the UX
// placeholder in tools.ts; Stories 5.4/5.5 replace it). It also lets the tests
// hold the submit in flight to exercise the double-submit guard.
interface InspectionPageProps {
  submitInspection?: () => Promise<void>
}

// InspectionPage is the SINGLE-COLUMN inspection screen foundation (Story 5.2,
// FR-8/UX-DR3/DR5/DR6/DR7/DR8/DR9/DR10): a tool header (name + identifier +
// the type's mode), large green/red Pass/Fail chips (UX-only — Stories 5.4/5.5
// wire them to the real execution) and ONE submit → inline confirmation →
// ~2s auto-return to a refreshed Dashboard (immediate under Reduce Motion).
// The inspection content NEVER splits into a two-column layout at any width
// (safety-critical input, UX-DR10). A result MUST be chosen before the submit
// enables — an inspection can never be saved without an outcome.
//
// Data comes from the eligible /start response, which the dashboard navigates
// here with as router state. On a refresh / deep link the state is GONE, so the
// page re-fetches via the startInspection client (it re-resolves the tool + its
// mode server-side) and shows a loading/error state meanwhile. The display
// falls back with `||` (not `??`) so an EMPTY string in the state never bypasses
// the fallback.
export function InspectionPage({ submitInspection = submitInspectionPlaceholder }: InspectionPageProps = {}) {
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

  // The UX-foundation local state (Story 5.2): the selected chip value plus the
  // submit lifecycle (in-flight placeholder + submitted → confirmation + the
  // auto-return delay). submitPendingRef is the SYNCHRONOUS double-submit guard
  // (mirrors the DashboardPage start guard); the disabled button is the visible
  // one.
  const [result, setResult] = useState<PassFailValue | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [submitted, setSubmitted] = useState(false)
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
  const mode = modeLabel(fetched?.inspection_mode || state.inspection_mode)
  // The confirmation names the recorded outcome (UX-DR7) — always set by the
  // time submit is possible (the button requires a selected chip).
  const outcomeLabel = result === 'pass' ? 'BESTANDEN' : result === 'fail' ? 'NICHT BESTANDEN' : null

  // handleSubmit is the Story 5.2 UX PLACEHOLDER submit (UX-DR7/DR8):
  //   ====================================================================
  //   UX PLACEHOLDER SEAM — no inspection-record endpoint exists yet.
  //   The persisted shape (identity/timestamp/per-item results/OOS) is owned
  //   by Stories 5.3/5.4/5.5. The `submitInspection` seam is the stand-in for
  //   the real record call 5.4/5.5 replace; it deliberately does NO fetch to a
  //   nonexistent endpoint. The button is disabled while it runs AND during
  //   the auto-return delay (double-submit guard).
  //   ====================================================================
  const handleSubmit = async (): Promise<void> => {
    // Defense-in-depth: a missing result can never be submitted (the button is
    // also disabled), keeping an outcome-less save impossible in a safety-
    // critical flow.
    if (result === null || submitPendingRef.current || submitted) return
    submitPendingRef.current = true
    setSubmitting(true)
    try {
      await submitInspection()
    } finally {
      submitPendingRef.current = false
      setSubmitting(false)
    }
    setSubmitted(true)
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
                    <dt className={styles.detailTerm}>Prüfmodus</dt>
                    <dd className={styles.detailValue}>{mode}</dd>
                  </div>
                </dl>
              </header>

              <PassFailChips
                name="inspection-result"
                legend="Ergebnis"
                selected={result}
                onSelect={setResult}
                disabled={submitting || submitted}
              />

              <div className={styles.submitRow}>
                <button
                  type="button"
                  className={styles.submitButton}
                  disabled={result === null || submitting || submitted}
                  onClick={() => void handleSubmit()}
                >
                  Prüfung speichern
                </button>
              </div>

              {submitted && (
                <p role="status" className={styles.confirmation}>
                  Die Prüfung für „{toolName}“ wurde gespeichert — Ergebnis: {outcomeLabel}. Du kehrst zur Werkzeugliste zurück.
                </p>
              )}
            </>
          )}
        </section>
      </main>
    </div>
  )
}