import { useEffect, useState } from 'react'
import {
  createBackupDestination,
  deleteBackupDestination,
  listBackupDestinations,
  testBackupDestination,
  updateBackupDestination,
} from '../../auth/settings.ts'
import type {
  BackupDestination,
  BackupDestinationInput,
  BackupMechanism,
} from '../../auth/settings.ts'
import styles from './AdminEinstellungenPage.module.css'
import type { Feedback, TabProps } from './settingsTabTypes.ts'
const MECHANISM_OPTIONS: ReadonlyArray<{ value: BackupMechanism; label: string }> = [
  { value: 'local', label: 'Lokal' },
  { value: 's3', label: 'S3-kompatibel' },
  { value: 'ftp', label: 'FTP' },
  { value: 'sftp', label: 'SFTP' },
]

// Interval units of the schedule catalog (FR-30/AD-16) — the shared
// INTERVAL_OPTIONS + scheduleDisplay vocabulary lives in the settings data
// module so the Zeitpläne list rows and the dedicated schedule editor page
// (Spec 4-6) cannot drift.

function mechanismLabel(m: BackupMechanism): string {
  return MECHANISM_OPTIONS.find((o) => o.value === m)?.label ?? m
}

// BackupSettingsTab is the Backup surface (Story 3.2, FR-29/AD-15): the
// destination list with a create/edit form (mechanism, endpoint, bucket/path,
// username, masked credential, optional schedule), "Verbindung testen" per
// row, delete, inline German feedback, and the ≥1-destination warning
// (NFR-R3). Credentials are write-only (masked, never returned).
export function BackupSettingsTab({ onApiError }: TabProps) {
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [destinations, setDestinations] = useState<BackupDestination[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const [editingId, setEditingId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [mechanism, setMechanism] = useState<BackupMechanism>('local')
  const [endpoint, setEndpoint] = useState('')
  const [bucketOrPath, setBucketOrPath] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [credentialConfigured, setCredentialConfigured] = useState(false)
  const [removeCredential, setRemoveCredential] = useState(false)
  const [schedule, setSchedule] = useState('')

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const dests = await listBackupDestinations()
        if (cancelled) return
        setDestinations(dests)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Backup-Ziele konnten nicht geladen werden.')
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

  function resetForm() {
    setEditingId(null)
    setName('')
    setMechanism('local')
    setEndpoint('')
    setBucketOrPath('')
    setUsername('')
    setPassword('')
    setCredentialConfigured(false)
    setRemoveCredential(false)
    setSchedule('')
  }

  function startEdit(d: BackupDestination) {
    setEditingId(d.id)
    setName(d.name)
    setMechanism(d.mechanism)
    setEndpoint(d.endpoint)
    setBucketOrPath(d.bucket_or_path)
    setUsername(d.username)
    setPassword('')
    setCredentialConfigured(d.credential_configured)
    setRemoveCredential(false)
    setSchedule(d.schedule ?? '')
    setFeedback(null)
  }

  async function save() {
    setBusy(true)
    setFeedback(null)
    const input: BackupDestinationInput = {
      name: name.trim(),
      mechanism,
      endpoint: endpoint.trim(),
      bucket_or_path: bucketOrPath.trim(),
      username: username.trim(),
      schedule: schedule.trim(),
      // Revoking the stored credential and providing a new one are mutually
      // exclusive: when "Anmeldedaten entfernen" is checked the password field
      // is disabled and only clear_credential travels (finding).
      ...(removeCredential
        ? { clear_credential: true }
        : password !== ''
          ? { password }
          : {}),
    }
    try {
      if (editingId) {
        const saved = await updateBackupDestination(editingId, input)
        setDestinations((prev) => prev.map((d) => (d.id === editingId ? saved : d)))
        setFeedback({ kind: 'success', message: saved.message })
        resetForm()
      } else {
        const created = await createBackupDestination(input)
        setDestinations((prev) => [...prev, created])
        setFeedback({ kind: 'success', message: created.message })
        resetForm()
      }
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Das Backup-Ziel konnte nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  async function remove(d: BackupDestination) {
    setBusy(true)
    setFeedback(null)
    try {
      const res = await deleteBackupDestination(d.id)
      setDestinations((prev) => prev.filter((x) => x.id !== d.id))
      setFeedback({ kind: 'success', message: res.message })
      if (editingId === d.id) resetForm()
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Das Backup-Ziel konnte nicht gelöscht werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  async function runTest(d: BackupDestination) {
    setBusy(true)
    setFeedback(null)
    try {
      const result = await testBackupDestination(d.id)
      setFeedback({ kind: result.ok ? 'success' : 'error', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die Verbindung konnte nicht getestet werden.',
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
        <div className={styles.skeleton} aria-busy="true" aria-label="Backup-Ziele werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          {destinations.length === 0 && (
            <p role="status" className={styles.warning}>
              Mindestens ein Backup-Ziel ist erforderlich.
            </p>
          )}

          {destinations.length > 0 && (
            <ul className={styles.list} aria-label="Backup-Ziele">
              {destinations.map((d) => (
                <li key={d.id} className={styles.row}>
                  <div className={styles.rowInfo}>
                    <span className={styles.rowName}>{d.name}</span>
                    <span className={styles.rowMeta}>
                      {mechanismLabel(d.mechanism)} · {d.endpoint || 'lokal'} · {d.bucket_or_path}
                      {d.username ? ` · ${d.username}` : ''}
                      {d.schedule ? ` · ${d.schedule}` : ''}
                    </span>
                    <span className={styles.rowMeta}>
                      {d.credential_configured ? 'Zugangsberechtigung konfiguriert' : 'Ohne Zugangsberechtigung'}
                    </span>
                  </div>
                  <div className={styles.rowActions}>
                    <button
                      type="button"
                      className={styles.rowButton}
                      disabled={busy}
                      onClick={() => startEdit(d)}
                    >
                      Bearbeiten
                    </button>
                    <button
                      type="button"
                      className={styles.testButton}
                      disabled={busy}
                      onClick={() => void runTest(d)}
                    >
                      Verbindung testen
                    </button>
                    <button
                      type="button"
                      className={styles.dangerButton}
                      disabled={busy}
                      onClick={() => void remove(d)}
                    >
                      Löschen
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
            <h3 className={styles.formTitle}>{editingId ? 'Backup-Ziel bearbeiten' : 'Neues Backup-Ziel'}</h3>

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-name">
                Name
              </label>
              <input
                id="backup-name"
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

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-mechanism">
                Mechanismus
              </label>
              <select
                id="backup-mechanism"
                className={styles.select}
                value={mechanism}
                onChange={(e) => {
                  setMechanism(e.target.value as BackupMechanism)
                  setFeedback(null)
                }}
              >
                {MECHANISM_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-endpoint">
                Endpunkt / Host
                <span className={styles.hint}>
                  {mechanism === 's3'
                    ? 'S3-kompatible Endpunkt-URL (z. B. https://s3.example.com).'
                    : mechanism === 'local'
                      ? 'Für lokale Ziele nicht erforderlich.'
                      : 'Hostname des FTP-/SFTP-Servers.'}
                </span>
              </label>
              <input
                id="backup-endpoint"
                className={styles.input}
                value={endpoint}
                onChange={(e) => {
                  setEndpoint(e.target.value)
                  setFeedback(null)
                }}
                maxLength={2048}
                autoComplete="off"
              />
            </div>

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-bucket-path">
                {mechanism === 's3' ? 'Bucket' : 'Pfad'}
              </label>
              <input
                id="backup-bucket-path"
                className={styles.input}
                value={bucketOrPath}
                onChange={(e) => {
                  setBucketOrPath(e.target.value)
                  setFeedback(null)
                }}
                maxLength={2048}
                autoComplete="off"
              />
            </div>

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-username">
                Benutzername
                <span className={styles.hint}>
                  {mechanism === 'local' ? 'Optional bei lokalen Zielen.' : 'Zugangsschlüssel (S3) bzw. Benutzer (FTP/SFTP).'}
                </span>
              </label>
              <input
                id="backup-username"
                className={styles.input}
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value)
                  setFeedback(null)
                }}
                maxLength={255}
                autoComplete="off"
              />
            </div>

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-password">
                Zugangsberechtigung
                <span className={styles.hint}>
                  {credentialConfigured
                    ? 'Eine Zugangsberechtigung ist gespeichert. Nur ausfüllen, um sie zu ändern.'
                    : mechanism === 'local'
                      ? 'Optional bei lokalen Zielen.'
                      : 'Für S3, FTP und SFTP erforderlich.'}
                </span>
              </label>
              <input
                id="backup-password"
                className={styles.input}
                type="password"
                value={password}
                placeholder={credentialConfigured ? '••••••••' : ''}
                disabled={removeCredential}
                onChange={(e) => {
                  setPassword(e.target.value)
                  if (removeCredential) setRemoveCredential(false)
                  setFeedback(null)
                }}
                maxLength={1024}
                autoComplete="new-password"
              />
            </div>

            {editingId && credentialConfigured && (
              <label className={styles.removeCredential}>
                <input
                  type="checkbox"
                  checked={removeCredential}
                  onChange={(e) => {
                    setRemoveCredential(e.target.checked)
                    if (e.target.checked) setPassword('')
                    setFeedback(null)
                  }}
                />
                <span>
                  Anmeldedaten entfernen
                  <span className={styles.hint}>
                    Die gespeicherte Zugangsberechtigung wird gelöscht (nicht mehr benötigt?).
                  </span>
                </span>
              </label>
            )}

            <div className={styles.field}>
              <label className={styles.label} htmlFor="backup-schedule">
                Zeitplan (optional)
              </label>
              <input
                id="backup-schedule"
                className={styles.input}
                value={schedule}
                onChange={(e) => {
                  setSchedule(e.target.value)
                  setFeedback(null)
                }}
                maxLength={255}
                autoComplete="off"
              />
            </div>

            <div className={styles.stickyActions}>
              <button type="submit" className={styles.saveButton} disabled={busy}>
                {busy ? 'Wird gespeichert...' : editingId ? 'Änderungen speichern' : 'Speichern'}
              </button>
              {editingId && (
                <button type="button" className={styles.testButton} disabled={busy} onClick={resetForm}>
                  Abbrechen
                </button>
              )}
            </div>
          </form>
        </>
      )}
    </>
  )
}
