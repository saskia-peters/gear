import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { adminForbiddenHandled, authHeaders } from '../auth/authState.ts'
import styles from './AdminRecoveryPage.module.css'

interface RequestErrors {
  email?: string
  general?: string
}

interface ApproveErrors {
  email?: string
  reason?: string
  confirmed?: string
  general?: string
}

interface CompleteErrors {
  token?: string
  newPassword?: string
  confirm?: string
  general?: string
}

interface PendingRequest {
  id: string
  user_id: string
  user?: { id: string; email: string; display_name?: string } | null
  created_at?: string
}

// AdminRecoveryPage is the dual-admin credential-recovery surface (FR-27). It
// has three sections:
//
//   - For the locked-out admin (A): enter your email to REQUEST recovery. This
//     creates a recovery request that ONLY the other admin (B) can approve; the
//     raw token is never shown here.
//   - For the approving admin (B, `admin.recovery.approve`): review a request by
//     entering the target admin's email, a mandatory Begründung (reason) and a
//     confirmation checkbox, then APPROVE. The single-use token is returned and
//     shown in a read-only, copyable input so B can hand it to A out-of-band
//     (e.g. via a secure channel) — the raw token is NEVER rendered as a
//     clickable URL (review finding 1.10), so it never leaks via browser
//     history or the Referer header.
//   - For the recovered admin (A): paste the delivered token and set a new
//     password; the completion form submits the token via the POST body to
//     /password/reset (never in a URL).
//
// Pending requests are also listed for the approving admin via the
// permission-gated GET /admin/recovery/pending endpoint (review finding 1.10).
export function AdminRecoveryPage() {
  const navigate = useNavigate()
  const [reqEmail, setReqEmail] = useState('')
  const [reqErrors, setReqErrors] = useState<RequestErrors>({})
  const [reqSubmitting, setReqSubmitting] = useState(false)
  const [requested, setRequested] = useState(false)

  const [apprEmail, setApprEmail] = useState('')
  const [reason, setReason] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [apprErrors, setApprErrors] = useState<ApproveErrors>({})
  const [apprSubmitting, setApprSubmitting] = useState(false)
  const [recoveryToken, setRecoveryToken] = useState('')

  const [pending, setPending] = useState<PendingRequest[]>([])
  const [pendingError, setPendingError] = useState('')
  const [pendingLoaded, setPendingLoaded] = useState(false)

  const [completeToken, setCompleteToken] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newPasswordConfirm, setNewPasswordConfirm] = useState('')
  const [completeErrors, setCompleteErrors] = useState<CompleteErrors>({})
  const [completeSubmitting, setCompleteSubmitting] = useState(false)
  const [completed, setCompleted] = useState(false)

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Admin-Wiederherstellung | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Load the pending recovery requests for the approving admin (FR-27, review
  // finding 1.10).
  useEffect(() => {
    let cancelled = false
    async function loadPending() {
      try {
        const res = await fetch('/api/v1/admin/recovery/pending', {
          headers: authHeaders(),
        })
        if (cancelled) return
        // Revocation downgrade (review finding 2.1-6): a 403 means the admin
        // role is gone — drop the cached admin flag and leave the admin module.
        if (adminForbiddenHandled(res)) {
          navigate('/')
          return
        }
        if (res.ok) {
          const data = await res.json().catch(() => null)
          setPending(data?.requests ?? [])
          setPendingLoaded(true)
        } else {
          setPendingError('Anfragen konnten nicht geladen werden.')
        }
      } catch {
        if (!cancelled) setPendingError('Anfragen konnten nicht geladen werden.')
      }
    }
    void loadPending()
    return () => {
      cancelled = true
    }
  }, [navigate])

  const handleRequest = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = reqEmail.trim()
    const next: RequestErrors = {}
    if (!trimmed) {
      next.email = 'Bitte gib deine E-Mail-Adresse ein.'
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) {
      next.email = 'Bitte gib eine gültige E-Mail-Adresse ein.'
    }
    if (Object.keys(next).length > 0) {
      setReqErrors(next)
      return
    }
    setReqSubmitting(true)
    setReqErrors({})
    try {
      const res = await fetch('/api/v1/admin/recovery/request', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({ email: trimmed }),
      })
      const data = await res.json().catch(() => null)
      if (adminForbiddenHandled(res)) {
        navigate('/')
        return
      }
      if (res.ok) {
        setRequested(true)
      } else if (res.status === 401) {
        setReqErrors({ general: 'Deine Sitzung ist abgelaufen. Bitte melde dich erneut an.' })
      } else {
        setReqErrors({
          general: data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.',
        })
      }
    } catch {
      setReqErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setReqSubmitting(false)
    }
  }

  const handleApprove = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = apprEmail.trim()
    const next: ApproveErrors = {}
    if (!trimmed) {
      next.email = 'Bitte gib die E-Mail-Adresse des betroffenen Administrators ein.'
    }
    if (!reason.trim()) {
      next.reason = 'Bitte gib eine Begründung für die Freigabe an.'
    }
    if (!confirmed) {
      next.confirmed = 'Bitte bestätige die Freigabe mit der Checkbox.'
    }
    if (Object.keys(next).length > 0) {
      setApprErrors(next)
      return
    }
    setApprSubmitting(true)
    setApprErrors({})
    try {
      const res = await fetch('/api/v1/admin/recovery/approve', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({ email: trimmed, reason: reason.trim(), confirmed }),
      })
      const data = await res.json().catch(() => null)
      if (adminForbiddenHandled(res)) {
        navigate('/')
        return
      }
      if (res.ok) {
        setRecoveryToken(data?.recovery_token || '')
      } else if (res.status === 401) {
        setApprErrors({ general: 'Deine Sitzung ist abgelaufen. Bitte melde dich erneut an.' })
      } else {
        setApprErrors({
          general: data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.',
        })
      }
    } catch {
      setApprErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setApprSubmitting(false)
    }
  }

  const handleComplete = async (e: FormEvent) => {
    e.preventDefault()
    const next: CompleteErrors = {}
    if (!completeToken.trim()) {
      next.token = 'Bitte füge den Einmal-Token ein.'
    }
    if (!newPassword) {
      next.newPassword = 'Bitte gib ein neues Passwort ein.'
    } else if (newPassword.length < 10) {
      next.newPassword = 'Das Passwort muss mindestens 10 Zeichen lang sein.'
    } else if (newPassword !== newPasswordConfirm) {
      next.confirm = 'Die Passwörter stimmen nicht überein.'
    }
    if (Object.keys(next).length > 0) {
      setCompleteErrors(next)
      return
    }
    setCompleteSubmitting(true)
    setCompleteErrors({})
    try {
      const res = await fetch('/api/v1/auth/password/reset', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({
          token: completeToken.trim(),
          new_password: newPassword,
          new_password_confirm: newPasswordConfirm,
        }),
      })
      const data = await res.json().catch(() => null)
      if (res.ok) {
        setCompleted(true)
      } else if (res.status === 400) {
        setCompleteErrors({
          general: data?.error?.message || 'Der Token ist ungültig oder abgelaufen.',
        })
      } else {
        setCompleteErrors({
          general: data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.',
        })
      }
    } catch {
      setCompleteErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setCompleteSubmitting(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Admin-Wiederherstellung</h2>
          <p className={styles.subtitle}>
            Dual-Admin-Konto-Wiederherstellung: Der andere Administrator muss jede Freigabe
            bestätigen.
          </p>

          <section className={styles.section}>
            <h3 className={styles.sectionTitle}>Wiederherstellung anfordern</h3>
            <p className={styles.sectionHint}>
              Für den ausgesperrten Administrator (A): Gib deine E-Mail-Adresse ein, um eine
              Wiederherstellung anzufordern.
            </p>
            {reqErrors.general && (
              <div className={styles.generalError} role="alert">
                {reqErrors.general}
              </div>
            )}
            {requested ? (
              <div className={styles.successBox} role="status">
                <p className={styles.successText}>
                  Deine Wiederherstellungsanfrage wurde erstellt. Der andere Administrator muss
                  sie freigeben.
                </p>
                <p className={styles.successHint}>
                  Du erhältst erst Zugang, wenn der andere Administrator die Anfrage mit
                  Begründung freigegeben hat.
                </p>
              </div>
            ) : (
              <form className={styles.form} onSubmit={handleRequest} noValidate>
                <div className={styles.fieldGroup}>
                  <label htmlFor="reqEmail" className={styles.label}>
                    E-Mail-Adresse
                  </label>
                  <input
                    id="reqEmail"
                    type="email"
                    className={`${styles.input} ${reqErrors.email ? styles.inputError : ''}`}
                    value={reqEmail}
                    onChange={(e) => setReqEmail(e.target.value)}
                    aria-invalid={!!reqErrors.email}
                    aria-describedby={reqErrors.email ? 'reqEmail-error' : undefined}
                    disabled={reqSubmitting}
                    autoComplete="email"
                    required
                  />
                  {reqErrors.email && (
                    <p id="reqEmail-error" className={styles.errorText} role="alert">
                      {reqErrors.email}
                    </p>
                  )}
                </div>
                <button type="submit" className={styles.submitButton} disabled={reqSubmitting}>
                  {reqSubmitting ? 'Wird gesendet...' : 'Wiederherstellung anfordern'}
                </button>
              </form>
            )}
          </section>

          <section className={styles.section}>
            <h3 className={styles.sectionTitle}>Anfrage freigeben</h3>
            <p className={styles.sectionHint}>
              Für den freigebenden Administrator (B): Prüfe die Anfrage und gib sie mit
              Begründung und Bestätigung frei.
            </p>
            {apprErrors.general && (
              <div className={styles.generalError} role="alert">
                {apprErrors.general}
              </div>
            )}
            {recoveryToken ? (
              <div className={styles.successBox} role="status">
                <p className={styles.successText}>Freigabe erteilt.</p>
                <p className={styles.successHint}>
                  Übermittle den folgenden Einmal-Token sicher (out-of-band) an den
                  Administrator:
                </p>
                <div className={styles.tokenInputGroup}>
                  <input
                    className={styles.tokenInput}
                    data-testid="recovery-token"
                    readOnly
                    value={recoveryToken}
                    onFocus={(e) => e.currentTarget.select()}
                    aria-label="Einmal-Token"
                  />
                </div>
                <p className={styles.successHint}>
                  Der Administrator nutzt den Token unter „Neues Passwort setzen“, ohne ihn als
                  Link zu öffnen. Der Token wird nie in eine URL eingebettet.
                </p>
              </div>
            ) : (
              <form className={styles.form} onSubmit={handleApprove} noValidate>
                <div className={styles.fieldGroup}>
                  <label htmlFor="apprEmail" className={styles.label}>
                    E-Mail-Adresse des betroffenen Administrators
                  </label>
                  <input
                    id="apprEmail"
                    type="email"
                    className={`${styles.input} ${apprErrors.email ? styles.inputError : ''}`}
                    value={apprEmail}
                    onChange={(e) => setApprEmail(e.target.value)}
                    aria-invalid={!!apprErrors.email}
                    aria-describedby={apprErrors.email ? 'apprEmail-error' : undefined}
                    disabled={apprSubmitting}
                    autoComplete="off"
                    required
                  />
                  {apprErrors.email && (
                    <p id="apprEmail-error" className={styles.errorText} role="alert">
                      {apprErrors.email}
                    </p>
                  )}
                </div>

                <div className={styles.fieldGroup}>
                  <label htmlFor="reason" className={styles.label}>
                    Begründung
                  </label>
                  <textarea
                    id="reason"
                    className={`${styles.textarea} ${apprErrors.reason ? styles.inputError : ''}`}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    aria-invalid={!!apprErrors.reason}
                    aria-describedby={apprErrors.reason ? 'reason-error' : undefined}
                    disabled={apprSubmitting}
                    rows={3}
                    required
                  />
                  {apprErrors.reason && (
                    <p id="reason-error" className={styles.errorText} role="alert">
                      {apprErrors.reason}
                    </p>
                  )}
                </div>

                <div className={styles.fieldGroup}>
                  <label className={styles.checkboxLabel}>
                    <input
                      id="confirmed"
                      type="checkbox"
                      checked={confirmed}
                      onChange={(e) => setConfirmed(e.target.checked)}
                      disabled={apprSubmitting}
                      aria-invalid={!!apprErrors.confirmed}
                      aria-describedby={apprErrors.confirmed ? 'confirmed-error' : undefined}
                    />
                    <span>
                      Ich bestätige, dass ich die Anfrage geprüft habe und der betroffene
                      Administrator der Freigabe zustimmt.
                    </span>
                  </label>
                  {apprErrors.confirmed && (
                    <p id="confirmed-error" className={styles.errorText} role="alert">
                      {apprErrors.confirmed}
                    </p>
                  )}
                </div>

                <button type="submit" className={styles.approveButton} disabled={apprSubmitting}>
                  {apprSubmitting ? 'Wird gesendet...' : 'Freigeben'}
                </button>
              </form>
            )}
          </section>

          <section className={styles.section}>
            <h3 className={styles.sectionTitle}>Offene Anfragen</h3>
            <p className={styles.sectionHint}>
              Ausstehende Wiederherstellungsanfragen, die auf Freigabe warten.
            </p>
            {pendingError ? (
              <p className={styles.generalError} role="alert">
                {pendingError}
              </p>
            ) : pendingLoaded && pending.length === 0 ? (
              <p className={styles.sectionHint}>Derzeit keine offenen Anfragen.</p>
            ) : (
              <ul className={styles.pendingList}>
                {pending.map((req) => (
                  <li key={req.id} className={styles.pendingItem}>
                    {req.user?.display_name || req.user?.email || req.user_id}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className={styles.section}>
            <h3 className={styles.sectionTitle}>Neues Passwort setzen</h3>
            <p className={styles.sectionHint}>
              Für den wiederhergestellten Administrator (A): Füge den erhaltenen Einmal-Token
              ein und setze ein neues Passwort (mindestens 10 Zeichen).
            </p>
            {completeErrors.general && (
              <div className={styles.generalError} role="alert">
                {completeErrors.general}
              </div>
            )}
            {completed ? (
              <div className={styles.successBox} role="status">
                <p className={styles.successText}>Passwort gesetzt.</p>
                <p className={styles.successHint}>
                  Der Administrator kann sich jetzt mit dem neuen Passwort anmelden.
                </p>
              </div>
            ) : (
              <form className={styles.form} onSubmit={handleComplete} noValidate>
                <div className={styles.fieldGroup}>
                  <label htmlFor="completeToken" className={styles.label}>
                    Einmal-Token
                  </label>
                  <input
                    id="completeToken"
                    type="text"
                    className={`${styles.input} ${completeErrors.token ? styles.inputError : ''}`}
                    value={completeToken}
                    onChange={(e) => setCompleteToken(e.target.value)}
                    aria-invalid={!!completeErrors.token}
                    aria-describedby={completeErrors.token ? 'completeToken-error' : undefined}
                    disabled={completeSubmitting}
                    autoComplete="off"
                    required
                  />
                  {completeErrors.token && (
                    <p id="completeToken-error" className={styles.errorText} role="alert">
                      {completeErrors.token}
                    </p>
                  )}
                </div>
                <div className={styles.fieldGroup}>
                  <label htmlFor="newPassword" className={styles.label}>
                    Neues Passwort
                  </label>
                  <input
                    id="newPassword"
                    type="password"
                    className={`${styles.input} ${completeErrors.newPassword ? styles.inputError : ''}`}
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    aria-invalid={!!completeErrors.newPassword}
                    aria-describedby={completeErrors.newPassword ? 'newPassword-error' : undefined}
                    disabled={completeSubmitting}
                    autoComplete="new-password"
                    required
                  />
                  {completeErrors.newPassword && (
                    <p id="newPassword-error" className={styles.errorText} role="alert">
                      {completeErrors.newPassword}
                    </p>
                  )}
                </div>
                <div className={styles.fieldGroup}>
                  <label htmlFor="newPasswordConfirm" className={styles.label}>
                    Passwort bestätigen
                  </label>
                  <input
                    id="newPasswordConfirm"
                    type="password"
                    className={`${styles.input} ${completeErrors.confirm ? styles.inputError : ''}`}
                    value={newPasswordConfirm}
                    onChange={(e) => setNewPasswordConfirm(e.target.value)}
                    aria-invalid={!!completeErrors.confirm}
                    aria-describedby={completeErrors.confirm ? 'newPasswordConfirm-error' : undefined}
                    disabled={completeSubmitting}
                    autoComplete="new-password"
                    required
                  />
                  {completeErrors.confirm && (
                    <p id="newPasswordConfirm-error" className={styles.errorText} role="alert">
                      {completeErrors.confirm}
                    </p>
                  )}
                </div>
                <button type="submit" className={styles.approveButton} disabled={completeSubmitting}>
                  {completeSubmitting ? 'Wird gesendet...' : 'Passwort setzen'}
                </button>
              </form>
            )}
          </section>

          <div className={styles.links}>
            <Link to="/admin" className={styles.link}>
              Zurück zum Admin-Modul
            </Link>
          </div>
        </div>
      </main>
    </div>
  )
}
