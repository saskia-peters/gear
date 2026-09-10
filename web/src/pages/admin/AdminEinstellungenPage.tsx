import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { getSmtpSettings, testSmtpEmail, updateSmtpSettings } from '../../auth/settings.ts'
import type { SmtpSecurity, SmtpSettings } from '../../auth/settings.ts'
import styles from './AdminEinstellungenPage.module.css'

const SECURITY_OPTIONS: ReadonlyArray<{ value: SmtpSecurity; label: string }> = [
  { value: 'none', label: 'Keine (ohne Verschlüsselung)' },
  { value: 'starttls', label: 'STARTTLS' },
  { value: 'tls', label: 'TLS (implizit)' },
]

type Feedback = { kind: 'success' | 'error'; message: string } | null

// AdminEinstellungenPage is the real Einstellungen → E-Mail surface (Story 3.1,
// FR-28/UX-DR6/UX-DR8/UX-DR9): host, port, connection security (none/STARTTLS/
// TLS), sender address & display name, username and a MASKED password, plus the
// "Sendetest-E-Mail" action. The password is write-only: GET exposes only
// password_configured, and saving with an empty password field omits it so the
// server keeps the existing encrypted one (NFR-S4). Inline German feedback
// (success or error), sticky actions, ≥48px targets, 401→login, 403→leave the
// admin module (review finding 2.1-6). The server remains the source of truth.
export function AdminEinstellungenPage() {
  const navigate = useNavigate()

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

  // handleApiError inspects an API error (findings 3 & 11): a 403 clears the
  // cached admin flag and leaves the admin module; a 401 (expired/revoked
  // session) clears the client auth state and redirects to /login. Returns
  // true when the error was handled (redirect), false otherwise.
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
        if (!handleApiError(err)) {
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
  }, [navigate, handleApiError])

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
      if (handleApiError(err)) return
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
      if (handleApiError(err)) return
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die Test-E-Mail konnte nicht gesendet werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          <h2 className={styles.title}>E-Mail-Einstellungen</h2>
          <p className={styles.description}>
            SMTP-Einstellungen für den E-Mail-Versand. Änderungen gelten sofort, ohne Neubereitstellung.
          </p>

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
              {/* Sticky action bar (Effort 2): always visible at the top. */}
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
        </main>
      </div>
    </div>
  )
}