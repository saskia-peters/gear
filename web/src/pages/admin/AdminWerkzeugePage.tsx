import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import {
  TOOL_TYPES_PERMISSION,
  TOOLS_PERMISSION,
  TOOL_EDIT_PERMISSION,
  listToolTypes,
  archiveToolType,
  listTools,
  archiveTool,
  importToolsCsv,
  toolImportTemplate,
} from '../../auth/tools.ts'
import type { ToolType, Tool, ToolImportResult } from '../../auth/tools.ts'
import { EmptyState } from '../../components/EmptyState.tsx'
import styles from './AdminWerkzeugePage.module.css'

type Feedback = { kind: 'success' | 'error'; message: string } | null
type Tab = 'typen' | 'werkzeuge'
type SortDir = 'asc' | 'desc'

// AdminWerkzeugePage is the Tool catalogue surface (Story 4.2 + 4.3, FR-8/
// FR-9/FR-10/FR-23/UX-DR6/UX-DR8, Spec 4-6): a tab bar over "Typen" (tool
// types, gated by tool_types.manage) and "Werkzeuge" (physical tools, gated by
// tools.manage, AD-6). Each tab is a COMPACT, SORTABLE one-line list (no inline
// create/edit form — creation and editing live on dedicated pages under
// /admin/werkzeuge/typen|tools/neu|:id, Spec 4-6) with a "Create new" button,
// a per-row "Bearbeiten" navigation to the edit page and a per-row archive
// action with a confirm. Inline German feedback, ≥48px targets, 401→login,
// 403→leave the admin module. The server remains the source of truth.
export function AdminWerkzeugePage() {
  const navigate = useNavigate()
  const location = useLocation()
  const perms = getPermissions()
  const canToolTypes = perms.includes(TOOL_TYPES_PERMISSION)
  // The Werkzeuge tab opens ANY-of [tools.manage, tool.edit] (Story 4-3b): a
  // tool.edit-only holder (e.g. a Führende) can view + edit tools.
  const canTools = perms.includes(TOOLS_PERMISSION) || perms.includes(TOOL_EDIT_PERMISSION)
  // create/archive stay tools.manage-ONLY — a tool.edit-only holder sees the
  // list + editor but NO create button and NO archive button.
  const canManageTools = perms.includes(TOOLS_PERMISSION)
  // The editors return via router state (Spec 4-6 review 1/4): the carried tab
  // restores the originating Typen/Werkzeuge tab instead of remounting to the
  // permission default, and the carried message shows once as a success notice.
  // The state is cleared after the first render so it never reappears on a
  // later remount.
  const carried = location.state as { tab?: Tab | ''; message?: string } | null
  // Defensive tab default (finding 15): default to a tab whose panel can
  // render. When the caller holds NO tool surface code (neither
  // tool_types.manage nor tools.manage/tool.edit), the tab stays '' — a value
  // NO panel renders for — and the EmptyState fallback shows (the render's
  // `&& canX` guards already prevent a tab-less panel; the '' sentinel makes
  // the no-access state explicit instead of defaulting to an inert 'typen').
  const [activeTab, setActiveTab] = useState<Tab | ''>(() => {
    const fromTab = carried?.tab
    if (fromTab === 'typen' && canToolTypes) return 'typen'
    if (fromTab === 'werkzeuge' && canTools) return 'werkzeuge'
    return canToolTypes ? 'typen' : canTools ? 'werkzeuge' : ''
  })
  const [notice] = useState<string | null>(carried?.message ?? null)

  useEffect(() => {
    // Drop the carried state after the first render: the success notice stays
    // visible for this view, but a later remount/navigation must not re-show it.
    if (notice) {
      navigate(location.pathname, { replace: true, state: null })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleApiError = useCallback(
    (err: unknown): boolean => {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 403) {
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return true
      }
      if (status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return true
      }
      return false
    },
    [navigate],
  )

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(perms)} />
        <main className={styles.main}>
          <h2 className={styles.title}>Werkzeuge</h2>
          <p className={styles.description}>Geräte und Gerätetypen verwalten.</p>

          {notice && (
            <p role="status" className={styles.feedbackSuccess}>
              {notice}
            </p>
          )}

          <div className={styles.tabs} role="tablist" aria-label="Werkzeuge">
            {canToolTypes && (
              <button
                type="button"
                id="tab-typen"
                role="tab"
                aria-selected={activeTab === 'typen'}
                aria-controls="panel-typen"
                className={activeTab === 'typen' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('typen')}
              >
                Typen
              </button>
            )}
            {canTools && (
              <button
                type="button"
                id="tab-werkzeuge"
                role="tab"
                aria-selected={activeTab === 'werkzeuge'}
                aria-controls="panel-werkzeuge"
                className={activeTab === 'werkzeuge' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('werkzeuge')}
              >
                Werkzeuge
              </button>
            )}
          </div>

          {activeTab === 'typen' && canToolTypes ? (
            <div id="panel-typen" role="tabpanel" aria-labelledby="tab-typen">
              <ToolTypesTab onApiError={handleApiError} />
            </div>
          ) : activeTab === 'werkzeuge' && canTools ? (
            <div id="panel-werkzeuge" role="tabpanel" aria-labelledby="tab-werkzeuge">
              <ToolsTab onApiError={handleApiError} canManageTools={canManageTools} />
            </div>
          ) : (
            // The fallback only renders when NEITHER Tab tab is shown, so it
            // carries no tabpanel/aria-labelledby wiring (a panel must not
            // reference a tab that does not exist).
            <div className={styles.emptyStateWrap}>
              <EmptyState message="Keine Werkzeuge vorhanden" description="Werkzeuge und Gerätetypen sind für dein Konto nicht verfügbar." />
            </div>
          )}
        </main>
      </div>
    </div>
  )
}

// modeLabel renders a tool type's inspection-mode cell: "Pass/Fail" or
// "Checkliste · N Punkte" (the item labels themselves are NOT dumped, Spec 4-6).
function modeLabel(tt: ToolType): string {
  const mode = tt.inspection_mode === 'checklist' ? 'Checkliste' : 'Pass/Fail'
  return tt.checklist_items.length > 0 ? `${mode} · ${tt.checklist_items.length} Punkte` : mode
}

// ToolTypesTab is the "Typen" surface (Story 4.2, Spec 4-6): a COMPACT one-line
// sortable list of the active tool types — Name · Modus · Edit — with a "Neuer
// Gerätetyp" button navigating to /admin/werkzeuge/typen/neu, a per-row
// "Bearbeiten" navigating to /admin/werkzeuge/typen/:id, and a per-row archive
// action with a confirm. The create/edit FORM lives on the dedicated editor
// page. Sorted presentation-only (Name asc by default, localeCompare 'de').
function ToolTypesTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const navigate = useNavigate()
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [toolTypes, setToolTypes] = useState<ToolType[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [sort, setSort] = useState<{ key: 'name' | 'modus'; dir: SortDir }>({ key: 'name', dir: 'asc' })

  useEffect(() => {
    let cancelled = false
    async function run() {
      // PRIMARY content load: the tool-type list. A 401/403 HERE is
      // authoritative — the caller cannot reach the Typen surface, so the
      // existing auth handling (401→login, 403→leave module) applies.
      try {
        const types = await listToolTypes()
        if (!cancelled) setToolTypes(types)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Gerätetypen konnten nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setLoaded(true)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [onApiError])

  function cycleSort(key: 'name' | 'modus') {
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }))
  }

  function ariaSort(key: 'name' | 'modus'): 'ascending' | 'descending' | 'none' {
    if (sort.key !== key) return 'none'
    return sort.dir === 'asc' ? 'ascending' : 'descending'
  }

  function sortLabel(key: 'name' | 'modus', label: string): string {
    const state = ariaSort(key)
    const hint = state === 'ascending' ? ' (absteigend)' : state === 'descending' ? ' (aufsteigend)' : ''
    return `Sortieren nach ${label}${hint}`
  }

  function sortIndicator(key: 'name' | 'modus'): string {
    return ariaSort(key) === 'ascending' ? '▲' : ariaSort(key) === 'descending' ? '▼' : ''
  }

  const sorted = [...toolTypes].sort((a, b) => {
    const av = sort.key === 'modus' ? modeLabel(a) : a.name
    const bv = sort.key === 'modus' ? modeLabel(b) : b.name
    const cmp = av.localeCompare(bv, 'de', { sensitivity: 'base' })
    return sort.dir === 'asc' ? cmp : -cmp
  })

  async function archive(tt: ToolType) {
    const ok = window.confirm(
      `Gerätetyp „${tt.name}“ wirklich archivieren? Archivierte Gerätetypen können nicht mehr bearbeitet werden.`,
    )
    if (!ok) return
    setBusy(true)
    setFeedback(null)
    try {
      const result = await archiveToolType(tt.id)
      setToolTypes((prev) => prev.filter((x) => x.id !== tt.id))
      setFeedback({ kind: 'success', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Gerätetyp konnte nicht archiviert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}
      {loadError && (
        <p role="alert" className={styles.feedbackError}>
          {loadError}
        </p>
      )}

      {!loaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="Gerätetypen werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          <div className={styles.toolbar}>
            <button type="button" className={styles.saveButton} onClick={() => navigate('/admin/werkzeuge/typen/neu', { state: { tab: 'typen' } })}>
              Neuer Gerätetyp
            </button>
          </div>

          {toolTypes.length === 0 ? (
            <p role="status" className={styles.emptyHint}>
              Keine Gerätetypen vorhanden. Lege den ersten Typ an.
            </p>
          ) : (
            <table className={styles.catalogTable} aria-label="Gerätetypen">
              <thead>
                <tr>
                  <th scope="col" aria-sort={ariaSort('name')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('name')}
                      aria-label={sortLabel('name', 'Name')}
                    >
                      Name
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('name')}
                      </span>
                    </button>
                  </th>
                  <th scope="col" aria-sort={ariaSort('modus')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('modus')}
                      aria-label={sortLabel('modus', 'Modus')}
                    >
                      Modus
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('modus')}
                      </span>
                    </button>
                  </th>
                  <th scope="col">
                    <span className={styles.visuallyHidden}>Aktionen</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((tt) => (
                  <tr key={tt.id} className={styles.catalogRow}>
                    <td className={styles.catalogName}>{tt.name}</td>
                    <td className={styles.catalogMeta}>{modeLabel(tt)}</td>
                    <td>
                      <div className={styles.rowActions}>
                        <button
                          type="button"
                          className={styles.rowButton}
                          disabled={busy}
                          onClick={() => navigate(`/admin/werkzeuge/typen/${tt.id}`, { state: { tab: 'typen' } })}
                        >
                          Bearbeiten
                        </button>
                        <button
                          type="button"
                          className={styles.dangerButton}
                          disabled={busy}
                          onClick={() => void archive(tt)}
                        >
                          Archivieren
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </>
  )
}

// ToolsTab is the "Werkzeuge" surface (Story 4.3 + 4-3b, FR-9/FR-10/AD-5/AD-6,
// Spec 4-6): a COMPACT one-line sortable list of the active physical tools —
// Name · Typ · Gerätenummer · Edit — with a "Neues Werkzeug" button navigating
// to /admin/werkzeuge/tools/neu, a per-row "Bearbeiten" navigating to
// /admin/werkzeuge/tools/:id, and a per-row archive action (tools.manage
// holders only). canManageTools (has tools.manage) toggles the create button
// and the archive buttons: a tool.edit-only holder sees the list + the edit
// navigation but NO create button and NO archive buttons (Story 4-3b). The
// create/edit FORM lives on the dedicated editor page. Sorted
// presentation-only (Name asc by default, localeCompare 'de').
function ToolsTab({ onApiError, canManageTools }: { onApiError: (err: unknown) => boolean; canManageTools: boolean }) {
  const navigate = useNavigate()
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [tools, setTools] = useState<Tool[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [sort, setSort] = useState<{ key: 'name' | 'typ' | 'inventory'; dir: SortDir }>({ key: 'name', dir: 'asc' })
  // Bulk CSV import (Story 4.5, FR-9/FR-23): importBusy disables the import
  // buttons while the upload runs; importResult renders the counts + per-row
  // error list + the error-report/template downloads; importError surfaces a
  // hard failure (a 400 German reason, e.g. a missing header).
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [importBusy, setImportBusy] = useState(false)
  const [importResult, setImportResult] = useState<ToolImportResult | null>(null)
  const [importError, setImportError] = useState('')

  useEffect(() => {
    let cancelled = false
    async function run() {
      // PRIMARY content load: the tool list. A 401/403 HERE is authoritative —
      // the caller cannot reach the Werkzeuge surface, so the existing auth
      // handling (401→login, 403→leave module) applies.
      try {
        const items = await listTools()
        if (!cancelled) setTools(items)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Werkzeuge konnten nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setLoaded(true)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [onApiError])

  function cycleSort(key: 'name' | 'typ' | 'inventory') {
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }))
  }

  function ariaSort(key: 'name' | 'typ' | 'inventory'): 'ascending' | 'descending' | 'none' {
    if (sort.key !== key) return 'none'
    return sort.dir === 'asc' ? 'ascending' : 'descending'
  }

  function sortLabel(key: 'name' | 'typ' | 'inventory', label: string): string {
    const state = ariaSort(key)
    const hint = state === 'ascending' ? ' (absteigend)' : state === 'descending' ? ' (aufsteigend)' : ''
    return `Sortieren nach ${label}${hint}`
  }

  function sortIndicator(key: 'name' | 'typ' | 'inventory'): string {
    return ariaSort(key) === 'ascending' ? '▲' : ariaSort(key) === 'descending' ? '▼' : ''
  }

  function toolSortValue(t: Tool, key: 'name' | 'typ' | 'inventory'): string {
    // Null-guard the JOINed type name (Spec 4-6 review 10): an undefined value
    // must never throw inside localeCompare.
    if (key === 'typ') return t.tool_type_name ?? ''
    if (key === 'inventory') return t.inventory_number ?? ''
    return t.name
  }

  const sorted = [...tools].sort((a, b) => {
    const cmp = toolSortValue(a, sort.key).localeCompare(toolSortValue(b, sort.key), 'de', { sensitivity: 'base' })
    return sort.dir === 'asc' ? cmp : -cmp
  })

  async function archive(tool: Tool) {
    const ok = window.confirm(
      `Werkzeug „${tool.name}“ wirklich archivieren? Archivierte Werkzeuge können nicht mehr bearbeitet werden.`,
    )
    if (!ok) return
    setBusy(true)
    setFeedback(null)
    try {
      const result = await archiveTool(tool.id)
      setTools((prev) => prev.filter((x) => x.id !== tool.id))
      setFeedback({ kind: 'success', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Das Werkzeug konnte nicht archiviert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  // runImport uploads the selected CSV (Story 4.5): the server imports valid
  // rows in a set-partitioned batch and reports per-row errors; the result
  // view shows the counts + the per-row error list, and the tool list is
  // RELOADED so the newly created/updated tools appear immediately. The reload
  // is BEST-EFFORT and isolated: the CSV was already persisted, so a reload
  // failure must never be misattributed as an import failure — it falls back
  // to a non-fatal stale-list note. A hard IMPORT failure (400 German, 403,
  // network) surfaces inline.
  async function runImport(file: File) {
    setImportBusy(true)
    setImportResult(null)
    setImportError('')
    try {
      const result = await importToolsCsv(file)
      setImportResult(result)
      if (result.imported > 0) {
        try {
          const items = await listTools()
          setTools(items)
        } catch {
          setFeedback({
            kind: 'error',
            message:
              'Die Liste konnte nach dem Import nicht aktualisiert werden. Bitte lade die Seite neu.',
          })
        }
      }
    } catch (err) {
      if (onApiError(err)) return
      setImportError(
        err instanceof Error && err.message !== '' ? err.message : 'Der CSV-Import ist fehlgeschlagen.',
      )
    } finally {
      setImportBusy(false)
    }
  }

  // downloadTemplate triggers the client-side template CSV download
  // (TEMPLATE_DOWNLOAD): the header + one example row — the exact columns the
  // parser accepts, so the template and the parser can never drift.
  function downloadTemplate() {
    const blob = toolImportTemplate()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'werkzeuge-import-vorlage.csv'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    // Defer the revoke so the browser's download initiation cannot race it.
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  // downloadErrorReport triggers the client-side error-report download built
  // from the JSON result (no server-side stored report file, Story 4.5).
  function downloadErrorReport() {
    if (!importResult) return
    const blob = new Blob([JSON.stringify(importResult, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'werkzeug-import-fehler.json'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  return (
    <>
      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}
      {loadError && (
        <p role="alert" className={styles.feedbackError}>
          {loadError}
        </p>
      )}

      {!loaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="Werkzeuge werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          {/* The create + import controls are HIDDEN for a tool.edit-only
              holder (no tools.manage, Story 4-3b + 4.5). */}
          {canManageTools && (
            <>
              <div className={styles.toolbar}>
                <button type="button" className={styles.saveButton} onClick={() => navigate('/admin/werkzeuge/tools/neu', { state: { tab: 'werkzeuge' } })}>
                  Neues Werkzeug
                </button>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".csv,text/csv"
                  className={styles.visuallyHidden}
                  aria-label="CSV-Datei für den Import wählen"
                  disabled={importBusy}
                  onChange={(e) => {
                    const file = e.target.files?.[0]
                    e.target.value = ''
                    if (file) void runImport(file)
                  }}
                />
                <button
                  type="button"
                  className={styles.rowButton}
                  disabled={importBusy}
                  onClick={() => fileInputRef.current?.click()}
                >
                  CSV importieren
                </button>
                <button type="button" className={styles.rowButton} disabled={importBusy} onClick={downloadTemplate}>
                  Vorlage herunterladen
                </button>
              </div>
              <p className={styles.importGuide}>
                CSV-Format: name (Pflicht), tool_type (Pflicht), schedule (optional), inventory_number
                (optional). Spaltennamen sind unabhängig von Groß-/Kleinschreibung. Eine leere
                schedule- oder inventory_number-Zelle lässt den bestehenden Wert unangetastet.
              </p>
              {importBusy && (
                <p role="status" className={styles.importBusy}>
                  CSV-Import läuft…
                </p>
              )}
              {importError && (
                <p role="alert" className={styles.feedbackError}>
                  {importError}
                </p>
              )}
              {importResult && (
                <div className={styles.importResult}>
                  <p
                    role="status"
                    className={importResult.errors.length > 0 ? styles.feedbackError : styles.feedbackSuccess}
                  >
                    {importResult.imported} Werkzeuge importiert, {importResult.errors.length} Fehler.
                  </p>
                  {importResult.errors.length > 0 && (
                    <>
                      <ul className={styles.importErrorList} aria-label="Importfehler">
                        {importResult.errors.map((importErr) => (
                          <li key={`${importErr.row}-${importErr.reason}`}>
                            Zeile {importErr.row}: {importErr.reason}
                          </li>
                        ))}
                      </ul>
                      <div className={styles.rowActions}>
                        <button type="button" className={styles.rowButton} onClick={downloadErrorReport}>
                          Fehlerreport herunterladen
                        </button>
                        <button type="button" className={styles.rowButton} onClick={() => setImportResult(null)}>
                          Erneut importieren
                        </button>
                      </div>
                    </>
                  )}
                </div>
              )}
            </>
          )}

          {tools.length === 0 ? (
            <p role="status" className={styles.emptyHint}>
              Keine Werkzeuge vorhanden. Lege das erste Werkzeug an.
            </p>
          ) : (
            <table className={styles.catalogTable} aria-label="Werkzeuge">
              <thead>
                <tr>
                  <th scope="col" aria-sort={ariaSort('name')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('name')}
                      aria-label={sortLabel('name', 'Name')}
                    >
                      Name
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('name')}
                      </span>
                    </button>
                  </th>
                  <th scope="col" aria-sort={ariaSort('typ')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('typ')}
                      aria-label={sortLabel('typ', 'Typ')}
                    >
                      Typ
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('typ')}
                      </span>
                    </button>
                  </th>
                  <th scope="col" aria-sort={ariaSort('inventory')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('inventory')}
                      aria-label={sortLabel('inventory', 'Gerätenummer')}
                    >
                      Gerätenummer
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('inventory')}
                      </span>
                    </button>
                  </th>
                  <th scope="col">
                    <span className={styles.visuallyHidden}>Aktionen</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((tool) => (
                  <tr key={tool.id} className={styles.catalogRow}>
                    <td className={styles.catalogName}>{tool.name}</td>
                    <td className={styles.catalogMeta}>
                      {tool.tool_type_name}
                      {tool.schedule_id !== '' && (
                        <span className={styles.overrideBadge}>Zeitplan überschrieben</span>
                      )}
                    </td>
                    <td className={styles.catalogMeta}>{tool.inventory_number}</td>
                    <td>
                      <div className={styles.rowActions}>
                        <button
                          type="button"
                          className={styles.rowButton}
                          disabled={busy}
                          onClick={() => navigate(`/admin/werkzeuge/tools/${tool.id}`, { state: { tab: 'werkzeuge' } })}
                        >
                          Bearbeiten
                        </button>
                        {canManageTools && (
                          <button
                            type="button"
                            className={styles.dangerButton}
                            disabled={busy}
                            onClick={() => void archive(tool)}
                          >
                            Archivieren
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </>
  )
}
