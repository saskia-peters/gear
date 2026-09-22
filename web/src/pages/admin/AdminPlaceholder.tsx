import { Link } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import type { AdminNavEntry } from '../../auth/permissions.ts'
import styles from './adminPlaceholder.module.css'

interface AdminPlaceholderProps {
  title: string
  description: string
  entries: AdminNavEntry[]
}

// AdminPlaceholder is the shared shell for the six non-landing admin surfaces
// (Story 2.3): Benutzer / Rollen / Qualifikationen / Werkzeuge / Einstellungen
// / DSGVO. Each is a navigable placeholder that later stories (2.4–2.8) fill
// with real surfaces. It renders the admin sub-nav (permission-filtered) beside
// an honest "folgt in einem späteren Schritt" note. Route gating happens in the
// route table (RequireAuth + permission guard), so this component never shows a
// "Zugriff verweigert" branch (FR-19).
export function AdminPlaceholder({
  title,
  description,
  entries,
}: AdminPlaceholderProps) {
  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={entries} />
        <main className={styles.main}>
          <h2 className={styles.title}>{title}</h2>
          <p className={styles.description}>{description}</p>
          <p className={styles.note}>
            Diese Ansicht wird in einem späteren Schritt ausgebaut.
          </p>
          <Link to="/admin" className={styles.backLink}>
            Zurück zur Übersicht
          </Link>
        </main>
      </div>
    </div>
  )
}
