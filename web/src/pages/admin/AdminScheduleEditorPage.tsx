import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listSchedules, createSchedule, updateSchedule, INTERVAL_OPTIONS } from '../../auth/settings.ts'
import type { ScheduleIntervalUnit } from '../../auth/settings.ts'
import styles from './AdminScheduleEditorPage.module.css'

type Feedback = { kind: 'success' | 'error'; message: string } | null

// AdminScheduleEditorPage is the DEDICATED create/edit page for a schedule
// (Spec 4-6, FR-30/AD-16): the shared schedule editor form (moved verbatim from
// the old inline ScheduleSettingsTab form) in create mode — reachable at
// /admin/einstellungen/zeitplaene/neu — and in edit mode at
// /admin/einstellungen/zeitplaene/:id, pre-filled via listSchedules().find.
// Saving runs createSchedule / updateSchedule and then navigates back to the
// Einstellungen list. 401→login, 403→leave the admin module, unknown id →
// German not-found with no form. The route is gated by schedules.manage.
export function AdminScheduleEditorPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { id } = useParams<{ id: string }>()
  const editingId = id ?? null
  const perms = getPermissions()
  // Returning to the Einstellungen list lands on the Zeitpläne tab even for a
  // holder of several tabs (Spec 4-6 review 4).
  const returnTab = (location.state as { tab?: string } | null)?.tab ?? 'schedules'

  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  // loadFailed separates a non-auth EDIT-load failure (500/network) from the
  // normal states: no form may render over it — an empty enabled form must
  // never overwrite the stored schedule with blank data (Spec 4-6 review 8).
  const [loadFailed, setLoadFailed] = useState(false)
  const [notFound, setNotFound] = useState(false)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const [name, setName] = useState('')
  const [intervalUnit, setIntervalUnit] = useState<ScheduleIntervalUnit>('year')
  const [magnitude, setMagnitude] = useState('')

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
      if (editingId) {
        // EDIT mode: load the whole active schedule catalog and find by id (the
        // shared list fetch). Unknown id → German not-found, no form.
        try {
          const scheds = await listSchedules()
          if (cancelled) return
          const s = scheds.find((x) => x.id === editingId)
          if (!s) {
            setNotFound(true)
          } else {
            setName(s.name)
            // Unknown interval unit → fall back to 'year' so the select never
            // renders blank (Spec 4-6 review 9).
            setIntervalUnit(
              INTERVAL_OPTIONS.some((o) => o.value === s.interval_unit) ? s.interval_unit : 'year',
            )
            setMagnitude(String(s.interval_magnitude))
          }
        } catch (err) {
          if (cancelled) return
          if (!handleApiError(err)) {
            setLoadError('Der Zeitplan konnte nicht geladen werden.')
            setLoadFailed(true)
          }
        } finally {
          if (!cancelled) setLoaded(true)
        }
        return
      }
      // CREATE mode: empty form (the magnitude is filled by the user).
      if (!cancelled) setLoaded(true)
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [editingId, handleApiError])

  // A schedule needs a non-empty name and a valid magnitude ≥ 1 (Spec 4-6
  // review 3): an empty name or Number('') = 0 must never reach the server.
  const magnitudeNumber = Number(magnitude)
  const canSave = name.trim() !== '' && magnitude !== '' && Number.isFinite(magnitudeNumber) && magnitudeNumber >= 1

  async function save() {
    setBusy(true)
    setFeedback(null)
    const input = {
      name: name.trim(),
      interval_unit: intervalUnit,
      interval_magnitude: magnitudeNumber,
    }
    try {
      if (editingId) {
        const saved = await updateSchedule(editingId, input)
        navigate('/admin/einstellungen', { state: { tab: returnTab, message: saved.message } })
      } else {
        const saved = await createSchedule(input)
        navigate('/admin/einstellungen', { state: { tab: returnTab, message: saved.message } })
      }
    } catch (err) {
      if (handleApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Zeitplan konnte nicht gespeichert werden.',
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
          <h2 className={styles.title}>Einstellungen</h2>
          <button type="button" className={styles.backButton} onClick={() => navigate('/admin/einstellungen', { state: { tab: returnTab } })}>
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
          {loadError && !loadFailed && (
            <p role="alert" className={styles.feedbackError}>
              {loadError}
            </p>
          )}

          {!loaded ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Zeitplan wird geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : loadFailed ? (
            // A non-auth edit-load failure renders the German error and NO form
            // (an empty enabled form must never overwrite the stored schedule).
            <p role="alert" className={styles.feedbackError}>
              {loadError}
            </p>
          ) : notFound ? (
            <p role="status" className={styles.emptyHint}>
              Zeitplan nicht gefunden.
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
                <h3 className={styles.formTitle}>{editingId ? 'Zeitplan bearbeiten' : 'Neuer Zeitplan'}</h3>
                <div className={styles.formHeaderActions}>
                  <button type="submit" className={styles.saveButton} disabled={busy || !canSave}>
                    {busy ? 'Wird gespeichert...' : editingId ? 'Änderungen speichern' : 'Speichern'}
                  </button>
                </div>
              </div>
              {!canSave && (
                <p role="status" className={styles.emptyHint}>
                  Gib einen Namen und eine gültige Intervallgröße an, um zu speichern.
                </p>
              )}

              <div className={styles.field}>
                <label className={styles.label} htmlFor="schedule-name">
                  Name
                </label>
                <input
                  id="schedule-name"
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

              <div className={styles.scheduleFieldRow}>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="schedule-unit">
                    Zeiteinheit
                  </label>
                  <select
                    id="schedule-unit"
                    className={styles.select}
                    value={intervalUnit}
                    onChange={(e) => {
                      setIntervalUnit(e.target.value as ScheduleIntervalUnit)
                      setFeedback(null)
                    }}
                  >
                    {INTERVAL_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="schedule-magnitude">
                    Intervallgröße
                  </label>
                  <input
                    id="schedule-magnitude"
                    className={styles.input}
                    type="number"
                    min={1}
                    max={1000000}
                    value={magnitude}
                    onChange={(e) => {
                      setMagnitude(e.target.value)
                      setFeedback(null)
                    }}
                  />
                </div>
              </div>
            </form>
          )}
        </main>
      </div>
    </div>
  )
}
