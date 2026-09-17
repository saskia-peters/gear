import { useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { clearAuthState, hasPermission } from '../auth/authState.ts'
import { HISTORY_PERMISSION, listDashboardTools, listToolHistory } from '../auth/tools.ts'
import type {
  DashboardTool,
  InspectionResult,
  ToolHistory,
  ToolInspectionHistory,
  ToolReinstatementHistory,
} from '../auth/tools.ts'
import { statusClassKey, statusLabel } from '../types/filters.ts'
import styles from './ToolDetailsPage.module.css'

// ToolDetailsState is the router state the dashboard row navigation carries
// here (Story 6.1b — the row-click navigation; the page is also reachable by
// URL): the tool header fields the dashboard list already has. On a direct
// visit / refresh the state is GONE, so the page re-resolves the header from
// listDashboardTools() find-by-id. Exported so the dashboard row navigation
// types its navigate state with this exact shape (field drift → compile error).
export interface ToolDetailsState {
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

// TrackRecordEntry is one row of the merged track record (Story 6.3 follow-up):
// either an inspection (inspector, outcome, notes, mode, per-item results) or a
// reinstatement (actor, reason), each carrying its timestamp as `at` for the
// newest-first interleave (inspections use submitted_at, reinstatements use
// created_at). The union keeps the two record shapes distinct.
type TrackRecordEntry =
  | ({ kind: 'inspection'; at: string } & ToolInspectionHistory)
  | ({ kind: 'reinstatement'; at: string } & ToolReinstatementHistory)

// trackRecordKey is the stable React key for one merged entry: the kind-prefixed
// id (an inspection and a reinstatement never share an id, but the prefix keeps
// the key collision-proof regardless).
function trackRecordKey(entry: TrackRecordEntry): string {
  return `${entry.kind}-${entry.id}`
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

  // trackRecord is the merged ONE-track-record view (Story 6.3 follow-up): the
  // tool's inspections AND reinstatements interleaved into a SINGLE newest-first
  // timeline (each entry tagged by its kind), instead of two separate sections.
  // Ordering is by timestamp desc (submitted_at for inspections, created_at for
  // reinstatements), tiebroken by id desc for equal timestamps — the same
  // deterministic convention the backend history queries use.
  const trackRecord = useMemo(() => {
    if (!currentHistory) return []
    // Defensive: the client normalizes the payload (lists are always arrays),
    // but a null here must never crash the merge.
    const inspections: TrackRecordEntry[] = (currentHistory.inspections ?? []).map((insp) => ({
      kind: 'inspection',
      at: insp.submitted_at,
      ...insp,
    }))
    const reinstatements: TrackRecordEntry[] = (currentHistory.reinstatements ?? []).map((rein) => ({
      kind: 'reinstatement',
      at: rein.created_at,
      ...rein,
    }))
    return [...inspections, ...reinstatements].sort((a, b) => {
      const ta = Date.parse(a.at) || 0
      const tb = Date.parse(b.at) || 0
      if (tb !== ta) return tb - ta
      return a.id < b.id ? 1 : a.id > b.id ? -1 : 0
    })
  }, [currentHistory])

  const toolName = state.tool_name || headerTool?.name || toolId || ''
  const inventoryNumber = state.inventory_number || headerTool?.inventory_number || toolName
  const toolTypeName = state.tool_type_name || headerTool?.tool_type_name || '–'
  const status = state.status ?? headerTool?.status

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.backRow}>
          <button type="button" className={styles.backButton} onClick={() => navigate('/')}>
            ← Zurück zur Übersicht
          </button>
        </div>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Werkzeugdetails</h2>
          <p className={styles.pageSubtitle}>Prüfungen und Wiederherstellungen dieses Geräts.</p>
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

          {/* History section (Story 6.3 + follow-up, FR-18): gated by
              inspection.history.view. The SPA pre-checks the permission and
              skips the fetch for non-holders (the courtesy); the SERVER is the
              real gate (AD-6). The inspections AND reinstatements render as ONE
              merged track record (newest-first), each entry tagged by its kind
              — a single complete audit trail instead of two separate sections. */}
          <section className={styles.historySection} aria-label="Prüf- und Wiederherstellungshistorie">
            <h3 className={styles.historyTitle}>Historie</h3>
            {!canViewHistory ? (
              <p className={styles.noPermission}>Du hast keine Berechtigung, die Prüfhistorie anzuzeigen.</p>
            ) : historyErrorCurrent ? (
              <p role="alert" className={styles.error}>
                {historyErrorCurrent}
              </p>
            ) : currentHistory ? (
              trackRecord.length === 0 ? (
                <p className={styles.emptyNote}>Keine Einträge vorhanden.</p>
              ) : (
                <ul className={styles.trackList} aria-label="Historie">
                  {trackRecord.map((entry) => (
                    <li
                      key={trackRecordKey(entry)}
                      className={`${styles.historyCard} ${
                        entry.kind === 'inspection'
                          ? styles.historyCardInspection
                          : styles.historyCardReinstatement
                      }`}
                    >
                      <div className={styles.cardHeader}>
                        <span
                          className={`${styles.entryBadge} ${
                            entry.kind === 'inspection'
                              ? styles.entryBadgeInspection
                              : styles.entryBadgeReinstatement
                          }`}
                        >
                          {entry.kind === 'inspection' ? 'Prüfung' : 'Wiederherstellung'}
                        </span>
                        <span className={styles.cardDate}>{formatTimestamp(entry.at)}</span>
                      </div>
                      {entry.kind === 'inspection' ? (
                        <>
                          <p className={styles.cardMeta}>
                            <span className={styles.cardActor} aria-label={`Prüfer/in: ${entry.inspector_name}`}>
                              {entry.inspector_name}
                            </span>
                            <span
                              className={`${styles.cardOutcome} ${
                                entry.overall_result === 'pass' ? styles.cardOutcomePass : styles.cardOutcomeFail
                              }`}
                              aria-label={`Ergebnis: ${outcomeLabel(entry.overall_result)}`}
                            >
                              {outcomeLabel(entry.overall_result)}
                            </span>
                            <span className={styles.cardMode} aria-label={`Modus: ${modeLabel(entry.mode)}`}>
                              {modeLabel(entry.mode)}
                            </span>
                          </p>
                          {entry.notes && (
                            <p className={styles.cardNotes}>
                              <span className={styles.cardNotesLabel}>Anmerkung:</span> {entry.notes}
                            </p>
                          )}
                          {entry.mode === 'checklist' && entry.items.length > 0 && (
                            <ul className={styles.itemsList} aria-label="Prüfpunkte">
                              {entry.items.map((item) => (
                                <li key={item.id} className={styles.itemRow}>
                                  <span className={styles.itemLabel}>{item.label}</span>
                                  <span
                                    className={`${styles.itemResult} ${
                                      item.result === 'pass' ? styles.itemPass : styles.itemFail
                                    }`}
                                  >
                                    {outcomeLabel(item.result)}
                                  </span>
                                </li>
                              ))}
                            </ul>
                          )}
                        </>
                      ) : (
                        <p className={styles.cardMeta}>
                          <span className={styles.cardActor} aria-label={`Durchgeführt von: ${entry.actor_name}`}>
                            {entry.actor_name}
                          </span>
                          {entry.reason && (
                            <span className={styles.cardReason} aria-label={`Grund: ${entry.reason}`}>
                              {entry.reason}
                            </span>
                          )}
                        </p>
                      )}
                    </li>
                  ))}
                </ul>
              )
            ) : (
              <div className={styles.loading} role="status" aria-live="polite">
                Historie wird geladen...
              </div>
            )}
          </section>
        </section>
      </main>
    </div>
  )
}