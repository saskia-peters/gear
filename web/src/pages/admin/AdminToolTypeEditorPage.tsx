import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listToolTypes, createToolType, updateToolType } from '../../auth/tools.ts'
import type { InspectionMode } from '../../auth/tools.ts'
import { listSchedules } from '../../auth/settings.ts'
import type { Schedule } from '../../auth/settings.ts'
import { listQualifications } from '../../auth/qualifications.ts'
import type { Qualification, QualificationList } from '../../auth/qualifications.ts'
import { AttributesEditor } from '../../components/AttributesEditor.tsx'
import styles from './AdminToolTypeEditorPage.module.css'

type Feedback = { kind: 'success' | 'error'; message: string } | null

// AdminToolTypeEditorPage is the DEDICATED create/edit page for a tool type
// (Spec 4-6, FR-8/FR-9/FR-10/FR-23/AD-6): the shared type editor form (moved
// verbatim from the old inline ToolTypesTab form) in create mode — reachable at
// /admin/werkzeuge/typen/neu — and in edit mode at /admin/werkzeuge/typen/:id,
// pre-filled via listToolTypes().find (there is no getToolType(id) client; the
// list fetch is cheap at catalogue scale). Saving runs createToolType /
// updateToolType and then navigates back to the Werkzeuge list. 401→login,
// 403→leave the admin module, unknown id → German not-found with no form. The
// route is gated by tool_types.manage (RequireAdminEntry).
export function AdminToolTypeEditorPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { id } = useParams<{ id: string }>()
  const editingId = id ?? null
  const perms = getPermissions()
  // The list carried the originating tab through router state (Spec 4-6 review
  // 4): returning to the list keeps the caller on the Typen tab.
  const returnTab = (location.state as { tab?: string } | null)?.tab ?? 'typen'

  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [notFound, setNotFound] = useState(false)
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [qualifications, setQualifications] = useState<Qualification[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const [name, setName] = useState('')
  const [defaultScheduleId, setDefaultScheduleId] = useState('')
  const [requiredQualificationId, setRequiredQualificationId] = useState('')
  const [inspectionMode, setInspectionMode] = useState<InspectionMode>('pass_fail')
  const [checklistItems, setChecklistItems] = useState<Array<{ label: string }>>([])
  // attributes / attributesKnown / attributesValid / formVersion mirror the
  // original editor's "Eigene Felder" contract (Story 4.4, FR-10): the loaded/
  // edited object, the touched flag (only then included in the save body) and
  // the validity gate (a bad key / over-size set disables Save).
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
      // BEST-EFFORT dropdown population: schedules and qualifications are
      // SEPARATE catalog surfaces gated by their OWN permissions
      // (schedules.manage / qualifications.manage). A 403 here degrades to an
      // EMPTY dropdown and the form still renders; it must never eject from the
      // module. A 401, however, means the session is expired/revoked and is
      // AUTHORITATIVE even here — create mode has no primary list load, so a
      // swallowed 401 would silently render a full form for a logged-out caller
      // (Spec 4-6 review 6).
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
      const [scheds, quals] = await Promise.all([
        loadBestEffort(() => listSchedules(), [] as Schedule[]),
        loadBestEffort(() => listQualifications(), { qualifications: [], users: [] } as QualificationList),
      ])
      if (cancelled) return
      const loadedScheds = Array.isArray(scheds) ? scheds : []
      const loadedQuals = Array.isArray(quals.qualifications) ? quals.qualifications : []
      setSchedules(loadedScheds)
      setQualifications(loadedQuals)

      if (editingId) {
        // EDIT mode: load the whole active type catalog and find by id (the
        // shared list fetch). Unknown id → German not-found, no form.
        try {
          const types = await listToolTypes()
          if (cancelled) return
          const tt = types.find((t) => t.id === editingId)
          if (!tt) {
            setNotFound(true)
          } else {
            setName(tt.name)
            // Stale-field guards (Spec 4-6 review 7): a default schedule or
            // required qualification whose catalog entry is absent (archived or
            // a degraded/empty catalog) must NOT be resubmitted — clear it to ''
            // so canSave turns off with the hint instead of a 400-ing stale id.
            setDefaultScheduleId(
              loadedScheds.some((s) => s.id === tt.default_schedule_id) ? tt.default_schedule_id : '',
            )
            setRequiredQualificationId(
              loadedQuals.some((q) => q.id === tt.required_qualification_id) ? tt.required_qualification_id : '',
            )
            setInspectionMode(tt.inspection_mode)
            setChecklistItems(tt.checklist_items.map((item) => ({ label: item.label })))
            // Load the stored attributes; once loaded they are always submitted
            // (round-tripped, or {} after the user cleared the section).
            setAttributes(tt.attributes ?? {})
            setAttributesKnown(true)
            setAttributesValid(true)
            setFormVersion((v) => v + 1)
          }
        } catch (err) {
          if (cancelled) return
          if (!handleApiError(err)) {
            setLoadError('Der Gerätetyp konnte nicht geladen werden.')
          }
        } finally {
          if (!cancelled) setLoaded(true)
        }
        return
      }

      // CREATE mode: empty form. Auto-select the first schedule entry so the
      // save is immediately valid (the form is disabled until a schedule is
      // chosen). The required qualification is OPTIONAL (000023).
      if (loadedScheds.length > 0) {
        setDefaultScheduleId(loadedScheds[0].id)
      }
      if (!cancelled) setLoaded(true)
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [editingId, handleApiError, navigate])

  // A tool type cannot be saved without a default schedule (a mandatory FK).
  // attributesValid blocks Save while the "Eigene Felder" section is invalid.
  const canSave = defaultScheduleId !== '' && attributesValid

  async function save() {
    setBusy(true)
    setFeedback(null)
    const input = {
      name: name.trim(),
      default_schedule_id: defaultScheduleId,
      required_qualification_id: requiredQualificationId,
      inspection_mode: inspectionMode,
      // In pass_fail mode the checklist is always empty (the editor hides it).
      items: inspectionMode === 'checklist' ? checklistItems : [],
      // attributes follows the shared contract (Story 4.4): OMITTED when the
      // section was never touched (absent = unchanged), {} to clear, the
      // edited object otherwise.
      attributes: attributesKnown ? attributes : undefined,
    }
    try {
      if (editingId) {
        const saved = await updateToolType(editingId, input)
        navigate('/admin/werkzeuge', { state: { tab: returnTab, message: saved.message } })
      } else {
        const saved = await createToolType(input)
        navigate('/admin/werkzeuge', { state: { tab: returnTab, message: saved.message } })
      }
    } catch (err) {
      if (handleApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Gerätetyp konnte nicht gespeichert werden.',
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
            <div className={styles.skeleton} aria-busy="true" aria-label="Gerätetyp wird geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : notFound ? (
            <p role="status" className={styles.emptyHint}>
              Gerätetyp nicht gefunden.
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
                <h3 className={styles.formTitle}>{editingId ? 'Gerätetyp bearbeiten' : 'Neuer Gerätetyp'}</h3>
                <div className={styles.formHeaderActions}>
                  <button type="submit" className={styles.saveButton} disabled={busy || !canSave}>
                    {busy ? 'Wird gespeichert...' : editingId ? 'Änderungen speichern' : 'Speichern'}
                  </button>
                </div>
              </div>
              {!canSave && (
                <p role="status" className={styles.emptyHint}>
                  Wähle einen Standard-Zeitplan aus, um zu speichern.
                </p>
              )}

              <div className={styles.field}>
                <label className={styles.label} htmlFor="tool-type-name">
                  Name
                </label>
                <input
                  id="tool-type-name"
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
                  <label className={styles.label} htmlFor="tool-type-schedule">
                    Standard-Zeitplan
                  </label>
                  <select
                    id="tool-type-schedule"
                    className={styles.select}
                    value={defaultScheduleId}
                    onChange={(e) => {
                      setDefaultScheduleId(e.target.value)
                      setFeedback(null)
                    }}
                  >
                    {schedules.length === 0 && <option value="">Keine Zeitpläne vorhanden</option>}
                    {schedules.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="tool-type-qualification">
                    Erforderliche Qualifikation
                  </label>
                  <select
                    id="tool-type-qualification"
                    className={styles.select}
                    value={requiredQualificationId}
                    onChange={(e) => {
                      setRequiredQualificationId(e.target.value)
                      setFeedback(null)
                    }}
                  >
                    <option value="">Keine Qualifikation erforderlich</option>
                    {qualifications.length === 0 && <option value="">Keine Qualifikationen vorhanden</option>}
                    {qualifications.map((q) => (
                      <option key={q.id} value={q.id}>
                        {q.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <fieldset className={styles.modeFieldset}>
                <legend className={styles.label}>Prüfmodus</legend>
                <div className={styles.modeSwitch}>
                  <label className={inspectionMode === 'pass_fail' ? styles.modeActive : styles.modeOption}>
                    <input
                      type="radio"
                      name="tool-type-mode"
                      value="pass_fail"
                      checked={inspectionMode === 'pass_fail'}
                      onChange={() => {
                        setInspectionMode('pass_fail')
                        setFeedback(null)
                      }}
                    />
                    <span>Pass/Fail</span>
                  </label>
                  <label className={inspectionMode === 'checklist' ? styles.modeActive : styles.modeOption}>
                    <input
                      type="radio"
                      name="tool-type-mode"
                      value="checklist"
                      checked={inspectionMode === 'checklist'}
                      onChange={() => {
                        setInspectionMode('checklist')
                        setFeedback(null)
                      }}
                    />
                    <span>Checkliste</span>
                  </label>
                </div>
              </fieldset>

              {inspectionMode === 'checklist' && (
                <ChecklistEditor
                  items={checklistItems}
                  onChange={setChecklistItems}
                  onDirty={() => setFeedback(null)}
                />
              )}

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

// ChecklistEditor is the ordered checklist-item editor (FR-23): add, remove and
// re-order (up/down) the labels of the type's inspection template. Only shown
// in checklist mode; the ordered list is submitted as-is (full replacement).
// Moved verbatim from the old inline ToolTypesTab form (Spec 4-6).
function ChecklistEditor({
  items,
  onChange,
  onDirty,
}: {
  items: Array<{ label: string }>
  onChange: (items: Array<{ label: string }>) => void
  onDirty: () => void
}) {
  const [draft, setDraft] = useState('')

  function add() {
    const label = draft.trim()
    if (label === '') return
    onChange([...items, { label }])
    setDraft('')
    onDirty()
  }

  function remove(index: number) {
    onChange(items.filter((_, i) => i !== index))
    onDirty()
  }

  function move(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= items.length) return
    const next = [...items]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    onChange(next)
    onDirty()
  }

  return (
    <div className={styles.checklist}>
      <span className={styles.label}>Checklisten-Einträge</span>
      {items.length === 0 && (
        <p role="status" className={styles.emptyHint}>
          Noch keine Einträge. Füge den ersten Prüfpunkt hinzu.
        </p>
      )}
      {items.length > 0 && (
        <ol className={styles.checklistList}>
          {items.map((item, index) => (
            <li key={index} className={styles.checklistRow}>
              <span className={styles.checklistIndex}>{index + 1}.</span>
              <span className={styles.checklistLabel}>{item.label}</span>
              <div className={styles.checklistActions}>
                <button
                  type="button"
                  className={styles.iconButton}
                  aria-label={`Nach oben: ${item.label}`}
                  disabled={index === 0}
                  onClick={() => move(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className={styles.iconButton}
                  aria-label={`Nach unten: ${item.label}`}
                  disabled={index === items.length - 1}
                  onClick={() => move(index, 1)}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className={styles.iconButton}
                  aria-label={`Entfernen: ${item.label}`}
                  onClick={() => remove(index)}
                >
                  ×
                </button>
              </div>
            </li>
          ))}
        </ol>
      )}
      <div className={styles.checklistAddRow}>
        <input
          className={styles.input}
          value={draft}
          maxLength={255}
          placeholder="Prüfpunkt hinzufügen"
          onChange={(e) => {
            setDraft(e.target.value)
            onDirty()
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
        />
        <button type="button" className={styles.rowButton} disabled={draft.trim() === ''} onClick={add}>
          Hinzufügen
        </button>
      </div>
    </div>
  )
}
