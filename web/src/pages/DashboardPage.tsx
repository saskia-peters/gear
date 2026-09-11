import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SummaryGrid } from '../components/SummaryGrid.tsx'
import { FilterChips } from '../components/FilterChips.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { clearAuthState } from '../auth/authState.ts'
import { listDashboardTools } from '../auth/tools.ts'
import type { DashboardTool } from '../auth/tools.ts'
import type { FilterStatus } from '../types/filters.ts'
import styles from './DashboardPage.module.css'

// DashboardPage is the GEAR-module landing surface. The "Werkzeugliste"
// section (Story 4-3b) fetches the minimal ACTIVE tool list from /api/v1/tools
// (gated by dashboard.view on the server — all base roles hold it) and renders
// name + type name + a static "verfügbar" label (green accent). No status or
// due-date derivation here — the color-coded dashboard is Story 6.1 (Epic 6).
// When the list is empty the existing "Keine Werkzeuge vorhanden" EmptyState is
// kept. 401 → /login (stale/revoked session); 403 should never happen for a
// logged-in dashboard.view holder but is handled defensively the same way
// (AD-6: no tool data is exposed either way).
export function DashboardPage() {
  const [selectedFilter, setSelectedFilter] = useState<FilterStatus>('Alle')
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [tools, setTools] = useState<DashboardTool[]>([])
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const items = await listDashboardTools()
        if (!cancelled) setTools(items)
      } catch (err) {
        if (cancelled) return
        const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
        if (status === 401 || status === 403) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        setLoadError('Die Werkzeugliste konnte nicht geladen werden.')
      } finally {
        if (!cancelled) setLoaded(true)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [navigate])

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Übersicht</h2>
          <p className={styles.pageSubtitle}>
            G.E.A.R. (Geräte-Einsatz-Assistenz &amp; Readiness) — Geräteverwaltung &amp; Einsatzbereitschaft
          </p>
        </section>

        <SummaryGrid />

        <section className={styles.section} aria-label="Werkzeugliste">
          <FilterChips
            selectedFilter={selectedFilter}
            onSelectFilter={setSelectedFilter}
          />
          {loadError && (
            <p role="alert" className={styles.error}>
              {loadError}
            </p>
          )}
          {!loaded ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Werkzeugliste wird geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : tools.length === 0 ? (
            <EmptyState />
          ) : (
            <ul className={styles.list} aria-label="Werkzeuge">
              {tools.map((tool) => (
                <li key={tool.id} className={styles.row}>
                  <div className={styles.rowInfo}>
                    <span className={styles.rowName}>{tool.name}</span>
                    <span className={styles.rowMeta}>{tool.tool_type_name}</span>
                  </div>
                  <span className={styles.available}>verfügbar</span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>
    </div>
  )
}