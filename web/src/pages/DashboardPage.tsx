import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SummaryGrid } from '../components/SummaryGrid.tsx'
import type { SummaryCounts } from '../components/SummaryGrid.tsx'
import { FilterChips } from '../components/FilterChips.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { clearAuthState } from '../auth/authState.ts'
import { listDashboardTools, startInspection } from '../auth/tools.ts'
import type { DashboardTool } from '../auth/tools.ts'
import { statusLabel, statusClassKey, type StatusCode } from '../types/filters.ts'
import styles from './DashboardPage.module.css'

// DashboardPage is the GEAR-module landing surface. The "Werkzeugliste"
// section (Story 4-3b + 6.1) fetches the ACTIVE tool list from /api/v1/tools
// (gated by dashboard.view on the server — all base roles hold it) and renders
// name + type name + inventory number plus the server-DERIVED status as a
// German label + color chip (FR-16/AD-4/AD-5 — Red past due / OOS, Orange ≤14
// days, Green current, never-inspected Red). The 2×2 summary grid shows the
// real per-status counts and its NON-ZERO cards are TAPPABLE: tapping a count
// activates the matching status filter; the filter chips support MULTIPLE
// active statuses at once (FR-16/UX-DR5). The selection identity is the
// stable status CODE, never the German label. When the fleet is empty the
// "Keine Werkzeuge vorhanden" EmptyState is kept; when a NON-EMPTY fleet is
// filtered to nothing a distinct message + "Alle anzeigen" is shown instead.
// 401 → /login (stale/revoked session); 403 should never happen for a logged-in
// dashboard.view holder but is handled defensively the same way (AD-6: no tool
// data is exposed either way).
//
// Story 5.1 (FR-11/AD-7): every row gains a "Prüfung starten" control — the
// qualification-gated inspection START. The button stays ENABLED until clicked
// (the 403 IS the gate — never a client-side pre-query, AD-6): 200 → navigate
// to /inspection/:toolId; 403 → the server's German reason shows inline and the
// row's button disables for the session (persisting across LIST REFETCHES — the
// disabled/pending ids live in refs, cleared only on a full reload); 401 →
// login; other → inline error (button stays enabled for a retry). A double
// click is guarded: the button disables while its request is in flight.
export function DashboardPage() {
  // selectedFilters holds the ACTIVE status CODES; the EMPTY set means "Alle"
  // (no filter). "Alle" is cleared via clearFilters (Story 6.1).
  const [selectedFilters, setSelectedFilters] = useState<ReadonlySet<StatusCode>>(new Set())
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [tools, setTools] = useState<DashboardTool[]>([])
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})
  // disabledStarts/pendingStarts hold tool ids whose start button is disabled
  // for the session (a 403) or in flight. They are COMPONENT STATE, so they
  // persist across the list REFETCH effect (the component instance is stable
  // across refetches) and are only cleared on a full reload (the component
  // remounts). pendingGuardRef adds the SYNCHRONOUS double-submit guard (a
  // second click in the same tick cannot slip past the async state update).
  const [disabledStarts, setDisabledStarts] = useState<ReadonlySet<string>>(new Set())
  const [pendingStarts, setPendingStarts] = useState<ReadonlySet<string>>(new Set())
  const pendingGuardRef = useRef<Set<string>>(new Set())
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const items = await listDashboardTools()
        if (cancelled) return
        setTools(items)
        // Clear stale inline start errors on a list reload (Story 5.1): a
        // transient failure must not linger after a successful refresh, and an
        // error for a tool that left the list disappears with it. The session
        // DISABLED set is deliberately NOT cleared (it persists across
        // refetches).
        setRowErrors({})
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

  // counts are the per-status totals of the CURRENT list (Story 6.1,
  // SPA_COUNTS): derived from the server-returned statuses, never guessed. An
  // unknown code is never silently uncounted (the default branch keeps it out
  // of every bucket).
  const counts: SummaryCounts = useMemo(() => {
    const c: SummaryCounts = { einsatzbereit: 0, ausstehend: 0, ueberfaellig: 0, ausserBetrieb: 0 }
    for (const tool of tools) {
      switch (tool.status.status) {
        case 'green':
          c.einsatzbereit!++
          break
        case 'orange':
          c.ausstehend!++
          break
        case 'red':
          c.ueberfaellig!++
          break
        case 'oos':
          c.ausserBetrieb!++
          break
        default:
          // Unknown/empty code: not counted anywhere (defensive).
          break
      }
    }
    return c
  }, [tools])

  // visibleTools applies the active status filters: the EMPTY set ("Alle")
  // shows the full list; otherwise the union of the selected statuses
  // (FR-16/UX-DR5, SPA_MULTI) — compared directly on the stable status code.
  const visibleTools = useMemo(() => {
    if (selectedFilters.size === 0) {
      return tools
    }
    return tools.filter((tool) => selectedFilters.has(tool.status.status))
  }, [tools, selectedFilters])

  // toggleStatusFilter (Story 6.1, FR-16/UX-DR5): a status CODE toggles
  // in/out — multiple statuses can be active at once.
  const toggleStatusFilter = (code: StatusCode): void => {
    setSelectedFilters((prev) => {
      const next = new Set(prev)
      if (next.has(code)) {
        next.delete(code)
      } else {
        next.add(code)
      }
      return next
    })
  }

  // clearFilters ("Alle") empties the selection → the full list is shown.
  const clearFilters = (): void => {
    setSelectedFilters(new Set())
  }

  // handleStart POSTs the qualification-gated inspection start (Story 5.1).
  // 200 → navigate to the stub inspection screen with the server's tool + mode
  // (router state); 403 → show the German reason inline and disable the row's
  // button for the session; 401 → login; other → inline error (retry allowed).
  // The pending guard + disabled button prevent a DOUBLE SUBMIT.
  const handleStart = async (tool: DashboardTool): Promise<void> => {
    if (pendingGuardRef.current.has(tool.id) || disabledStarts.has(tool.id)) {
      return
    }
    pendingGuardRef.current.add(tool.id)
    setPendingStarts((prev) => new Set(prev).add(tool.id))
    try {
      const result = await startInspection(tool.id)
      // Story 5.2: the inspection header shows the inventory number as the tool
      // identifier and the type display name; the mode-aware surface needs the
      // type's checklist items for a checklist-mode inspection. The /start
      // payload carries the type name + checklist items, so they travel in the
      // router state; the inventory number is added from the tool LIST (which
      // has it) — the header falls back to the tool name when the state lacks
      // it (refresh/deep link).
      navigate(`/inspection/${tool.id}`, {
        state: {
          tool_name: result.tool_name,
          tool_type_name: result.tool_type_name,
          inspection_mode: result.inspection_mode,
          inventory_number: tool.inventory_number,
          checklist_items: result.checklist_items,
        },
      })
    } catch (err) {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      const message = err instanceof Error && err.message !== '' ? err.message : 'Die Prüfung konnte nicht gestartet werden.'
      setRowErrors((prev) => ({ ...prev, [tool.id]: message }))
      if (status === 403) {
        setDisabledStarts((prev) => new Set(prev).add(tool.id))
      }
    } finally {
      pendingGuardRef.current.delete(tool.id)
      setPendingStarts((prev) => {
        const next = new Set(prev)
        next.delete(tool.id)
        return next
      })
    }
  }

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

        <SummaryGrid counts={counts} onToggleFilter={toggleStatusFilter} />

        <section className={styles.section} aria-label="Werkzeugliste">
          <FilterChips
            selectedFilters={selectedFilters}
            onToggleFilter={toggleStatusFilter}
            onClear={clearFilters}
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
          ) : visibleTools.length === 0 ? (
            // Story 6.1 (patch 2): a NON-EMPTY fleet filtered to nothing gets a
            // distinct state with a way out — never the fleet-empty EmptyState.
            <div className={styles.filteredEmpty} role="status">
              <p>Keine Werkzeuge mit dem ausgewählten Status.</p>
              <button type="button" className={styles.showAllButton} onClick={clearFilters}>
                Alle anzeigen
              </button>
            </div>
          ) : (
            <ul className={styles.list} aria-label="Werkzeuge">
              {visibleTools.map((tool) => (
                <li key={tool.id} className={styles.row}>
                  <div className={styles.rowMain}>
                    <div className={styles.rowInfo}>
                      <span className={styles.rowName}>{tool.name}</span>
                      <span className={styles.rowMeta}>{tool.tool_type_name}</span>
                      <span className={styles.rowMeta}>{tool.inventory_number}</span>
                    </div>
                    <div className={styles.rowActions}>
                      <span
                        className={`${styles.statusChip} ${styles[statusClassKey(tool.status.status)]}`}
                      >
                        {statusLabel(tool.status.status)}
                      </span>
                      <button
                        type="button"
                        className={styles.startButton}
                        disabled={disabledStarts.has(tool.id) || pendingStarts.has(tool.id)}
                        aria-label={`Prüfung starten für ${tool.name}`}
                        onClick={() => void handleStart(tool)}
                      >
                        Prüfung starten
                      </button>
                    </div>
                  </div>
                  {rowErrors[tool.id] && (
                    <p role="alert" className={styles.rowError}>
                      {rowErrors[tool.id]}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>
    </div>
  )
}
