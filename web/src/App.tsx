import { type ReactNode } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { ThemeProvider } from './context/ThemeContext.tsx'
import { ErrorBoundary } from './components/ErrorBoundary.tsx'
import { DashboardPage } from './pages/DashboardPage.tsx'
import { LoginPage } from './pages/LoginPage.tsx'
import { MfaPage } from './pages/MfaPage.tsx'
import { RegisterPage } from './pages/RegisterPage.tsx'
import { ChangePasswordPage } from './pages/ChangePasswordPage.tsx'
import { ProfilePage } from './pages/ProfilePage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import { SESSION_TOKEN_KEY } from './auth/authState.ts'

function hasSessionToken(): boolean {
  return Boolean(localStorage.getItem(SESSION_TOKEN_KEY))
}

function RequireAuth({ children }: { children: ReactNode }) {
  if (!hasSessionToken()) {
    return <Navigate to="/login" replace />
  }
  return children
}

export function AppRoutes() {
  return (
    <Routes>
      <Route
        path="/"
        element={
          <RequireAuth>
            <DashboardPage />
          </RequireAuth>
        }
      />
      <Route
        path="/mfa"
        element={
          <RequireAuth>
            <MfaPage />
          </RequireAuth>
        }
      />
      <Route
        path="/password"
        element={
          <RequireAuth>
            <ChangePasswordPage />
          </RequireAuth>
        }
      />
      <Route
        path="/profil"
        element={
          <RequireAuth>
            <ProfilePage />
          </RequireAuth>
        }
      />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
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
