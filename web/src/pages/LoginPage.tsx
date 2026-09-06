import { useState, useEffect, useRef, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { saveAuthState } from '../auth/authState.ts'
import styles from './LoginPage.module.css'

const LOCKOUT_STORAGE_KEY = 'gear.login_lockout_until'

interface LoginErrors {
  email?: string
  password?: string
  totpCode?: string
  general?: string
}

interface LockoutState {
  endsAt: number
}

// readPersistedLockout rehydrates an active lockout from localStorage (an
// absolute expiry timestamp) so a page reload keeps the retry button disabled
// until the window truly expires. The server enforces 429 regardless.
function readPersistedLockout(): { lockout: LockoutState | null; countdown: number } {
  const raw = localStorage.getItem(LOCKOUT_STORAGE_KEY)
  if (!raw) {
    return { lockout: null, countdown: 0 }
  }
  const endsAt = Number(raw)
  if (!Number.isFinite(endsAt) || endsAt <= Date.now()) {
    return { lockout: null, countdown: 0 }
  }
  return {
    lockout: { endsAt },
    countdown: Math.ceil((endsAt - Date.now()) / 1000),
  }
}

// TotpStep describes the two-step MFA phase (FR-4): after a valid password the
// server signals mfa_required and the client submits a 6-digit code next.
type TotpStep = 'none' | 'pending' | 'verifying'

export function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpStep, setTotpStep] = useState<TotpStep>('none')
  const [errors, setErrors] = useState<LoginErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [lockout, setLockout] = useState<LockoutState | null>(() => readPersistedLockout().lockout)
  const [countdown, setCountdown] = useState(() => readPersistedLockout().countdown)
  const countdownRef = useRef(countdown)
  const navigate = useNavigate()

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Anmeldung | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Progressive lockout countdown (FR-3 / UX-DR6 / UX-DR8): while a lockout is
  // active the form stays visible but the retry button stays disabled until the
  // timer expires. State updates happen in the deferred interval callback —
  // never inside a state updater — so the countdown and lockout clear cleanly.
  useEffect(() => {
    if (lockout === null) {
      return
    }
    const interval = setInterval(() => {
      const next = countdownRef.current - 1
      if (next <= 0) {
        clearInterval(interval)
        localStorage.removeItem(LOCKOUT_STORAGE_KEY)
        countdownRef.current = 0
        setCountdown(0)
        setLockout(null)
        return
      }
      countdownRef.current = next
      setCountdown(next)
    }, 1000)
    return () => clearInterval(interval)
  }, [lockout])

  const validate = (): boolean => {
    const nextErrors: LoginErrors = {}
    let isValid = true

    if (totpStep !== 'none') {
      const trimmedCode = totpCode.trim()
      if (!/^\d{6}$/.test(trimmedCode)) {
        nextErrors.totpCode = 'Bitte gib den 6-stelligen Code aus deiner Authenticator-App ein.'
        isValid = false
      }
      setErrors(nextErrors)
      return isValid
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
      nextErrors.password = 'Bitte gib dein Passwort ein.'
      isValid = false
    }

    setErrors(nextErrors)
    return isValid
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (lockout !== null) {
      // A lockout countdown is still active (e.g. an implicit Enter submit):
      // return early so the window is neither cleared nor restarted.
      return
    }
    if (!validate()) {
      return
    }

    setIsSubmitting(true)
    setErrors({})

    try {
      const payload =
        totpStep !== 'none'
          ? {
              email: email.trim(),
              password,
              totp_code: totpCode.trim(),
            }
          : {
              email: email.trim(),
              password,
            }

      const response = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      const data = await response.json().catch(() => null)

      if (response.ok && data?.mfa_required) {
        // Two-step login (FR-4): valid password, MFA enabled — prompt for the
        // 6-digit TOTP code. No session token is issued yet.
        setTotpStep('pending')
        return
      }

      if (response.ok && data?.must_change_password) {
        // LOGIN_MUST_CHANGE (FR-26): credentials are valid but the account is
        // flagged for a mandatory password change (SMTP-not-configured
        // fallback / Epic 2 one-time password). NO app session was issued; the
        // server returned a single-use reset token that drives the forced
        // change flow. Completing it clears the flag and requires a re-login.
        // The server's German note (review finding 1.8-4) is carried to the
        // reset page via navigation state.
        if (data?.reset_token) {
          navigate(`/reset-password/${data.reset_token}`, {
            state: { notice: data?.message },
          })
        } else {
          setErrors({ general: 'Bitte wende dich an deinen Administrator.' })
        }
        return
      }

      if (response.ok && data?.token) {
        saveAuthState(
          data.token,
          Boolean(data.user?.is_mfa_enabled),
          { displayName: data.user?.display_name },
          Boolean(data.user?.is_admin),
        )
        navigate('/')
        return
      }

      if (response.status === 429) {
        // Progressive lockout (FR-3): show the countdown and disable the retry
        // button until the server-provided Retry-After window expires
        // (UX-DR6/UX-DR8). Default to 30s if the header is missing. The expiry
        // is persisted so a reload keeps the lockout until it truly expires.
        const raw = response.headers.get('Retry-After')
        const parsed = raw ? Number(raw) : 30
        const seconds = Number.isFinite(parsed) && parsed > 0 ? Math.ceil(parsed) : 30
        const endsAt = Date.now() + seconds * 1000
        localStorage.setItem(LOCKOUT_STORAGE_KEY, String(endsAt))
        countdownRef.current = seconds
        setCountdown(seconds)
        setLockout({ endsAt })
        return
      }

      if (response.status === 401) {
        // Anti-enumeration: identical microcopy for any invalid credentials
        // (UX-DR7) — the server never distinguishes wrong password, unknown
        // email, a non-active account or an invalid TOTP code.
        setErrors({
          general: 'E-Mail oder Passwort ist falsch.',
        })
        return
      }

      // 5xx and other unexpected failures are a server problem, not bad input.
      setErrors({
        general: 'Ein interner Fehler ist aufgetreten. Bitte versuche es erneut.',
      })
    } catch {
      setErrors({
        general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  // Back to the credentials step (FR-4): re-enter the password before trying
  // another TOTP code.
  const backToCredentials = () => {
    setTotpStep('none')
    setTotpCode('')
    setErrors({})
  }

  const mfaActive = totpStep !== 'none'

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Anmeldung</h2>
          <p className={styles.subtitle}>
            G.E.A.R. (Geräte-Einsatz-Assistenz &amp; Readiness) — melde dich an, um auf die Geräteverwaltung zuzugreifen.
          </p>

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {lockout && (
            <div className={styles.lockout} role="alert" aria-live="assertive">
              Zu viele Fehlversuche. Bitte warte {countdown} Sekunden.
            </div>
          )}

          {mfaActive && (
            <div className={styles.mfaActive} role="status" aria-live="polite">
              <span className={styles.mfaActiveDot} aria-hidden="true" />
              MFA aktiv — Zwei-Faktor-Authentifizierung erforderlich
            </div>
          )}

          <form className={styles.form} onSubmit={handleSubmit} noValidate>
            {mfaActive ? (
              <div className={styles.fieldGroup}>
                <label htmlFor="totpCode" className={styles.label}>
                  Code aus der Authenticator-App
                </label>
                <input
                  id="totpCode"
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  className={`${styles.input} ${styles.totpInput} ${errors.totpCode ? styles.inputError : ''}`}
                  value={totpCode}
                  onChange={(e) => setTotpCode(e.target.value)}
                  aria-invalid={!!errors.totpCode}
                  aria-describedby={errors.totpCode ? 'totpCode-error' : undefined}
                  disabled={isSubmitting}
                  placeholder="123456"
                  maxLength={6}
                  required
                />
                {errors.totpCode && (
                  <p id="totpCode-error" className={styles.errorText} role="alert">
                    {errors.totpCode}
                  </p>
                )}
              </div>
            ) : (
              <>
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
                    autoComplete="current-password"
                    required
                  />
                  {errors.password && (
                    <p id="password-error" className={styles.errorText} role="alert">
                      {errors.password}
                    </p>
                  )}
                </div>
              </>
            )}

            <button
              type="submit"
              className={styles.submitButton}
              disabled={isSubmitting || lockout !== null}
            >
              {isSubmitting
                ? 'Wird gesendet...'
                : lockout !== null
                  ? 'Bitte warten...'
                  : mfaActive
                    ? 'Code prüfen'
                    : 'Anmelden'}
            </button>
          </form>

          {mfaActive && (
            <button type="button" className={styles.backButton} onClick={backToCredentials}>
              Zurück zur E-Mail-/Passwort-Eingabe
            </button>
          )}

          <div className={styles.links}>
            <Link to="/forgot-password" className={styles.link}>
              Passwort vergessen?
            </Link>
            <Link to="/register" className={styles.link}>
              Noch kein Konto? Jetzt registrieren
            </Link>
            <Link to="/" className={styles.link}>
              Zurück zur Übersicht
            </Link>
          </div>
        </div>
      </main>
    </div>
  )
}
