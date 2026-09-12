import { useEffect, useState, useRef, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { focusFirstInvalid } from '../auth/focus.ts'
import styles from './ForgotPasswordPage.module.css'

interface ForgotErrors {
  email?: string
  general?: string
}

// FORGOT_MESSAGES is the allowlist of server messages the client may render
// verbatim. Both are deployment-wide (never account-specific), so showing them
// cannot leak account existence/state (UX-DR7). Any OTHER server string is
// ignored — the client falls back to the frozen anti-enumeration text, so even
// a buggy/leaky server body can never surface account details.
const FORGOT_MESSAGE_LINK = 'Wenn deine E-Mail registriert ist, erhältst du einen Link.'
const FORGOT_MESSAGE_CONTACT_ADMIN = 'Bitte kontaktiere deinen Administrator.'

const FORGOT_MESSAGES = [FORGOT_MESSAGE_LINK, FORGOT_MESSAGE_CONTACT_ADMIN]

const FORGOT_MESSAGE_FALLBACK = FORGOT_MESSAGE_LINK

export function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [errors, setErrors] = useState<ForgotErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  // submitted holds the address that was submitted (used only to keep the form
  // stable); the shown confirmation is ALWAYS a deployment-wide message.
  const [submitted, setSubmitted] = useState(false)
  const [confirmation, setConfirmation] = useState(FORGOT_MESSAGE_FALLBACK)
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
      // FR-26/UX-DR7: the server ALWAYS answers 200 with a deployment-wide
      // anti-enumeration confirmation (either the frozen "link" text when SMTP
      // is configured, or the "contact your administrator" text when SMTP is
      // not). The client shows that message ONLY when it is on the known-safe
      // allowlist; anything else (e.g. a leaky server body) falls back to the
      // frozen text so account existence/state is never revealed.
      if (res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null
        setConfirmation(
          typeof body?.message === 'string' && FORGOT_MESSAGES.includes(body.message)
            ? body.message
            : FORGOT_MESSAGE_FALLBACK,
        )
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
              <p className={styles.successText}>{confirmation}</p>
              {confirmation === FORGOT_MESSAGE_LINK && (
                <p className={styles.successHint}>
                  Falls du keinen Link erhältst, wende dich an deinen Administrator.
                </p>
              )}
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