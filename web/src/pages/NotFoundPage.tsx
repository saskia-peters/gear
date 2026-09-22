import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import styles from './PlaceholderPage.module.css'

export function NotFoundPage() {
  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <div className={styles.card}>
          <h2 className={styles.title}>404 — Seite nicht gefunden</h2>
          <p className={styles.description}>
            Die aufgerufene Seite existiert nicht oder wurde verschoben.
          </p>
          <Link to="/" className={styles.backLink}>
            Zurück zur Übersicht
          </Link>
        </div>
      </main>
    </div>
  )
}
