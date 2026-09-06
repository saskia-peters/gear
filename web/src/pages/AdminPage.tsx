import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import styles from './PlaceholderPage.module.css'

// AdminPage is a deliberate STUB (Epic 2, FR-21/FR-27): the ADMIN module lands
// in a later epic. Until then the sidebar's ADMIN link must not 404, so this
// placeholder documents the upcoming module. It is gated by RequireAuth (only
// authenticated users can reach it); admin-only authorization for the real
// module is enforced server-side in Epic 2.
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
          <Link to="/" className={styles.backLink}>
            Zurück zur Übersicht
          </Link>
        </div>
      </main>
    </div>
  )
}