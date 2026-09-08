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
import { AdminRecoveryPage } from './pages/AdminRecoveryPage.tsx'
import { AdminBenutzerPage } from './pages/admin/AdminBenutzerPage.tsx'
import { AdminPendingApprovalsPage } from './pages/admin/AdminPendingApprovalsPage.tsx'
import { AdminBenutzergruppenPage } from './pages/admin/AdminBenutzergruppenPage.tsx'
import { AdminRollenPage } from './pages/admin/AdminRollenPage.tsx'
import { AdminQualifikationenPage } from './pages/admin/AdminQualifikationenPage.tsx'
import { AdminWerkzeugePage } from './pages/admin/AdminWerkzeugePage.tsx'
import { AdminEinstellungenPage } from './pages/admin/AdminEinstellungenPage.tsx'
import { AdminDsgvoPage } from './pages/admin/AdminDsgvoPage.tsx'
import { ForgotPasswordPage } from './pages/ForgotPasswordPage.tsx'
import { ResetPasswordPage } from './pages/ResetPasswordPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import {
  SESSION_TOKEN_KEY,
  authHeaders,
  clearAuthState,
  setIsAdmin,
  savePermissions,
  getPermissions,
} from './auth/authState.ts'
import { hasAnyAdminCode, adminNavCodes } from './auth/permissions.ts'

function hasAnyCode(codes: readonly string[]): boolean {
  const set = new Set(getPermissions())
  return codes.some((code) => set.has(code))
}

function hasSessionToken(): boolean {
  return Boolean(localStorage.getItem(SESSION_TOKEN_KEY))
}

// RequireAuth guards an authenticated route (Story 1.8). A token in localStorage
// alone is NOT sufficient: on mount (and on `pageshow` to defeat the
// back-forward cache) the session is validated server-side via GET
// /api/v1/auth/profile. A 401 (or any non-200) clears the auth state and
// redirects to /login — so logout and remotely revoked sessions are enforced,
// even via the browser back button. The caller's resolved permission set is
// also loaded server-side (GET /api/v1/auth/me/permissions, Story 2.2) and
// cached, so the module nav (Sidebar) and admin guard can filter on the
// server-authoritative set. Loading permissions is non-fatal to the session: if
// it fails, the set is treated as empty (fail closed for admin visibility,
// NAV_FETCH_FAIL). While validating, a loading state is shown instead of
// flashing the protected page. A thrown network error does NOT log the user out
// (availability): the session may still be valid offline, and the server
// remains authoritative on the next successful request.
function RequireAuth({ children }: { children: ReactNode }) {
  const [validating, setValidating] = useState(true)
  const [valid, setValid] = useState(false)

  useEffect(() => {
    let cancelled = false

    const validate = async (): Promise<void> => {
      if (!hasSessionToken()) {
        // No stored token: clear any stale cached auth state (display name,
        // is_admin, permissions, MFA flag) so a logged-out visitor never sees
        // stale data (review finding 1.8-8) — consistent with the 401 path.
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
          setValidating(false)
          return
        }
      } catch {
        if (!cancelled) setValid(true)
      }
      // Load the server-authoritative resolved permission set for module nav +
      // admin gating (Story 2.3). Non-fatal to the session.
      try {
        const pRes = await fetch('/api/v1/auth/me/permissions', { headers: authHeaders() })
        if (cancelled) return
        if (pRes.ok) {
          const pData = await pRes.json().catch(() => null)
          const perms = Array.isArray(pData?.permissions) ? pData.permissions : []
          savePermissions(perms)
        } else {
          savePermissions([])
        }
      } catch {
        savePermissions([])
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

// RequireAdminModule guards the ADMIN module's surfaces (Story 2.3). It does
// NOT trust the forgeable localStorage permissions cache (review finding 2.1-2):
// on mount it re-resolves the caller's permission set server-side via GET
// /api/v1/auth/me/permissions with authHeaders(), keeps the server response in
// the cache (so RequireAdminEntry and the pages read server-fresh data), gates
// on the RESPONSE — not the cache — and shows a loading state while resolving.
// A caller whose resolved set holds no admin-module code is redirected to the
// Dashboard so the admin module's existence is never hinted (FR-19/UX-DR6). On
// a fetch failure the caller is denied — fail closed (NAV_FETCH_FAIL). Because
// this mounts on every navigation into an admin route (RequireAuth's session
// check does NOT rerun on SPA navigation), an attacker who forges the cache
// after the initial load still cannot unlock the admin module.
function RequireAdminModule({ children }: { children: ReactNode }) {
  const [validating, setValidating] = useState(true)
  const [permissions, setPermissions] = useState<string[]>([])

  useEffect(() => {
    let cancelled = false

    const resolve = async (): Promise<void> => {
      if (!hasSessionToken()) {
        clearAuthState()
        if (!cancelled) {
          setPermissions([])
          setValidating(false)
        }
        return
      }
      try {
        const res = await fetch('/api/v1/auth/me/permissions', { headers: authHeaders() })
        if (cancelled) return
        if (res.ok) {
          const data = await res.json().catch(() => null)
          const perms = Array.isArray(data?.permissions) ? data.permissions : []
          // Refresh the server-authoritative cache before rendering children so
          // RequireAdminEntry and the pages read the response, never a stale or
          // forged snapshot.
          savePermissions(perms)
          if (!cancelled) setPermissions(perms)
        } else {
          savePermissions([])
          if (!cancelled) setPermissions([])
        }
      } catch {
        // Fail closed: without a server answer the admin gate cannot be
        // trusted, so a network error denies the admin module (FR-19).
        savePermissions([])
        if (!cancelled) setPermissions([])
      } finally {
        if (!cancelled) setValidating(false)
      }
    }

    void resolve()

    // Defeat the back-forward cache: re-resolve on bfcache restore so the gate
    // cannot be unlocked by a stale/forged set after back navigation.
    const onPageshow = (event: PageTransitionEvent): void => {
      if (event.persisted) {
        void resolve()
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
  if (!hasAnyAdminCode(permissions)) {
    return <Navigate to="/" replace />
  }
  return children
}

// RequireAdminEntry gates a single admin sub-route to its entry's permission
// codes (Story 2.3, AD-6/FR-19). A caller without any of the entry's codes is
// redirected to the Dashboard (never a "Zugriff verweigert" leak). The resolved
// set is the server-authoritative cache loaded by RequireAuth.
function RequireAdminEntry({ codes, children }: { codes: readonly string[]; children: ReactNode }) {
  if (!hasAnyCode(codes)) {
    return <Navigate to="/" replace />
  }
  return children
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
            <RequireAdminModule>
              <AdminPage />
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/recovery"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={['admin.recovery.approve']}>
                <AdminRecoveryPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/benutzer"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('benutzer')}>
                <AdminBenutzerPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/benutzer/pending"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={['users.approve']}>
                <AdminPendingApprovalsPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/benutzergruppen"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('benutzergruppen')}>
                <AdminBenutzergruppenPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/rollen"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('rollen')}>
                <AdminRollenPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/qualifikationen"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('qualifikationen')}>
                <AdminQualifikationenPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/werkzeuge"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('werkzeuge')}>
                <AdminWerkzeugePage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/einstellungen"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('einstellungen')}>
                <AdminEinstellungenPage />
              </RequireAdminEntry>
            </RequireAdminModule>
          </AuthenticatedPage>
        }
      />
      <Route
        path="/admin/dsgvo"
        element={
          <AuthenticatedPage>
            <RequireAdminModule>
              <RequireAdminEntry codes={adminNavCodes('dsgvo')}>
                <AdminDsgvoPage />
              </RequireAdminEntry>
            </RequireAdminModule>
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