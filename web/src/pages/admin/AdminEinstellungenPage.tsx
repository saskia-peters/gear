import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import {
  BACKUP_SETTINGS_PERMISSION,
  SMTP_SETTINGS_PERMISSION,
  SCHEDULES_PERMISSION,
  SYSTEM_SETTINGS_PERMISSION,
  getSmtpSettings,
  testSmtpEmail,
  updateSmtpSettings,
  listBackupDestinations,
  createBackupDestination,
  updateBackupDestination,
  deleteBackupDestination,
  testBackupDestination,
  listSchedules,
  archiveSchedule,
  getSystemSettings,
  updateSystemSetting,
  scheduleDisplay,
} from '../../auth/settings.ts'
import type {
  SmtpSecurity,
  SmtpSettings,
  BackupMechanism,
  BackupDestination,
  BackupDestinationInput,
  Schedule,
  ScheduleIntervalUnit,
  SystemSetting,
} from '../../auth/settings.ts'
import { InfoPopup } from '../../components/InfoPopup.tsx'
import { PromptDialog } from '../../components/PromptDialog.tsx'
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

// Interval units of the schedule catalog (FR-30/AD-16) — the shared
// INTERVAL_OPTIONS + scheduleDisplay vocabulary lives in the settings data
// module so the Zeitpläne list rows and the dedicated schedule editor page
// (Spec 4-6) cannot drift.

type Feedback = { kind: 'success' | 'error'; message: string } | null
type Tab = 'email' | 'backup' | 'schedules' | 'system'
type SortDir = 'asc' | 'desc'

// Schedule sorting is CHRONOLOGICAL (Spec 4-6 review 2): a week is shorter
// than a month, so the rendered German string ("Wöchentlich − 2 Wochen" vs
// "Monatlich − 1 Monat") must never drive the order. Each unit maps to its
// representative duration in days; the sort compares (weight × magnitude).
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

function mechanismLabel(m: BackupMechanism): string {
  return MECHANISM_OPTIONS.find((o) => o.value === m)?.label ?? m
}

// AdminEinstellungenPage is the Einstellungen surface (Story 3.1 + 3.2 + 4.1 +
// 5-2b, FR-28/FR-29/FR-30/UX-DR6/UX-DR8/UX-DR9): a tab bar over the E-Mail,
// Backup, Zeitpläne and System settings surfaces. Each tab is gated by its OWN
// permission code (AD-6) — E-Mail by admin.settings.email, Backup by
// admin.settings.backup, Zeitpläne by schedules.manage, System by
// admin.settings.system — so a holder of only one code sees only that surface.
// Credentials are write-only: GET exposes only *configured booleans and saving
// with an empty credential omits it so the server keeps the existing encrypted
// one (NFR-S4). Inline German feedback, sticky actions, ≥48px targets,
// 401→login, 403→leave the admin module. The server remains the source of
// truth.
export function AdminEinstellungenPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const perms = getPermissions()
  const canEmail = perms.includes(SMTP_SETTINGS_PERMISSION)
  const canBackup = perms.includes(BACKUP_SETTINGS_PERMISSION)
  const canSchedules = perms.includes(SCHEDULES_PERMISSION)
  const canSystem = perms.includes(SYSTEM_SETTINGS_PERMISSION)
  // The schedule editor returns via router state (Spec 4-6 review 1/4): the
  // carried tab puts a multi-tab holder back on Zeitpläne, and the carried
  // message is shown once as a success notice. The state is cleared after the
  // first render so it never reappears on a later remount.
  const carried = location.state as { tab?: Tab | ''; message?: string } | null
  const [activeTab, setActiveTab] = useState<Tab>(() => {
    const fromTab = carried?.tab
    if (fromTab === 'email' && canEmail) return 'email'
    if (fromTab === 'backup' && canBackup) return 'backup'
    if (fromTab === 'schedules' && canSchedules) return 'schedules'
    if (fromTab === 'system' && canSystem) return 'system'
    return canEmail ? 'email' : canBackup ? 'backup' : canSchedules ? 'schedules' : 'system'
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
            E-Mail-Versand, Backup-Ziele, Zeitpläne und System-Einstellungen. Änderungen gelten sofort, ohne Neubereitstellung.
          </p>

          {notice && (
            <p role="status" className={styles.feedbackSuccess}>
              {notice}
            </p>
          )}

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
            {canSchedules && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'schedules'}
                className={activeTab === 'schedules' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('schedules')}
              >
                Zeitpläne
              </button>
            )}
            {canSystem && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'system'}
                className={activeTab === 'system' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('system')}
              >
                System
              </button>
            )}
          </div>

          {activeTab === 'email' && canEmail ? (
            <EmailSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'backup' && canBackup ? (
            <BackupSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'schedules' && canSchedules ? (
            <ScheduleSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'system' && canSystem ? (
            <SystemSettingsTab onApiError={handleApiError} />
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
  const [testDialogOpen, setTestDialogOpen] = useState(false)

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

  async function sendTestEmail(recipient: string) {
    setBusy(true)
    setFeedback(null)
    try {
      const result = await testSmtpEmail(recipient)
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
              onClick={() => setTestDialogOpen(true)}
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

      {testDialogOpen && (
        <PromptDialog
          title="Test-E-Mail senden"
          label="Empfängeradresse"
          placeholder="z. B. max@beispiel.de"
          submitLabel="Senden"
          cancelLabel="Abbrechen"
          emptyMessage="Bitte gib eine Empfängeradresse ein."
          validate={(value) => (value.includes('@') ? null : 'Bitte gib eine gültige Empfängeradresse an.')}
          onSubmit={(recipient) => {
            setTestDialogOpen(false)
            void sendTestEmail(recipient)
          }}
          onClose={() => setTestDialogOpen(false)}
        />
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

// ScheduleSettingsTab is the Zeitpläne surface (Story 4.1, FR-30/AD-16, Spec
// 4-6): a COMPACT one-line sortable list of the active schedule-catalog rows —
// Name · Intervall ("Jährlich − 1 Jahr") · Edit — with a "Neuer Zeitplan"
// button navigating to /admin/einstellungen/zeitplaene/neu, a per-row
// "Bearbeiten" navigating to /admin/einstellungen/zeitplaene/:id, and a
// per-row archive action with a confirm. Archive is SOFT — the row leaves the
// active list and is never hard-deleted; archived schedules are not shown (the
// server filters them). The create/edit FORM lives on the dedicated editor
// page. Sorted presentation-only (Name asc by default, localeCompare 'de').
function ScheduleSettingsTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const navigate = useNavigate()
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [sort, setSort] = useState<{ key: 'name' | 'intervall'; dir: SortDir }>({ key: 'name', dir: 'asc' })

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

// ---------------------------------------------------------------------------
// System tab (Story 5-2b): the configurable system settings table. Each of the
// 21 atomic settings renders as one row: German name, formatted current value,
// a value-typed editable input (duration → whole seconds, integer → number,
// text → string) and a "?" InfoPopup explaining the setting. Saving is per-row
// with inline German feedback — the server is authoritative, so its 400s (and
// its German message) surface inline. Durations are STORED and EDITED in whole
// seconds; the current-value column shows a friendly German label derived from
// the seconds value.
// ---------------------------------------------------------------------------

interface SystemSettingMeta {
  label: string
  help: string
}

// SYSTEM_SETTING_META is the SPA-side catalog of the 21 seeded settings: German
// label + read-only help text for the "?" popup (UX-DR8). Keys mirror the
// server's seeded keys; the server remains authoritative for the value and the
// value type.
const SYSTEM_SETTING_META: Record<string, SystemSettingMeta> = {
  smtp_dial_timeout: {
    label: 'SMTP-Verbindungsaufbau-Timeout',
    help: 'Zeit in Sekunden, bis ein SMTP-Server den Verbindungsaufbau beantwortet haben muss. Erhöhe den Wert, wenn ein langsames Relay fälschlich als Fehler erscheint.',
  },
  smtp_protocol_timeout: {
    label: 'SMTP-Protokoll-Timeout',
    help: 'Maximale Dauer der gesamten SMTP-Unterhaltung in Sekunden. Das ist der größte Hebel gegen hängende E-Mail-Versuche.',
  },
  backup_dial_timeout: {
    label: 'Backup-Verbindungsaufbau-Timeout',
    help: 'Zeit in Sekunden für den TCP-Verbindungsaufbau zu einem Backup-Ziel. Entfernte Endpunkte variieren stark.',
  },
  backup_protocol_timeout: {
    label: 'Backup-Protokoll-Timeout',
    help: 'Zeit in Sekunden für die FTP-/SFTP-/S3-Unterhaltung mit einem Backup-Ziel während des Verbindungstests.',
  },
  password_reset_ttl: {
    label: 'Gültigkeit Passwort-Reset-Link',
    help: 'Gültigkeitsdauer des Links zum Zurücksetzen des Passworts in Sekunden (Standard 30 Minuten). Auch der Zeitraum, in dem die E-Mail noch als „30 Minuten gültig“ angezeigt wird.',
  },
  admin_recovery_ttl: {
    label: 'Gültigkeit Dual-Admin-Wiederherstellung',
    help: 'Gültigkeitsdauer eines Wiederherstellungstokens für die Kontowiederherstellung in Sekunden (Standard 30 Minuten).',
  },
  forgot_throttle_interval: {
    label: 'Sperrintervall „Passwort vergessen“',
    help: 'Mindestabstand in Sekunden zwischen zwei „Passwort vergessen“-Anfragen für dieselbe E-Mail-Adresse (Standard 60).',
  },
  otp_ttl: {
    label: 'Gültigkeit Einmalpasswort (OTP)',
    help: 'Gültigkeitsdauer eines Einmalpassworts in Sekunden (Standard 15 Minuten). Bestimmt das operative Zeitfenster für die Eingabe.',
  },
  otp_length: {
    label: 'OTP-Länge',
    help: 'Anzahl der Zeichen eines Einmalpassworts (Standard 10). Längere Codes erhöhen die Entropie, erschweren aber die Eingabe.',
  },
  mfa_enrollment_window: {
    label: 'Gültigkeit MFA-Anmeldung',
    help: 'Zeitfenster in Sekunden, in dem ein frisch angelegter MFA-Schlüssel bestätigt werden muss (Standard 10 Minuten).',
  },
  lockout_threshold_short: {
    label: 'Sperrschwelle (kurz)',
    help: 'Anzahl der Fehlversuche, ab denen die kurze Sperre greift (Standard 3).',
  },
  lockout_threshold_long: {
    label: 'Sperrschwelle (lang)',
    help: 'Anzahl der Fehlversuche, ab denen die lange Sperre greift (Standard 4).',
  },
  lockout_duration_short: {
    label: 'Sperrdauer (kurz)',
    help: 'Dauer der kurzen Anmeldesperre in Sekunden (Standard 30).',
  },
  lockout_duration_long: {
    label: 'Sperrdauer (lang)',
    help: 'Dauer der langen Anmeldesperre in Sekunden (Standard 60).',
  },
  lockout_max_failed_count: {
    label: 'Max. Fehlversuche gesamt',
    help: 'Obergrenze der erfassten Fehlversuche pro Konto (Standard 10). Schützt vor übermäßigem Sperr-Datenwachstum.',
  },
  attribute_key_max_runes: {
    label: 'Max. Länge Attributschlüssel',
    help: 'Maximale Zeichenzahl eines benutzerdefinierten Attributschlüssels (Standard 64). Gilt einheitlich für Benutzer- und Geräteattribute.',
  },
  attributes_max_size: {
    label: 'Max. Größe Attribute',
    help: 'Maximale Gesamtgröße der Attributsammlung eines Datensatzes in Bytes (Standard 16 KB).',
  },
  inventory_prefix: {
    label: 'Präfix Inventarnummer',
    help: 'Buchstaben-Präfix der automatisch vergebenen Inventarnummern (Standard „GEAR“).',
  },
  inventory_width: {
    label: 'Breite Inventarnummer',
    help: 'Breite des numerischen Teils der Inventarnummer (Standard 6). Zusammen mit dem Präfix ergibt sich z. B. „GEAR000001“.',
  },
  inspection_orange_window_days: {
    label: 'Orange-Fenster Prüfung',
    help: 'Tage vor der Fälligkeit, ab denen ein Gerät auf dem Dashboard orange dargestellt wird (Standard 14).',
  },
  qualification_expiring_soon_window: {
    label: 'Qualifikation „bald ablaufend“',
    help: 'Zeitfenster in Sekunden vor dem Ablauf, ab dem eine Qualifikation als „bald ablaufend“ gilt (Standard 30 Tage).',
  },
}

// formatDuration renders a whole-second duration as a friendly German label
// (e.g. "30 Minuten", "15 Minuten", "30 Tage", "10 Sekunden").
function formatDuration(totalSeconds: number): string {
  const minute = 60
  const hour = 60 * minute
  const day = 24 * hour
  if (totalSeconds % day === 0 && totalSeconds >= day) {
    const d = totalSeconds / day
    return `${d} ${d === 1 ? 'Tag' : 'Tage'}`
  }
  if (totalSeconds % hour === 0 && totalSeconds >= hour) {
    const h = totalSeconds / hour
    return `${h} ${h === 1 ? 'Stunde' : 'Stunden'}`
  }
  if (totalSeconds % minute === 0 && totalSeconds >= minute) {
    const m = totalSeconds / minute
    return `${m} ${m === 1 ? 'Minute' : 'Minuten'}`
  }
  return `${totalSeconds} ${totalSeconds === 1 ? 'Sekunde' : 'Sekunden'}`
}

// formatCurrentValue renders the current-value cell per value_type: durations
// as a friendly German label, integers/text with the setting's unit appended
// (e.g. "10 Zeichen", "14 Tage", "16384 Bytes") so days and seconds never look
// alike. unit is the server-authoritative display unit.
function formatCurrentValue(setting: SystemSetting): string {
  if (setting.value_type === 'duration') {
    return formatDuration(Number(setting.value))
  }
  const unit = setting.unit ?? ''
  return `${String(setting.value)}${unit !== '' ? ` ${unit}` : ''}`
}

// SystemSettingsTab is the System surface (Story 5-2b): a table of the 21
// seeded settings — German name, formatted current value, a value-typed input
// (duration in whole seconds, integer, text) and a "?" InfoPopup per row.
// Saving is per-row with inline German feedback; the server is authoritative
// (its 400s surface inline). Skeleton loading, 403 → leave the admin module,
// 401 → login.
function SystemSettingsTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [settings, setSettings] = useState<SystemSetting[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [feedback, setFeedback] = useState<Record<string, Feedback>>({})
  const [busyKeys, setBusyKeys] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const rows = await getSystemSettings()
        if (cancelled) return
        setSettings(rows)
        const next: Record<string, string> = {}
        for (const row of rows) {
          next[row.key] = String(row.value)
        }
        setDrafts(next)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die System-Einstellungen konnten nicht geladen werden.')
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

  function setRowFeedback(key: string, fb: Feedback) {
    setFeedback((prev) => {
      const next = { ...prev }
      if (fb) {
        next[key] = fb
      } else {
        delete next[key]
      }
      return next
    })
  }

  async function saveRow(setting: SystemSetting) {
    const raw = drafts[setting.key] ?? String(setting.value)
    // An emptied numeric field must NOT become Number('') = 0 and silently
    // persist 0 — block the empty raw input client-side with inline feedback
    // (the server is never reached).
    if (setting.value_type !== 'text' && raw.trim() === '') {
      setRowFeedback(setting.key, { kind: 'error', message: 'Bitte gib einen Wert für diese Einstellung ein.' })
      return
    }
    const numeric = Number(raw)
    // A non-finite number (e.g. 1e309) would serialize as JSON null and hit
    // the server as a type mismatch — reject it inline instead.
    if (setting.value_type !== 'text' && !Number.isFinite(numeric)) {
      setRowFeedback(setting.key, { kind: 'error', message: 'Ungültiger Wert.' })
      return
    }
    const value: number | string = setting.value_type === 'text' ? raw : numeric
    setBusyKeys((prev) => new Set(prev).add(setting.key))
    setRowFeedback(setting.key, null)
    try {
      const saved = await updateSystemSetting(setting.key, value)
      setSettings((prev) => prev.map((s) => (s.key === setting.key ? saved : s)))
      setDrafts((prev) => ({ ...prev, [setting.key]: String(saved.value) }))
      setRowFeedback(setting.key, { kind: 'success', message: saved.message })
    } catch (err) {
      if (onApiError(err)) return
      setRowFeedback(setting.key, {
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die System-Einstellung konnte nicht gespeichert werden.',
      })
    } finally {
      setBusyKeys((prev) => {
        const next = new Set(prev)
        next.delete(setting.key)
        return next
      })
    }
  }

  return (
    <>
      {loadError && (
        <p role="alert" className={styles.feedbackError}>
          {loadError}
        </p>
      )}

      {!loaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="System-Einstellungen werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : settings.length === 0 ? (
        <p role="status" className={styles.warning}>
          Keine System-Einstellungen vorhanden.
        </p>
      ) : (
        <table className={styles.systemTable} aria-label="System-Einstellungen">
          <thead>
            <tr>
              <th scope="col" className={styles.systemTh}>
                Einstellung
              </th>
              <th scope="col" className={styles.systemTh}>
                Aktueller Wert
              </th>
              <th scope="col" className={styles.systemTh}>
                Neuer Wert
              </th>
              <th scope="col" className={styles.systemTh}>
                <span className={styles.visuallyHidden}>Info</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {settings.map((setting) => {
              const meta = SYSTEM_SETTING_META[setting.key] ?? { label: setting.key, help: '' }
              const busy = busyKeys.has(setting.key)
              const rowFeedback = feedback[setting.key]
              return (
                <tr key={setting.key} className={styles.systemRow}>
                  <td className={styles.systemCell}>
                    <span className={styles.systemName}>{meta.label}</span>
                    <span className={styles.systemKey}>{setting.key}</span>
                  </td>
                  <td className={styles.systemCell}>
                    <span className={styles.systemValue}>{formatCurrentValue(setting)}</span>
                  </td>
                  <td className={styles.systemCell}>
                    <div className={styles.systemEdit}>
                      <label className={styles.visuallyHidden} htmlFor={`setting-${setting.key}`}>
                        {meta.label} bearbeiten
                      </label>
                      {setting.value_type === 'text' ? (
                        <input
                          id={`setting-${setting.key}`}
                          className={styles.input}
                          value={drafts[setting.key] ?? String(setting.value)}
                          disabled={busy}
                          onChange={(e) => {
                            setDrafts((prev) => ({ ...prev, [setting.key]: e.target.value }))
                            setRowFeedback(setting.key, null)
                          }}
                          maxLength={64}
                          autoComplete="off"
                        />
                      ) : (
                        <input
                          id={`setting-${setting.key}`}
                          className={styles.input}
                          type="number"
                          min={0}
                          step={1}
                          value={drafts[setting.key] ?? String(setting.value)}
                          disabled={busy}
                          onChange={(e) => {
                            setDrafts((prev) => ({ ...prev, [setting.key]: e.target.value }))
                            setRowFeedback(setting.key, null)
                          }}
                        />
                      )}
                      {setting.value_type === 'duration' && (
                        <span className={styles.hint}>in Sekunden</span>
                      )}
                      {rowFeedback && (
                        <p
                          role={rowFeedback.kind === 'error' ? 'alert' : 'status'}
                          className={rowFeedback.kind === 'error' ? styles.systemFeedbackError : styles.systemFeedbackSuccess}
                        >
                          {rowFeedback.message}
                        </p>
                      )}
                    </div>
                  </td>
                  <td className={styles.systemCell}>
                    <div className={styles.systemActions}>
                      <InfoPopup title={meta.label} description={meta.help} />
                      <button
                        type="button"
                        className={styles.rowButton}
                        disabled={busy}
                        onClick={() => void saveRow(setting)}
                      >
                        {busy ? 'Speichert...' : 'Speichern'}
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </>
  )
}
