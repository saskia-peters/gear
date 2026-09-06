import { type ReactNode, useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { ThemeProvider } from './context/ThemeContext.tsx'
import { ErrorBoundary } from './components/ErrorBoundary.tsx'
import { AppShell } from './components/AppShell.tsx'
import { DashboardPage } from './pages/DashboardPage.tsx'
import { LoginPage } from './pages/LoginPage.tsx'
import { MfaPage } from './pages/MfaPage.tsx'
import { RegisterPage } from './pages/RegisterPage.tsx'
import { ChangePasswordPage } from './pages/ChangePasswordPage.tsx'
import { ProfilePage } from './pages/ProfilePage.tsx'
import { AdminPage } from './pages/AdminPage.tsx'
import { ForgotPasswordPage } from './pages/ForgotPasswordPage.tsx'
import { ResetPasswordPage } from './pages/ResetPasswordPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import {
  SESSION_TOKEN_KEY,
  clearAuthState,
  setIsAdmin,
} from './auth/authState.ts'

function hasSessionToken(): boolean {
  return Boolean(localStorage.getItem(SESSION_TOKEN_KEY))
}

// RequireAuth guards an authenticated route (Story 1.8). A token in localStorage
// alone is NOT sufficient: on mount (and on `pageshow` to defeat the
// back-forward cache) the session is validated server-side via GET
// /api/v1/auth/profile. A 401 (or any non-200) clears the auth state and
// redirects to /login — so logout and remotely revoked sessions are enforced,
// even via the browser back button. While validating, a loading state is shown
// instead of flashing the protected page. A thrown network error does NOT log
// the user out (availability): the session may still be valid offline, and the
// server remains authoritative on the next successful request.
function RequireAuth({ children }: { children: ReactNode }) {
  const [validating, setValidating] = useState(true)
  const [valid, setValid] = useState(false)

  useEffect(() => {
    let cancelled = false

    const validate = async (): Promise<void> => {
      if (!hasSessionToken()) {
        // No stored token: clear any stale cached auth state (display name,
        // is_admin, MFA flag) so a logged-out visitor never sees stale data
        // (review finding 1.8-8) — consistent with the 401 path below.
        clearAuthState()
        if (!cancelled) {
          setValid(false)
          setValidating(false)
        }
        return
      }
      try {
        const res = await fetch('/api/v1/auth/profile', {
          headers: {
            Authorization: `Bearer ${localStorage.getItem(SESSION_TOKEN_KEY)}`,
          },
        })
        if (cancelled) return
        if (res.ok) {
          const data = await res.json().catch(() => null)
          if (data && typeof data.is_admin === 'boolean') {
            setIsAdmin(data.is_admin)
          }
          setValid(true)
        } else {
          clearAuthState()
          setValid(false)
        }
      } catch {
        if (!cancelled) setValid(true)
      } finally {
        if (!cancelled) setValidating(false)
      }
    }

    void validate()

    // Defeat the back-forward cache: when the user returns via the browser
    // back button, re-validate the session (logout is enforced client-side).
    const onPageshow = (event: PageTransitionEvent): void => {
      if (event.persisted) {
        void validate()
      }
    }
    window.addEventListener('pageshow', onPageshow)

    return () => {
      cancelled = true
      window.removeEventListener('pageshow', onPageshow)
    }
  }, [])

  if (validating) {
    return <div>Lädt...</div>
  }
  if (!valid) {
    return <Navigate to="/login" replace />
  }
  return children
}

// AuthenticatedPage wraps a protected page in the auth guard AND the module
// shell (sidebar + content), so the GEAR/ADMIN navigation is persistent across
// every authenticated route (Story 1.8).
function AuthenticatedPage({ children }: { children: ReactNode }) {
  return (
    <RequireAuth>
      <AppShell>{children}</AppShell>
    </RequireAuth>
  )
}

export function AppRoutes() {
  return (
    <Routes>
      <Route
        path="/"
        element={
          <AuthenticatedPage>
            <DashboardPage />
          </AuthenticatedPage>
        }
      />
      <Route
        path="/mfa"
        element={
          <AuthenticatedPage>
            <MfaPage />
          </AuthenticatedPage>
        }
      />
      <Route
        path="/password"
        element={
          <AuthenticatedPage>
            <ChangePasswordPage />
          </AuthenticatedPage>
        }
      />
      <Route
        path="/profil"
        element={
          <AuthenticatedPage>
            <ProfilePage />
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin"
        element={
          <AuthenticatedPage>
            <AdminPage />
          </AuthenticatedPage>
        }
      />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route path="/forgot-password" element={<ForgotPasswordPage />} />
      <Route path="/reset-password/:token" element={<ResetPasswordPage />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <BrowserRouter>
          <AppRoutes />
        </BrowserRouter>
      </ThemeProvider>
    </ErrorBoundary>
  )
}

export default App