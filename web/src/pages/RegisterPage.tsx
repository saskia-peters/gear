import { useState, useEffect, useRef, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { focusFirstInvalid } from '../auth/focus.ts'
import styles from './RegisterPage.module.css'

interface FieldErrors {
  firstName?: string
  lastName?: string
  email?: string
  password?: string
  passwordConfirm?: string
  general?: string
}

export function RegisterPage() {
  const [firstName, setFirstName] = useState('')
  const [lastName, setLastName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [passwordConfirm, setPasswordConfirm] = useState('')

  const [errors, setErrors] = useState<FieldErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isSubmitted, setIsSubmitted] = useState(false)
  const formRef = useRef<HTMLFormElement>(null)

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Registrierung | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  const validate = (): boolean => {
    const nextErrors: FieldErrors = {}
    let isValid = true

    if (!firstName.trim()) {
      nextErrors.firstName = 'Bitte gib deinen Vornamen ein.'
      isValid = false
    }

    if (!lastName.trim()) {
      nextErrors.lastName = 'Bitte gib deinen Nachnamen ein.'
      isValid = false
    }

    const trimmedEmail = email.trim()
    if (!trimmedEmail) {
      nextErrors.email = 'Bitte gib deine E-Mail-Adresse ein.'
      isValid = false
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmedEmail)) {
      nextErrors.email = 'Bitte gib eine gültige E-Mail-Adresse ein.'
      isValid = false
    }

    if (!password) {
      nextErrors.password = 'Bitte gib ein Passwort ein.'
      isValid = false
    } else if (password.length < 10) {
      nextErrors.password = 'Das Passwort muss mindestens 10 Zeichen lang sein.'
      isValid = false
    }

    if (!passwordConfirm) {
      nextErrors.passwordConfirm = 'Bitte bestätige dein Passwort.'
      isValid = false
    } else if (password !== passwordConfirm) {
      nextErrors.passwordConfirm = 'Die Passwörter stimmen nicht überein.'
      isValid = false
    }

    setErrors(nextErrors)
    return isValid
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!validate()) {
      // UX-DR9 SCREEN_READER: move focus to the first failing field.
      focusFirstInvalid(formRef.current)
      return
    }

    setIsSubmitting(true)
    setErrors({})

    try {
      const response = await fetch('/api/v1/auth/register', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          first_name: firstName.trim(),
          last_name: lastName.trim(),
          email: email.trim(),
          password,
          password_confirm: passwordConfirm,
        }),
      })

      if (response.ok) {
        setIsSubmitted(true)
        setPassword('')
        setPasswordConfirm('')
      } else {
        const data = await response.json().catch(() => null)
        const message = data?.error?.message || 'Ein Fehler ist aufgetreten. Bitte versuche es erneut.'

        if (message.includes('mindestens 10 Zeichen')) {
          setErrors({ password: message })
        } else if (message.includes('stimmen nicht überein')) {
          setErrors({ passwordConfirm: message })
        } else if (message.includes('gültige E-Mail-Adresse')) {
          setErrors({ email: message })
        } else {
          setErrors({ general: message })
        }
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
          {isSubmitted ? (
            <div className={styles.confirmationContainer}>
              <h2 className={styles.title}>Registrierung eingegangen</h2>
              <div className={styles.confirmationNotice} role="status">
                Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe.
              </div>
              <p className={styles.confirmationText}>
                Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.
              </p>
              <div className={styles.links}>
                <Link to="/login" className={styles.primaryLink}>
                  Zur Anmeldung
                </Link>
                <Link to="/" className={styles.link}>
                  Zurück zur Übersicht
                </Link>
              </div>
            </div>
          ) : (
            <>
              <h2 className={styles.title}>Registrierung</h2>
              <p className={styles.subtitle}>
                Erstelle ein Konto für den Zugriff auf G.E.A.R.
              </p>

              {errors.general && (
                <div className={styles.generalError} role="alert">
                  {errors.general}
                </div>
              )}

              <form ref={formRef} className={styles.form} onSubmit={handleSubmit} noValidate>
                <div className={styles.fieldGroup}>
                  <label htmlFor="firstName" className={styles.label}>
                    Vorname
                  </label>
                  <input
                    id="firstName"
                    type="text"
                    className={`${styles.input} ${errors.firstName ? styles.inputError : ''}`}
                    value={firstName}
                    onChange={(e) => setFirstName(e.target.value)}
                    aria-invalid={!!errors.firstName}
                    aria-describedby={errors.firstName ? 'firstName-error' : undefined}
                    disabled={isSubmitting}
                    autoComplete="given-name"
                    required
                  />
                  {errors.firstName && (
                    <p id="firstName-error" className={styles.errorText} role="alert">
                      {errors.firstName}
                    </p>
                  )}
                </div>

                <div className={styles.fieldGroup}>
                  <label htmlFor="lastName" className={styles.label}>
                    Nachname
                  </label>
                  <input
                    id="lastName"
                    type="text"
                    className={`${styles.input} ${errors.lastName ? styles.inputError : ''}`}
                    value={lastName}
                    onChange={(e) => setLastName(e.target.value)}
                    aria-invalid={!!errors.lastName}
                    aria-describedby={errors.lastName ? 'lastName-error' : undefined}
                    disabled={isSubmitting}
                    autoComplete="family-name"
                    required
                  />
                  {errors.lastName && (
                    <p id="lastName-error" className={styles.errorText} role="alert">
                      {errors.lastName}
                    </p>
                  )}
                </div>

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

                <div className={styles.fieldGroup}>
                  <label htmlFor="password" className={styles.label}>
                    Passwort
                  </label>
                  <input
                    id="password"
                    type="password"
                    className={`${styles.input} ${errors.password ? styles.inputError : ''}`}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-invalid={!!errors.password}
                    aria-describedby={errors.password ? 'password-error' : undefined}
                    disabled={isSubmitting}
                    autoComplete="new-password"
                    required
                  />
                  {errors.password && (
                    <p id="password-error" className={styles.errorText} role="alert">
                      {errors.password}
                    </p>
                  )}
                </div>

                <div className={styles.fieldGroup}>
                  <label htmlFor="passwordConfirm" className={styles.label}>
                    Passwort bestätigen
                  </label>
                  <input
                    id="passwordConfirm"
                    type="password"
                    className={`${styles.input} ${errors.passwordConfirm ? styles.inputError : ''}`}
                    value={passwordConfirm}
                    onChange={(e) => setPasswordConfirm(e.target.value)}
                    aria-invalid={!!errors.passwordConfirm}
                    aria-describedby={errors.passwordConfirm ? 'passwordConfirm-error' : undefined}
                    disabled={isSubmitting}
                    autoComplete="new-password"
                    required
                  />
                  {errors.passwordConfirm && (
                    <p id="passwordConfirm-error" className={styles.errorText} role="alert">
                      {errors.passwordConfirm}
                    </p>
                  )}
                </div>

                <button
                  type="submit"
                  className={styles.submitButton}
                  disabled={isSubmitting}
                >
                  {isSubmitting ? 'Wird gesendet...' : 'Registrieren'}
                </button>
              </form>

              <div className={styles.links}>
                <Link to="/login" className={styles.link}>
                  Bereits registriert? Zur Anmeldung
                </Link>
                <Link to="/" className={styles.link}>
                  Zurück zur Übersicht
                </Link>
              </div>
            </>
          )}
        </div>
      </main>
    </div>
  )
}
