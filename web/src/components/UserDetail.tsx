import { useState } from 'react'
import { deactivateUser, qualificationStatusLabel, userStatusLabel } from '../auth/users.ts'
import type { AdminUserDetail } from '../auth/users.ts'
import { permissionLabel } from '../auth/roles.ts'
import styles from './UserDetail.module.css'

interface UserDetailProps {
  user: AdminUserDetail
  canManage: boolean
  onEdit: () => void
  onBack: () => void
  /** Invoked after a successful deactivation so the parent can refresh. */
  onDeactivated: (message: string) => void
  /** Invoked when a deactivation answers 403 (revoked role). */
  onForbidden: () => void
}

type Feedback = { kind: 'error'; message: string } | null

// UserDetail is the read view of a user (Story 2.6, UX-DR6/UX-DR8/UX-DR9):
// the profile, the roles (permission groups), the organisational user groups
// (teams, AD-12), the direct permission grants and the qualification
// assignments with their status (Gültig / Bald ablaufend / Abgelaufen /
// Unbegrenzt). Deactivation runs through an inline confirmation step
// ("→ Sofort kein Login") and requires `users.manage`.
//
// The server remains the source of truth: the detail payload is
// server-authoritative and the deactivate action is server-gated.
export function UserDetail({ user, canManage, onEdit, onBack, onDeactivated, onForbidden }: UserDetailProps) {
  const [confirmingDeactivate, setConfirmingDeactivate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const isActive = user.status === 'active'

  async function confirmDeactivate() {
    setBusy(true)
    setFeedback(null)
    try {
      const res = await deactivateUser(user.id)
      onDeactivated(res.message)
    } catch (err) {
      if (err instanceof Error && 'status' in err && (err as { status: number }).status === 403) {
        onForbidden()
        return
      }
      setFeedback({
        kind: 'error',
        message: err instanceof Error && err.message ? err.message : 'Die Deaktivierung ist fehlgeschlagen.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={styles.detail}>
      <div className={styles.header}>
        <button type="button" className={styles.backButton} onClick={onBack}>
          ← Zurück zur Liste
        </button>
        <div className={styles.titleRow}>
          <h3 className={styles.title}>
            {user.vorname} {user.nachname}
          </h3>
          <span
            className={`${styles.statusBadge} ${styles[`status-${user.status}`]}`}
          >
            {userStatusLabel(user.status)}
          </span>
        </div>
      </div>

      {feedback && (
        <p role="alert" className={styles.feedbackError}>
          {feedback.message}
        </p>
      )}

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Kontakt</h4>
        <p className={styles.line}>
          E-Mail: <span className={styles.value}>{user.email}</span>
        </p>
      </div>

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Rollen</h4>
        {user.roles.length === 0 ? (
          <p className={styles.emptyNote}>Keine Rollen zugewiesen.</p>
        ) : (
          <ul className={styles.badgeList}>
            {user.roles.map((role) => (
              <li key={role.id} className={styles.badge}>
                {role.name}
                {role.is_base_role ? <span className={styles.baseTag}>Basis</span> : null}
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Benutzergruppen</h4>
        <span className={styles.hint}>Organisatorische Teams (vergeben keine Rechte).</span>
        {user.user_groups.length === 0 ? (
          <p className={styles.emptyNote}>Keinen Benutzergruppen zugeordnet.</p>
        ) : (
          <ul className={styles.badgeList}>
            {user.user_groups.map((group) => (
              <li key={group.id} className={styles.badge}>
                {group.name}
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Direkte Berechtigungen</h4>
        {user.direct_grants.length === 0 ? (
          <p className={styles.emptyNote}>Keine direkten Berechtigungen.</p>
        ) : (
          <ul className={styles.badgeList}>
            {user.direct_grants.map((grant) => (
              <li key={grant.permission_id} className={styles.badge}>
                {permissionLabel(grant.code)}
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Qualifikationen</h4>
        {user.qualifications.length === 0 ? (
          <p className={styles.emptyNote}>Keine Qualifikationen zugewiesen.</p>
        ) : (
          <ul className={styles.qualList}>
            {user.qualifications.map((q) => (
              <li key={q.id} className={styles.qualRow}>
                <span className={styles.qualName}>{q.name}</span>
                <span className={`${styles.qualStatus} ${styles[`qual-${q.status}`]}`}>
                  {qualificationStatusLabel(q.status)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={styles.actions}>
        {canManage && isActive && (
          <button
            type="button"
            className={styles.editButton}
            onClick={onEdit}
          >
            Bearbeiten
          </button>
        )}

        {canManage && isActive && !confirmingDeactivate && (
          <button
            type="button"
            className={styles.deactivateButton}
            onClick={() => setConfirmingDeactivate(true)}
          >
            Deaktivieren
          </button>
        )}

        {confirmingDeactivate && (
          <div className={styles.confirmBox} role="alert">
            <p className={styles.confirmText}>
              Benutzer deaktivieren? Der Benutzer kann sich ab sofort nicht mehr
              anmelden („Sofort kein Login“).
            </p>
            <div className={styles.confirmActions}>
              <button type="button" className={styles.confirmButton} onClick={() => void confirmDeactivate()} disabled={busy}>
                {busy ? 'Wird deaktiviert...' : 'Ja, deaktivieren'}
              </button>
              <button
                type="button"
                className={styles.cancelButton}
                onClick={() => setConfirmingDeactivate(false)}
                disabled={busy}
              >
                Abbrechen
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}