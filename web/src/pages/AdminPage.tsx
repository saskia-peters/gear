import { Link } from 'react-router-dom'
import { Header } from '../components/Header.tsx'
import { AdminNav } from '../components/AdminNav.tsx'
import { PendingApprovals } from '../components/PendingApprovals.tsx'
import { getPermissions, hasPermission } from '../auth/authState.ts'
import { filteredAdminNav } from '../auth/permissions.ts'
import styles from './AdminPage.module.css'

// AdminPage is the admin module's landing hub — "Verwaltung — Start" (Story
// 2.3). It is a warm, plain-language home for people who do not work with IT
// systems every day: one big tappable card per permitted nav entry (each with a
// short German purpose, no jargon, no permission codes), an empty state where
// pending approvals will appear, and the Dual-Admin-Wiederherstellung link for
// admins. The nav and cards are filtered by the caller's resolved permission
// set, so a caller only ever sees the entries they hold (anti-enumeration,
// FR-19). Route gating happens in the route table — no "Zugriff verweigert"
// branch lives here.
export function AdminPage() {
  const entries = filteredAdminNav(getPermissions())
  // The pending-approvals section only matters to callers who can ACT on it:
  // the server's /users/* endpoints require `users.approve` (AD-6/FR-20). A
  // caller holding only users.view (or a tools-only schirrmeister/fuehrende)
  // must NOT mount the widget — the server would 403 them and
  // adminForbiddenHandled would force them out of the admin module. Matching
  // the server gate exactly keeps a users.view-only caller safely on the page.
  const canViewApprovals = hasPermission('users.approve')

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={entries} />
        <main className={styles.main}>
          <h2 className={styles.title}>Verwaltung — Start</h2>
          <p className={styles.subtitle}>
            Willkommen in der Verwaltung. Hier kümmern Sie sich um Mitglieder,
            Geräte und Einstellungen.
          </p>

          <section aria-label="Verwaltungsbereiche" className={styles.grid}>
            {entries.map((entry) => (
              <Link
                key={entry.key}
                to={entry.route}
                className={styles.card}
              >
                <span className={styles.cardTitle}>{entry.label}</span>
                <span className={styles.cardDescription}>{entry.description}</span>
              </Link>
            ))}

            {/* Benutzergruppen management card (Effort 2): a focused entry for
                user↔user-group assignment, gated by user_groups.manage. It
                lands on the Benutzer surface where the groups live. */}
            {hasPermission('user_groups.manage') && (
              <Link
                to="/admin/benutzer"
                className={styles.card}
              >
                <span className={styles.cardTitle}>Benutzergruppen</span>
                <span className={styles.cardDescription}>
                  Mitglieder Teams zuordnen und Team-Rollen vergeben.
                </span>
              </Link>
            )}
          </section>

          {canViewApprovals && (
            <section aria-label="Ausstehende Anträge" className={styles.pending}>
              <h3 className={styles.pendingTitle}>Ausstehende Anträge</h3>
              <PendingApprovals />
            </section>
          )}

          {hasPermission('admin.recovery.approve') && (
            <Link to="/admin/recovery" className={styles.secondaryLink}>
              Dual-Admin-Wiederherstellung
            </Link>
          )}
        </main>
      </div>
    </div>
  )
}
