import { useState } from 'react'
import { ApiError, createUser, updateUser } from '../auth/users.ts'
import type { AdminUserDetail, AdminUserInput, UserGroup } from '../auth/users.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../auth/roles.ts'
import styles from './UserEditor.module.css'

interface UserEditorProps {
  user: AdminUserDetail | null
  roles: RoleGroup[]
  userGroups: UserGroup[]
  availablePermissions: PermissionCatalogEntry[]
  onSaved: (user: AdminUserDetail, message: string) => void
  onCancel: () => void
  /** Invoked when a create/update save answers 403 (revoked role): the parent
      clears the cached admin flag and leaves the admin module. */
  onForbidden: () => void
}

type Feedback =
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string }
  | null

// UserEditor is the create/edit form for a user (Story 2.6, UX-DR6/UX-DR8/
// UX-DR9). It captures Vorname, Nachname, E-Mail, a status (aktiv/pending) and
// the three assignment sets — roles (permission groups), organisational user
// groups (teams, AD-12) and direct permission grants (additive, checked =
// granted). Saving a new user POSTs; editing an existing user PUTs with the
// same body. Inline German feedback (validation, errors, success), ≥48px
// targets, keyboard/focus/SR (role="alert"/"status").
//
// The editor is mounted only when the caller may act on the user: create/edit/
// deactivate require `users.manage`, and the direct-grant grid is the same
// additive 22-code catalog as the role editor. The server remains the source
// of truth.
export function UserEditor({ user, roles, userGroups, availablePermissions, onSaved, onCancel, onForbidden }: UserEditorProps) {
  const isEdit = user !== null
  const [vorname, setVorname] = useState(user?.vorname ?? '')
  const [nachname, setNachname] = useState(user?.nachname ?? '')
  const [email, setEmail] = useState(user?.email ?? '')
  const [status, setStatus] = useState<'active' | 'pending_approval'>(user?.status === 'pending_approval' ? 'pending_approval' : 'active')
  const [roleIDs, setRoleIDs] = useState<Set<string>>(() => new Set(user?.roles.map((r) => r.id) ?? []))
  const [groupIDs, setGroupIDs] = useState<Set<string>>(() => new Set(user?.user_groups.map((g) => g.id) ?? []))
  const [grantCodes, setGrantCodes] = useState<Set<string>>(() => new Set(user?.direct_grants.map((g) => g.code) ?? []))
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  function toggle(set: Set<string>, code: string, apply: (next: Set<string>) => void) {
    const next = new Set(set)
    if (next.has(code)) {
      next.delete(code)
    } else {
      next.add(code)
    }
    apply(next)
    setFeedback(null)
  }

  async function submit() {
    if (vorname.trim() === '' || nachname.trim() === '' || email.trim() === '') {
      setFeedback({ kind: 'error', message: 'Bitte fülle alle Pflichtfelder aus.' })
      return
    }
    setBusy(true)
    setFeedback(null)
    const input: AdminUserInput = {
      vorname: vorname.trim(),
      nachname: nachname.trim(),
      email: email.trim(),
      status,
      role_ids: [...roleIDs],
      user_group_ids: [...groupIDs],
      direct_grant_codes: [...grantCodes],
    }
    try {
      const res = isEdit ? await updateUser(user!.id, input) : await createUser(input)
      // The success message comes from the server (finding 8) so the SPA never
      // hardcodes its own confirmation text.
      onSaved(res.user, res.message)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        // Server-side revocation downgrade: the parent clears the cached admin
        // flag and navigates away (review finding 2.1-6).
        onForbidden()
        return
      }
      setFeedback({
        kind: 'error',
        message: err instanceof ApiError ? err.message : 'Der Benutzer konnte nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className={styles.editor}
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <h3 className={styles.title}>{isEdit ? `Benutzer „${user!.vorname} ${user!.nachname}“ bearbeiten` : 'Neuer Benutzer'}</h3>

      {/* Sticky action bar (Effort 2): always visible at the top so the user
          never scrolls to reach Speichern/Abbrechen in a long editor. */}
      <div className={styles.stickyActions}>
        <button type="submit" className={styles.saveButton} disabled={busy}>
          {busy ? 'Wird gespeichert...' : 'Speichern'}
        </button>
        <button type="button" className={styles.cancelButton} onClick={onCancel} disabled={busy}>
          Abbrechen
        </button>
      </div>

      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}

      <div className={styles.field}>
        <label className={styles.label} htmlFor="user-vorname">
          Vorname
        </label>
        <input
          id="user-vorname"
          className={styles.input}
          value={vorname}
          onChange={(e) => {
            setVorname(e.target.value)
            setFeedback(null)
          }}
          maxLength={100}
          autoFocus={!isEdit}
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor="user-nachname">
          Nachname
        </label>
        <input
          id="user-nachname"
          className={styles.input}
          value={nachname}
          onChange={(e) => {
            setNachname(e.target.value)
            setFeedback(null)
          }}
          maxLength={100}
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor="user-email">
          E-Mail
        </label>
        <input
          id="user-email"
          type="email"
          className={styles.input}
          value={email}
          onChange={(e) => {
            setEmail(e.target.value)
            setFeedback(null)
          }}
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor="user-status">
          Status
        </label>
        <select
          id="user-status"
          className={styles.select}
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as 'active' | 'pending_approval')
            setFeedback(null)
          }}
        >
          <option value="active">aktiv</option>
          <option value="pending_approval">pending</option>
        </select>
        <span className={styles.hint}>
          Zugangsdaten werden separat vergeben.
        </span>
      </div>

      <fieldset className={styles.assignmentGroup}>
        <legend className={styles.label}>Rollen</legend>
        {roles.length === 0 ? (
          <p className={styles.emptyNote}>Keine Rollen verfügbar.</p>
        ) : (
          <div className={styles.checkList}>
            {roles.map((role) => (
              <label key={role.id} className={styles.checkRow}>
                <input
                  type="checkbox"
                  className={styles.checkbox}
                  checked={roleIDs.has(role.id)}
                  onChange={() => toggle(roleIDs, role.id, setRoleIDs)}
                />
                <span className={styles.checkLabel}>
                  {role.name}
                  {role.is_base_role ? <span className={styles.baseTag}>Basis</span> : null}
                </span>
              </label>
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className={styles.assignmentGroup}>
        <legend className={styles.label}>Benutzergruppen</legend>
        <span className={styles.hint}>Organisatorische Teams (vergeben keine Rechte).</span>
        {userGroups.length === 0 ? (
          <p className={styles.emptyNote}>Keine Benutzergruppen verfügbar.</p>
        ) : (
          <div className={styles.checkList}>
            {userGroups.map((group) => (
              <label key={group.id} className={styles.checkRow}>
                <input
                  type="checkbox"
                  className={styles.checkbox}
                  checked={groupIDs.has(group.id)}
                  onChange={() => toggle(groupIDs, group.id, setGroupIDs)}
                />
                <span className={styles.checkLabel}>{group.name}</span>
              </label>
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className={styles.assignmentGroup}>
        <legend className={styles.label}>
          Direkte Berechtigungen
          <span className={styles.hint}>Angekreuzte Berechtigungen werden zusätzlich vergeben.</span>
        </legend>
        <div className={styles.checkList}>
          {availablePermissions.map((perm) => (
            <label key={perm.code} className={styles.checkRow}>
              <input
                type="checkbox"
                className={styles.checkbox}
                checked={grantCodes.has(perm.code)}
                onChange={() => toggle(grantCodes, perm.code, setGrantCodes)}
              />
              <span className={styles.checkLabel}>{perm.label}</span>
            </label>
          ))}
        </div>
      </fieldset>
    </form>
  )
}
