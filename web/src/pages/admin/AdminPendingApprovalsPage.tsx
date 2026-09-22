import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { PendingApprovals } from '../../components/PendingApprovals.tsx'
import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { useNavigate } from 'react-router-dom'
import styles from './AdminPendingApprovalsPage.module.css'

// AdminPendingApprovalsPage is the dedicated "Ausstehende Anträge" surface
// (Story 2.4, FR-20/UX-DR6/UX-DR8): it hosts the live PendingApprovals widget
// on its own page instead of the Verwaltung landing, so an admin can review
// self-registration requests without the rest of the admin hub. Reachable from
// the "Ausstehende Anträge" button on the Benutzer surface.
//
// The whole page is gated on `users.approve` (matching the server /users/*
// gate, AD-6); the server remains the source of truth. A 403 (revoked role)
// clears the cached admin flag and leaves the admin module (review finding
// 2.1-6); a 401 (expired session) clears auth and redirects to /login.
export function AdminPendingApprovalsPage() {
  const navigate = useNavigate()

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          <h2 className={styles.title}>Ausstehende Anträge</h2>
          <p className={styles.description}>
            Neue Freigaben von Mitgliedern prüfen und bearbeiten.
          </p>
          <div className={styles.backRow}>
            <button type="button" className={styles.backButton} onClick={() => navigate('/admin/benutzer')}>
              ← Zurück zu Benutzer
            </button>
          </div>
          <PendingApprovals />
        </main>
      </div>
    </div>
  )
}