import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { archiveSchedule, listSchedules, scheduleDisplay } from '../../auth/settings.ts'
import type { Schedule, ScheduleIntervalUnit } from '../../auth/settings.ts'
import styles from './AdminEinstellungenPage.module.css'
import type { Feedback, SortDir, TabProps } from './settingsTabTypes.ts'
const INTERVAL_WEIGHT_DAYS: Record<ScheduleIntervalUnit, number> = {
  year: 365,
  quarter: 91,
  month: 30,
  week: 7,
  day: 1,
}

function scheduleDurationDays(s: Schedule): number {
  return (INTERVAL_WEIGHT_DAYS[s.interval_unit] ?? 0) * s.interval_magnitude
}

// 4-6): a COMPACT one-line sortable list of the active schedule-catalog rows —
// Name · Intervall ("Jährlich − 1 Jahr") · Edit — with a "Neuer Zeitplan"
// button navigating to /admin/einstellungen/zeitplaene/neu, a per-row
// "Bearbeiten" navigating to /admin/einstellungen/zeitplaene/:id, and a
// per-row archive action with a confirm. Archive is SOFT — the row leaves the
// active list and is never hard-deleted; archived schedules are not shown (the
// server filters them). The create/edit FORM lives on the dedicated editor
// page. Sorted presentation-only (Intervall/Duration asc by default —
// Story 5-2c FR-30: the Zeitpläne list opens 3 Tage < 1 Woche < 2 Wochen <
// 1 Monat < 1 Quartal < 1 Jahr, not alphabetically).
export function ScheduleSettingsTab({ onApiError }: TabProps) {
  const navigate = useNavigate()
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [sort, setSort] = useState<{ key: 'name' | 'intervall'; dir: SortDir }>({ key: 'intervall', dir: 'asc' })

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const scheds = await listSchedules()
        if (cancelled) return
        setSchedules(scheds)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Zeitpläne konnten nicht geladen werden.')
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

  function cycleSort(key: 'name' | 'intervall') {
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }))
  }

  function ariaSort(key: 'name' | 'intervall'): 'ascending' | 'descending' | 'none' {
    if (sort.key !== key) return 'none'
    return sort.dir === 'asc' ? 'ascending' : 'descending'
  }

  function sortLabel(key: 'name' | 'intervall', label: string): string {
    const state = ariaSort(key)
    const hint = state === 'ascending' ? ' (absteigend)' : state === 'descending' ? ' (aufsteigend)' : ''
    return `Sortieren nach ${label}${hint}`
  }

  function sortIndicator(key: 'name' | 'intervall'): string {
    return ariaSort(key) === 'ascending' ? '▲' : ariaSort(key) === 'descending' ? '▼' : ''
  }

  const sorted = [...schedules].sort((a, b) => {
    if (sort.key === 'intervall') {
      // Chronological duration, NOT the rendered German string (Spec 4-6
      // review 2): a "Wöchentlich − 2 Wochen" (14 days) sorts before a
      // "Monatlich − 1 Monat" (30 days) even though "W" precedes "M".
      const cmp = scheduleDurationDays(a) - scheduleDurationDays(b)
      return sort.dir === 'asc' ? cmp : -cmp
    }
    const cmp = a.name.localeCompare(b.name, 'de', { sensitivity: 'base' })
    return sort.dir === 'asc' ? cmp : -cmp
  })

  async function archive(s: Schedule) {
    const ok = window.confirm(`Zeitplan „${s.name}“ wirklich archivieren? Archivierte Zeitpläne können nicht mehr bearbeitet werden.`)
    if (!ok) return
    setBusy(true)
    setFeedback(null)
    try {
      const result = await archiveSchedule(s.id)
      setSchedules((prev) => prev.filter((x) => x.id !== s.id))
      setFeedback({ kind: 'success', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Zeitplan konnte nicht archiviert werden.',
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
        <div className={styles.skeleton} aria-busy="true" aria-label="Zeitpläne werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          <div className={styles.toolbar}>
            <button type="button" className={styles.saveButton} onClick={() => navigate('/admin/einstellungen/zeitplaene/neu')}>
              Neuer Zeitplan
            </button>
          </div>

          {schedules.length === 0 ? (
            <p role="status" className={styles.emptyHint}>
              Keine Zeitpläne vorhanden. Lege den ersten Zeitplan an.
            </p>
          ) : (
            <table className={styles.catalogTable} aria-label="Zeitpläne">
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
                  <th scope="col" aria-sort={ariaSort('intervall')}>
                    <button
                      type="button"
                      className={styles.sortButton}
                      onClick={() => cycleSort('intervall')}
                      aria-label={sortLabel('intervall', 'Intervall')}
                    >
                      Intervall
                      <span className={styles.sortIndicator} aria-hidden="true">
                        {sortIndicator('intervall')}
                      </span>
                    </button>
                  </th>
                  <th scope="col">
                    <span className={styles.visuallyHidden}>Aktionen</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((s) => (
                  <tr key={s.id} className={styles.catalogRow}>
                    <td className={styles.catalogName}>{s.name}</td>
                    <td className={styles.catalogMeta}>{scheduleDisplay(s)}</td>
                    <td>
                      <div className={styles.rowActions}>
                        <button
                          type="button"
                          className={styles.rowButton}
                          disabled={busy}
                          onClick={() => navigate(`/admin/einstellungen/zeitplaene/${s.id}`)}
                        >
                          Bearbeiten
                        </button>
                        <button
                          type="button"
                          className={styles.dangerButton}
                          disabled={busy}
                          onClick={() => void archive(s)}
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