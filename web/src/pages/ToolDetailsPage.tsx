import { useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { clearAuthState, hasPermission } from '../auth/authState.ts'
import { HISTORY_PERMISSION, listDashboardTools, listToolHistory } from '../auth/tools.ts'
import type { DashboardTool, InspectionResult, ToolHistory } from '../auth/tools.ts'
import { statusClassKey, statusLabel } from '../types/filters.ts'
import styles from './ToolDetailsPage.module.css'

// ToolDetailsState is the router state a future dashboard row navigation can
// carry here (Story 6.3 — the row-click navigation is a follow-up story; this
// page is reachable by URL only in this story): the tool header fields the
// dashboard list already has. On a direct visit / refresh the state is GONE,
// so the page re-resolves the header from listDashboardTools() find-by-id.
interface ToolDetailsState {
  tool_name?: string
  inventory_number?: string
  tool_type_name?: string
  status?: DashboardTool['status']
}

// outcomeLabel maps a server inspection outcome to its German display label
// (FR-18): pass → BESTANDEN, fail → NICHT BESTANDEN. Unknown values fall back
// to the raw value defensively (never a crash).
function outcomeLabel(result: InspectionResult): string {
  if (result === 'pass') return 'BESTANDEN'
  if (result === 'fail') return 'NICHT BESTANDEN'
  return result
}

// modeLabel maps the wire inspection mode to its German display label
// (mirroring the InspectionPage vocabulary): checklist → Checkliste, anything
// else → Pass/Fail.
function modeLabel(mode: string): string {
  return mode === 'checklist' ? 'Checkliste' : 'Pass/Fail'
}

// formatTimestamp renders the server's RFC3339 UTC timestamp in a readable
// German locale form. A malformed/empty value renders as a dash defensively
// (never a crash).
function formatTimestamp(ts: string): string {
  if (!ts) return '–'
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '–'
  return d.toLocaleString('de-DE')
}

// ToolDetailsPage is the per-tool details surface (Story 6.3, FR-18): the tool
// header (name + Gerätenummer + Gerätetyp + status chip) from router state or a
// listDashboardTools() refetch on a deep link/refresh (dashboard.view — every
// authenticated user opens it), and — gated by `inspection.history.view` — the
// reverse-chronological inspection history (each naming the inspector, the
// timestamp, the outcome, the notes, the mode and the per-checklist-item
// results) and the reinstatement ledger (actor + reason).
//
// HISTORY GATING (user decision): every authenticated user sees the header;
// the HISTORY content is gated by inspection.history.view. The SPA pre-checks
// hasPermission() and SKIPS the fetch for non-holders (showing the German
// no-permission note) as a courtesy only — the SERVER is the real gate (a
// non-holder answers the uniform 403 with no data). A 401 clears the auth
// state and redirects to /login; other errors (incl. a server-side 403 after
// the permission cache went stale) show inline German. Read-only: no
// reinstatement action lives here (the dashboard keeps the "Wiederherstellen"
// action).
export function ToolDetailsPage() {
  const { toolId } = useParams<{ toolId: string }>()
  const location = useLocation()
  const navigate = useNavigate()
  const state = (location.state ?? {}) as ToolDetailsState

  const canViewHistory = hasPermission(HISTORY_PERMISSION)
  const hasStateData = Boolean(state.tool_name)

  // Header resolution: router state is authoritative when present; otherwise a
  // listDashboardTools() find-by-id recovers the display data after a
  // refresh/deep link (the dashboard list is the dashboard.view-gated surface
  // every authenticated user holds). The fetched data is BOUND to the toolId it
  // was fetched for (HeaderState.toolId / the error's toolId): a client-side
  // navigation between /tools/:id URLs must never flash the previous tool's
  // header (or a stale loading/error state) under the new URL — the render only
  // shows data whose id matches the CURRENT URL id (an identity guard, so no
  // synchronous setState is needed in the effect; the async completion still
  // stores it under the id it belongs to).
  const [header, setHeader] = useState<{ toolId: string; tool: DashboardTool } | null>(null)
  const [headerError, setHeaderError] = useState<{ toolId: string; message: string } | null>(null)

  // History: only fetched when the caller HOLDS inspection.history.view (the
  // courtesy skip — the server 403 is the gate). Same identity guard as the
  // header: the fetched history + its error are bound to the toolId they belong
  // to.
  const [history, setHistory] = useState<{ toolId: string; history: ToolHistory } | null>(null)
  const [historyError, setHistoryError] = useState<{ toolId: string; message: string } | null>(null)

  useEffect(() => {
    if (!toolId) return
    if (hasStateData) return

    let cancelled = false
    async function load(id: string) {
      try {
        const items = await listDashboardTools()
        if (cancelled) return
        const found = items.find((t) => t.id === id)
        if (!found) {
          setHeaderError({ toolId: id, message: 'Das Werkzeug wurde nicht gefunden.' })
          return
        }
        setHeader({ toolId: id, tool: found })
        setHeaderError(null)
      } catch (err) {
        if (cancelled) return
        const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
        if (status === 401) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        setHeaderError({
          toolId: id,
          message:
            err instanceof Error && err.message !== '' ? err.message : 'Die Werkzeugdaten konnten nicht geladen werden.',
        })
      }
    }
    void load(toolId)
    return () => {
      cancelled = true
    }
  }, [toolId, hasStateData, navigate])

  useEffect(() => {
    if (!toolId || !canViewHistory) return

    let cancelled = false
    async function load(id: string) {
      try {
        const result = await listToolHistory(id)
        if (cancelled) return
        setHistory({ toolId: id, history: result })
        setHistoryError(null)
      } catch (err) {
        if (cancelled) return
        const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
        if (status === 401) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        // A 403 (the permission cache went stale — the SERVER is the gate)
        // and every other error surface the server's German message inline.
        setHistoryError({
          toolId: id,
          message:
            err instanceof Error && err.message !== '' ? err.message : 'Die Prüfhistorie konnte nicht geladen werden.',
        })
      }
    }
    void load(toolId)
    return () => {
      cancelled = true
    }
  }, [toolId, canViewHistory, navigate])

  // The CURRENT tool's data: only the entry whose id matches the URL id is
  // visible — a stale entry from a previous tool never renders.
  const headerTool = header !== null && header.toolId === toolId ? header.tool : null
  const headerErrorCurrent = headerError !== null && headerError.toolId === toolId ? headerError.message : null
  const headerReady = hasStateData || headerTool !== null
  const currentHistory = history !== null && history.toolId === toolId ? history.history : null
  const historyErrorCurrent = historyError !== null && historyError.toolId === toolId ? historyError.message : null

  const toolName = state.tool_name || headerTool?.name || toolId || ''
  const inventoryNumber = state.inventory_number || headerTool?.inventory_number || toolName
  const toolTypeName = state.tool_type_name || headerTool?.tool_type_name || '–'
  const status = state.status ?? headerTool?.status

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Werkzeugdetails</h2>
          <p className={styles.pageSubtitle}>Prüfhistorie und Wiederherstellungen dieses Geräts.</p>
        </section>

        <section className={styles.section} aria-label="Werkzeugdetails">
          {headerReady ? (
            <header className={styles.toolHeader}>
              <dl className={styles.detail}>
                <div className={styles.detailRow}>
                  <dt className={styles.detailTerm}>Werkzeug</dt>
                  <dd className={styles.detailValue}>{toolName}</dd>
                </div>
                <div className={styles.detailRow}>
                  <dt className={styles.detailTerm}>Gerätenummer</dt>
                  <dd className={styles.detailValue}>{inventoryNumber}</dd>
                </div>
                <div className={styles.detailRow}>
                  <dt className={styles.detailTerm}>Gerätetyp</dt>
                  <dd className={styles.detailValue}>{toolTypeName}</dd>
                </div>
                <div className={styles.detailRow}>
                  <dt className={styles.detailTerm}>Status</dt>
                  <dd className={styles.detailValue}>
                    {status ? (
                      <span className={`${styles.statusChip} ${styles[statusClassKey(status.status)]}`}>
                        {statusLabel(status.status)}
                      </span>
                    ) : (
                      '–'
                    )}
                  </dd>
                </div>
              </dl>
            </header>
          ) : headerErrorCurrent ? (
            <p role="alert" className={styles.error}>
              {headerErrorCurrent}
            </p>
          ) : (
            <div className={styles.skeleton} aria-busy="true" aria-label="Werkzeugdetails werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          )}

          {/* History section (Story 6.3, FR-18): gated by inspection.history.view.
              The SPA pre-checks the permission and skips the fetch for non-holders
              (the courtesy); the SERVER is the real gate (AD-6). */}
          <section className={styles.historySection} aria-label="Prüfhistorie">
            <h3 className={styles.historyTitle}>Prüfhistorie</h3>
            {!canViewHistory ? (
              <p className={styles.noPermission}>Du hast keine Berechtigung, die Prüfhistorie anzuzeigen.</p>
            ) : historyErrorCurrent ? (
              <p role="alert" className={styles.error}>
                {historyErrorCurrent}
              </p>
            ) : currentHistory ? (
              currentHistory.inspections.length === 0 ? (
                <p className={styles.emptyNote}>Keine Prüfungen vorhanden.</p>
              ) : (
                <ul className={styles.inspectionList} aria-label="Prüfungen">
                  {currentHistory.inspections.map((insp) => (
                    <li key={insp.id} className={styles.historyCard}>
                      <dl className={styles.historyDetail}>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Prüfer/in</dt>
                          <dd className={styles.detailValue}>{insp.inspector_name}</dd>
                        </div>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Datum</dt>
                          <dd className={styles.detailValue}>{formatTimestamp(insp.submitted_at)}</dd>
                        </div>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Ergebnis</dt>
                          <dd className={styles.detailValue}>{outcomeLabel(insp.overall_result)}</dd>
                        </div>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Modus</dt>
                          <dd className={styles.detailValue}>{modeLabel(insp.mode)}</dd>
                        </div>
                        {insp.notes && (
                          <div className={styles.detailRow}>
                            <dt className={styles.detailTerm}>Anmerkung</dt>
                            <dd className={styles.detailValue}>{insp.notes}</dd>
                          </div>
                        )}
                        {insp.mode === 'checklist' && insp.items.length > 0 && (
                          <ul className={styles.itemsList} aria-label="Prüfpunkte">
                            {insp.items.map((item) => (
                              <li key={item.id} className={styles.itemRow}>
                                <span className={styles.itemLabel}>{item.label}</span>
                                <span
                                  className={`${styles.itemResult} ${item.result === 'pass' ? styles.itemPass : styles.itemFail}`}
                                >
                                  {outcomeLabel(item.result)}
                                </span>
                              </li>
                            ))}
                          </ul>
                        )}
                      </dl>
                    </li>
                  ))}
                </ul>
              )
            ) : (
              <div className={styles.loading} role="status" aria-live="polite">
                Prüfhistorie wird geladen...
              </div>
            )}
          </section>

          <section className={styles.historySection} aria-label="Wiederherstellungen">
            <h3 className={styles.historyTitle}>Wiederherstellungen</h3>
            {!canViewHistory ? (
              <p className={styles.noPermission}>Du hast keine Berechtigung, die Prüfhistorie anzuzeigen.</p>
            ) : historyErrorCurrent ? (
              <p role="alert" className={styles.error}>
                {historyErrorCurrent}
              </p>
            ) : currentHistory ? (
              currentHistory.reinstatements.length === 0 ? (
                <p className={styles.emptyNote}>Keine Wiederherstellungen vorhanden.</p>
              ) : (
                <ul className={styles.inspectionList} aria-label="Wiederherstellungen">
                  {currentHistory.reinstatements.map((rein) => (
                    <li key={rein.id} className={styles.historyCard}>
                      <dl className={styles.historyDetail}>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Durchgeführt von</dt>
                          <dd className={styles.detailValue}>{rein.actor_name}</dd>
                        </div>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Datum</dt>
                          <dd className={styles.detailValue}>{formatTimestamp(rein.created_at)}</dd>
                        </div>
                        <div className={styles.detailRow}>
                          <dt className={styles.detailTerm}>Grund</dt>
                          <dd className={styles.detailValue}>{rein.reason}</dd>
                        </div>
                      </dl>
                    </li>
                  ))}
                </ul>
              )
            ) : (
              <div className={styles.loading} role="status" aria-live="polite">
                Prüfhistorie wird geladen...
              </div>
            )}
          </section>
        </section>
      </main>
    </div>
  )
}