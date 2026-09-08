import { useEffect, useState } from 'react'
import {
  deactivateUser,
  qualificationStatusLabel,
  userStatusLabel,
  assignUserQualification,
  revokeUserQualification,
  updateUserQualificationExpiry,
} from '../auth/users.ts'
import type { AdminUserDetail, QualificationAssignment } from '../auth/users.ts'
import { permissionLabel } from '../auth/roles.ts'
import { listQualifications } from '../auth/qualifications.ts'
import type { Qualification } from '../auth/qualifications.ts'
import styles from './UserDetail.module.css'

interface UserDetailProps {
  user: AdminUserDetail
  canManage: boolean
  /** The caller holds users.qualifications.manage (fuehrende/schirrmeister/admin). */
  canManageQualifications: boolean
  onEdit: () => void
  onBack: () => void
  /** Re-fetches the current user detail after a qualification write so the view stays open and refreshed. */
  onRefreshDetail: () => void
  /** Invoked after a successful deactivation so the parent can refresh. */
  onDeactivated: (message: string) => void
  /** Invoked when a 403 answers an in-page fetch (revoked role). */
  onForbidden: () => void
  /** Invoked when a 401 answers an in-page fetch (expired session → login). */
  onUnauthorized: () => void
}

type Feedback = { kind: 'error' | 'success'; message: string } | null

// German source labels for the "Alle Berechtigungen" provenance view (Effort
// 2). The wire source_kind is mapped to German display; "Direkt" is the direct
// one-off grant. Never expose the raw permission-code names in microcopy.
function sourceLabel(kind: string, name: string): string {
  switch (kind) {
    case 'role':
      return `Rolle: ${name}`
    case 'group':
      return `Benutzergruppe: ${name}`
    case 'direct':
      return 'Direkt'
    default:
      return name || kind
  }
}

// UserDetail is the read view of a user (Story 2.6, Effort 2, UX-DR6/8/9):
// the profile, the three source sections (Rollen · Benutzergruppen · Direkte
// Berechtigungen), a COLLAPSED "Alle Berechtigungen" provenance view (the
// resolved union annotated with its source(s)), and the qualification
// assignments with their status. When the caller holds
// users.qualifications.manage the Qualifikationen section becomes editable:
// assign from the vocabulary, set/update the per-user valid-until (or
// "Unbegrenzt" for unlimited quals), and revoke. Without that code the section
// stays read-only (status badges only). Deactivation runs through an inline
// confirmation step and requires `users.manage`.
//
// The server remains the source of truth: the detail payload is
// server-authoritative and every write is server-gated (defense-in-depth).
export function UserDetail({
  user,
  canManage,
  canManageQualifications,
  onEdit,
  onBack,
  onRefreshDetail,
  onDeactivated,
  onForbidden,
  onUnauthorized,
}: UserDetailProps) {
  const [confirmingDeactivate, setConfirmingDeactivate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  // Editable-qualification state (Effort 2): the vocabulary to choose from, the
  // currently selected qualification id, the valid-until input, and the
  // edit-target (id) for updating an existing assignment's expiry.
  const [vocabulary, setVocabulary] = useState<Qualification[]>([])
  const [vocabularyFailed, setVocabularyFailed] = useState(false)
  const [selectedQualId, setSelectedQualId] = useState('')
  const [expiryInput, setExpiryInput] = useState('')
  const [qualBusy, setQualBusy] = useState(false)
  const [editExpiryFor, setEditExpiryFor] = useState<string | null>(null)
  const [editExpiryValue, setEditExpiryValue] = useState('')

  const isActive = user.status === 'active'
  const canEditQualifications = canManageQualifications && isActive

  // Load the qualification vocabulary when the section is editable. The server
  // opens GET /qualifications to any caller who can assign qualifications
  // (users.qualifications.manage), so a holder normally always gets it. A 401
  // (expired session) redirects to login; a 403 (unexpected for a code holder,
  // i.e. a revoked role) leaves the module; any other failure is a genuine
  // fetch error surfaced as "Auswahlliste nicht verfügbar".
  useEffect(() => {
    if (!canEditQualifications) return
    let cancelled = false
    listQualifications()
      .then((data) => {
        if (!cancelled) setVocabulary(data.qualifications)
      })
      .catch((err) => {
        if (cancelled) return
        const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
        if (status === 401) {
          onUnauthorized()
          return
        }
        if (status === 403) {
          onForbidden()
          return
        }
        setVocabularyFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [canEditQualifications, onUnauthorized, onForbidden])

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
      if (err instanceof Error && 'status' in err && (err as { status: number }).status === 401) {
        onUnauthorized()
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

  async function handleAssignQualification() {
    if (!selectedQualId) return
    const qual = vocabulary.find((q) => q.id === selectedQualId)
    if (!qual) return
    // Fixed qualifications REQUIRE a valid-until (Spec 2.9): reject an empty
    // date client-side before sending the server a null it would 400 on.
    if (qual.expiry_kind === 'fixed' && expiryInput === '') {
      setFeedback({ kind: 'error', message: 'Gültig bis ist erforderlich.' })
      return
    }
    setQualBusy(true)
    setFeedback(null)
    try {
      const expiresAt = qual.expiry_kind === 'unlimited' ? null : new Date(expiryInput).toISOString()
      const res = await assignUserQualification(user.id, selectedQualId, expiresAt)
      setFeedback({ kind: 'success', message: res.message || 'Qualifikation zugewiesen.' })
      setSelectedQualId('')
      setExpiryInput('')
      onRefreshDetail()
    } catch (err) {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 403) {
        onForbidden()
        return
      }
      if (status === 401) {
        onUnauthorized()
        return
      }
      setFeedback({ kind: 'error', message: err instanceof Error && err.message ? err.message : 'Die Qualifikation konnte nicht zugewiesen werden.' })
    } finally {
      setQualBusy(false)
    }
  }

  async function handleRevokeQualification(qual: QualificationAssignment) {
    setQualBusy(true)
    setFeedback(null)
    try {
      const res = await revokeUserQualification(user.id, qual.id)
      setFeedback({ kind: 'success', message: res.message || 'Qualifikation entzogen.' })
      onRefreshDetail()
    } catch (err) {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 403) {
        onForbidden()
        return
      }
      if (status === 401) {
        onUnauthorized()
        return
      }
      setFeedback({ kind: 'error', message: err instanceof Error && err.message ? err.message : 'Die Qualifikation konnte nicht entzogen werden.' })
    } finally {
      setQualBusy(false)
    }
  }

  async function handleUpdateExpiry(qual: QualificationAssignment) {
    setQualBusy(true)
    setFeedback(null)
    try {
      const expiresAt = qual.expiry_kind === 'unlimited' || editExpiryValue === '' ? null : new Date(editExpiryValue).toISOString()
      const res = await updateUserQualificationExpiry(user.id, qual.id, expiresAt)
      setFeedback({ kind: 'success', message: res.message || 'Ablaufdatum aktualisiert.' })
      setEditExpiryFor(null)
      setEditExpiryValue('')
      onRefreshDetail()
    } catch (err) {
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
      if (status === 403) {
        onForbidden()
        return
      }
      if (status === 401) {
        onUnauthorized()
        return
      }
      setFeedback({ kind: 'error', message: err instanceof Error && err.message ? err.message : 'Das Gültig-bis-Datum konnte nicht aktualisiert werden.' })
    } finally {
      setQualBusy(false)
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
        <p
          role={feedback.kind === 'success' ? 'status' : 'alert'}
          className={feedback.kind === 'success' ? styles.feedbackSuccess : styles.feedbackError}
        >
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

        <details className={styles.provenance}>
        <summary className={styles.provenanceSummary}>
          Alle Berechtigungen
          <span className={styles.provenanceCount}>
            {new Set((user.resolved_permissions ?? []).map((p) => p.code)).size}
          </span>
        </summary>
        <div className={styles.provenanceBody}>
          {(user.resolved_permissions ?? []).length === 0 ? (
            <p className={styles.emptyNote}>Keine Berechtigungen.</p>
          ) : (
            <ul className={styles.provenanceList}>
              {(user.resolved_permissions ?? []).map((perm, idx) => (
                <li key={`${perm.code}-${idx}`} className={styles.provenanceRow}>
                  <span className={styles.provenanceCode}>{permissionLabel(perm.code)}</span>
                  <span className={styles.provenanceSource}>
                    ({sourceLabel(perm.source_kind, perm.source_name)})
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </details>

      <div className={styles.section}>
        <h4 className={styles.sectionTitle}>Qualifikationen</h4>
        {user.qualifications.length === 0 ? (
          <p className={styles.emptyNote}>Keine Qualifikationen zugewiesen.</p>
        ) : (
          <ul className={styles.qualList}>
            {user.qualifications.map((q) => (
              <li key={q.id} className={styles.qualRow}>
                <div className={styles.qualMain}>
                  <span className={styles.qualName}>{q.name}</span>
                  <span className={`${styles.qualStatus} ${styles[`qual-${q.status}`]}`}>
                    {qualificationStatusLabel(q.status)}
                  </span>
                  {q.expiry_kind === 'fixed' && q.expires_at && (
                    <span className={styles.qualExpiry}>bis {new Date(q.expires_at).toLocaleDateString('de-DE')}</span>
                  )}
                </div>
                {canEditQualifications && (
                  <div className={styles.qualActions}>
                    <button
                      type="button"
                      className={styles.qualEditButton}
                      onClick={() => {
                        setEditExpiryFor(editExpiryFor === q.id ? null : q.id)
                        setEditExpiryValue(q.expires_at ? q.expires_at.slice(0, 10) : '')
                      }}
                      disabled={qualBusy}
                    >
                      Gültig bis
                    </button>
                    <button
                      type="button"
                      className={styles.qualRevokeButton}
                      onClick={() => void handleRevokeQualification(q)}
                      disabled={qualBusy}
                    >
                      Entziehen
                    </button>
                  </div>
                )}
                {canEditQualifications && editExpiryFor === q.id && (
                  <div className={styles.expiryEditor}>
                    <label className={styles.expiryLabel} htmlFor={`expiry-${q.id}`}>
                      {q.expiry_kind === 'unlimited' ? 'Gültig bis (leer = unbegrenzt)' : 'Gültig bis'}
                    </label>
                    <input
                      id={`expiry-${q.id}`}
                      type="date"
                      className={styles.expiryInput}
                      value={editExpiryValue}
                      onChange={(e) => setEditExpiryValue(e.target.value)}
                      disabled={qualBusy}
                    />
                    <button type="button" className={styles.qualSaveButton} onClick={() => void handleUpdateExpiry(q)} disabled={qualBusy}>
                      {qualBusy ? 'Wird gespeichert...' : 'Speichern'}
                    </button>
                    <button type="button" className={styles.qualCancelButton} onClick={() => setEditExpiryFor(null)} disabled={qualBusy}>
                      Abbrechen
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}

        {canManageQualifications && !isActive && (
          <p className={styles.hint}>
            Qualifikationen können nur für aktive Benutzer zugewiesen werden.
          </p>
        )}

        {canEditQualifications && (
          <div className={styles.qualAssign}>
            <h5 className={styles.qualAssignTitle}>Qualifikation zuweisen</h5>
            {(() => {
              const selectedQual = vocabulary.find((q) => q.id === selectedQualId)
              const fixedRequiresExpiry = selectedQual?.expiry_kind === 'fixed'
              const expiryMissing = fixedRequiresExpiry && expiryInput === ''
              const assignDisabled = qualBusy || !selectedQualId || expiryMissing
              return vocabulary.length === 0 ? (
                vocabularyFailed ? (
                  <p className={styles.emptyNote}>
                    Die Auswahlliste ist nicht verfügbar. Bereits zugewiesene Qualifikationen können entzogen oder angepasst werden.
                  </p>
                ) : (
                  <p className={styles.emptyNote}>Keine Qualifikationen verfügbar.</p>
                )
              ) : (
                <div className={styles.qualAssignRow}>
                  <label className={styles.label} htmlFor="qual-select">
                    Qualifikation
                  </label>
                  <select
                    id="qual-select"
                    className={styles.select}
                    value={selectedQualId}
                    onChange={(e) => {
                      setSelectedQualId(e.target.value)
                      setExpiryInput('')
                    }}
                    disabled={qualBusy}
                  >
                    <option value="">Auswählen…</option>
                    {vocabulary.map((q) => (
                      <option key={q.id} value={q.id}>
                        {q.name}
                      </option>
                    ))}
                  </select>
                  {selectedQual && selectedQual.expiry_kind === 'fixed' && (
                    <>
                      <label className={styles.label} htmlFor="qual-expiry">
                        Gültig bis
                      </label>
                      <input
                        id="qual-expiry"
                        type="date"
                        className={styles.expiryInput}
                        value={expiryInput}
                        onChange={(e) => setExpiryInput(e.target.value)}
                        disabled={qualBusy}
                      />
                    </>
                  )}
                  {selectedQual && selectedQual.expiry_kind === 'unlimited' && (
                    <p className={styles.hint}>Unbegrenzt gültig (kein Ablaufdatum).</p>
                  )}
                  {expiryMissing && (
                    <p role="alert" className={styles.qualError}>
                      Gültig bis ist erforderlich.
                    </p>
                  )}
                  <button
                    type="button"
                    className={styles.qualSaveButton}
                    onClick={() => void handleAssignQualification()}
                    disabled={assignDisabled}
                  >
                    {qualBusy ? 'Wird zugewiesen...' : 'Zuweisen'}
                  </button>
                </div>
              )
            })()}
          </div>
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
