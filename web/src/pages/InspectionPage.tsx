import { useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { clearAuthState } from '../auth/authState.ts'
import { startInspection } from '../auth/tools.ts'
import type { InspectionStart } from '../auth/tools.ts'
import styles from './InspectionPage.module.css'

// modeLabel maps a wire inspection_mode to its German display label. An empty
// or unknown mode falls back to the neutral "–".
function modeLabel(mode: string | undefined): string {
  if (mode === 'checklist') return 'Checkliste'
  if (mode === 'pass_fail') return 'Pass/Fail'
  return '–'
}

// InspectionPage is the STUB inspection screen (Story 5.1, FR-11): it renders
// the tool name + its type's inspection_mode. The real inspection screen is a
// later story — this placeholder only proves that the qualification-gated start
// opens a route.
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
  const state = (location.state ?? {}) as { tool_name?: string; inspection_mode?: string }

  // Router state is authoritative when present; otherwise a server re-fetch
  // recovers the display data after a refresh/deep link.
  const hasStateData = Boolean(state.tool_name || state.inspection_mode)
  const [fetched, setFetched] = useState<InspectionStart | null>(null)
  const [loading, setLoading] = useState(!hasStateData)
  const [error, setError] = useState('')

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

  const toolName = fetched?.tool_name || state.tool_name || toolId
  const mode = modeLabel(fetched?.inspection_mode || state.inspection_mode)

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Prüfung</h2>
          <p className={styles.pageSubtitle}>
            Die Prüfungsoberfläche wird in einer späteren Version bereitgestellt.
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
            <dl className={styles.detail}>
              <div className={styles.detailRow}>
                <dt className={styles.detailTerm}>Werkzeug</dt>
                <dd className={styles.detailValue}>{toolName}</dd>
              </div>
              <div className={styles.detailRow}>
                <dt className={styles.detailTerm}>Prüfmodus</dt>
                <dd className={styles.detailValue}>{mode}</dd>
              </div>
            </dl>
          )}
        </section>
      </main>
    </div>
  )
}