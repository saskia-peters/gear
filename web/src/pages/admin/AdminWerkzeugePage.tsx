import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import {
  TOOL_TYPES_PERMISSION,
  listToolTypes,
  createToolType,
  updateToolType,
  archiveToolType,
} from '../../auth/tools.ts'
import type { ToolType, InspectionMode } from '../../auth/tools.ts'
import { listSchedules } from '../../auth/settings.ts'
import type { Schedule } from '../../auth/settings.ts'
import { listQualifications } from '../../auth/qualifications.ts'
import type { Qualification } from '../../auth/qualifications.ts'
import { EmptyState } from '../../components/EmptyState.tsx'
import styles from './AdminWerkzeugePage.module.css'

type Feedback = { kind: 'success' | 'error'; message: string } | null
type Tab = 'typen' | 'werkzeuge'

// AdminWerkzeugePage is the Tool catalogue surface (Story 4.2, FR-8/FR-10/
// FR-23/UX-DR6/UX-DR8): a tab bar over "Typen" (tool types, gated by
// tool_types.manage) and "Werkzeuge" (physical tools — Story 4.3 fills it;
// until then an honest empty state). The type editor offers Name, the default
// schedule dropdown (reusing the schedule-catalog client, AD-16), the required
// qualification dropdown (reusing the qualification-vocabulary client, AD-7),
// the inspection-mode switch and an ordered checklist-item editor when mode is
// checklist. Inline German feedback, sticky actions, ≥48px targets, 401→login,
// 403→leave the admin module. The server remains the source of truth.
export function AdminWerkzeugePage() {
  const navigate = useNavigate()
  const perms = getPermissions()
  const canToolTypes = perms.includes(TOOL_TYPES_PERMISSION)
  const [activeTab, setActiveTab] = useState<Tab>(canToolTypes ? 'typen' : 'werkzeuge')

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
          </div>

          {activeTab === 'typen' && canToolTypes ? (
            <div id="panel-typen" role="tabpanel" aria-labelledby="tab-typen">
              <ToolTypesTab onApiError={handleApiError} />
            </div>
          ) : (
            <div
              id="panel-werkzeuge"
              role="tabpanel"
              aria-labelledby="tab-werkzeuge"
              className={styles.emptyStateWrap}
            >
              <EmptyState
                message="Keine Werkzeuge vorhanden"
                description="Die Verwaltung einzelner Werkzeuge folgt in einem späteren Schritt."
              />
            </div>
          )}
        </main>
      </div>
    </div>
  )
}

// ToolTypesTab is the "Typen" surface (Story 4.2): the active tool-type list
// with its ordered checklist items, a create/edit form and a per-row archive
// action with a confirm.
function ToolTypesTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [toolTypes, setToolTypes] = useState<ToolType[]>([])
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [qualifications, setQualifications] = useState<Qualification[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const [editingId, setEditingId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [defaultScheduleId, setDefaultScheduleId] = useState('')
  const [requiredQualificationId, setRequiredQualificationId] = useState('')
  const [inspectionMode, setInspectionMode] = useState<InspectionMode>('pass_fail')
  const [checklistItems, setChecklistItems] = useState<Array<{ label: string }>>([])

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
      if (cancelled) return

      // BEST-EFFORT dropdown population: schedules and qualifications are
      // SEPARATE catalog surfaces gated by their OWN permissions
      // (schedules.manage / qualifications.manage). A Schirrmeister/Führende
      // holder of tool_types.manage WITHOUT those codes gets a 403 here — the
      // catalog fetch degrades to an EMPTY dropdown and the type list still
      // renders; it must never eject from the module (only a 401/403 on the
      // tool-types request itself may trigger the auth handling).
      const [scheds, quals] = await Promise.all([
        listSchedules().catch(() => [] as Schedule[]),
        listQualifications().catch(() => ({ qualifications: [] as Qualification[] })),
      ])
      if (cancelled) return
      const loadedScheds = Array.isArray(scheds) ? scheds : []
      const loadedQuals = Array.isArray(quals.qualifications) ? quals.qualifications : []
      setSchedules(loadedScheds)
      setQualifications(loadedQuals)
      // Fresh create form: auto-select the first catalog entry so the save is
      // immediately valid (the form is disabled until a schedule AND a
      // qualification are chosen). Edit mode keeps the type's stored FKs.
      if (loadedScheds.length > 0) {
        setDefaultScheduleId((cur) => (cur === '' ? loadedScheds[0].id : cur))
      }
      if (loadedQuals.length > 0) {
        setRequiredQualificationId((cur) => (cur === '' ? loadedQuals[0].id : cur))
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [onApiError])

  function resetForm() {
    setEditingId(null)
    setName('')
    setDefaultScheduleId(schedules[0]?.id ?? '')
    setRequiredQualificationId(qualifications[0]?.id ?? '')
    setInspectionMode('pass_fail')
    setChecklistItems([])
  }

  function startEdit(tt: ToolType) {
    setEditingId(tt.id)
    setName(tt.name)
    setDefaultScheduleId(tt.default_schedule_id)
    setRequiredQualificationId(tt.required_qualification_id)
    setInspectionMode(tt.inspection_mode)
    setChecklistItems(
      tt.checklist_items.map((item) => ({ label: item.label })),
    )
    setFeedback(null)
  }

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
    }
    try {
      if (editingId) {
        const saved = await updateToolType(editingId, input)
        setToolTypes((prev) => prev.map((tt) => (tt.id === editingId ? saved : tt)))
        setFeedback({ kind: 'success', message: saved.message })
        resetForm()
      } else {
        const created = await createToolType(input)
        setToolTypes((prev) => [...prev, created])
        setFeedback({ kind: 'success', message: created.message })
        resetForm()
      }
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Gerätetyp konnte nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

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
      if (editingId === tt.id) resetForm()
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

  // A tool type cannot be saved without a default schedule and a required
  // qualification (both are mandatory server-side FKs). When a catalog is
  // empty (degraded fetch or genuinely unpopulated) the save is disabled with
  // an inline hint instead of submitting a 400-ing placeholder value.
  const canSave = defaultScheduleId !== '' && requiredQualificationId !== ''

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
          {toolTypes.length === 0 && (
            <p role="status" className={styles.emptyHint}>
              Keine Gerätetypen vorhanden. Lege den ersten Typ an.
            </p>
          )}

          {toolTypes.length > 0 && (
            <ul className={styles.list} aria-label="Gerätetypen">
              {toolTypes.map((tt) => (
                <li key={tt.id} className={styles.row}>
                  <div className={styles.rowInfo}>
                    <span className={styles.rowName}>{tt.name}</span>
                    <span className={styles.rowMeta}>
                      {tt.inspection_mode === 'checklist' ? 'Checkliste' : 'Pass/Fail'}
                      {tt.checklist_items.length > 0 ? ` · ${tt.checklist_items.length} Punkte` : ''}
                    </span>
                    {tt.checklist_items.length > 0 && (
                      <span className={styles.rowMeta}>
                        {tt.checklist_items.map((item) => item.label).join(' · ')}
                      </span>
                    )}
                  </div>
                  <div className={styles.rowActions}>
                    <button
                      type="button"
                      className={styles.rowButton}
                      disabled={busy}
                      onClick={() => startEdit(tt)}
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
                </li>
              ))}
            </ul>
          )}

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
                {editingId && (
                  <button type="button" className={styles.testButton} disabled={busy} onClick={resetForm}>
                    Abbrechen
                  </button>
                )}
              </div>
            </div>
            {!canSave && (
              <p role="status" className={styles.emptyHint}>
                Wähle einen Standard-Zeitplan und eine erforderliche Qualifikation aus, um zu speichern.
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
          </form>
        </>
      )}
    </>
  )
}

// ChecklistEditor is the ordered checklist-item editor (FR-23): add, remove and
// re-order (up/down) the labels of the type's inspection template. Only shown
// in checklist mode; the ordered list is submitted as-is (full replacement).
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