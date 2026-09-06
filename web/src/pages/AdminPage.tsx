import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import styles from './PlaceholderPage.module.css'

// AdminPage is the admin module's home (Story 2.1). The ADMIN module lands in
// Epic 2; until then this placeholder documents the upcoming module. It is
// gated by RequireAuth + RequireAdmin in the route table: the guard resolves
// admin status server-side (GET /api/v1/auth/profile) and redirects a non-admin
// to the Dashboard, so this page is only ever reached by an admin (review
// finding 2.1-3). No "Zugriff verweigert" branch lives here — that would leak
// the admin module's existence (FR-19).
export function AdminPage() {
  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>Admin-Modul</h2>
          <p className={styles.description}>
            Das Admin-Modul folgt in Epic 2. Hier entstehen Verwaltung und
            Freigaben (z.&nbsp;B. Kontofreigabe, Einmal-Passwörter, Rollen).
          </p>
          <Link to="/admin/recovery" className={styles.backLink}>
            Dual-Admin-Wiederherstellung
          </Link>
          <Link to="/" className={styles.backLink}>
            Zurück zur Übersicht
          </Link>
        </div>
      </main>
    </div>
  )
}