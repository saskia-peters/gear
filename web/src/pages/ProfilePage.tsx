import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SESSION_TOKEN_KEY, clearAuthState, setDisplayName } from '../auth/authState.ts'
import styles from './ProfilePage.module.css'

interface Profile {
  id: string
  email: string
  first_name: string
  last_name: string
  display_name: string
  pending_email?: string
}

interface FieldErrors {
  firstName?: string
  lastName?: string
  displayName?: string
  email?: string
  general?: string
}

function authHeaders(): HeadersInit {
  const token = localStorage.getItem(SESSION_TOKEN_KEY)
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

// handleAuthResponse is the shared 401 path (Story 2.1, mirroring the
// change-password flow, review finding 1.7-7): a stale/revoked session clears
// the client auth state and redirects to /login. Returns true when redirected.
function handleAuthResponse(status: number, navigate: ReturnType<typeof useNavigate>): boolean {
  if (status === 401) {
    clearAuthState()
    navigate('/login', { replace: true })
    return true
  }
  return false
}

export function ProfilePage() {
  const [firstName, setFirstName] = useState('')
  const [lastName, setLastName] = useState('')
  const [displayName, setDisplayNameState] = useState('')
  const [email, setEmail] = useState('')
  // currentEmail is the ACTIVE login email loaded from the server. The account
  // stays on it until an admin approves a staged change (Story 2.1).
  const [currentEmail, setCurrentEmail] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [pendingEmail, setPendingEmail] = useState<string | undefined>(undefined)
  const [errors, setErrors] = useState<FieldErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [baseSaved, setBaseSaved] = useState(false)
  const [emailStaged, setEmailStaged] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Profil | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Load the profile base data on mount. On 401 the caller is redirected to
  // /login; on any other failure an error state is shown. An abort timeout
  // prevents a hung loading state.
  useEffect(() => {
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 10_000)

    fetch('/api/v1/auth/profile', {
      headers: authHeaders(),
      signal: controller.signal,
    })
      .then(async (res) => {
        if (handleAuthResponse(res.status, navigate)) {
          return null
        }
        if (!res.ok) {
          return null
        }
        return res.json()
      })
      .then((data: Profile | null) => {
        if (data && typeof data.email === 'string') {
          setFirstName(data.first_name || '')
          setLastName(data.last_name || '')
          setDisplayNameState(data.display_name || '')
          setEmail(data.email)
          setCurrentEmail(data.email)
          setPendingEmail(data.pending_email || undefined)
          setLoaded(true)
        } else {
          setLoadError(true)
        }
      })
      .catch(() => {
        setLoadError(true)
      })
      .finally(() => {
        clearTimeout(timeoutId)
      })

    return () => {
      clearTimeout(timeoutId)
      controller.abort()
    }
  }, [navigate])

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setIsSubmitting(true)
    setErrors({})
    setBaseSaved(false)
    setEmailStaged(false)

    // Client-side validation (the form uses noValidate, so the HTML required
    // attributes do not gate submission): every name field must be non-empty
    // after trimming and the email must be present and well-formed — mirroring
    // the server-side rules so a cleared field is never silently ignored.
    const trimmedFirst = firstName.trim()
    const trimmedLast = lastName.trim()
    const trimmedDisplay = displayName.trim()
    const trimmedEmail = email.trim()

    const nextErrors: FieldErrors = {}
    if (!trimmedFirst) nextErrors.firstName = 'Bitte gib deinen Vornamen ein.'
    if (!trimmedLast) nextErrors.lastName = 'Bitte gib deinen Nachnamen ein.'
    if (!trimmedDisplay) nextErrors.displayName = 'Bitte gib deinen Anzeigenamen ein.'
    if (!trimmedEmail) {
      nextErrors.email = 'Bitte gib eine E-Mail-Adresse an.'
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmedEmail)) {
      nextErrors.email = 'Bitte gib eine gültige E-Mail-Adresse ein.'
    }
    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors)
      setIsSubmitting(false)
      return
    }

    // The email is only submitted when it differs from the active login email.
    const emailChanged = trimmedEmail.toLowerCase() !== currentEmail.toLowerCase()

    // Base data is saved immediately. baseOk is the result of THIS submission
    // (a local value, not the render-closure errors state) — a failed base
    // save must not fire the email POST, and a valid resubmit must not be
    // suppressed by a stale general error (review finding).
    let baseOk = false
    try {
      const res = await fetch('/api/v1/auth/profile', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({
          first_name: trimmedFirst,
          last_name: trimmedLast,
          display_name: trimmedDisplay,
        }),
      })
      if (handleAuthResponse(res.status, navigate)) {
        return
      }
      if (res.ok) {
        baseOk = true
        setBaseSaved(true)
        // Re-sync the form from the returned profile so trimmed/server-
        // normalized values are shown immediately (not after reload).
        const data = await res.json().catch(() => null)
        if (data && typeof data.first_name === 'string') {
          setFirstName(data.first_name)
          setLastName(data.last_name)
          setDisplayNameState(data.display_name || '')
          setDisplayName(data.display_name || trimmedDisplay)
        } else {
          // Keep the header greeting in sync with the saved display name.
          setDisplayName(trimmedDisplay)
        }
      } else {
        const data = await res.json().catch(() => null)
        setErrors({ general: data?.error?.message || 'Speichern fehlgeschlagen. Bitte versuche es erneut.' })
      }
    } catch {
      setErrors({ general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.' })
    }

    if (emailChanged && baseOk) {
      try {
        const res = await fetch('/api/v1/auth/profile/email', {
          method: 'POST',
          headers: authHeaders(),
          body: JSON.stringify({ email: trimmedEmail }),
        })
        if (handleAuthResponse(res.status, navigate)) {
          return
        }
        if (res.ok) {
          const data = await res.json().catch(() => null)
          setPendingEmail(data?.pending_email || trimmedEmail)
          setEmailStaged(true)
          // The account stays active on the current email; reset the field so
          // the staged change is not submitted again.
          setEmail(currentEmail)
        } else {
          const data = await res.json().catch(() => null)
          const message = data?.error?.message || 'Die E-Mail-Adresse konnte nicht gespeichert werden.'
          setErrors({ email: message })
        }
      } catch {
        setErrors({ general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.' })
      }
    }

    setIsSubmitting(false)
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Profil</h2>
          <p className={styles.subtitle}>Deine persönlichen Angaben und Einstellungen</p>

          {loadError && (
            <div className={styles.generalError} role="alert">
              Profil konnte nicht geladen werden. Bitte versuche es erneut.
            </div>
          )}

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {baseSaved && (
            <div className={styles.notice} role="status">
              Profil aktualisiert.
            </div>
          )}

          {emailStaged && (
            <div className={styles.notice} role="status">
              E-Mail-Änderung wartet auf Admin-Freigabe.
            </div>
          )}

          {pendingEmail && !emailStaged && (
            <div className={styles.notice} role="status">
              E-Mail-Änderung auf {pendingEmail} wartet auf Admin-Freigabe.
            </div>
          )}

          {loaded && (
            <form className={styles.form} onSubmit={handleSubmit} noValidate>
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
                <label htmlFor="displayName" className={styles.label}>
                  Anzeigename
                </label>
                <input
                  id="displayName"
                  type="text"
                  className={`${styles.input} ${errors.displayName ? styles.inputError : ''}`}
                  value={displayName}
                  onChange={(e) => setDisplayNameState(e.target.value)}
                  aria-invalid={!!errors.displayName}
                  aria-describedby={errors.displayName ? 'displayName-error' : undefined}
                  required
                />
                {errors.displayName && (
                  <p id="displayName-error" className={styles.errorText} role="alert">
                    {errors.displayName}
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
                  autoComplete="email"
                  required
                />
                {errors.email && (
                  <p id="email-error" className={styles.errorText} role="alert">
                    {errors.email}
                  </p>
                )}
                <p className={styles.hint}>
                  Die Änderung der E-Mail-Adresse wird erst nach einer Admin-Freigabe wirksam.
                </p>
              </div>

              <button type="submit" className={styles.submitButton} disabled={isSubmitting}>
                {isSubmitting ? 'Wird gespeichert...' : 'Speichern'}
              </button>
            </form>
          )}

          <div className={styles.navCards}>
            <Link to="/mfa" className={styles.navCard}>
              Zwei-Faktor-Authentifizierung verwalten
              <span className={styles.navCardArrow} aria-hidden="true">
                →
              </span>
            </Link>
            <Link to="/password" className={styles.navCard}>
              Passwort ändern
              <span className={styles.navCardArrow} aria-hidden="true">
                →
              </span>
            </Link>
          </div>

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