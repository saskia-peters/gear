import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SummaryGrid } from '../components/SummaryGrid.tsx'
import type { SummaryCounts } from '../components/SummaryGrid.tsx'
import { FilterChips } from '../components/FilterChips.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { PromptDialog } from '../components/PromptDialog.tsx'
import { clearAuthState, hasPermission } from '../auth/authState.ts'
import { listDashboardTools, reinstateTool, REINSTATE_PERMISSION, startInspection, exportStatusReportPdf, REPORT_EXPORT_PERMISSION } from '../auth/tools.ts'
import type { DashboardTool } from '../auth/tools.ts'
import { statusLabel, statusClassKey, type StatusCode } from '../types/filters.ts'
import type { ToolDetailsState } from './ToolDetailsPage.tsx'
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
//
// Story 5.6 (FR-14/AD-4 + FR-15/AD-9): an OOS tool is NOT inspectable — its
// "Prüfung starten" button is DISABLED (no start click, no dead 403). Only a
// `tool.reinstate` holder (Fuehrung/Admin) sees the "Wiederherstellen" button
// on an OOS row; clicking it opens the PromptDialog asking for the MANDATORY
// reason, then POSTs the reinstatement. On success the list refetches (the tool
// leaves OOS; statuses/colors/counts update) + a confirmation shows; on error
// the server's German message shows inline. Non-holders never see the button
// (the 403 IS the gate, AD-6 — never client-side trust).
export function DashboardPage() {
  // selectedFilters holds the ACTIVE status CODES; the EMPTY set means "Alle"
  // (no filter). "Alle" is cleared via clearFilters (Story 6.1).
  const [selectedFilters, setSelectedFilters] = useState<ReadonlySet<StatusCode>>(new Set())
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [tools, setTools] = useState<DashboardTool[]>([])
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})
  // confirmMessage is the transient success confirmation after a reinstatement
  // (Story 5.6): it shows the server's German message. It is CLEARED when a new
  // action starts or an error is set — a stale success must never sit next to a
  // newer error.
  const [confirmMessage, setConfirmMessage] = useState('')
  // reinstateDialog is the PromptDialog target CAPTURED at open time (Story
  // 5.6): { toolId, toolName }. It is set when "Wiederherstellen" is clicked
  // and cleared on submit/close. It is deliberately NOT derived live from the
  // `tools` list — a list refetch while the dialog is open must not resolve it
  // to null and close the dialog mid-input (the captured data is all the dialog
  // needs).
  const [reinstateDialog, setReinstateDialog] = useState<{ toolId: string; toolName: string } | null>(null)
  // reinstateBusy disables the dialog form + the row's reinstate buttons while
  // the reinstatement is in flight (double-submit guard).
  const [reinstateBusy, setReinstateBusy] = useState(false)
  // disabledStarts/pendingStarts hold tool ids whose start button is disabled
  // for the session (a 403) or in flight. They are COMPONENT STATE, so they
  // persist across the list REFETCH effect (the component instance is stable
  // across refetches) and are only cleared on a full reload (the component
  // remounts). pendingGuardRef adds the SYNCHRONOUS double-submit guard (a
  // second click in the same tick cannot slip past the async state update).
  const [disabledStarts, setDisabledStarts] = useState<ReadonlySet<string>>(new Set())
  const [pendingStarts, setPendingStarts] = useState<ReadonlySet<string>>(new Set())
  const pendingGuardRef = useRef<Set<string>>(new Set())
  // cancelledRef is the in-flight cancellation guard for loadTools (Story 5.6
  // patch): set on unmount so a mount or post-reinstate refetch can never call
  // setTools/setLoaded after the component is gone. Shared by every in-flight
  // load (the flag applies to whichever load is pending at unmount time).
  const cancelledRef = useRef(false)
  const navigate = useNavigate()
  // canReinstate (Story 5.6, FR-15/AD-9): the "Wiederherstellen" button renders
  // ONLY for tool.reinstate holders — mirroring the server gate (AD-6).
  const canReinstate = hasPermission(REINSTATE_PERMISSION)
  // canExportReport (Story 6.2, FR-17/AD-6): the "Als PDF exportieren" button
  // renders ONLY for report.export holders — mirroring the server gate (AD-6).
  const canExportReport = hasPermission(REPORT_EXPORT_PERMISSION)
  // exportBusy disables the export button while the fetch is in flight
  // (double-submit guard); exportError is the inline German error (a stale
  // 403 permission cache, a server failure, ...). The export downloads the
  // server-rendered PDF — the SPA never generates one.
  const [exportBusy, setExportBusy] = useState(false)
  const [exportError, setExportError] = useState('')

  // loadTools fetches the dashboard list (shared by the mount effect and the
  // post-reinstate refresh, Story 5.6): statuses/colors/counts update because
  // they are all DERIVED from the refetched list. Stale inline errors are
  // cleared on a successful refresh; the session DISABLED set is deliberately
  // NOT cleared (it persists across refetches). The `cancelledRef` guard (set by
  // the effect cleanup on unmount) is checked after every await so a load that
  // resolves after unmount never touches component state.
  const loadTools = useCallback(async (): Promise<void> => {
    try {
      const items = await listDashboardTools()
      if (cancelledRef.current) return
      setTools(items)
      setRowErrors({})
    } catch (err) {
      if (cancelledRef.current) return
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 401 || status === 403) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setLoadError('Die Werkzeugliste konnte nicht geladen werden.')
      setConfirmMessage('')
    } finally {
      if (!cancelledRef.current) setLoaded(true)
    }
  }, [navigate])

  useEffect(() => {
    cancelledRef.current = false
    void loadTools()
    return () => {
      cancelledRef.current = true
    }
  }, [loadTools])

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
    // Story 5.6 patch: a new action starts → a stale reinstate confirmation
    // must not linger next to the upcoming outcome.
    setConfirmMessage('')
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

  // handleReinstate POSTs the reinstatement (Story 5.6, FR-15/AD-9) with the
  // reason the PromptDialog collected. On success the dialog closes, the
  // server's confirmation shows and the list refetches (the tool leaves OOS);
  // on error the dialog closes and the server's German message shows inline on
  // the row. 401 → login (stale/revoked session).
  // handleOpenDetails (Story 6.1b, FR-16/6.3): the row is an interactive
  // target — clicking anywhere except a button opens the tool's details page
  // carrying the header data as router state in the EXACT 6.3 ToolDetailsState
  // shape (tool_name / tool_type_name / inventory_number / status, typed so any
  // field drift is a compile error) so the details page can render the header
  // without a fetch. A deep-link/refresh has no state and the details page
  // refetches the dashboard list — unchanged. The in-flight guard: navigating
  // away while a start or reinstate for this tool is pending would drop the
  // in-flight confirmation/dialog state, so the navigation is skipped.
  const handleOpenDetails = (tool: DashboardTool): void => {
    if (pendingStarts.has(tool.id) || reinstateBusy) return
    const state: ToolDetailsState = {
      tool_name: tool.name,
      tool_type_name: tool.tool_type_name,
      inventory_number: tool.inventory_number,
      status: tool.status,
    }
    navigate(`/tools/${tool.id}`, { state })
  }

  // handleReinstate POSTs the reinstatement (Story 5.6, FR-15/AD-9) with the
  // reason the PromptDialog collected. The target comes from the CAPTURED
  // reinstateDialog (set at open time — never a live list lookup, so a refetch
  // can't drop it mid-input). On success the dialog closes, the SERVER's
  // confirmation shows (the handler always sends it — no client literal) and
  // the list refetches (the tool leaves OOS); on error the dialog closes and
  // the server's German message shows inline. 401 → login (stale/revoked
  // session). The confirmation is cleared at the start of every attempt, so a
  // stale success never sits next to a newer error.
  const handleReinstate = async (reason: string): Promise<void> => {
    if (!reinstateDialog) return
    const { toolId } = reinstateDialog
    setReinstateBusy(true)
    setConfirmMessage('')
    try {
      const result = await reinstateTool(toolId, reason)
      setReinstateDialog(null)
      setConfirmMessage(result.message)
      await loadTools()
    } catch (err) {
      setReinstateDialog(null)
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      const message =
        err instanceof Error && err.message !== '' ? err.message : 'Die Wiederherstellung ist fehlgeschlagen.'
      setRowErrors((prev) => ({ ...prev, [toolId]: message }))
    } finally {
      setReinstateBusy(false)
    }
  }

  // handleExport downloads the status report PDF (Story 6.2, FR-17): the ACTIVE
  // status filters travel as ?status=… (the EMPTY set omits the param → "Alle");
  // the server RE-DERIVES the statuses and renders the PDF — the SPA only
  // triggers the download (a[download] + URL.createObjectURL). 401 → login
  // (stale/revoked session); 403 → inline German error (stale permission cache
  // — the server is the gate, AD-6); other → inline German error. The button is
  // disabled while the fetch is in flight.
  const handleExport = async (): Promise<void> => {
    if (exportBusy) return
    setExportBusy(true)
    setExportError('')
    // A new action starts → a stale success confirmation must not linger next
    // to the upcoming outcome.
    setConfirmMessage('')
    try {
      const blob = await exportStatusReportPdf([...selectedFilters])
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'statusbericht.pdf'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      // The revoke is DEFERRED: revoking synchronously right after click() can
      // race the browser's download initiation (e.g. Safari drops the file).
      window.setTimeout(() => URL.revokeObjectURL(url), 0)
    } catch (err) {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      const message =
        err instanceof Error && err.message !== '' ? err.message : 'Der Export ist fehlgeschlagen.'
      setExportError(message)
    } finally {
      setExportBusy(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <div className={styles.headerRow}>
            <div className={styles.headerText}>
              <h2 className={styles.pageTitle}>Übersicht</h2>
              <p className={styles.pageSubtitle}>
                G.E.A.R. (Geräte-Einsatz-Assistenz &amp; Readiness) — Geräteverwaltung &amp; Einsatzbereitschaft
              </p>
            </div>
            {/* Story 6.2 (FR-17/AD-6): the "Als PDF exportieren" control renders
                ONLY for report.export holders (hide = the UX half of AD-6; the
                SERVER is the gate — a non-holder answers 403 with no PDF bytes).
                On click it downloads the server-rendered PDF of the CURRENTLY
                FILTERED tool list (?status=… from the active filters). */}
            {canExportReport && (
              <button
                type="button"
                className={styles.exportButton}
                disabled={exportBusy}
                onClick={() => void handleExport()}
              >
                Als PDF exportieren
              </button>
            )}
          </div>
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
          {exportError && (
            // Story 6.2: an inline German error when the export fails (e.g. a
            // stale 403 permission cache — the server is the gate, AD-6).
            <p role="alert" className={styles.error}>
              {exportError}
            </p>
          )}
          {confirmMessage && (
            <p role="status" className={styles.confirm}>
              {confirmMessage}
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
                // Story 6.1b (FR-16): the row is ONE horizontal line (status
                // chip, name, type, Gerätenummer, action buttons) and an
                // INTERACTIVE target: clicking anywhere except a button opens
                // the tool details page /tools/:toolId with the EXISTING 6.3
                // ToolDetailsState (the fast header path; a deep-link still
                // refetches). Enter/Space activate the same navigation
                // (keyboard parity, WCAG AA). Every interactive child that
                // must NOT navigate stops propagation.
                <li
                  key={tool.id}
                  className={styles.row}
                  onClick={() => handleOpenDetails(tool)}
                  onKeyDown={(e) => {
                    // Only the row ITSELF (tabIndex 0) activates the details —
                    // a keydown that bubbles from a child control (e.g. the
                    // start/reinstate button) must NOT also navigate. A HELD
                    // key fires repeated keydowns (e.repeat) — only the first
                    // press may navigate.
                    if (e.target !== e.currentTarget || e.repeat) return
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      handleOpenDetails(tool)
                    }
                  }}
                  tabIndex={0}
                  aria-label={`Details für ${tool.name} öffnen`}
                >
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
                      <div className={styles.rowButtons}>
                        <button
                          type="button"
                          className={styles.startButton}
                          disabled={
                            // Story 5.6: an OOS tool is NOT inspectable — the
                            // start button is DISABLED (FR-14/AD-4, no start
                            // click). Otherwise the session 403-disable + the
                            // in-flight guard apply.
                            tool.status.status === 'oos' ||
                            disabledStarts.has(tool.id) ||
                            pendingStarts.has(tool.id)
                          }
                          aria-label={`Prüfung starten für ${tool.name}`}
                          // stopPropagation: the start action runs WITHOUT row
                          // navigation (Story 6.1b, ROW_START).
                          onClick={(e) => {
                            e.stopPropagation()
                            void handleStart(tool)
                          }}
                        >
                          Prüfung starten
                        </button>
                        {tool.status.status === 'oos' && canReinstate && (
                          <button
                            type="button"
                            className={styles.reinstateButton}
                            disabled={reinstateBusy}
                            aria-label={`Wiederherstellen für ${tool.name}`}
                            // stopPropagation: the reinstate dialog opens
                            // WITHOUT row navigation (Story 6.1b, ROW_REINSTATE).
                            onClick={(e) => {
                              e.stopPropagation()
                              setReinstateDialog({ toolId: tool.id, toolName: tool.name })
                            }}
                          >
                            Wiederherstellen
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                  {rowErrors[tool.id] && (
                    // stopPropagation: a row error never triggers navigation
                    // (Story 6.1b, ROW_ERROR).
                    <p
                      role="alert"
                      className={styles.rowError}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {rowErrors[tool.id]}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>

      {reinstateDialog && (
        <PromptDialog
          title="Werkzeug wiederherstellen"
          label={`Grund für die Wiederherstellung von ${reinstateDialog.toolName}`}
          placeholder="Grund für die Wiederherstellung"
          submitLabel="Wiederherstellen"
          cancelLabel="Abbrechen"
          // Client-side pre-check only; the wording matches the server's
          // MsgReinstatementReasonRequired (the server remains the gate).
          emptyMessage="Bitte gib einen Grund für die Wiederherstellung an."
          busy={reinstateBusy}
          onSubmit={(reason) => void handleReinstate(reason)}
          onClose={() => {
            if (!reinstateBusy) setReinstateDialog(null)
          }}
        />
      )}
    </div>
  )
}
