import { useEffect, useState, useRef, type FormEvent } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { focusFirstInvalid } from '../auth/focus.ts'
import styles from './ResetPasswordPage.module.css'

interface FieldErrors {
  newPassword?: string
  newPasswordConfirm?: string
  general?: string
}

export function ResetPasswordPage() {
  const { token } = useParams<{ token: string }>()
  const location = useLocation()
  const [newPassword, setNewPassword] = useState('')
  const [newPasswordConfirm, setNewPasswordConfirm] = useState('')
  const [errors, setErrors] = useState<FieldErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isDone, setIsDone] = useState(false)
  // invalidToken is set when the server rejects the token (expired/used/unknown).
  const [invalidToken, setInvalidToken] = useState(false)
  const formRef = useRef<HTMLFormElement>(null)
  // notice is a German note carried over from the login forced-change flow
  // (review finding 1.8-4): it tells the user their password must be changed
  // and that the admins have been notified / a one-time password was provided.
  const notice = (location.state as { notice?: string } | null)?.notice

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Neues Passwort | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Inline validation (FR-2 / UX-DR8): ≥10 characters and a matching
  // confirmation, mirroring the server-side rules. Length counts Unicode code
  // points so multi-byte characters agree with the server (review finding
  // 1.7-5).
  const validate = (): boolean => {
    const nextErrors: FieldErrors = {}
    let isValid = true

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
    if (!token || invalidToken || isDone) {
      return
    }
    if (!validate()) {
      // UX-DR9 SCREEN_READER: move focus to the first failing field.
      focusFirstInvalid(formRef.current)
      return
    }

    setIsSubmitting(true)
    setErrors({})

    try {
      const res = await fetch('/api/v1/auth/password/reset', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          token,
          new_password: newPassword,
          new_password_confirm: newPasswordConfirm,
        }),
      })

      const data = await res.json().catch(() => null)

      if (res.ok) {
        setIsDone(true)
        return
      }

      const code = data?.error?.code
      const message =
        data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.'

      if (code === 'invalid_token') {
        // RESET_EXPIRED / RESET_USED: the link is dead — require a new one.
        setInvalidToken(true)
        setErrors({ general: message })
        return
      }

      // invalid_request (short / mismatched) normally caught by inline
      // validation; fall back to the general box.
      setErrors({ general: message })
    } catch {
      setErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  if (invalidToken) {
    return (
      <div className={styles.page}>
        <Header />
        <main className={styles.main}>
          <div className={styles.card}>
            <h2 className={styles.title}>Link ungültig</h2>
            <div className={styles.generalError} role="alert">
              Dieser Link ist ungültig oder abgelaufen. Bitte fordere einen neuen Link an.
            </div>
            <div className={styles.links}>
              <Link to="/forgot-password" className={styles.link}>
                Neuen Link anfordern
              </Link>
              <Link to="/login" className={styles.link}>
                Zurück zur Anmeldung
              </Link>
            </div>
          </div>
        </main>
      </div>
    )
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Neues Passwort festlegen</h2>
          <p className={styles.subtitle}>
            Lege ein neues Passwort fest (mindestens 10 Zeichen). Deine anderen Sitzungen werden
            beendet.
          </p>

          {notice && (
            <div className={styles.notice} role="status">
              {notice}
            </div>
          )}

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {isDone ? (
            <div className={styles.successBox} role="status">
              <p className={styles.successText}>Passwort geändert. Du kannst dich jetzt anmelden.</p>
              <Link to="/login" className={styles.submitButton}>
                Zur Anmeldung
              </Link>
            </div>
          ) : (
            <form ref={formRef} className={styles.form} onSubmit={handleSubmit} noValidate>
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
            <Link to="/login" className={styles.link}>
              Zurück zur Anmeldung
            </Link>
          </div>
        </div>
      </main>
    </div>
  )
}