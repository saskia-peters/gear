import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SESSION_TOKEN_KEY, clearAuthState } from '../auth/authState.ts'
import styles from './ChangePasswordPage.module.css'

interface FieldErrors {
  currentPassword?: string
  newPassword?: string
  newPasswordConfirm?: string
  general?: string
}

function authHeaders(): HeadersInit {
  const token = localStorage.getItem(SESSION_TOKEN_KEY)
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

export function ChangePasswordPage() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newPasswordConfirm, setNewPasswordConfirm] = useState('')
  const [errors, setErrors] = useState<FieldErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isChanged, setIsChanged] = useState(false)
  const [sessionsRevoked, setSessionsRevoked] = useState(true)
  const navigate = useNavigate()

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Passwort ändern | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Inline validation (FR-2 / UX-DR8): the new password must be ≥10 characters
  // and match its confirmation, exactly mirroring the server-side rules.
  // Length counts Unicode code points (review finding 1.7-5) so multi-byte
  // characters agree with the server's utf8.RuneCountInString.
  const validate = (): boolean => {
    const nextErrors: FieldErrors = {}
    let isValid = true

    if (!currentPassword) {
      nextErrors.currentPassword = 'Bitte gib dein aktuelles Passwort ein.'
      isValid = false
    }

    if (!newPassword) {
      nextErrors.newPassword = 'Bitte gib ein neues Passwort ein.'
      isValid = false
    } else if ([...newPassword].length < 10) {
      nextErrors.newPassword = 'Das Passwort muss mindestens 10 Zeichen lang sein.'
      isValid = false
    }

    if (!newPasswordConfirm) {
      nextErrors.newPasswordConfirm = 'Bitte wiederhole das neue Passwort.'
      isValid = false
    } else if (newPassword !== newPasswordConfirm) {
      nextErrors.newPasswordConfirm = 'Die Passwörter stimmen nicht überein.'
      isValid = false
    }

    setErrors(nextErrors)
    return isValid
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!validate()) {
      return
    }

    setIsSubmitting(true)
    setErrors({})

    try {
      const res = await fetch('/api/v1/auth/password/change', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({
          current_password: currentPassword,
          new_password: newPassword,
          new_password_confirm: newPasswordConfirm,
        }),
      })

      const data = await res.json().catch(() => null)

      if (res.ok) {
        setIsChanged(true)
        // The success line depends on the actual revocation outcome
        // (review finding 1.7-8): only show "→ Andere Sitzungen beendet" when
        // the server really revoked the other sessions.
        setSessionsRevoked(data?.sessions_revoked !== false)
        setCurrentPassword('')
        setNewPassword('')
        setNewPasswordConfirm('')
        return
      }

      if (res.status === 401) {
        // Session no longer valid (e.g. revoked elsewhere): clear the stale
        // client auth state before redirecting (review finding 1.7-7).
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }

      // Field attribution by the envelope code, not by German microcopy
      // (review finding 1.7-6): invalid_current_password is the current-password
      // field; anything else (invalid_request for length/mismatch/too-long) is
      // normally caught by inline validation and falls back to the general box.
      const code = data?.error?.code
      const message =
        data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.'

      if (code === 'invalid_current_password') {
        setErrors({ currentPassword: message })
      } else {
        setErrors({ general: message })
      }
    } catch {
      setErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Passwort ändern</h2>
          <p className={styles.subtitle}>
            Lege ein neues Passwort fest. Andere Sitzungen werden beendet — du bleibst angemeldet.
          </p>

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {isChanged ? (
            <div className={styles.successBox} role="status">
              <p className={styles.successTitle}>Passwort geändert.</p>
              {sessionsRevoked ? (
                <p className={styles.successNote}>→ Andere Sitzungen beendet</p>
              ) : (
                <p className={styles.successWarn}>
                  Das Passwort wurde geändert, aber andere Sitzungen konnten nicht beendet werden.
                </p>
              )}
              <Link to="/" className={styles.primaryLink}>
                Zurück zur Übersicht
              </Link>
            </div>
          ) : (
            <form className={styles.form} onSubmit={handleSubmit} noValidate>
              <div className={styles.fieldGroup}>
                <label htmlFor="currentPassword" className={styles.label}>
                  Aktuelles Passwort
                </label>
                <input
                  id="currentPassword"
                  type="password"
                  className={`${styles.input} ${errors.currentPassword ? styles.inputError : ''}`}
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  aria-invalid={!!errors.currentPassword}
                  aria-describedby={errors.currentPassword ? 'currentPassword-error' : undefined}
                  disabled={isSubmitting}
                  autoComplete="current-password"
                  required
                />
                {errors.currentPassword && (
                  <p id="currentPassword-error" className={styles.errorText} role="alert">
                    {errors.currentPassword}
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
                  className={`${styles.input} ${errors.newPassword ? styles.inputError : ''}`}
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  aria-invalid={!!errors.newPassword}
                  aria-describedby={errors.newPassword ? 'newPassword-error' : undefined}
                  disabled={isSubmitting}
                  autoComplete="new-password"
                  required
                />
                {errors.newPassword && (
                  <p id="newPassword-error" className={styles.errorText} role="alert">
                    {errors.newPassword}
                  </p>
                )}
              </div>

              <div className={styles.fieldGroup}>
                <label htmlFor="newPasswordConfirm" className={styles.label}>
                  Wiederholung
                </label>
                <input
                  id="newPasswordConfirm"
                  type="password"
                  className={`${styles.input} ${errors.newPasswordConfirm ? styles.inputError : ''}`}
                  value={newPasswordConfirm}
                  onChange={(e) => setNewPasswordConfirm(e.target.value)}
                  aria-invalid={!!errors.newPasswordConfirm}
                  aria-describedby={errors.newPasswordConfirm ? 'newPasswordConfirm-error' : undefined}
                  disabled={isSubmitting}
                  autoComplete="new-password"
                  required
                />
                {errors.newPasswordConfirm && (
                  <p id="newPasswordConfirm-error" className={styles.errorText} role="alert">
                    {errors.newPasswordConfirm}
                  </p>
                )}
              </div>

              <button type="submit" className={styles.submitButton} disabled={isSubmitting}>
                {isSubmitting ? 'Wird gesendet...' : 'Passwort ändern'}
              </button>
            </form>
          )}

          <div className={styles.links}>
            <Link to="/" className={styles.link}>
              Zurück zur Übersicht
            </Link>
          </div>
        </div>
      </main>
    </div>
  )
}
