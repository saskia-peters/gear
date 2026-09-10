import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import {
  BACKUP_SETTINGS_PERMISSION,
  SMTP_SETTINGS_PERMISSION,
  getSmtpSettings,
  testSmtpEmail,
  updateSmtpSettings,
  listBackupDestinations,
  createBackupDestination,
  updateBackupDestination,
  deleteBackupDestination,
  testBackupDestination,
} from '../../auth/settings.ts'
import type {
  SmtpSecurity,
  SmtpSettings,
  BackupMechanism,
  BackupDestination,
  BackupDestinationInput,
} from '../../auth/settings.ts'
import styles from './AdminEinstellungenPage.module.css'

const SECURITY_OPTIONS: ReadonlyArray<{ value: SmtpSecurity; label: string }> = [
  { value: 'none', label: 'Keine (ohne Verschlüsselung)' },
  { value: 'starttls', label: 'STARTTLS' },
  { value: 'tls', label: 'TLS (implizit)' },
]

const MECHANISM_OPTIONS: ReadonlyArray<{ value: BackupMechanism; label: string }> = [
  { value: 'local', label: 'Lokal' },
  { value: 's3', label: 'S3-kompatibel' },
  { value: 'ftp', label: 'FTP' },
  { value: 'sftp', label: 'SFTP' },
]

type Feedback = { kind: 'success' | 'error'; message: string } | null
type Tab = 'email' | 'backup'

function mechanismLabel(m: BackupMechanism): string {
  return MECHANISM_OPTIONS.find((o) => o.value === m)?.label ?? m
}

// AdminEinstellungenPage is the Einstellungen surface (Story 3.1 + 3.2,
// FR-28/FR-29/UX-DR6/UX-DR8/UX-DR9): a tab bar over the E-Mail and Backup
// settings surfaces. Each tab is gated by its OWN permission code (AD-6) —
// E-Mail by admin.settings.email, Backup by admin.settings.backup — so a
// holder of only one code sees only that surface. Credentials are write-only:
// GET exposes only *configured booleans and saving with an empty credential
// omits it so the server keeps the existing encrypted one (NFR-S4). Inline
// German feedback, sticky actions, ≥48px targets, 401→login, 403→leave the
// admin module. The server remains the source of truth.
export function AdminEinstellungenPage() {
  const navigate = useNavigate()
  const perms = getPermissions()
  const canEmail = perms.includes(SMTP_SETTINGS_PERMISSION)
  const canBackup = perms.includes(BACKUP_SETTINGS_PERMISSION)
  const [activeTab, setActiveTab] = useState<Tab>(canEmail ? 'email' : 'backup')

  // handleApiError inspects an API error: a 403 clears the cached admin flag
  // and leaves the admin module; a 401 (expired/revoked session) clears the
  // client auth state and redirects to /login. Returns true when the error was
  // handled (redirect), false otherwise.
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
          <h2 className={styles.title}>Einstellungen</h2>
          <p className={styles.description}>
            E-Mail-Versand und Backup-Ziele. Änderungen gelten sofort, ohne Neubereitstellung.
          </p>

          <div className={styles.tabs} role="tablist" aria-label="Einstellungen">
            {canEmail && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'email'}
                className={activeTab === 'email' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('email')}
              >
                E-Mail
              </button>
            )}
            {canBackup && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'backup'}
                className={activeTab === 'backup' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('backup')}
              >
                Backup
              </button>
            )}
          </div>

          {activeTab === 'email' && canEmail ? (
            <EmailSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'backup' && canBackup ? (
            <BackupSettingsTab onApiError={handleApiError} />
          ) : null}
        </main>
      </div>
    </div>
  )
}

// EmailSettingsTab is the E-Mail surface (Story 3.1, FR-28): host, port,
// connection security, sender address & display name, username and a MASKED
// password, plus the "Sendetest-E-Mail" action.
function EmailSettingsTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [security, setSecurity] = useState<SmtpSecurity>('none')
  const [senderAddress, setSenderAddress] = useState('')
  const [senderName, setSenderName] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [passwordConfigured, setPasswordConfigured] = useState(false)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  // apply populates the form from the server settings, never the password
  // (write-only, NFR-S4).
  function apply(settings: SmtpSettings) {
    setHost(settings.host)
    setPort(String(settings.port))
    setSecurity(settings.security)
    setSenderAddress(settings.sender_address)
    setSenderName(settings.sender_name)
    setUsername(settings.username)
    setPasswordConfigured(settings.password_configured)
    setPassword('')
  }

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const settings = await getSmtpSettings()
        if (cancelled) return
        apply(settings)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die SMTP-Einstellungen konnten nicht geladen werden.')
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

  async function save() {
    setBusy(true)
    setFeedback(null)
    try {
      const input = {
        host: host.trim(),
        port: Number(port),
        security,
        sender_address: senderAddress.trim(),
        sender_name: senderName.trim(),
        username: username.trim(),
        ...(password !== '' ? { password } : {}),
      }
      const saved = await updateSmtpSettings(input)
      apply(saved)
      setFeedback({ kind: 'success', message: saved.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die SMTP-Einstellungen konnten nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  async function sendTestEmail() {
    setBusy(true)
    setFeedback(null)
    try {
      const result = await testSmtpEmail()
      setFeedback({ kind: result.ok ? 'success' : 'error', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die Test-E-Mail konnte nicht gesendet werden.',
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
        <div className={styles.skeleton} aria-busy="true" aria-label="SMTP-Einstellungen werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <form
          className={styles.editor}
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          {/* Sticky action bar: always visible at the top. */}
          <div className={styles.stickyActions}>
            <button type="submit" className={styles.saveButton} disabled={busy}>
              {busy ? 'Wird gespeichert...' : 'Speichern'}
            </button>
            <button
              type="button"
              className={styles.testButton}
              disabled={busy}
              onClick={() => void sendTestEmail()}
            >
              Sendetest-E-Mail
            </button>
          </div>

          <div className={styles.field}>
            <label className={styles.label} htmlFor="smtp-host">
              SMTP-Host
            </label>
            <input
              id="smtp-host"
              className={styles.input}
              value={host}
              onChange={(e) => {
                setHost(e.target.value)
                setFeedback(null)
              }}
              maxLength={255}
              autoComplete="off"
            />
          </div>

          <div className={styles.fieldRow}>
            <div className={styles.field}>
              <label className={styles.label} htmlFor="smtp-port">
                Port
              </label>
              <input
                id="smtp-port"
                className={styles.input}
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => {
                  setPort(e.target.value)
                  setFeedback(null)
                }}
              />
            </div>
            <div className={styles.field}>
              <label className={styles.label} htmlFor="smtp-security">
                Verschlüsselung
              </label>
              <select
                id="smtp-security"
                className={styles.select}
                value={security}
                onChange={(e) => {
                  setSecurity(e.target.value as SmtpSecurity)
                  setFeedback(null)
                }}
              >
                {SECURITY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className={styles.field}>
            <label className={styles.label} htmlFor="smtp-sender-address">
              Absenderadresse
            </label>
            <input
              id="smtp-sender-address"
              className={styles.input}
              type="email"
              value={senderAddress}
              onChange={(e) => {
                setSenderAddress(e.target.value)
                setFeedback(null)
              }}
              maxLength={254}
            />
          </div>

          <div className={styles.field}>
            <label className={styles.label} htmlFor="smtp-sender-name">
              Absendername (optional)
            </label>
            <input
              id="smtp-sender-name"
              className={styles.input}
              value={senderName}
              onChange={(e) => {
                setSenderName(e.target.value)
                setFeedback(null)
              }}
              maxLength={255}
            />
          </div>

          <div className={styles.field}>
            <label className={styles.label} htmlFor="smtp-username">
              Benutzername (optional)
            </label>
            <input
              id="smtp-username"
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
            <label className={styles.label} htmlFor="smtp-password">
              Passwort
              <span className={styles.hint}>
                {passwordConfigured
                  ? 'Ein Passwort ist gespeichert. Nur ausfüllen, um es zu ändern.'
                  : 'Nur ausfüllen, wenn der SMTP-Server eine Anmeldung erfordert.'}
              </span>
            </label>
            <input
              id="smtp-password"
              className={styles.input}
              type="password"
              value={password}
              placeholder={passwordConfigured ? '••••••••' : ''}
              onChange={(e) => {
                setPassword(e.target.value)
                setFeedback(null)
              }}
              maxLength={1024}
              autoComplete="new-password"
            />
          </div>
        </form>
      )}
    </>
  )
}

// BackupSettingsTab is the Backup surface (Story 3.2, FR-29/AD-15): the
// destination list with a create/edit form (mechanism, endpoint, bucket/path,
// username, masked credential, optional schedule), "Verbindung testen" per
// row, delete, inline German feedback, and the ≥1-destination warning
// (NFR-R3). Credentials are write-only (masked, never returned).
function BackupSettingsTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
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