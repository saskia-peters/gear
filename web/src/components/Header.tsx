import { useNavigate } from 'react-router-dom'
import { useTheme } from '../context/useTheme.ts'
import {
  SESSION_TOKEN_KEY,
  isMfaEnabled,
  getDisplayName,
  clearAuthState,
} from '../auth/authState.ts'
import styles from './Header.module.css'

export function Header() {
  const { resolvedTheme, toggleTheme } = useTheme()
  const navigate = useNavigate()

  const isDark = resolvedTheme === 'dark'
  const toggleLabel = isDark ? 'Hellmodus aktivieren' : 'Dunkelmodus aktivieren'
  const mfaActive = isMfaEnabled()
  const displayName = getDisplayName()
  // The greeting is gated on an actual session (review finding 1.7-11): a
  // logged-out visitor must never see a stale cached display name.
  const isLoggedIn = Boolean(localStorage.getItem(SESSION_TOKEN_KEY))

  // handleLogout calls POST /api/v1/auth/logout to invalidate the session
  // server-side (NFR-S2), then clears the client auth state (including on a
  // 401 from a stale token) and redirects to /login. The server call is
  // best-effort: even if it fails (offline), the client still clears its local
  // state so the UI does not appear logged in.
  const handleLogout = async () => {
    const token = localStorage.getItem(SESSION_TOKEN_KEY)
    if (token) {
      try {
        const res = await fetch('/api/v1/auth/logout', {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
        })
        if (res.status === 401) {
          clearAuthState()
        }
      } catch {
        // Best-effort — local logout must still proceed.
      }
    }
    clearAuthState()
    navigate('/login', { replace: true })
  }

  return (
    <header className={styles.header}>
      <div className={styles.headerInner}>
        <div className={styles.brandGroup}>
          <h1 className={styles.title}>G.E.A.R.</h1>
          {mfaActive && (
            <span className={styles.mfaBadge} role="status" aria-live="polite">
              <span className={styles.mfaBadgeDot} aria-hidden="true" />
              MFA aktiv
            </span>
          )}
        </div>
        <div className={styles.actions}>
          {isLoggedIn && displayName && (
            <span className={styles.userName} title={displayName}>
              {displayName}
            </span>
          )}
          {isLoggedIn && (
            <button
              type="button"
              className={styles.logoutButton}
              onClick={handleLogout}
              aria-label="Abmelden"
              title="Abmelden"
            >
              Abmelden
            </button>
          )}
          <button
            type="button"
            className={styles.themeToggle}
            onClick={toggleTheme}
            aria-label={toggleLabel}
            title={toggleLabel}
          >
            <span aria-hidden="true">{isDark ? '☀️' : '🌙'}</span>
            <span>{isDark ? 'Hell' : 'Dunkel'}</span>
          </button>
        </div>
      </div>
    </header>
  )
}
