import { useEffect, useState, type FormEvent } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { Link, useNavigate } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { SESSION_TOKEN_KEY } from '../auth/authState.ts'
import styles from './MfaPage.module.css'

interface MfaErrors {
  code?: string
  general?: string
}

type EnrollPhase = 'idle' | 'requested' | 'confirming' | 'enabled' | 'error'

function authHeaders(): HeadersInit {
  const token = localStorage.getItem(SESSION_TOKEN_KEY)
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

export function MfaPage() {
  const [enabled, setEnabled] = useState(false)
  const [statusLoading, setStatusLoading] = useState(true)
  const [phase, setPhase] = useState<EnrollPhase>('idle')
  const [enroll, setEnroll] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState('')
  const [errors, setErrors] = useState<MfaErrors>({})
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [notice, setNotice] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    const prevTitle = document.title
    document.title = 'Zwei-Faktor-Authentifizierung | G.E.A.R.'
    return () => {
      document.title = prevTitle
    }
  }, [])

  // Load the current MFA state so the surface branches between enable/disable.
  // A non-ok or network error is NOT treated as "MFA disabled" (review finding
  // 1.6-5): on 401 the caller is redirected to /login; on any other failure an
  // error state is shown. An abort timeout prevents a hung loading state.
  useEffect(() => {
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 10_000)

    fetch('/api/v1/auth/mfa/status', {
      headers: authHeaders(),
      signal: controller.signal,
    })
      .then((res) => {
        if (res.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        return res.json()
      })
      .then((data) => {
        if (data && typeof data.enabled === 'boolean') {
          setEnabled(data.enabled)
          setPhase(data.enabled ? 'enabled' : 'idle')
        } else {
          setPhase('error')
        }
      })
      .catch(() => {
        // Abort (timeout), network failure, or non-JSON body: show an error
        // state rather than incorrectly assuming MFA is disabled.
        setPhase('error')
      })
      .finally(() => {
        clearTimeout(timeoutId)
        setStatusLoading(false)
      })

    return () => {
      clearTimeout(timeoutId)
      controller.abort()
    }
  }, [navigate])

  const startEnroll = async () => {
    setIsSubmitting(true)
    setErrors({})
    setNotice('')
    try {
      const res = await fetch('/api/v1/auth/mfa/enroll', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({}),
      })
      const data = await res.json().catch(() => null)
      if (res.ok && data?.secret && data?.uri) {
        setEnroll({ secret: data.secret, uri: data.uri })
        setPhase('requested')
      } else {
        setErrors({ general: data?.error?.message || 'Aktivierung fehlgeschlagen. Bitte versuche es erneut.' })
      }
    } catch {
      setErrors({ general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.' })
    } finally {
      setIsSubmitting(false)
    }
  }

  const confirmEnroll = async (e: FormEvent) => {
    e.preventDefault()
    if (!/^\d{6}$/.test(code.trim())) {
      setErrors({ code: 'Bitte gib den 6-stelligen Code aus deiner Authenticator-App ein.' })
      return
    }
    setIsSubmitting(true)
    setErrors({})
    setNotice('')
    try {
      const res = await fetch('/api/v1/auth/mfa/enroll', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({ secret: enroll?.secret, code: code.trim() }),
      })
      if (res.ok) {
        setEnabled(true)
        setPhase('enabled')
        setCode('')
        setEnroll(null)
        setNotice('Zwei-Faktor-Authentifizierung wurde erfolgreich aktiviert.')
      } else {
        const data = await res.json().catch(() => null)
        setErrors({ code: data?.error?.message || 'Der Bestätigungscode ist ungültig oder abgelaufen.' })
      }
    } catch {
      setErrors({ general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.' })
    } finally {
      setIsSubmitting(false)
    }
  }

  const disableMfa = async (e: FormEvent) => {
    e.preventDefault()
    if (!/^\d{6}$/.test(code.trim())) {
      setErrors({ code: 'Bitte gib den 6-stelligen Code aus deiner Authenticator-App ein.' })
      return
    }
    setIsSubmitting(true)
    setErrors({})
    setNotice('')
    try {
      const res = await fetch('/api/v1/auth/mfa/disable', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({ code: code.trim() }),
      })
      if (res.ok) {
        setEnabled(false)
        setPhase('idle')
        setCode('')
        setNotice('Zwei-Faktor-Authentifizierung wurde deaktiviert.')
      } else {
        const data = await res.json().catch(() => null)
        setErrors({ code: data?.error?.message || 'Der Bestätigungscode ist ungültig oder abgelaufen.' })
      }
    } catch {
      setErrors({ general: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.' })
    } finally {
      setIsSubmitting(false)
    }
  }

  const cancelEnroll = () => {
    setEnroll(null)
    setPhase('idle')
    setCode('')
    setErrors({})
  }

  const renderBody = () => {
    if (statusLoading) {
      return <p className={styles.hint}>Status wird geladen...</p>
    }

    if (phase === 'error') {
      // Review finding 1.6-5: a failed status fetch must not be treated as MFA
      // disabled — show an error state instead.
      return (
        <div className={styles.form}>
          <div className={styles.generalError} role="alert">
            Status konnte nicht geladen werden. Bitte versuche es erneut.
          </div>
          <button type="button" className={styles.primaryButton} onClick={() => window.location.reload()}>
            Erneut versuchen
          </button>
        </div>
      )
    }

    // Enabled state: show the indicator + disable form.
    if (phase === 'enabled' || enabled) {
      return (
        <div className={styles.form}>
          <div className={styles.mfaActive} role="status" aria-live="polite">
            <span className={styles.mfaActiveDot} aria-hidden="true" />
            MFA aktiv — dieses Konto ist durch Zwei-Faktor-Authentifizierung geschützt
          </div>
          <form onSubmit={disableMfa} noValidate>
            <div className={styles.fieldGroup}>
              <label htmlFor="disableCode" className={styles.label}>
                Aktueller Code aus der Authenticator-App
              </label>
              <input
                id="disableCode"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                className={`${styles.input} ${styles.totpInput} ${errors.code ? styles.inputError : ''}`}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                aria-invalid={!!errors.code}
                aria-describedby={errors.code ? 'disableCode-error' : undefined}
                disabled={isSubmitting}
                placeholder="123456"
                maxLength={6}
                required
              />
              {errors.code && (
                <p id="disableCode-error" className={styles.errorText} role="alert">
                  {errors.code}
                </p>
              )}
            </div>
            <button type="submit" className={styles.dangerButton} disabled={isSubmitting}>
              {isSubmitting ? 'Wird gesendet...' : 'Zwei-Faktor-Authentifizierung deaktivieren'}
            </button>
          </form>
        </div>
      )
    }

    // Requested state: show secret + QR once, then confirm.
    if (phase === 'requested' && enroll) {
      return (
        <div className={styles.form}>
          <div className={styles.qrBox} aria-label="QR-Code für die Authenticator-App">
            <QRCodeSVG value={enroll.uri} size={180} level="M" />
          </div>
          <p className={styles.hint}>
            Scanne den QR-Code mit deiner Authenticator-App oder gib den geheimen Schlüssel manuell ein. Der
            Schlüssel wird nur einmal angezeigt.
          </p>
          <div className={styles.secretBox} aria-label="Geheimer Schlüssel">
            {enroll.secret}
          </div>
          <form onSubmit={confirmEnroll} noValidate>
            <div className={styles.fieldGroup}>
              <label htmlFor="confirmCode" className={styles.label}>
                Code aus der Authenticator-App
              </label>
              <input
                id="confirmCode"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                className={`${styles.input} ${styles.totpInput} ${errors.code ? styles.inputError : ''}`}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                aria-invalid={!!errors.code}
                aria-describedby={errors.code ? 'confirmCode-error' : undefined}
                disabled={isSubmitting}
                placeholder="123456"
                maxLength={6}
                required
              />
              {errors.code && (
                <p id="confirmCode-error" className={styles.errorText} role="alert">
                  {errors.code}
                </p>
              )}
            </div>
            <button type="submit" className={styles.primaryButton} disabled={isSubmitting}>
              {isSubmitting ? 'Wird gesendet...' : 'Aktivierung bestätigen'}
            </button>
          </form>
          <button type="button" className={styles.backButton} onClick={cancelEnroll}>
            Abbrechen
          </button>
        </div>
      )
    }

    // Idle state: offer to enable.
    return (
      <div className={styles.form}>
        <p className={styles.hint}>
          Aktiviere die Zwei-Faktor-Authentifizierung, um dein Konto zusätzlich zum Passwort zu schützen. Nach der
          Aktivierung ist beim Login ein 6-stelliger Code aus deiner Authenticator-App erforderlich.
        </p>
        <button type="button" className={styles.primaryButton} onClick={startEnroll} disabled={isSubmitting}>
          {isSubmitting ? 'Wird gesendet...' : 'Zwei-Faktor-Authentifizierung aktivieren'}
        </button>
      </div>
    )
  }

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Zwei-Faktor-Authentifizierung</h2>
          <p className={styles.subtitle}>
            Zusätzliche Sicherheit für dein G.E.A.R.-Konto per TOTP (Authenticator-App)
          </p>

          {errors.general && (
            <div className={styles.generalError} role="alert">
              {errors.general}
            </div>
          )}

          {notice && (
            <div className={styles.notice} role="status">
              {notice}
            </div>
          )}

          {renderBody()}

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
