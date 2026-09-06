import { useEffect, useState, useRef, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { focusFirstInvalid } from '../auth/focus.ts'
import styles from './ForgotPasswordPage.module.css'

interface ForgotErrors {
  email?: string
  general?: string
}

export function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [errors, setErrors] = useState<ForgotErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  // submitted holds the address that was submitted (used only to keep the form
  // stable); the shown confirmation is ALWAYS the uniform text.
  const [submitted, setSubmitted] = useState(false)
  const formRef = useRef<HTMLFormElement>(null)

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Passwort vergessen | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmedEmail = email.trim()
    const nextErrors: ForgotErrors = {}
    if (!trimmedEmail) {
      nextErrors.email = 'Bitte gib deine E-Mail-Adresse ein.'
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmedEmail)) {
      nextErrors.email = 'Bitte gib eine gültige E-Mail-Adresse ein.'
    }
    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors)
      // UX-DR9 SCREEN_READER: move focus to the first failing field.
      focusFirstInvalid(formRef.current)
      return
    }

    setIsSubmitting(true)
    setErrors({})

    try {
      const res = await fetch('/api/v1/auth/password/forgot', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: trimmedEmail }),
      })
      // FR-26/UX-DR7: the server ALWAYS answers 200 with the uniform
      // anti-enumeration confirmation — the account's existence/state is never
      // revealed. The client shows the identical text regardless of the
      // response, so even a failed request cannot leak.
      if (res.ok) {
        setSubmitted(true)
      } else {
        setErrors({ general: 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.' })
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
          <h2 className={styles.title}>Passwort vergessen</h2>
          <p className={styles.subtitle}>
            Gib deine E-Mail-Adresse ein, um einen Link zum Zurücksetzen deines Passworts zu erhalten.
          </p>

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {submitted ? (
            <div className={styles.successBox} role="status">
              <p className={styles.successText}>Wenn deine E-Mail registriert ist, erhältst du einen Link.</p>
              <p className={styles.successHint}>
                Falls du keinen Link erhältst, wende dich an deinen Administrator.
              </p>
            </div>
          ) : (
            <form ref={formRef} className={styles.form} onSubmit={handleSubmit} noValidate>
              <div className={styles.fieldGroup}>
                <label htmlFor="email" className={styles.label}>
                  E-Mail-Adresse
                </label>
                <input
                  id="email"
                  type="email"
                  className={`${styles.input} ${errors.email ? styles.inputError : ''}`}
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  aria-invalid={!!errors.email}
                  aria-describedby={errors.email ? 'email-error' : undefined}
                  disabled={isSubmitting}
                  autoComplete="email"
                  required
                />
                {errors.email && (
                  <p id="email-error" className={styles.errorText} role="alert">
                    {errors.email}
                  </p>
                )}
              </div>

              <button type="submit" className={styles.submitButton} disabled={isSubmitting}>
                {isSubmitting ? 'Wird gesendet...' : 'Link anfordern'}
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