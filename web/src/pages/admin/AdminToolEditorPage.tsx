import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { TOOLS_PERMISSION, listTools, createTool, updateTool } from '../../auth/tools.ts'
import { listToolTypes } from '../../auth/tools.ts'
import type { ToolType } from '../../auth/tools.ts'
import { listSchedules } from '../../auth/settings.ts'
import type { Schedule } from '../../auth/settings.ts'
import { AttributesEditor } from '../../components/AttributesEditor.tsx'
import styles from './AdminToolEditorPage.module.css'

type Feedback = { kind: 'success' | 'error'; message: string } | null

// AdminToolEditorPage is the DEDICATED create/edit page for a physical tool
// (Spec 4-6, FR-9/FR-10/AD-5/AD-6): the shared tool editor form (moved verbatim
// from the old inline ToolsTab form) in create mode — reachable at
// /admin/werkzeuge/tools/neu — and in edit mode at /admin/werkzeuge/tools/:id,
// pre-filled via listTools().find. CREATE mode is ONLY for tools.manage holders
// (a tool.edit-only holder reaches edit mode via the list; reaching the create
// URL directly shows a German no-permission note, never the form). Saving runs
// createTool / updateTool and then navigates back to the Werkzeuge list.
// 401→login, 403→leave the admin module, unknown id → German not-found.
export function AdminToolEditorPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { id } = useParams<{ id: string }>()
  const editingId = id ?? null
  const perms = getPermissions()
  // The list carried the originating tab through router state (Spec 4-6 review
  // 4): returning to the list keeps the caller on the Werkzeuge tab.
  const returnTab = (location.state as { tab?: string } | null)?.tab ?? 'werkzeuge'
  // create stays tools.manage-ONLY — a tool.edit-only holder can view + edit
  // tools but never create/archive (Story 4-3b). The route guard lets both
  // codes reach this page (the Werkzeuge tab opens any-of), so create mode is
  // additionally gated here on tools.manage alone.
  const canManageTools = perms.includes(TOOLS_PERMISSION)

  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [notFound, setNotFound] = useState(false)
  const [toolTypes, setToolTypes] = useState<ToolType[]>([])
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const [name, setName] = useState('')
  const [toolTypeId, setToolTypeId] = useState('')
  const [scheduleId, setScheduleId] = useState('')
  const [inventoryNumber, setInventoryNumber] = useState('')
  // attributes / attributesKnown / attributesValid / formVersion mirror the
  // original editor's "Eigene Felder" contract (Story 4.4, FR-10).
  const [attributes, setAttributes] = useState<Record<string, unknown>>({})
  const [attributesKnown, setAttributesKnown] = useState(false)
  const [attributesValid, setAttributesValid] = useState(true)
  const [formVersion, setFormVersion] = useState(0)

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

  useEffect(() => {
    let cancelled = false
    async function run() {
      // BEST-EFFORT dropdown population: tool types and schedules are SEPARATE
      // catalog surfaces gated by their OWN permissions (tool_types.manage /
      // schedules.manage). A 403 here degrades to an EMPTY dropdown and the
      // form still renders; it must never eject from the module. A 401,
      // however, means the session is expired/revoked and is AUTHORITATIVE even
      // here — create mode has no primary list load, so a swallowed 401 would
      // silently render a full form for a logged-out caller (Spec 4-6 review 6).
      async function loadBestEffort<T>(fetcher: () => Promise<T>, fallback: T): Promise<T> {
        try {
          return await fetcher()
        } catch (err) {
          if (err instanceof Error && 'status' in err && (err as { status: number }).status === 401) {
            clearAuthState()
            navigate('/login', { replace: true })
          }
          return fallback
        }
      }
      const [types, scheds] = await Promise.all([
        loadBestEffort(() => listToolTypes(), [] as ToolType[]),
        loadBestEffort(() => listSchedules(), [] as Schedule[]),
      ])
      if (cancelled) return
      const loadedTypes = Array.isArray(types) ? types : []
      const loadedScheds = Array.isArray(scheds) ? scheds : []
      setToolTypes(loadedTypes)
      setSchedules(loadedScheds)

      if (editingId) {
        // EDIT mode: load the whole active tool list and find by id (the
        // shared list fetch). Unknown id → German not-found, no form.
        try {
          const items = await listTools()
          if (cancelled) return
          const tool = items.find((t) => t.id === editingId)
          if (!tool) {
            setNotFound(true)
          } else {
            setName(tool.name)
            // Only carry over the stored tool type when the freshly-loaded
            // catalog actually contains it: a degraded/empty catalog must fall
            // back to '' so canSave turns off instead of showing a blank select
            // with an enabled save.
            setToolTypeId(
              loadedTypes.some((tt) => tt.id === tool.tool_type_id) ? tool.tool_type_id : '',
            )
            // A stale schedule override (its schedule was archived since the
            // tool was saved) must not be resubmitted — clear it to '' (the
            // inherit default, AD-5) instead of failing the save with an
            // unclear 400.
            setScheduleId(
              loadedScheds.some((s) => s.id === tool.schedule_id) ? tool.schedule_id : '',
            )
            // The inventory number is editable on edit (Story 4-3b); it starts
            // from the stored value.
            setInventoryNumber(tool.inventory_number)
            // Load the stored attributes; once loaded they are always submitted.
            setAttributes(tool.attributes ?? {})
            setAttributesKnown(true)
            setAttributesValid(true)
            setFormVersion((v) => v + 1)
          }
        } catch (err) {
          if (cancelled) return
          if (!handleApiError(err)) {
            setLoadError('Das Werkzeug konnte nicht geladen werden.')
          }
        } finally {
          if (!cancelled) setLoaded(true)
        }
        return
      }

      // CREATE mode: empty form. Auto-select the first tool type so the save is
      // immediately valid (the form is disabled until a type is chosen). The
      // schedule override is OPTIONAL (AD-5).
      if (loadedTypes.length > 0) {
        setToolTypeId(loadedTypes[0].id)
      }
      if (!cancelled) setLoaded(true)
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [editingId, handleApiError, navigate])

  // A tool cannot be saved without a tool type (a mandatory FK). attributesValid
  // blocks Save while the "Eigene Felder" section is invalid.
  const canSave = toolTypeId !== '' && attributesValid

  async function save() {
    setBusy(true)
    setFeedback(null)
    const input = {
      name: name.trim(),
      tool_type_id: toolTypeId,
      schedule_id: scheduleId,
      // attributes follows the shared contract (Story 4.4): OMITTED when the
      // section was never touched (absent = unchanged), {} to clear, the edited
      // object otherwise.
      attributes: attributesKnown ? attributes : undefined,
      // The inventory number travels ONLY on the edit path (Story 4-3b): on
      // create the server auto-assigns it — the create body never carries one.
      inventory_number: editingId ? inventoryNumber : undefined,
    }
    try {
      if (editingId) {
        const saved = await updateTool(editingId, input)
        navigate('/admin/werkzeuge', { state: { tab: returnTab, message: saved.message } })
      } else {
        const saved = await createTool(input)
        navigate('/admin/werkzeuge', { state: { tab: returnTab, message: saved.message } })
      }
    } catch (err) {
      if (handleApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Das Werkzeug konnte nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(perms)} />
        <main className={styles.main}>
          <h2 className={styles.title}>Werkzeuge</h2>
          <button type="button" className={styles.backButton} onClick={() => navigate('/admin/werkzeuge', { state: { tab: returnTab } })}>
            ← Zurück zur Liste
          </button>

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
            <div className={styles.skeleton} aria-busy="true" aria-label="Werkzeug wird geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : notFound ? (
            <p role="status" className={styles.emptyHint}>
              Werkzeug nicht gefunden.
            </p>
          ) : editingId === null && !canManageTools ? (
            // CREATE mode is tools.manage-only (Story 4-3b): a tool.edit-only
            // holder reaching the create URL directly sees a no-permission
            // note, never the form.
            <p role="status" className={styles.emptyHint}>
              Keine Berechtigung zum Anlegen neuer Werkzeuge.
            </p>
          ) : (
            <form
              className={styles.editor}
              onSubmit={(e) => {
                e.preventDefault()
                void save()
              }}
            >
              <div className={styles.formHeader}>
                <h3 className={styles.formTitle}>{editingId ? 'Werkzeug bearbeiten' : 'Neues Werkzeug'}</h3>
                <div className={styles.formHeaderActions}>
                  <button type="submit" className={styles.saveButton} disabled={busy || !canSave}>
                    {busy ? 'Wird gespeichert...' : editingId ? 'Änderungen speichern' : 'Speichern'}
                  </button>
                </div>
              </div>
              {!canSave && (
                <p role="status" className={styles.emptyHint}>
                  Wähle einen Gerätetyp aus, um zu speichern.
                </p>
              )}

              <div className={styles.field}>
                <label className={styles.label} htmlFor="tool-name">
                  Name
                </label>
                <input
                  id="tool-name"
                  className={styles.input}
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    setFeedback(null)
                  }}
                  maxLength={255}
                  autoComplete="off"
                />
              </div>

              <div className={styles.fieldRow}>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="tool-type">
                    Gerätetyp
                  </label>
                  <select
                    id="tool-type"
                    className={styles.select}
                    value={toolTypeId}
                    onChange={(e) => {
                      setToolTypeId(e.target.value)
                      setFeedback(null)
                    }}
                  >
                    {toolTypes.length === 0 && <option value="">Keine Gerätetypen vorhanden</option>}
                    {toolTypes.map((tt) => (
                      <option key={tt.id} value={tt.id}>
                        {tt.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="tool-schedule">
                    Zeitplan
                  </label>
                  <select
                    id="tool-schedule"
                    className={styles.select}
                    value={scheduleId}
                    onChange={(e) => {
                      setScheduleId(e.target.value)
                      setFeedback(null)
                    }}
                  >
                    {/* The DEFAULT/empty option is EXACTLY "Standard für diesen
                        Typ" — empty schedule_id means the tool inherits its
                        type's default schedule (AD-5). It is ALWAYS the single
                        valid empty-value option: an empty schedule catalog still
                        allows the inherit default (the override is optional,
                        never blocks the save). */}
                    <option value="">Standard für diesen Typ</option>
                    {schedules.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className={styles.field}>
                <label className={styles.label} htmlFor="tool-inventory">
                  Gerätenummer
                </label>
                {editingId ? (
                  <input
                    id="tool-inventory"
                    className={styles.input}
                    value={inventoryNumber}
                    onChange={(e) => {
                      setInventoryNumber(e.target.value)
                      setFeedback(null)
                    }}
                    // Mirrors the server's InventoryNumberMaxLength (16 runes)
                    // and the DB CHECK (char_length(inventory_number) <= 16,
                    // 000025) — keep in sync if the bound changes.
                    maxLength={16}
                    autoComplete="off"
                  />
                ) : (
                  <>
                    <input
                      id="tool-inventory"
                      className={styles.inputReadonly}
                      value="wird automatisch vergeben"
                      readOnly
                      tabIndex={-1}
                      aria-describedby="tool-inventory-hint"
                    />
                    <span id="tool-inventory-hint" className={styles.fieldHint}>
                      Die Gerätenummer wird beim Speichern automatisch vergeben.
                    </span>
                  </>
                )}
              </div>

              <AttributesEditor
                key={formVersion}
                attributes={attributes}
                onChange={(next) => {
                  setAttributes(next)
                  setAttributesKnown(true)
                }}
                onDirty={() => setFeedback(null)}
                onValidityChange={setAttributesValid}
              />
            </form>
          )}
        </main>
      </div>
    </div>
  )
}
